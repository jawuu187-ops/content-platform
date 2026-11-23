package identity

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// storedCredential is a registered authenticator as read back for verification.
type storedCredential struct {
	ID           string
	UserID       string
	CredentialID []byte
	PublicKey    []byte
	Alg          int
	SignCount    uint64
	Label        string
	Transports   []string
	CreatedAt    time.Time
	LastUsedAt   *time.Time
}

func (s *store) insertChallenge(ctx context.Context, kind string, userID *string, challenge []byte, expires time.Time) (string, error) {
	var id string
	err := s.pool.QueryRow(ctx, `
		INSERT INTO identity.webauthn_challenges (kind, user_id, challenge, expires_at)
		VALUES ($1, $2::uuid, $3, $4)
		RETURNING id::text`, kind, userID, challenge, expires).Scan(&id)
	if err != nil {
		return "", fmt.Errorf("identity: store webauthn challenge: %w", err)
	}
	return id, nil
}

// consumeChallenge marks a challenge used and returns it. The UPDATE is the lock:
// two concurrent ceremonies carrying the same challenge see exactly one row, so
// a captured response cannot be replayed.
func (s *store) consumeChallenge(ctx context.Context, id string) (string, *string, []byte, error) {
	var kind string
	var userID *string
	var challenge []byte
	err := s.pool.QueryRow(ctx, `
		UPDATE identity.webauthn_challenges
		SET consumed_at = now()
		WHERE id = $1::uuid AND consumed_at IS NULL AND expires_at > now()
		RETURNING kind, user_id::text, challenge`, id).Scan(&kind, &userID, &challenge)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil, nil, ErrChallengeConsumed
	}
	if err != nil {
		return "", nil, nil, fmt.Errorf("identity: consume webauthn challenge: %w", err)
	}
	return kind, userID, challenge, nil
}

// auditPool writes an audit row keyed on an arbitrary actor and target type.
func (s *store) auditTyped(ctx context.Context, actorType, actorID, action, targetType, targetID, requestID string) {
	if actorType == "" {
		actorType = "user"
	}
	_, _ = s.pool.Exec(ctx, `
		SELECT ops.append_audit($1, $2, $3, $4, $5, NULL, '{}'::jsonb, NULLIF($6, ''))`,
		actorType, actorID, action, targetType, targetID, requestID)
}

func (s *store) insertCredential(ctx context.Context, userID string, c verifiedCredential, label string) (storedCredential, error) {
	out := storedCredential{
		UserID:       userID,
		CredentialID: c.credentialID,
		PublicKey:    c.publicKey,
		Alg:          c.alg,
		SignCount:    c.signCount,
		Label:        label,
		Transports:   c.transports,
	}
	var aaguid *string
	if len(c.aaguid) == 16 {
		zero := true
		for _, b := range c.aaguid {
			if b != 0 {
				zero = false
				break
			}
		}
		if !zero {
			g := formatUUID(c.aaguid)
			aaguid = &g
		}
	}
	err := s.pool.QueryRow(ctx, `
		INSERT INTO identity.webauthn_credentials
			(user_id, credential_id, public_key_cose, alg, sign_count, aaguid, transports, label)
		VALUES ($1::uuid, $2, $3, $4, $5, $6::uuid, $7, $8)
		RETURNING id::text, created_at`,
		userID, c.credentialID, c.publicKey, c.alg, int64(c.signCount), aaguid, c.transports, label,
	).Scan(&out.ID, &out.CreatedAt)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			// The same authenticator cannot be enrolled twice, and the unique
			// credential id is what prevents it.
			return storedCredential{}, fmt.Errorf("%w: this authenticator is already registered", ErrInvalidInput)
		}
		return storedCredential{}, fmt.Errorf("identity: insert webauthn credential: %w", err)
	}
	return out, nil
}

// credentialByCredentialID resolves a credential presented during a
// discoverable login. Revoked credentials are reported as not found so they
// cannot be used and cannot confirm that they ever existed.
func (s *store) credentialByCredentialID(ctx context.Context, credID []byte) (storedCredential, error) {
	var c storedCredential
	var lastUsed *time.Time
	err := s.pool.QueryRow(ctx, `
		SELECT id::text, user_id::text, credential_id, public_key_cose, alg, sign_count,
		       label, transports, created_at, last_used_at
		FROM identity.webauthn_credentials
		WHERE credential_id = $1 AND revoked_at IS NULL`, credID,
	).Scan(&c.ID, &c.UserID, &c.CredentialID, &c.PublicKey, &c.Alg, &c.SignCount,
		&c.Label, &c.Transports, &c.CreatedAt, &lastUsed)
	if errors.Is(err, pgx.ErrNoRows) {
		return storedCredential{}, ErrNotFound
	}
	if err != nil {
		return storedCredential{}, fmt.Errorf("identity: load webauthn credential: %w", err)
	}
	c.LastUsedAt = lastUsed
	return c, nil
}

func (s *store) credentialsOf(ctx context.Context, userID string) ([]storedCredential, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id::text, user_id::text, credential_id, public_key_cose, alg, sign_count,
		       label, transports, created_at, last_used_at
		FROM identity.webauthn_credentials
		WHERE user_id = $1::uuid AND revoked_at IS NULL
		ORDER BY created_at`, userID)
	if err != nil {
		return nil, fmt.Errorf("identity: list webauthn credentials: %w", err)
	}
	defer rows.Close()
	var out []storedCredential
	for rows.Next() {
		var c storedCredential
		var lastUsed *time.Time
		if err := rows.Scan(&c.ID, &c.UserID, &c.CredentialID, &c.PublicKey, &c.Alg, &c.SignCount,
			&c.Label, &c.Transports, &c.CreatedAt, &lastUsed); err != nil {
			return nil, fmt.Errorf("identity: scan webauthn credential: %w", err)
		}
		c.LastUsedAt = lastUsed
		out = append(out, c)
	}
	return out, rows.Err()
}

func (s *store) touchCredential(ctx context.Context, id string, signCount uint64) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE identity.webauthn_credentials
		SET sign_count = $2, last_used_at = now()
		WHERE id = $1::uuid`, id, int64(signCount))
	if err != nil {
		return fmt.Errorf("identity: update webauthn credential: %w", err)
	}
	return nil
}

// revokeCredential soft-deletes one credential owned by userID. Returns
// ErrNotFound when it is not theirs, which also prevents probing for ids.
func (s *store) revokeCredential(ctx context.Context, userID, id string) error {
	tag, err := s.pool.Exec(ctx, `
		UPDATE identity.webauthn_credentials
		SET revoked_at = now()
		WHERE id = $1::uuid AND user_id = $2::uuid AND revoked_at IS NULL`, id, userID)
	if err != nil {
		return fmt.Errorf("identity: revoke webauthn credential: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// formatUUID renders 16 raw bytes as a canonical UUID string for aaguid storage.
func formatUUID(b []byte) string {
	if len(b) != 16 {
		return ""
	}
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}
