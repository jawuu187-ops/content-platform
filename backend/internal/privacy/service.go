package privacy

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/jawuu187-ops/content-platform/backend/internal/identity"
)

var (
	ErrInvalid  = errors.New("privacy: invalid request")
	ErrNotFound = errors.New("privacy: not found")
)

type Consent struct {
	Kind      string     `json:"kind"`
	Version   string     `json:"version"`
	GrantedAt time.Time  `json:"granted_at"`
	RevokedAt *time.Time `json:"revoked_at,omitempty"`
}

type DeletionRequest struct {
	ID           string     `json:"id"`
	Status       string     `json:"status"`
	RequestedAt  time.Time  `json:"requested_at"`
	ScheduledFor time.Time  `json:"scheduled_for"`
	CancelledAt  *time.Time `json:"cancelled_at,omitempty"`
	CompletedAt  *time.Time `json:"completed_at,omitempty"`
}

type Service struct {
	pool  *pgxpool.Pool
	ident *identity.Service
	now   func() time.Time
}

func New(pool *pgxpool.Pool, ident *identity.Service) *Service {
	return &Service{pool: pool, ident: ident, now: time.Now}
}

func (s *Service) SaveConsent(ctx context.Context, userID, kind string, granted bool) ([]Consent, error) {
	switch kind {
	case "terms", "privacy", "marketing", "analytics":
	default:
		return nil, fmt.Errorf("%w: unknown consent kind", ErrInvalid)
	}
	_, err := s.pool.Exec(ctx, `
		INSERT INTO privacy.consents (user_id, kind, granted_at, revoked_at, updated_at)
		VALUES ($1::uuid, $2, now(), CASE WHEN $3 THEN NULL ELSE now() END, now())
		ON CONFLICT (user_id, kind) DO UPDATE
		SET granted_at = CASE WHEN $3 THEN now() ELSE privacy.consents.granted_at END,
		    revoked_at = CASE WHEN $3 THEN NULL ELSE now() END,
		    updated_at = now()`, userID, kind, granted)
	if err != nil {
		return nil, fmt.Errorf("privacy: save consent: %w", err)
	}
	return s.Consents(ctx, userID)
}

func (s *Service) Consents(ctx context.Context, userID string) ([]Consent, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT kind, version, granted_at, revoked_at FROM privacy.consents
		WHERE user_id = $1::uuid ORDER BY kind`, userID)
	if err != nil {
		return nil, fmt.Errorf("privacy: list consents: %w", err)
	}
	defer rows.Close()
	out := []Consent{}
	for rows.Next() {
		var c Consent
		if err := rows.Scan(&c.Kind, &c.Version, &c.GrantedAt, &c.RevokedAt); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// Export returns the caller's account data as a portable JSON object. It never
// includes password material or another participant's private profile data.
func (s *Service) Export(ctx context.Context, userID string) (map[string]any, error) {
	out := map[string]any{}
	var email, status string
	var createdAt time.Time
	if err := s.pool.QueryRow(ctx, `
		SELECT email::text, status, created_at FROM identity.users WHERE id = $1::uuid`, userID).
		Scan(&email, &status, &createdAt); err != nil {
		return nil, fmt.Errorf("privacy: export account: %w", err)
	}
	out["account"] = map[string]any{"id": userID, "email": email, "status": status, "created_at": createdAt}

	rows, err := s.pool.Query(ctx, `SELECT handle::text, display_name, bio, category, kyc_status, created_at
		FROM creator.profiles WHERE user_id = $1::uuid`, userID)
	if err == nil {
		if rows.Next() {
			var handle, name, bio, category, kyc string
			var created time.Time
			if rows.Scan(&handle, &name, &bio, &category, &kyc, &created) == nil {
				out["creator_profile"] = map[string]any{"handle": handle, "display_name": name, "bio": bio,
					"category": category, "kyc_status": kyc, "created_at": created}
			}
		}
		rows.Close()
	}

	subs, _ := s.queryMaps(ctx, `
		SELECT s.id::text, s.creator_id::text, s.status, s.price_minor, s.currency::text,
		       s.current_period_start, s.current_period_end, s.is_trial, s.promotion_code
		FROM creator.subscriptions s WHERE s.fan_id = $1::uuid ORDER BY s.created_at DESC`, userID)
	out["subscriptions"] = subs
	payments, _ := s.queryMaps(ctx, `
		SELECT id::text, purpose, amount_minor, currency::text, provider, status, created_at
		FROM payments.intents WHERE user_id = $1::uuid ORDER BY created_at DESC LIMIT 1000`, userID)
	out["payments"] = payments
	messages, _ := s.queryMaps(ctx, `
		SELECT m.id::text, m.conversation_id::text, m.sender_id::text, m.body, m.sent_at
		FROM messaging.messages m
		JOIN messaging.conversations c ON c.id = m.conversation_id
		WHERE c.fan_id = $1::uuid OR c.creator_id = $1::uuid
		ORDER BY m.sent_at DESC LIMIT 5000`, userID)
	out["messages"] = messages
	consents, _ := s.Consents(ctx, userID)
	out["consents"] = consents
	return out, nil
}

func (s *Service) queryMaps(ctx context.Context, sql string, args ...any) ([]map[string]any, error) {
	rows, err := s.pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	fields := rows.FieldDescriptions()
	out := []map[string]any{}
	for rows.Next() {
		values, err := rows.Values()
		if err != nil {
			return nil, err
		}
		m := make(map[string]any, len(fields))
		for i, f := range fields {
			m[string(f.Name)] = values[i]
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

func (s *Service) RequestDeletion(ctx context.Context, p identity.Principal, password, requestID string) (DeletionRequest, error) {
	if err := s.ident.VerifyPassword(ctx, p.UserID, password); err != nil {
		return DeletionRequest{}, err
	}
	var r DeletionRequest
	var cancelled, completed *time.Time
	when := s.now().UTC().AddDate(0, 0, 7)
	err := s.pool.QueryRow(ctx, `
		INSERT INTO privacy.deletion_requests (user_id, scheduled_for)
		VALUES ($1::uuid, $2)
		ON CONFLICT (user_id) WHERE status = 'scheduled' DO NOTHING
		RETURNING id::text, status, requested_at, scheduled_for, cancelled_at, completed_at`,
		p.UserID, when).Scan(&r.ID, &r.Status, &r.RequestedAt, &r.ScheduledFor, &cancelled, &completed)
	if errors.Is(err, pgx.ErrNoRows) {
		return DeletionRequest{}, fmt.Errorf("%w: a deletion is already scheduled", ErrInvalid)
	}
	if err != nil {
		return DeletionRequest{}, fmt.Errorf("privacy: request deletion: %w", err)
	}
	r.CancelledAt, r.CompletedAt = cancelled, completed
	_, _ = s.pool.Exec(ctx, `
		SELECT ops.append_audit('user', $1::text, 'account.deletion_scheduled', 'user', $1::text,
		NULL, jsonb_build_object('scheduled_for', $2::text), NULLIF($3::text, ''))`,
		p.UserID, when.Format(time.RFC3339), requestID)
	return r, nil
}

func (s *Service) CancelDeletion(ctx context.Context, userID string) (DeletionRequest, error) {
	var r DeletionRequest
	var cancelled, completed *time.Time
	err := s.pool.QueryRow(ctx, `
		UPDATE privacy.deletion_requests SET status = 'cancelled', cancelled_at = now()
		WHERE user_id = $1::uuid AND status = 'scheduled'
		RETURNING id::text, status, requested_at, scheduled_for, cancelled_at, completed_at`,
		userID).Scan(&r.ID, &r.Status, &r.RequestedAt, &r.ScheduledFor, &cancelled, &completed)
	if errors.Is(err, pgx.ErrNoRows) {
		return DeletionRequest{}, ErrNotFound
	}
	if err != nil {
		return DeletionRequest{}, err
	}
	r.CancelledAt, r.CompletedAt = cancelled, completed
	return r, nil
}

func (s *Service) LatestDeletion(ctx context.Context, userID string) (*DeletionRequest, error) {
	var r DeletionRequest
	var cancelled, completed *time.Time
	err := s.pool.QueryRow(ctx, `
		SELECT id::text, status, requested_at, scheduled_for, cancelled_at, completed_at
		FROM privacy.deletion_requests WHERE user_id = $1::uuid
		ORDER BY requested_at DESC LIMIT 1`, userID).
		Scan(&r.ID, &r.Status, &r.RequestedAt, &r.ScheduledFor, &cancelled, &completed)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	r.CancelledAt, r.CompletedAt = cancelled, completed
	return &r, nil
}

// ExecuteDue anonymizes due accounts in one transaction per account. Ledger and
// payment rows remain, but the account can no longer authenticate and its
// profile is hidden.
func (s *Service) ExecuteDue(ctx context.Context) (int, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id::text, user_id::text FROM privacy.deletion_requests
		WHERE status = 'scheduled' AND scheduled_for <= now() LIMIT 50`)
	if err != nil {
		return 0, err
	}
	type due struct{ id, userID string }
	var items []due
	for rows.Next() {
		var d due
		if err := rows.Scan(&d.id, &d.userID); err != nil {
			rows.Close()
			return 0, err
		}
		items = append(items, d)
	}
	rows.Close()
	done := 0
	for _, d := range items {
		tx, err := s.pool.Begin(ctx)
		if err != nil {
			return done, err
		}
		_, err = tx.Exec(ctx, `
			UPDATE identity.users
			SET email = 'deleted+' || id::text || '@invalid.local',
			    password_hash = '!deleted', password_algo = 'deleted', password_params = '{}'::jsonb,
			    status = 'deleted', deleted_at = now(), email_verified_at = NULL
			WHERE id = $1::uuid AND deleted_at IS NULL`, d.userID)
		if err == nil {
			_, err = tx.Exec(ctx, `
				UPDATE identity.sessions SET revoked_at = now(), revoked_reason = 'account_deleted'
				WHERE user_id = $1::uuid AND revoked_at IS NULL`, d.userID)
		}
		if err == nil {
			_, err = tx.Exec(ctx, `
				UPDATE creator.profiles
				SET display_name = 'Deleted creator', bio = '', avatar_url = NULL, cover_url = NULL,
				    avatar_media_id = NULL, cover_media_id = NULL, is_published = false,
				    kyc_status = 'unverified', updated_at = now()
				WHERE user_id = $1::uuid`, d.userID)
		}
		if err == nil {
			_, err = tx.Exec(ctx, `
				UPDATE privacy.deletion_requests SET status = 'completed', completed_at = now()
				WHERE id = $1::uuid`, d.id)
		}
		if err == nil {
			_, err = tx.Exec(ctx, `
				SELECT ops.append_audit('system', 'retention', 'account.deleted', 'user', $1::text,
				NULL, '{}'::jsonb, NULL)`, d.userID)
		}
		if err != nil {
			_ = tx.Rollback(ctx)
			continue
		}
		if err := tx.Commit(ctx); err == nil {
			done++
		}
	}
	return done, nil
}
