// Package agereview implements manual age-verification review.
//
// A request carries no evidence: the reviewer is expected to see the account
// holder present a government-issued ID live, then records a decision code. No
// document, image, scan, selfie or date of birth is accepted or stored by this
// package, and the schema refuses such columns.
package agereview

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/jawuu187-ops/content-platform/backend/internal/identity"
)

var (
	ErrForbidden       = errors.New("agereview: owner access required")
	ErrMFARequired     = errors.New("agereview: recent MFA required")
	ErrSelfReview      = errors.New("agereview: cannot review your own verification")
	ErrNotFound        = errors.New("agereview: review request not found")
	ErrNotPending      = errors.New("agereview: review request is not pending")
	ErrAlreadyPending  = errors.New("agereview: a review request is already pending")
	ErrAlreadyVerified = errors.New("agereview: account is already verified")
	ErrNoVerification  = errors.New("agereview: account has no live verification")
	ErrInvalidReason   = errors.New("agereview: invalid rejection reason")
	ErrInvalidInput    = errors.New("agereview: invalid input")
)

// MFAStepUpWindow matches owner activation: a decision needs a second factor
// proof from the same session within five minutes.
const MFAStepUpWindow = 5 * time.Minute

// Reasons are a fixed, non-sensitive set. Free text is deliberately not stored:
// a reviewer must never paste document details into a field.
var Reasons = []string{
	"age_not_established",
	"identity_mismatch",
	"presentation_failed",
	"suspected_fraud",
	"other",
}

// Actor is the authenticated caller. Roles and MFAAt come from the session, not
// from the request body.
type Actor struct {
	UserID    string
	SessionID string
	Roles     []string
	MFAAt     *time.Time
}

// Request is the metadata-only record shown to the user and the owner.
type Request struct {
	ID          string     `json:"id"`
	UserID      string     `json:"user_id"`
	Status      string     `json:"status"`
	Method      string     `json:"method"`
	ReasonCode  string     `json:"reason_code,omitempty"`
	ReviewerID  string     `json:"reviewer_id,omitempty"`
	CheckID     string     `json:"check_id,omitempty"`
	RequestedAt time.Time  `json:"requested_at"`
	DecidedAt   *time.Time `json:"decided_at,omitempty"`
	ExpiresAt   *time.Time `json:"expires_at,omitempty"`
}

// Service owns the manual review lifecycle.
type Service struct {
	pool     *pgxpool.Pool
	validity time.Duration
	now      func() time.Time
}

// New builds the review service. validity is how long an approval grants access.
func New(pool *pgxpool.Pool, validity time.Duration, opts ...func(*Service)) *Service {
	s := &Service{pool: pool, validity: validity, now: time.Now}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// WithClock overrides the clock for tests.
func WithClock(now func() time.Time) func(*Service) {
	return func(s *Service) { s.now = now }
}

func (s *Service) authorize(a Actor) error {
	if !slices.Contains(a.Roles, identity.RoleOwner) ||
		!identity.HasPermission(a.Roles, identity.PermVerifyReview) {
		return ErrForbidden
	}
	if a.MFAAt == nil || s.now().Sub(*a.MFAAt) > MFAStepUpWindow {
		return ErrMFARequired
	}
	return nil
}

// RequestManual opens a pending review request for the caller.
func (s *Service) RequestManual(ctx context.Context, userID, requestID string) (Request, error) {
	if userID == "" {
		return Request{}, fmt.Errorf("%w: account is required", ErrInvalidInput)
	}
	now := s.now().UTC()
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Request{}, fmt.Errorf("agereview: begin request: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var live bool
	if err := tx.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM verify.checks
			WHERE user_id = $1::uuid AND status = 'verified' AND revoked_at IS NULL
			  AND (expires_at IS NULL OR expires_at > $2)
		)`, userID, now).Scan(&live); err != nil {
		return Request{}, fmt.Errorf("agereview: check live verification: %w", err)
	}
	if live {
		return Request{}, ErrAlreadyVerified
	}
	var pending bool
	if err := tx.QueryRow(ctx, `
		SELECT EXISTS (SELECT 1 FROM verify.review_requests
			WHERE user_id = $1::uuid AND status = 'pending')`, userID).Scan(&pending); err != nil {
		return Request{}, fmt.Errorf("agereview: check pending: %w", err)
	}
	if pending {
		return Request{}, ErrAlreadyPending
	}

	var r Request
	err = tx.QueryRow(ctx, `
		INSERT INTO verify.review_requests (user_id, status, method)
		VALUES ($1::uuid, 'pending', 'manual_review')
		RETURNING id::text, user_id::text, status, method, reason_code,
		          COALESCE(reviewer_id::text, ''), COALESCE(check_id::text, ''),
		          requested_at, decided_at, expires_at`, userID,
	).Scan(&r.ID, &r.UserID, &r.Status, &r.Method, &r.ReasonCode, &r.ReviewerID,
		&r.CheckID, &r.RequestedAt, &r.DecidedAt, &r.ExpiresAt)
	if err != nil {
		return Request{}, fmt.Errorf("agereview: insert request: %w", err)
	}
	if err := recordAttempt(ctx, tx, userID, "started", "manual_review"); err != nil {
		return Request{}, err
	}
	hash, err := appendAudit(ctx, tx, userID, "age_review.requested", r.ID,
		map[string]any{"method": r.Method}, requestID)
	if err != nil {
		return Request{}, err
	}
	if _, err := tx.Exec(ctx, `UPDATE verify.review_requests SET audit_ref = $2 WHERE id = $1::uuid`,
		r.ID, hash); err != nil {
		return Request{}, fmt.Errorf("agereview: store audit reference: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return Request{}, fmt.Errorf("agereview: commit request: %w", err)
	}
	return r, nil
}

// LatestForUser returns the newest request, or nil when the account has none.
func (s *Service) LatestForUser(ctx context.Context, userID string) (*Request, error) {
	r, err := scanRequest(s.pool.QueryRow(ctx, `
		SELECT id::text, user_id::text, status, method, reason_code,
		       COALESCE(reviewer_id::text, ''), COALESCE(check_id::text, ''),
		       requested_at, decided_at, expires_at
		FROM verify.review_requests WHERE user_id = $1::uuid
		ORDER BY requested_at DESC LIMIT 1`, userID))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("agereview: latest request: %w", err)
	}
	return &r, nil
}

// Queue lists pending requests. It requires owner and recent MFA.
func (s *Service) Queue(ctx context.Context, a Actor) ([]Request, error) {
	if err := s.authorize(a); err != nil {
		return nil, err
	}
	rows, err := s.pool.Query(ctx, `
		SELECT id::text, user_id::text, status, method, reason_code,
		       COALESCE(reviewer_id::text, ''), COALESCE(check_id::text, ''),
		       requested_at, decided_at, expires_at
		FROM verify.review_requests WHERE status = 'pending'
		ORDER BY requested_at ASC LIMIT 100`)
	if err != nil {
		return nil, fmt.Errorf("agereview: queue: %w", err)
	}
	defer rows.Close()
	out := make([]Request, 0)
	for rows.Next() {
		r, err := scanRequest(rows)
		if err != nil {
			return nil, fmt.Errorf("agereview: scan queue: %w", err)
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// Get returns one request. It requires owner and recent MFA.
func (s *Service) Get(ctx context.Context, a Actor, id string) (Request, error) {
	if err := s.authorize(a); err != nil {
		return Request{}, err
	}
	r, err := scanRequest(s.pool.QueryRow(ctx, `
		SELECT id::text, user_id::text, status, method, reason_code,
		       COALESCE(reviewer_id::text, ''), COALESCE(check_id::text, ''),
		       requested_at, decided_at, expires_at
		FROM verify.review_requests WHERE id = $1::uuid`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return Request{}, ErrNotFound
	}
	if err != nil {
		return Request{}, fmt.Errorf("agereview: get request: %w", err)
	}
	return r, nil
}

// Approve creates a manual_review verification. Every decision is audited in the
// same transaction; an audit failure rolls the decision back.
func (s *Service) Approve(ctx context.Context, a Actor, id, requestID string) (Request, error) {
	if err := s.authorize(a); err != nil {
		return Request{}, err
	}
	now := s.now().UTC()
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Request{}, fmt.Errorf("agereview: begin approve: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	r, err := lockPending(ctx, tx, id)
	if err != nil {
		return Request{}, err
	}
	if r.UserID == a.UserID {
		return Request{}, ErrSelfReview
	}

	var live bool
	if err := tx.QueryRow(ctx, `
		SELECT EXISTS (SELECT 1 FROM verify.checks
			WHERE user_id = $1::uuid AND status = 'verified' AND revoked_at IS NULL
			  AND (expires_at IS NULL OR expires_at > $2))`,
		r.UserID, now).Scan(&live); err != nil {
		return Request{}, fmt.Errorf("agereview: check existing verification: %w", err)
	}
	if live {
		return Request{}, ErrAlreadyVerified
	}
	if _, err := tx.Exec(ctx, `
		UPDATE verify.checks SET status = 'expired', updated_at = now()
		WHERE user_id = $1::uuid AND status = 'verified' AND revoked_at IS NULL`, r.UserID); err != nil {
		return Request{}, fmt.Errorf("agereview: supersede verification: %w", err)
	}

	expires := now.Add(s.validity)
	var checkID string
	err = tx.QueryRow(ctx, `
		INSERT INTO verify.checks (
			user_id, status, method, provider, provider_ref, assurance,
			min_age, verified_at, expires_at
		) VALUES ($1::uuid, 'verified', 'manual_review', 'manual', '', 'manual_review', 18, $2, $3)
		RETURNING id::text`, r.UserID, now, expires).Scan(&checkID)
	if err != nil {
		return Request{}, fmt.Errorf("agereview: create verification: %w", err)
	}

	if _, err := tx.Exec(ctx, `
		UPDATE verify.review_requests
		SET status = 'approved', reviewer_id = $2::uuid, decided_at = $3,
		    expires_at = $4, check_id = $5::uuid
		WHERE id = $1::uuid`, r.ID, a.UserID, now, expires, checkID); err != nil {
		return Request{}, fmt.Errorf("agereview: approve request: %w", err)
	}
	if err := recordAttempt(ctx, tx, r.UserID, "verified", "manual_review"); err != nil {
		return Request{}, err
	}
	hash, err := appendAudit(ctx, tx, a.UserID, "age_review.approved", r.ID,
		map[string]any{"user_id": r.UserID, "check_id": checkID, "method": "manual_review"}, requestID)
	if err != nil {
		return Request{}, err
	}
	if _, err := tx.Exec(ctx, `UPDATE verify.review_requests SET audit_ref = $2 WHERE id = $1::uuid`,
		r.ID, hash); err != nil {
		return Request{}, fmt.Errorf("agereview: store audit reference: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return Request{}, fmt.Errorf("agereview: commit approve: %w", err)
	}
	r.Status = "approved"
	r.ReviewerID = a.UserID
	r.DecidedAt, r.ExpiresAt, r.CheckID = &now, &expires, checkID
	return r, nil
}

// Reject closes a pending request without creating a verification.
func (s *Service) Reject(ctx context.Context, a Actor, id, reason, requestID string) (Request, error) {
	if err := s.authorize(a); err != nil {
		return Request{}, err
	}
	if !slices.Contains(Reasons, reason) {
		return Request{}, ErrInvalidReason
	}
	now := s.now().UTC()
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Request{}, fmt.Errorf("agereview: begin reject: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	r, err := lockPending(ctx, tx, id)
	if err != nil {
		return Request{}, err
	}
	if r.UserID == a.UserID {
		return Request{}, ErrSelfReview
	}
	if _, err := tx.Exec(ctx, `
		UPDATE verify.review_requests
		SET status = 'rejected', reason_code = $2, reviewer_id = $3::uuid, decided_at = $4
		WHERE id = $1::uuid`, r.ID, reason, a.UserID, now); err != nil {
		return Request{}, fmt.Errorf("agereview: reject request: %w", err)
	}
	if err := recordAttempt(ctx, tx, r.UserID, "rejected", reason); err != nil {
		return Request{}, err
	}
	hash, err := appendAudit(ctx, tx, a.UserID, "age_review.rejected", r.ID,
		map[string]any{"user_id": r.UserID, "reason_code": reason}, requestID)
	if err != nil {
		return Request{}, err
	}
	if _, err := tx.Exec(ctx, `UPDATE verify.review_requests SET audit_ref = $2 WHERE id = $1::uuid`,
		r.ID, hash); err != nil {
		return Request{}, fmt.Errorf("agereview: store audit reference: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return Request{}, fmt.Errorf("agereview: commit reject: %w", err)
	}
	r.Status = "rejected"
	r.ReasonCode = reason
	r.ReviewerID = a.UserID
	r.DecidedAt = &now
	return r, nil
}

// Revoke withdraws every live verification an account holds and closes the
// associated approved review request.
func (s *Service) Revoke(ctx context.Context, a Actor, userID, reason, requestID string) (int64, error) {
	if err := s.authorize(a); err != nil {
		return 0, err
	}
	if userID == "" {
		return 0, fmt.Errorf("%w: user is required", ErrInvalidInput)
	}
	if userID == a.UserID {
		return 0, ErrSelfReview
	}
	if !slices.Contains(Reasons, reason) {
		return 0, ErrInvalidReason
	}
	now := s.now().UTC()
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return 0, fmt.Errorf("agereview: begin revoke: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	rows, err := tx.Query(ctx, `
		UPDATE verify.checks
		SET status = 'revoked', revoked_at = $2, failure_reason = $3, updated_at = now()
		WHERE user_id = $1::uuid AND status = 'verified' AND revoked_at IS NULL
		RETURNING id::text`, userID, now, reason)
	if err != nil {
		return 0, fmt.Errorf("agereview: revoke checks: %w", err)
	}
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return 0, fmt.Errorf("agereview: scan revoked: %w", err)
		}
		ids = append(ids, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, fmt.Errorf("agereview: revoke checks: %w", err)
	}
	if len(ids) == 0 {
		return 0, ErrNoVerification
	}
	for _, id := range ids {
		if _, err := tx.Exec(ctx, `
			INSERT INTO verify.revocations (user_id, check_id, actor_id, reason)
			VALUES ($1::uuid, $2::uuid, $3::uuid, $4)`, userID, id, a.UserID, reason); err != nil {
			return 0, fmt.Errorf("agereview: record revocation: %w", err)
		}
	}
	if _, err := tx.Exec(ctx, `
		UPDATE verify.review_requests
		SET status = 'revoked', reason_code = $2, reviewer_id = $3::uuid, decided_at = $4
		WHERE user_id = $1::uuid AND status = 'approved' AND check_id::text = ANY($5::text[])`,
		userID, reason, a.UserID, now, ids); err != nil {
		return 0, fmt.Errorf("agereview: close review request: %w", err)
	}
	if err := recordAttempt(ctx, tx, userID, "revoked", reason); err != nil {
		return 0, err
	}
	hash, err := appendAudit(ctx, tx, a.UserID, "age_review.revoked", userID,
		map[string]any{"reason_code": reason, "checks": len(ids)}, requestID)
	if err != nil {
		return 0, err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE verify.review_requests SET audit_ref = $3
		WHERE user_id = $1::uuid AND status = 'revoked' AND check_id::text = ANY($2::text[])`,
		userID, ids, hash); err != nil {
		return 0, fmt.Errorf("agereview: store audit reference: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, fmt.Errorf("agereview: commit revoke: %w", err)
	}
	return int64(len(ids)), nil
}

func lockPending(ctx context.Context, tx pgx.Tx, id string) (Request, error) {
	r, err := scanRequest(tx.QueryRow(ctx, `
		SELECT id::text, user_id::text, status, method, reason_code,
		       COALESCE(reviewer_id::text, ''), COALESCE(check_id::text, ''),
		       requested_at, decided_at, expires_at
		FROM verify.review_requests WHERE id = $1::uuid FOR UPDATE`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return Request{}, ErrNotFound
	}
	if err != nil {
		return Request{}, fmt.Errorf("agereview: lock request: %w", err)
	}
	if r.Status != "pending" {
		return Request{}, ErrNotPending
	}
	return r, nil
}

type scanner interface{ Scan(...any) error }

func scanRequest(row scanner) (Request, error) {
	var r Request
	err := row.Scan(&r.ID, &r.UserID, &r.Status, &r.Method, &r.ReasonCode,
		&r.ReviewerID, &r.CheckID, &r.RequestedAt, &r.DecidedAt, &r.ExpiresAt)
	return r, err
}

func recordAttempt(ctx context.Context, tx pgx.Tx, userID, outcome, reason string) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO verify.attempts (user_id, outcome, reason) VALUES ($1::uuid, $2, $3)`,
		userID, outcome, reason)
	if err != nil {
		return fmt.Errorf("agereview: record attempt: %w", err)
	}
	return nil
}

func appendAudit(ctx context.Context, tx pgx.Tx, actorID, action, targetID string,
	metadata map[string]any, requestID string) (string, error) {
	var hash string
	err := tx.QueryRow(ctx, `
		SELECT ops.append_audit('user', $1::text, $2, 'user', $3, NULL, $4::jsonb, NULLIF($5, ''))`,
		actorID, action, targetID, metadata, requestID).Scan(&hash)
	if err != nil {
		return "", fmt.Errorf("agereview: audit %s: %w", action, err)
	}
	return hash, nil
}
