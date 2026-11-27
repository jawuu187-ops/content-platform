package identity

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// store holds every SQL statement that touches the identity schema. Keeping the
// SQL in one file makes it reviewable as a set and keeps the service free of
// query strings.
type store struct {
	pool *pgxpool.Pool
}

// pgUniqueViolation is the SQLSTATE for a unique constraint breach.
const pgUniqueViolation = "23505"

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == pgUniqueViolation
}

func (s *store) insertUser(ctx context.Context, tx pgx.Tx, email, hash, algo string, params string) (User, error) {
	var u User
	var verifiedAt *time.Time
	err := tx.QueryRow(ctx, `
		INSERT INTO identity.users (email, password_hash, password_algo, password_params)
		VALUES ($1, $2, $3, $4::jsonb)
		RETURNING id::text, email::text, status, email_verified_at, is_creator, created_at`,
		email, hash, algo, params,
	).Scan(&u.ID, &u.Email, &u.Status, &verifiedAt, &u.IsCreator, &u.CreatedAt)
	if err != nil {
		if isUniqueViolation(err) {
			return User{}, ErrEmailTaken
		}
		return User{}, fmt.Errorf("identity: insert user: %w", err)
	}
	u.EmailVerified = verifiedAt != nil
	u.PasswordUpgraded = true
	return u, nil
}

func (s *store) grantRole(ctx context.Context, tx pgx.Tx, userID, role, grantedBy string) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO identity.role_grants (user_id, role, granted_by)
		VALUES ($1::uuid, $2, $3)
		ON CONFLICT (user_id, role) DO NOTHING`, userID, role, grantedBy)
	if err != nil {
		return fmt.Errorf("identity: grant role: %w", err)
	}
	return nil
}

func (s *store) userByEmail(ctx context.Context, email string) (User, string, error) {
	var u User
	var hash string
	var verifiedAt *time.Time
	err := s.pool.QueryRow(ctx, `
		SELECT id::text, email::text, status, email_verified_at, is_creator, created_at, password_hash
		FROM identity.users
		WHERE email = $1 AND deleted_at IS NULL`, email,
	).Scan(&u.ID, &u.Email, &u.Status, &verifiedAt, &u.IsCreator, &u.CreatedAt, &hash)
	if errors.Is(err, pgx.ErrNoRows) {
		return User{}, "", ErrInvalidCredentials
	}
	if err != nil {
		return User{}, "", fmt.Errorf("identity: load user: %w", err)
	}
	u.EmailVerified = verifiedAt != nil
	return u, hash, nil
}

func (s *store) upgradeHash(ctx context.Context, userID, hash, algo string, params string) {
	// Best effort: a failed rehash must never fail a successful login.
	_, _ = s.pool.Exec(ctx, `
		UPDATE identity.users
		SET password_hash = $2, password_algo = $3, password_params = $4::jsonb
		WHERE id = $1::uuid`, userID, hash, algo, params)
}

func (s *store) upsertDevice(ctx context.Context, tx pgx.Tx, userID string, d DeviceInfo) (string, error) {
	platform := d.Platform
	switch platform {
	case "android", "web", "admin":
	default:
		platform = "unknown"
	}
	name := d.Name
	if name == "" {
		name = "Unnamed device"
	}
	var id string
	err := tx.QueryRow(ctx, `
		INSERT INTO identity.devices (user_id, fingerprint, platform, name)
		VALUES ($1::uuid, $2, $3, $4)
		ON CONFLICT (user_id, fingerprint) DO UPDATE
		SET last_seen_at = now(),
		    name = CASE WHEN EXCLUDED.name = 'Unnamed device' THEN identity.devices.name ELSE EXCLUDED.name END
		RETURNING id::text`,
		userID, deviceFingerprint(d.Fingerprint), platform, name,
	).Scan(&id)
	if err != nil {
		return "", fmt.Errorf("identity: upsert device: %w", err)
	}
	return id, nil
}

func (s *store) insertSession(ctx context.Context, tx pgx.Tx, userID, familyID string, deviceID *string,
	accessHash, refreshHash []byte, accessExp, refreshExp time.Time, d DeviceInfo, mfaAt *time.Time) (string, error) {
	var id string
	err := tx.QueryRow(ctx, `
		INSERT INTO identity.sessions (
			user_id, family_id, device_id, access_hash, access_expires_at,
			refresh_hash, refresh_expires_at, user_agent, ip, mfa_at
		) VALUES (
			$1::uuid, $2::uuid, NULLIF($3, '')::uuid, $4, $5, $6, $7, NULLIF($8, ''), NULLIF($9, '')::inet, $10
		) RETURNING id::text`,
		userID, familyID, deref(deviceID), accessHash, accessExp, refreshHash, refreshExp, d.UserAgent, d.IP, mfaAt,
	).Scan(&id)
	if err != nil {
		return "", fmt.Errorf("identity: insert session: %w", err)
	}
	return id, nil
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func (s *store) sessionByAccess(ctx context.Context, accessHash []byte) (Principal, time.Time, error) {
	var p Principal
	var expires time.Time
	var revokedAt, verifiedAt *time.Time
	err := s.pool.QueryRow(ctx, `
		SELECT s.id::text, s.user_id::text, s.access_expires_at, s.revoked_at, u.email_verified_at, s.mfa_at
		FROM identity.sessions s
		JOIN identity.users u ON u.id = s.user_id
		WHERE s.access_hash = $1`, accessHash,
	).Scan(&p.SessionID, &p.UserID, &expires, &revokedAt, &verifiedAt, &p.MFAAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Principal{}, time.Time{}, ErrInvalidCredentials
	}
	if err != nil {
		return Principal{}, time.Time{}, fmt.Errorf("identity: load session: %w", err)
	}
	if revokedAt != nil {
		return Principal{}, time.Time{}, ErrSessionRevoked
	}
	p.EmailVerified = verifiedAt != nil
	return p, expires, nil
}

func (s *store) touchSession(ctx context.Context, sessionID string) {
	_, _ = s.pool.Exec(ctx, `UPDATE identity.sessions SET last_used_at = now() WHERE id = $1::uuid`, sessionID)
}

func (s *store) refreshRow(ctx context.Context, tx pgx.Tx, refreshHash []byte) (row sessionRow, err error) {
	var revokedAt *time.Time
	err = tx.QueryRow(ctx, `
		SELECT id::text, user_id::text, family_id::text,
		       refresh_expires_at, revoked_at, replaced_by::text, user_agent, ip::text, mfa_at
		FROM identity.sessions
		WHERE refresh_hash = $1
		FOR UPDATE`, refreshHash,
	).Scan(&row.ID, &row.UserID, &row.FamilyID, &row.RefreshExpiresAt, &revokedAt, &row.ReplacedBy,
		&row.UserAgent, &row.IP, &row.MFAAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return sessionRow{}, ErrInvalidCredentials
	}
	if err != nil {
		return sessionRow{}, fmt.Errorf("identity: load refresh session: %w", err)
	}
	row.RevokedAt = revokedAt
	return row, nil
}

type sessionRow struct {
	ID               string
	UserID           string
	FamilyID         string
	RefreshExpiresAt time.Time
	RevokedAt        *time.Time
	ReplacedBy       *string
	UserAgent        *string
	IP               *string
	MFAAt            *time.Time
}

func (s *store) revokeSession(ctx context.Context, tx pgx.Tx, sessionID, reason string) error {
	_, err := tx.Exec(ctx, `
		UPDATE identity.sessions
		SET revoked_at = COALESCE(revoked_at, now()), revoked_reason = COALESCE(revoked_reason, $2)
		WHERE id = $1::uuid`, sessionID, reason)
	if err != nil {
		return fmt.Errorf("identity: revoke session: %w", err)
	}
	return nil
}

func (s *store) revokeFamily(ctx context.Context, tx pgx.Tx, familyID, reason string) error {
	_, err := tx.Exec(ctx, `
		UPDATE identity.sessions
		SET revoked_at = COALESCE(revoked_at, now()), revoked_reason = COALESCE(revoked_reason, $2)
		WHERE family_id = $1::uuid`, familyID, reason)
	if err != nil {
		return fmt.Errorf("identity: revoke session family: %w", err)
	}
	return nil
}

func (s *store) markRotated(ctx context.Context, tx pgx.Tx, oldID, newID string) error {
	_, err := tx.Exec(ctx, `
		UPDATE identity.sessions
		SET revoked_at = now(), revoked_reason = 'rotated', replaced_by = $2::uuid
		WHERE id = $1::uuid`, oldID, newID)
	if err != nil {
		return fmt.Errorf("identity: mark session rotated: %w", err)
	}
	return nil
}

func (s *store) roles(ctx context.Context, userID string) ([]string, error) {
	rows, err := s.pool.Query(ctx, `SELECT role FROM identity.role_grants WHERE user_id = $1::uuid ORDER BY role`, userID)
	if err != nil {
		return nil, fmt.Errorf("identity: load roles: %w", err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var r string
		if err := rows.Scan(&r); err != nil {
			return nil, fmt.Errorf("identity: scan role: %w", err)
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *store) listSessions(ctx context.Context, userID, currentID string) ([]Session, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT s.id::text, s.device_id::text, d.name, d.platform, s.user_agent, s.ip::text,
		       s.created_at, s.last_used_at, s.refresh_expires_at, s.revoked_at
		FROM identity.sessions s
		LEFT JOIN identity.devices d ON d.id = s.device_id
		WHERE s.user_id = $1::uuid
		ORDER BY s.created_at DESC
		LIMIT 100`, userID)
	if err != nil {
		return nil, fmt.Errorf("identity: list sessions: %w", err)
	}
	defer rows.Close()

	var out []Session
	for rows.Next() {
		var item Session
		if err := rows.Scan(&item.ID, &item.DeviceID, &item.DeviceName, &item.Platform, &item.UserAgent,
			&item.IP, &item.CreatedAt, &item.LastUsedAt, &item.ExpiresAt, &item.RevokedAt); err != nil {
			return nil, fmt.Errorf("identity: scan session: %w", err)
		}
		item.Current = item.ID == currentID
		out = append(out, item)
	}
	return out, rows.Err()
}

func (s *store) revokeOwnedSession(ctx context.Context, userID, sessionID string) error {
	tag, err := s.pool.Exec(ctx, `
		UPDATE identity.sessions
		SET revoked_at = COALESCE(revoked_at, now()), revoked_reason = 'user_revoked'
		WHERE id = $1::uuid AND user_id = $2::uuid AND revoked_at IS NULL`, sessionID, userID)
	if err != nil {
		return fmt.Errorf("identity: revoke owned session: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// userByID loads a user outside a transaction.
func (s *store) userByID(ctx context.Context, id string) (User, error) {
	var u User
	var verifiedAt *time.Time
	err := s.pool.QueryRow(ctx, `
		SELECT id::text, email::text, status, email_verified_at, is_creator, created_at
		FROM identity.users
		WHERE id = $1::uuid AND deleted_at IS NULL`, id,
	).Scan(&u.ID, &u.Email, &u.Status, &verifiedAt, &u.IsCreator, &u.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return User{}, ErrNotFound
	}
	if err != nil {
		return User{}, fmt.Errorf("identity: load user by id: %w", err)
	}
	u.EmailVerified = verifiedAt != nil
	return u, nil
}

func (s *store) userByIDTx(ctx context.Context, tx pgx.Tx, id string) (User, error) {
	var u User
	var verifiedAt *time.Time
	err := tx.QueryRow(ctx, `
		SELECT id::text, email::text, status, email_verified_at, is_creator, created_at
		FROM identity.users
		WHERE id = $1::uuid AND deleted_at IS NULL`, id,
	).Scan(&u.ID, &u.Email, &u.Status, &verifiedAt, &u.IsCreator, &u.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return User{}, ErrNotFound
	}
	if err != nil {
		return User{}, fmt.Errorf("identity: load user by id: %w", err)
	}
	u.EmailVerified = verifiedAt != nil
	return u, nil
}

// auditPool appends an audit entry outside a transaction. Failures are dropped:
// auditing must never make a user-visible action fail after it already happened.
func (s *store) auditPool(ctx context.Context, actorID, action, targetID, requestID string) {
	_, _ = s.pool.Exec(ctx, `
		SELECT ops.append_audit('user', $1, $2, 'session', $3, NULL, '{}'::jsonb, NULLIF($4, ''))`,
		actorID, action, targetID, requestID)
}

func (s *store) audit(ctx context.Context, tx pgx.Tx, actorType, actorID, action, targetType, targetID, requestID string) {
	_, _ = tx.Exec(ctx, `
		SELECT ops.append_audit($1, $2, $3, $4, $5, NULL, '{}'::jsonb, NULLIF($6, ''))`,
		actorType, actorID, action, targetType, targetID, requestID)
}

// setSessionMFA records a successful second-factor step-up for one session.
func (s *store) setSessionMFA(ctx context.Context, sessionID string, at time.Time) error {
	_, err := s.pool.Exec(ctx, `UPDATE identity.sessions SET mfa_at = $2 WHERE id = $1::uuid`, sessionID, at)
	if err != nil {
		return fmt.Errorf("identity: record session mfa: %w", err)
	}
	return nil
}

// clearUserMFA invalidates step-up state for every session of a user, used when
// the second factor itself changes.
func (s *store) clearUserMFA(ctx context.Context, userID string) error {
	_, err := s.pool.Exec(ctx, `UPDATE identity.sessions SET mfa_at = NULL WHERE user_id = $1::uuid`, userID)
	if err != nil {
		return fmt.Errorf("identity: clear session mfa: %w", err)
	}
	return nil
}
