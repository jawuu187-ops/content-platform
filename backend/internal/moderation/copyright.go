package moderation

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/jawuu187-ops/content-platform/backend/internal/identity"
)

type CopyrightClaim struct {
	ID              string     `json:"id"`
	ClaimantID      string     `json:"claimant_id"`
	TargetType      string     `json:"target_type"`
	TargetID        string     `json:"target_id"`
	WorkDescription string     `json:"work_description"`
	OriginalURL     string     `json:"original_url,omitempty"`
	Status          string     `json:"status"`
	CounterNotice   string     `json:"counter_notice,omitempty"`
	DecisionNote    string     `json:"decision_note,omitempty"`
	CreatedAt       time.Time  `json:"created_at"`
	DecidedAt       *time.Time `json:"decided_at,omitempty"`
}

type CopyrightInput struct {
	TargetType      string `json:"target_type"`
	TargetID        string `json:"target_id"`
	WorkDescription string `json:"work_description"`
	OriginalURL     string `json:"original_url"`
}

func (s *Service) SubmitCopyright(ctx context.Context, p identity.Principal, in CopyrightInput) (CopyrightClaim, error) {
	if in.TargetType != "post" && in.TargetType != "message" {
		return CopyrightClaim{}, fmt.Errorf("%w: target must be post or message", ErrInvalidInput)
	}
	in.WorkDescription = strings.TrimSpace(in.WorkDescription)
	if len([]rune(in.WorkDescription)) < 10 || len([]rune(in.WorkDescription)) > 2000 {
		return CopyrightClaim{}, fmt.Errorf("%w: describe the copyrighted work in 10-2000 characters", ErrInvalidInput)
	}
	if len(in.OriginalURL) > 500 {
		return CopyrightClaim{}, fmt.Errorf("%w: URL is too long", ErrInvalidInput)
	}
	var claim CopyrightClaim
	var decided *time.Time
	err := s.pool.QueryRow(ctx, `
		INSERT INTO moderation.copyright_claims
			(claimant_id, target_type, target_id, work_description, original_url)
		VALUES ($1::uuid, $2, $3::uuid, $4, $5)
		RETURNING id::text, claimant_id::text, target_type, target_id::text,
		          work_description, original_url, status, counter_notice, decision_note,
		          created_at, decided_at`,
		p.UserID, in.TargetType, in.TargetID, in.WorkDescription, strings.TrimSpace(in.OriginalURL)).
		Scan(&claim.ID, &claim.ClaimantID, &claim.TargetType, &claim.TargetID,
			&claim.WorkDescription, &claim.OriginalURL, &claim.Status, &claim.CounterNotice,
			&claim.DecisionNote, &claim.CreatedAt, &decided)
	if err != nil {
		return CopyrightClaim{}, fmt.Errorf("moderation: copyright claim: %w", err)
	}
	claim.DecidedAt = decided
	return claim, nil
}

func (s *Service) CopyrightMine(ctx context.Context, p identity.Principal) ([]CopyrightClaim, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id::text, claimant_id::text, target_type, target_id::text, work_description,
		       original_url, status, counter_notice, decision_note, created_at, decided_at
		FROM moderation.copyright_claims WHERE claimant_id = $1::uuid
		ORDER BY created_at DESC LIMIT 100`, p.UserID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanClaims(rows)
}

func (s *Service) CopyrightQueue(ctx context.Context) ([]CopyrightClaim, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id::text, claimant_id::text, target_type, target_id::text, work_description,
		       original_url, status, counter_notice, decision_note, created_at, decided_at
		FROM moderation.copyright_claims
		WHERE status IN ('open', 'reviewing', 'counter_notice')
		ORDER BY created_at LIMIT 100`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanClaims(rows)
}

func scanClaims(rows pgx.Rows) ([]CopyrightClaim, error) {
	out := []CopyrightClaim{}
	for rows.Next() {
		var c CopyrightClaim
		var decided *time.Time
		if err := rows.Scan(&c.ID, &c.ClaimantID, &c.TargetType, &c.TargetID, &c.WorkDescription,
			&c.OriginalURL, &c.Status, &c.CounterNotice, &c.DecisionNote, &c.CreatedAt, &decided); err != nil {
			return nil, err
		}
		c.DecidedAt = decided
		out = append(out, c)
	}
	return out, rows.Err()
}

// CounterNotice lets the uploader dispute a claim before a takedown decision.
func (s *Service) CounterNotice(ctx context.Context, p identity.Principal, claimID, notice string) (CopyrightClaim, error) {
	notice = strings.TrimSpace(notice)
	if len([]rune(notice)) < 10 || len([]rune(notice)) > 2000 {
		return CopyrightClaim{}, fmt.Errorf("%w: counter notice must be 10-2000 characters", ErrInvalidInput)
	}
	var targetID string
	var targetType string
	if err := s.pool.QueryRow(ctx, `SELECT target_type, target_id::text FROM moderation.copyright_claims WHERE id=$1::uuid`, claimID).Scan(&targetType, &targetID); err != nil {
		return CopyrightClaim{}, ErrNotFound
	}
	if targetType == "post" {
		var owner bool
		_ = s.pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM creator.posts WHERE id=$1::uuid AND creator_id=$2::uuid)`, targetID, p.UserID).Scan(&owner)
		if !owner {
			return CopyrightClaim{}, ErrForbidden
		}
	}
	var c CopyrightClaim
	var decided *time.Time
	err := s.pool.QueryRow(ctx, `
		UPDATE moderation.copyright_claims
		SET status = 'counter_notice', counter_notice = $2, updated_at = now()
		WHERE id = $1::uuid AND status IN ('open', 'reviewing')
		RETURNING id::text, claimant_id::text, target_type, target_id::text, work_description,
		          original_url, status, counter_notice, decision_note, created_at, decided_at`,
		claimID, notice).Scan(&c.ID, &c.ClaimantID, &c.TargetType, &c.TargetID, &c.WorkDescription,
		&c.OriginalURL, &c.Status, &c.CounterNotice, &c.DecisionNote, &c.CreatedAt, &decided)
	if errors.Is(err, pgx.ErrNoRows) {
		return CopyrightClaim{}, ErrConflict
	}
	c.DecidedAt = decided
	return c, err
}

// DecideCopyright records a takedown or rejection. An upheld post claim also
// removes the post in the same transaction.
func (s *Service) DecideCopyright(ctx context.Context, p identity.Principal, claimID string, takedown bool, note string) (CopyrightClaim, error) {
	if len([]rune(note)) > 2000 {
		return CopyrightClaim{}, fmt.Errorf("%w: note too long", ErrInvalidInput)
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return CopyrightClaim{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	status := "rejected"
	if takedown {
		status = "takedown"
	}
	var c CopyrightClaim
	var decided *time.Time
	err = tx.QueryRow(ctx, `
		UPDATE moderation.copyright_claims
		SET status = $2, reviewer_id = $3::uuid, decision_note = $4, decided_at = now(), updated_at = now()
		WHERE id = $1::uuid AND status IN ('open', 'reviewing', 'counter_notice')
		RETURNING id::text, claimant_id::text, target_type, target_id::text, work_description,
		          original_url, status, counter_notice, decision_note, created_at, decided_at`,
		claimID, status, p.UserID, note).Scan(&c.ID, &c.ClaimantID, &c.TargetType, &c.TargetID,
		&c.WorkDescription, &c.OriginalURL, &c.Status, &c.CounterNotice, &c.DecisionNote,
		&c.CreatedAt, &decided)
	if errors.Is(err, pgx.ErrNoRows) {
		return CopyrightClaim{}, ErrConflict
	}
	if err != nil {
		return CopyrightClaim{}, err
	}
	c.DecidedAt = decided
	if takedown && c.TargetType == "post" {
		if _, err := tx.Exec(ctx, `UPDATE creator.posts SET status = 'removed', published_at = NULL, scheduled_at = NULL, updated_at = now() WHERE id = $1::uuid`, c.TargetID); err != nil {
			return CopyrightClaim{}, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return CopyrightClaim{}, err
	}
	return c, nil
}
