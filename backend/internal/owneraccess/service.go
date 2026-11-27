// Package owneraccess implements the single one-time owner activation.
//
// The owner role unlocks age-verification review. It is deliberately not part of
// the role-grant API: it exists only by consuming a deployment secret bound to
// one pre-designated account, and the database allows exactly one owner.
package owneraccess

import (
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/jawuu187-ops/content-platform/backend/internal/identity"
)

var (
	// ErrDisabled means no activation secret is configured.
	ErrDisabled = errors.New("owneraccess: owner activation is not configured")
	// ErrDenied covers a wrong secret or a caller the secret is not bound to.
	ErrDenied = errors.New("owneraccess: activation denied")
	// ErrMFARequired means the session has no recent second-factor proof.
	ErrMFARequired = errors.New("owneraccess: step-up MFA required")
	// ErrAlreadyActivated means the owner slot was already consumed.
	ErrAlreadyActivated = errors.New("owneraccess: owner already activated")
	// ErrOwnerExists means an owner grant already exists.
	ErrOwnerExists = errors.New("owneraccess: an owner already exists")
)

// MFAStepUpWindow is how recent a second-factor proof must be for activation and
// age-review decisions.
const MFAStepUpWindow = 5 * time.Minute

// Service consumes the deployment-provided owner slot.
type Service struct {
	pool   *pgxpool.Pool
	secret string
	userID string
	email  string
	slot   int16
	now    func() time.Time
}

// WithSlot selects the activation slot. Deployment configures only slot 1; the
// option exists so tests can exercise the cross-slot race that must stay safe if
// slot 2 is ever enabled.
func WithSlot(slot int16) func(*Service) {
	return func(s *Service) { s.slot = slot }
}

// New builds the service. An empty secret leaves it disabled; the caller may
// still boot because owner access is not required for ordinary operation.
func New(pool *pgxpool.Pool, secret, userID, email string, opts ...func(*Service)) *Service {
	s := &Service{
		pool:   pool,
		secret: secret,
		userID: strings.TrimSpace(userID),
		email:  strings.ToLower(strings.TrimSpace(email)),
		slot:   1,
		now:    time.Now,
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// Enabled reports whether a secret is configured.
func (s *Service) Enabled() bool { return s != nil && s.secret != "" }

// Activate consumes the configured slot for the authenticated caller.
func (s *Service) Activate(ctx context.Context, p identity.Principal, secret, requestID string) error {
	if !s.Enabled() {
		return ErrDisabled
	}
	if !p.EmailVerified {
		s.deny(ctx, p, "email_unverified", requestID)
		return ErrDenied
	}
	if p.MFAAt == nil || s.now().Sub(*p.MFAAt) > MFAStepUpWindow {
		return ErrMFARequired
	}
	if !s.boundTo(ctx, p.UserID) {
		s.deny(ctx, p, "not_designated", requestID)
		return ErrDenied
	}
	if subtle.ConstantTimeCompare([]byte(secret), []byte(s.secret)) != 1 {
		s.deny(ctx, p, "invalid_secret", requestID)
		return ErrDenied
	}
	return s.consume(ctx, p, requestID)
}

func (s *Service) boundTo(ctx context.Context, userID string) bool {
	if s.userID != "" {
		return strings.EqualFold(s.userID, userID)
	}
	if s.email == "" {
		return false
	}
	var email string
	if err := s.pool.QueryRow(ctx,
		`SELECT lower(email) FROM identity.users WHERE id = $1::uuid`, userID).Scan(&email); err != nil {
		return false
	}
	return strings.EqualFold(email, s.email)
}

func (s *Service) consume(ctx context.Context, p identity.Principal, requestID string) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("owneraccess: begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// Lock the configured slot. Slot 2 exists but is never selected unless a
	// future deployment explicitly configures it.
	var consumed *time.Time
	err = tx.QueryRow(ctx,
		`SELECT consumed_at FROM ops.owner_activation_slots WHERE slot = $1 FOR UPDATE`, s.slot).Scan(&consumed)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrDisabled
	}
	if err != nil {
		return fmt.Errorf("owneraccess: lock slot: %w", err)
	}
	if consumed != nil {
		return ErrAlreadyActivated
	}

	var owners int
	if err := tx.QueryRow(ctx,
		`SELECT count(*) FROM identity.role_grants WHERE role = 'owner'`).Scan(&owners); err != nil {
		return fmt.Errorf("owneraccess: count owners: %w", err)
	}
	if owners > 0 {
		_ = tx.Rollback(ctx)
		s.deny(ctx, p, "existing_grant", requestID)
		return ErrOwnerExists
	}

	tag, err := tx.Exec(ctx, `
		UPDATE ops.owner_activation_slots
		   SET consumed_at = now(), consumed_by = $1::uuid, designated_user_id = $1::uuid
		 WHERE slot = $2 AND consumed_at IS NULL`, p.UserID, s.slot)
	if err != nil {
		return fmt.Errorf("owneraccess: consume slot: %w", err)
	}
	if tag.RowsAffected() != 1 {
		return ErrAlreadyActivated
	}

	// Plain INSERT: never ON CONFLICT DO NOTHING. If another transaction won the
	// race, the partial unique index raises and this whole transaction rolls
	// back, including the slot update.
	if _, err := tx.Exec(ctx, `
		INSERT INTO identity.role_grants (user_id, role, granted_by)
		VALUES ($1::uuid, 'owner', 'activation')`, p.UserID); err != nil {
		if isOwnerUniqueViolation(err) {
			_ = tx.Rollback(ctx)
			s.deny(ctx, p, "existing_grant", requestID)
			return ErrOwnerExists
		}
		return fmt.Errorf("owneraccess: grant owner: %w", err)
	}

	if _, err := tx.Exec(ctx, `
		SELECT ops.append_audit(
			'user', $1::text, 'owner.activated', 'user', $1::text,
			NULL, jsonb_build_object('slot', $2::int), NULLIF($3, ''))`,
		p.UserID, s.slot, requestID); err != nil {
		return fmt.Errorf("owneraccess: audit activation: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("owneraccess: commit: %w", err)
	}
	return nil
}

// deny records a failed activation in its own transaction. It never writes a
// success action and never carries the submitted secret.
func (s *Service) deny(ctx context.Context, p identity.Principal, reason, requestID string) {
	_, _ = s.pool.Exec(ctx, `
		SELECT ops.append_audit(
			'user', $1::text, 'owner.activation_denied', 'user', $1::text,
			NULL, jsonb_build_object('reason', $2::text), NULLIF($3, ''))`,
		p.UserID, reason, requestID)
}

func isOwnerUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		return false
	}
	return pgErr.Code == "23505" && pgErr.ConstraintName == "role_grants_one_owner"
}
