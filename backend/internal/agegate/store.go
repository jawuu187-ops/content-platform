package agegate

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const checkColumns = `id::text, user_id::text, status, method, provider, provider_ref, assurance,
	min_age, verified_at, expires_at, revoked_at, failure_reason, created_at`

type store struct{ pool *pgxpool.Pool }

func scanCheck(row pgx.Row) (Check, error) {
	var c Check
	err := row.Scan(&c.ID, &c.UserID, &c.Status, &c.Method, &c.Provider, &c.ProviderRef,
		&c.Assurance, &c.MinAge, &c.VerifiedAt, &c.ExpiresAt, &c.RevokedAt, &c.Failure, &c.CreatedAt)
	return c, err
}

func (s *store) insertCheck(ctx context.Context, userID, method, provider, ref, assurance string, minAge int) (Check, error) {
	c, err := scanCheck(s.pool.QueryRow(ctx, `
		INSERT INTO verify.checks (user_id, status, method, provider, provider_ref, assurance, min_age)
		VALUES ($1::uuid, 'pending', $2, $3, $4, $5, $6)
		RETURNING `+checkColumns,
		userID, method, provider, ref, assurance, minAge))
	if err != nil {
		return Check{}, fmt.Errorf("agegate: insert check: %w", err)
	}
	return c, nil
}

// liveVerified returns the account's verified, unrevoked check. Expiry is
// applied by the caller against its own clock so the decision is not split
// between two clocks.
func (s *store) liveVerified(ctx context.Context, userID string) (Check, error) {
	c, err := scanCheck(s.pool.QueryRow(ctx, `
		SELECT `+checkColumns+` FROM verify.checks
		WHERE user_id = $1::uuid AND status = 'verified' AND revoked_at IS NULL
		ORDER BY verified_at DESC LIMIT 1`, userID))
	if errors.Is(err, pgx.ErrNoRows) {
		return Check{}, ErrNoLiveCheck
	}
	if err != nil {
		return Check{}, fmt.Errorf("agegate: load live check: %w", err)
	}
	return c, nil
}

func (s *store) latestCheck(ctx context.Context, userID string) (Check, error) {
	c, err := scanCheck(s.pool.QueryRow(ctx, `
		SELECT `+checkColumns+` FROM verify.checks
		WHERE user_id = $1::uuid
		ORDER BY created_at DESC LIMIT 1`, userID))
	if errors.Is(err, pgx.ErrNoRows) {
		return Check{}, ErrNoLiveCheck
	}
	if err != nil {
		return Check{}, fmt.Errorf("agegate: load latest check: %w", err)
	}
	return c, nil
}

func (s *store) countStartsSince(ctx context.Context, userID string, since time.Time) (int, error) {
	var n int
	err := s.pool.QueryRow(ctx, `
		SELECT count(*) FROM verify.attempts
		WHERE user_id = $1::uuid AND outcome = 'started' AND created_at >= $2`, userID, since).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("agegate: count attempts: %w", err)
	}
	return n, nil
}

func (s *store) recordAttempt(ctx context.Context, tx pgx.Tx, userID, outcome, reason string) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO verify.attempts (user_id, outcome, reason) VALUES ($1::uuid, $2, $3)`,
		userID, outcome, reason)
	if err != nil {
		return fmt.Errorf("agegate: record attempt: %w", err)
	}
	return nil
}

func (s *store) recordAttemptPool(ctx context.Context, userID, outcome, reason string) error {
	var id any
	if userID != "" {
		id = userID
	}
	_, err := s.pool.Exec(ctx, `
		INSERT INTO verify.attempts (user_id, outcome, reason) VALUES ($1::uuid, $2, $3)`, id, outcome, reason)
	if err != nil {
		return fmt.Errorf("agegate: record attempt: %w", err)
	}
	return nil
}

// applyCallback writes the provider's answer. The event id is inserted first,
// so a replayed webhook is rejected by the primary key rather than by a race.
func (s *store) applyCallback(ctx context.Context, provider string, cb Callback, now time.Time) (Check, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Check{}, fmt.Errorf("agegate: begin callback: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	tag, err := tx.Exec(ctx, `
		INSERT INTO verify.callbacks (provider, event_id) VALUES ($1, $2)
		ON CONFLICT DO NOTHING`, provider, cb.EventID)
	if err != nil {
		return Check{}, fmt.Errorf("agegate: record callback: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return Check{}, ErrDuplicateCallback
	}

	// Lock the check row: a concurrent revocation and a late "verified" callback
	// must not interleave into a verified account that a moderator revoked.
	var existing Check
	existing, err = scanCheck(tx.QueryRow(ctx,
		`SELECT `+checkColumns+` FROM verify.checks WHERE provider = $1 AND provider_ref = $2 FOR UPDATE`,
		provider, cb.Reference))
	if errors.Is(err, pgx.ErrNoRows) {
		return Check{}, ErrUnknownReference
	}
	if err != nil {
		return Check{}, fmt.Errorf("agegate: lock check: %w", err)
	}
	if existing.UserID != cb.AccountID {
		return Check{}, ErrAccountMismatch
	}
	if existing.Status != StatusPending {
		// The check is already decided. A second decision for the same attempt is
		// ignored instead of overwriting the first.
		return Check{}, ErrDuplicateCallback
	}

	if cb.Status == StatusVerified {
		// Supersede an older live verification before inserting the new one, so the
		// partial unique index is never violated.
		if _, err := tx.Exec(ctx, `
			UPDATE verify.checks SET status = 'expired', updated_at = now()
			WHERE user_id = $1::uuid AND status = 'verified' AND revoked_at IS NULL AND id <> $2::uuid`,
			existing.UserID, existing.ID); err != nil {
			return Check{}, fmt.Errorf("agegate: supersede previous verification: %w", err)
		}
	}

	var verifiedAt *time.Time
	var expiresAt *time.Time
	var failure string
	var revokedAt *time.Time
	switch cb.Status {
	case StatusVerified:
		v := now
		if cb.VerifiedAt != nil {
			v = *cb.VerifiedAt
		}
		verifiedAt = &v
		expiresAt = cb.ExpiresAt
	case StatusFailed:
		failure = cb.Reason
	case StatusRevoked:
		r := now
		revokedAt = &r
		failure = cb.Reason
	case StatusExpired:
		failure = cb.Reason
	}

	updated, err := scanCheck(tx.QueryRow(ctx, `
		UPDATE verify.checks
		SET status = $3, verified_at = $4, expires_at = $5, revoked_at = $6,
		    failure_reason = $7, updated_at = now()
		WHERE id = $1::uuid AND user_id = $2::uuid
		RETURNING `+checkColumns,
		existing.ID, existing.UserID, cb.Status, verifiedAt, expiresAt, revokedAt, failure))
	if err != nil {
		return Check{}, fmt.Errorf("agegate: apply callback: %w", err)
	}
	if err := s.recordAttempt(ctx, tx, existing.UserID, string(cb.Status), cb.Reason); err != nil {
		return Check{}, err
	}
	if _, err := tx.Exec(ctx, `
		SELECT ops.append_audit('user', $1, $2, 'age_verification', $3, NULL,
			jsonb_build_object('assurance', $4::text, 'provider', $5::text), NULL)`,
		existing.UserID, "age_verification."+string(cb.Status), existing.ID,
		cb.Assurance, provider); err != nil {
		return Check{}, fmt.Errorf("agegate: audit callback: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return Check{}, fmt.Errorf("agegate: commit callback: %w", err)
	}
	return updated, nil
}

// revoke withdraws every live verification an account holds.
func (s *store) revoke(ctx context.Context, userID, actorID, reason string, now time.Time) (int64, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return 0, fmt.Errorf("agegate: begin revoke: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	rows, err := tx.Query(ctx, `
		UPDATE verify.checks SET status = 'revoked', revoked_at = $2, failure_reason = $3, updated_at = now()
		WHERE user_id = $1::uuid AND status = 'verified' AND revoked_at IS NULL
		RETURNING id::text`, userID, now, reason)
	if err != nil {
		return 0, fmt.Errorf("agegate: revoke checks: %w", err)
	}
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return 0, fmt.Errorf("agegate: scan revoked id: %w", err)
		}
		ids = append(ids, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, fmt.Errorf("agegate: revoke checks: %w", err)
	}
	for _, id := range ids {
		if _, err := tx.Exec(ctx, `
			INSERT INTO verify.revocations (user_id, check_id, actor_id, reason)
			VALUES ($1::uuid, $2::uuid, NULLIF($3, '')::uuid, $4)`, userID, id, actorID, reason); err != nil {
			return 0, fmt.Errorf("agegate: record revocation: %w", err)
		}
	}
	if len(ids) > 0 {
		if err := s.recordAttempt(ctx, tx, userID, "revoked", reason); err != nil {
			return 0, err
		}
		if _, err := tx.Exec(ctx, `
			SELECT ops.append_audit('user', NULLIF($1, ''), 'age_verification.revoked',
				'user', $2, NULL, jsonb_build_object('reason', $3::text), NULL)`,
			actorID, userID, reason); err != nil {
			return 0, fmt.Errorf("agegate: audit revocation: %w", err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, fmt.Errorf("agegate: commit revoke: %w", err)
	}
	return int64(len(ids)), nil
}

// expireDue flips verifications whose validity window has passed.
func (s *store) expireDue(ctx context.Context, now time.Time) (int64, error) {
	tag, err := s.pool.Exec(ctx, `
		UPDATE verify.checks SET status = 'expired', updated_at = now()
		WHERE status = 'verified' AND revoked_at IS NULL AND expires_at IS NOT NULL AND expires_at <= $1`, now)
	if err != nil {
		return 0, fmt.Errorf("agegate: expire verifications: %w", err)
	}
	return tag.RowsAffected(), nil
}
