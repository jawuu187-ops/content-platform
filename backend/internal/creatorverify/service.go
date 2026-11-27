// Package creatorverify owns the metadata-only creator identity/eligibility
// review. It never receives or stores an identity document.
package creatorverify

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/jawuu187-ops/content-platform/backend/internal/identity"
)

var (
	ErrNotFound  = errors.New("creatorverify: not found")
	ErrInvalid   = errors.New("creatorverify: invalid input")
	ErrForbidden = errors.New("creatorverify: forbidden")
)

const mfaWindow = 5 * time.Minute

type Request struct {
	ID          string     `json:"id"`
	UserID      string     `json:"user_id"`
	Status      string     `json:"status"`
	LegalName   string     `json:"legal_name"`
	CountryCode string     `json:"country_code"`
	DateOfBirth string     `json:"date_of_birth"`
	ReasonCode  string     `json:"reason_code,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
	DecidedAt   *time.Time `json:"decided_at,omitempty"`
	ExpiresAt   *time.Time `json:"expires_at,omitempty"`
}

type SubmitInput struct {
	LegalName   string `json:"legal_name"`
	CountryCode string `json:"country_code"`
	DateOfBirth string `json:"date_of_birth"`
	Attestation bool   `json:"attestation"`
}

type Service struct {
	pool *pgxpool.Pool
	now  func() time.Time
}

func New(pool *pgxpool.Pool) *Service { return &Service{pool: pool, now: time.Now} }

func scan(row pgx.Row) (Request, error) {
	var r Request
	var dob time.Time
	var decided, expires *time.Time
	err := row.Scan(&r.ID, &r.UserID, &r.Status, &r.LegalName, &r.CountryCode, &dob,
		&r.ReasonCode, &r.CreatedAt, &decided, &expires)
	r.DateOfBirth = dob.Format("2006-01-02")
	r.DecidedAt, r.ExpiresAt = decided, expires
	return r, err
}

const selectCols = `id::text, user_id::text, status, legal_name, country_code,
	date_of_birth, reason_code, created_at, decided_at, expires_at`

// Submit creates or replaces the caller's pending application. A rejected
// creator may reapply; an approved one is told to stop.
func (s *Service) Submit(ctx context.Context, p identity.Principal, in SubmitInput, requestID string) (Request, error) {
	name := strings.TrimSpace(in.LegalName)
	if len([]rune(name)) < 2 || len([]rune(name)) > 120 {
		return Request{}, fmt.Errorf("%w: legal name must be 2-120 characters", ErrInvalid)
	}
	country := strings.ToUpper(strings.TrimSpace(in.CountryCode))
	if len(country) != 2 || country[0] < 'A' || country[0] > 'Z' || country[1] < 'A' || country[1] > 'Z' {
		return Request{}, fmt.Errorf("%w: country must be a two-letter code", ErrInvalid)
	}
	dob, err := time.Parse("2006-01-02", strings.TrimSpace(in.DateOfBirth))
	if err != nil {
		return Request{}, fmt.Errorf("%w: date of birth must be YYYY-MM-DD", ErrInvalid)
	}
	now := s.now().UTC()
	cutoff := now.AddDate(-18, 0, 0)
	if dob.After(cutoff) {
		return Request{}, fmt.Errorf("%w: creator must be at least 18", ErrInvalid)
	}
	if dob.Before(now.AddDate(-120, 0, 0)) {
		return Request{}, fmt.Errorf("%w: date of birth is not plausible", ErrInvalid)
	}
	if !in.Attestation {
		return Request{}, fmt.Errorf("%w: the attestation is required", ErrInvalid)
	}

	var status string
	if err := s.pool.QueryRow(ctx, `SELECT kyc_status FROM creator.profiles WHERE user_id = $1::uuid`, p.UserID).Scan(&status); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Request{}, ErrNotFound
		}
		return Request{}, fmt.Errorf("creatorverify: profile lookup: %w", err)
	}
	if status == "verified" {
		return Request{}, fmt.Errorf("%w: creator is already verified", ErrInvalid)
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Request{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `
		UPDATE creator.verification_requests SET status = 'revoked', decided_at = now(), reviewer_id = $1::uuid,
		    reason_code = 'other' WHERE user_id = $1::uuid AND status = 'pending'`, p.UserID); err != nil {
		return Request{}, fmt.Errorf("creatorverify: retire pending: %w", err)
	}
	r, err := scan(tx.QueryRow(ctx, `
		INSERT INTO creator.verification_requests
			(user_id, legal_name, country_code, date_of_birth, attestation)
		VALUES ($1::uuid, $2, $3, $4, true)
		RETURNING `+selectCols, p.UserID, name, country, dob))
	if err != nil {
		return Request{}, fmt.Errorf("creatorverify: insert: %w", err)
	}
	if _, err := tx.Exec(ctx, `UPDATE creator.profiles SET kyc_status = 'pending', updated_at = now() WHERE user_id = $1::uuid`, p.UserID); err != nil {
		return Request{}, err
	}
	if _, err := tx.Exec(ctx, `
		SELECT ops.append_audit('user', $1::text, 'creator.verification_requested', 'user', $1::text, NULL,
		    jsonb_build_object('request_id', $2::text), NULLIF($3::text, ''))`, p.UserID, r.ID, requestID); err != nil {
		return Request{}, fmt.Errorf("creatorverify: audit: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return Request{}, err
	}
	return r, nil
}

func (s *Service) Latest(ctx context.Context, userID string) (*Request, error) {
	r, err := scan(s.pool.QueryRow(ctx, `
		SELECT `+selectCols+` FROM creator.verification_requests
		WHERE user_id = $1::uuid ORDER BY created_at DESC LIMIT 1`, userID))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("creatorverify: latest: %w", err)
	}
	return &r, nil
}

func (s *Service) Queue(ctx context.Context, limit int) ([]Request, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	rows, err := s.pool.Query(ctx, `
		SELECT `+selectCols+` FROM creator.verification_requests
		WHERE status = 'pending' ORDER BY created_at LIMIT $1`, limit)
	if err != nil {
		return nil, fmt.Errorf("creatorverify: queue: %w", err)
	}
	defer rows.Close()
	out := make([]Request, 0, limit)
	for rows.Next() {
		r, err := scan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *Service) Get(ctx context.Context, id string) (Request, error) {
	r, err := scan(s.pool.QueryRow(ctx, `SELECT `+selectCols+` FROM creator.verification_requests WHERE id = $1::uuid`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return Request{}, ErrNotFound
	}
	return r, err
}

func recentMFA(p identity.Principal) bool {
	return p.MFAAt != nil && time.Since(*p.MFAAt) <= mfaWindow
}

func (s *Service) Decide(ctx context.Context, p identity.Principal, id string, approve bool, reason, requestID string) (Request, error) {
	if !identity.HasPermission(p.Roles, identity.PermVerifyReview) || !recentMFA(p) {
		return Request{}, ErrForbidden
	}
	reason = strings.TrimSpace(reason)
	if !approve {
		switch reason {
		case "identity_mismatch", "age_not_established", "sanctions_or_fraud", "incomplete_presentation", "other":
		default:
			return Request{}, fmt.Errorf("%w: a rejection reason is required", ErrInvalid)
		}
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Request{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var userID string
	if err := tx.QueryRow(ctx, `SELECT user_id::text FROM creator.verification_requests WHERE id = $1::uuid FOR UPDATE`, id).Scan(&userID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Request{}, ErrNotFound
		}
		return Request{}, err
	}
	status, profileStatus := "approved", "verified"
	if !approve {
		status, profileStatus = "rejected", "rejected"
	}
	var expires *time.Time
	if approve {
		e := s.now().UTC().AddDate(1, 0, 0)
		expires = &e
	}
	r, err := scan(tx.QueryRow(ctx, `
		UPDATE creator.verification_requests
		SET status = $2, reviewer_id = $3::uuid, reason_code = $4, decided_at = now(), expires_at = $5
		WHERE id = $1::uuid AND status = 'pending'
		RETURNING `+selectCols, id, status, p.UserID, reason, expires))
	if errors.Is(err, pgx.ErrNoRows) {
		return Request{}, fmt.Errorf("%w: request is no longer pending", ErrInvalid)
	}
	if err != nil {
		return Request{}, err
	}
	if _, err := tx.Exec(ctx, `UPDATE creator.profiles SET kyc_status = $2, updated_at = now() WHERE user_id = $1::uuid`, userID, profileStatus); err != nil {
		return Request{}, err
	}
	if _, err := tx.Exec(ctx, `
		SELECT ops.append_audit('user', $1::text, $2::text, 'user', $3::text, NULL,
		    jsonb_build_object('request_id', $4::text, 'reason', $5::text), NULLIF($6::text, ''))`,
		p.UserID, "creator.verification_"+status, userID, id, reason, requestID); err != nil {
		return Request{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Request{}, err
	}
	return r, nil
}
