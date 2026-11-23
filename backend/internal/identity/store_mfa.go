package identity

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// ---------------------------------------------------------------------------
// Email verification
// ---------------------------------------------------------------------------

func (s *store) insertEmailCode(ctx context.Context, userID string, codeHash []byte, expires time.Time) error {
	_, err := s.pool.Exec(ctx, `
		-- Supersede any live code for this user: a new request must invalidate the
		-- previous one, otherwise an old email stays usable.
		UPDATE identity.email_verifications
		SET consumed_at = now()
		WHERE user_id = $1::uuid AND consumed_at IS NULL`, userID)
	if err != nil {
		return fmt.Errorf("identity: supersede email codes: %w", err)
	}
	_, err = s.pool.Exec(ctx, `
		INSERT INTO identity.email_verifications (user_id, code_hash, expires_at)
		VALUES ($1::uuid, $2, $3)`, userID, codeHash, expires)
	if err != nil {
		return fmt.Errorf("identity: insert email code: %w", err)
	}
	return nil
}

// consumeEmailCode marks a code used. It returns ErrInvalidMFACode for a wrong,
// expired, already-used or over-attempted code; the caller cannot distinguish
// these cases, and neither can an attacker.
func (s *store) consumeEmailCode(ctx context.Context, userID string, codeHash []byte, now time.Time) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("identity: begin consume email code: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var id string
	var expires time.Time
	err = tx.QueryRow(ctx, `
		SELECT id::text, expires_at
		FROM identity.email_verifications
		WHERE user_id = $1::uuid AND code_hash = $2 AND consumed_at IS NULL AND attempts < $3
		FOR UPDATE`, userID, codeHash, maxVerificationAttempts).Scan(&id, &expires)
	if errors.Is(err, pgx.ErrNoRows) {
		// Count the failure against every live code so guessing burns the window.
		_, _ = tx.Exec(ctx, `
			UPDATE identity.email_verifications
			SET attempts = attempts + 1
			WHERE user_id = $1::uuid AND consumed_at IS NULL`, userID)
		if err := tx.Commit(ctx); err != nil {
			return fmt.Errorf("identity: commit failed verification attempt: %w", err)
		}
		return ErrInvalidMFACode
	}
	if err != nil {
		return fmt.Errorf("identity: load email code: %w", err)
	}
	if now.After(expires) {
		return ErrInvalidMFACode
	}

	if _, err := tx.Exec(ctx, `
		UPDATE identity.email_verifications SET consumed_at = now() WHERE id = $1::uuid`, id); err != nil {
		return fmt.Errorf("identity: consume email code: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		UPDATE identity.users SET email_verified_at = COALESCE(email_verified_at, now())
		WHERE id = $1::uuid`, userID); err != nil {
		return fmt.Errorf("identity: mark email verified: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("identity: commit email verification: %w", err)
	}
	return nil
}

// ---------------------------------------------------------------------------
// MFA factors
// ---------------------------------------------------------------------------

func (s *store) insertTOTPFactor(ctx context.Context, userID, label string, cipherText []byte) (string, error) {
	// A pending (unconfirmed) factor is replaced, so a user who abandoned
	// enrolment does not accumulate orphan secrets.
	if _, err := s.pool.Exec(ctx, `
		DELETE FROM identity.mfa_factors
		WHERE user_id = $1::uuid AND kind = 'totp' AND confirmed_at IS NULL`, userID); err != nil {
		return "", fmt.Errorf("identity: clear pending totp factor: %w", err)
	}
	var id string
	err := s.pool.QueryRow(ctx, `
		INSERT INTO identity.mfa_factors (user_id, kind, label, secret_cipher)
		VALUES ($1::uuid, 'totp', $2, $3)
		RETURNING id::text`, userID, label, cipherText).Scan(&id)
	if err != nil {
		return "", fmt.Errorf("identity: insert totp factor: %w", err)
	}
	return id, nil
}

func (s *store) confirmedTOTP(ctx context.Context, userID string) (factorID string, cipherText []byte, err error) {
	err = s.pool.QueryRow(ctx, `
		SELECT id::text, secret_cipher
		FROM identity.mfa_factors
		WHERE user_id = $1::uuid AND kind = 'totp' AND confirmed_at IS NOT NULL
		LIMIT 1`, userID).Scan(&factorID, &cipherText)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil, ErrNotFound
	}
	if err != nil {
		return "", nil, fmt.Errorf("identity: load totp factor: %w", err)
	}
	return factorID, cipherText, nil
}

func (s *store) pendingTOTP(ctx context.Context, userID string) (factorID string, cipherText []byte, err error) {
	err = s.pool.QueryRow(ctx, `
		SELECT id::text, secret_cipher
		FROM identity.mfa_factors
		WHERE user_id = $1::uuid AND kind = 'totp' AND confirmed_at IS NULL
		ORDER BY created_at DESC
		LIMIT 1`, userID).Scan(&factorID, &cipherText)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil, ErrNotFound
	}
	if err != nil {
		return "", nil, fmt.Errorf("identity: load pending totp factor: %w", err)
	}
	return factorID, cipherText, nil
}

func (s *store) confirmFactor(ctx context.Context, factorID string) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE identity.mfa_factors SET confirmed_at = now(), last_used_at = now()
		WHERE id = $1::uuid AND confirmed_at IS NULL`, factorID)
	if err != nil {
		return fmt.Errorf("identity: confirm factor: %w", err)
	}
	return nil
}

func (s *store) touchFactor(ctx context.Context, factorID string) {
	_, _ = s.pool.Exec(ctx, `UPDATE identity.mfa_factors SET last_used_at = now() WHERE id = $1::uuid`, factorID)
}

func (s *store) deleteFactors(ctx context.Context, userID string) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM identity.mfa_factors WHERE user_id = $1::uuid`, userID)
	if err != nil {
		return fmt.Errorf("identity: delete factors: %w", err)
	}
	return nil
}

func (s *store) deleteRecoveryCodes(ctx context.Context, userID string) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM identity.recovery_codes WHERE user_id = $1::uuid AND used_at IS NULL`, userID)
	if err != nil {
		return fmt.Errorf("identity: delete recovery codes: %w", err)
	}
	return nil
}

func (s *store) insertRecoveryCodes(ctx context.Context, userID string, hashes [][]byte) error {
	batch := &pgx.Batch{}
	for _, h := range hashes {
		batch.Queue(`INSERT INTO identity.recovery_codes (user_id, code_hash) VALUES ($1::uuid, $2)
			ON CONFLICT (user_id, code_hash) DO NOTHING`, userID, h)
	}
	results := s.pool.SendBatch(ctx, batch)
	defer func() { _ = results.Close() }()
	for range hashes {
		if _, err := results.Exec(); err != nil {
			return fmt.Errorf("identity: insert recovery code: %w", err)
		}
	}
	return nil
}

func (s *store) useRecoveryCode(ctx context.Context, userID string, hash []byte) (bool, error) {
	tag, err := s.pool.Exec(ctx, `
		UPDATE identity.recovery_codes SET used_at = now()
		WHERE user_id = $1::uuid AND code_hash = $2 AND used_at IS NULL`, userID, hash)
	if err != nil {
		return false, fmt.Errorf("identity: use recovery code: %w", err)
	}
	return tag.RowsAffected() == 1, nil
}

func (s *store) countUnusedRecoveryCodes(ctx context.Context, userID string) (int, error) {
	var n int
	err := s.pool.QueryRow(ctx, `
		SELECT count(*) FROM identity.recovery_codes
		WHERE user_id = $1::uuid AND used_at IS NULL`, userID).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("identity: count recovery codes: %w", err)
	}
	return n, nil
}

func (s *store) emailVerifiedState(ctx context.Context, userID string) (bool, error) {
	var verified bool
	err := s.pool.QueryRow(ctx, `
		SELECT email_verified_at IS NOT NULL FROM identity.users WHERE id = $1::uuid`, userID).Scan(&verified)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, ErrNotFound
	}
	if err != nil {
		return false, fmt.Errorf("identity: load email state: %w", err)
	}
	return verified, nil
}

func (s *store) emailOf(ctx context.Context, userID string) (string, error) {
	var email string
	err := s.pool.QueryRow(ctx, `SELECT email::text FROM identity.users WHERE id = $1::uuid`, userID).Scan(&email)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", fmt.Errorf("identity: load email: %w", err)
	}
	return email, nil
}
