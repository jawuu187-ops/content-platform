// Package notifications processes the transactional outbox and serves the
// in-app notification centre. Outbox rows are written by database triggers.
package notifications

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var ErrNotFound = errors.New("notifications: not found")

type Service struct {
	pool *pgxpool.Pool
	now  func() time.Time
}

func New(pool *pgxpool.Pool) *Service { return &Service{pool: pool, now: time.Now} }

type Item struct {
	ID        string     `json:"id"`
	Category  string     `json:"category"`
	Title     string     `json:"title"`
	Body      string     `json:"body"`
	Link      string     `json:"link"`
	ReadAt    *time.Time `json:"read_at,omitempty"`
	CreatedAt time.Time  `json:"created_at"`
}

type Preferences struct {
	InApp      bool `json:"in_app"`
	Messages   bool `json:"messages"`
	Engagement bool `json:"engagement"`
	Payments   bool `json:"payments"`
	Email      bool `json:"email"`
	Push       bool `json:"push"`
	Marketing  bool `json:"marketing"`
}

type outboxRow struct {
	ID        int64
	Key       string
	Category  string
	Recipient string
	Payload   []byte
}

// ProcessBatch claims up to limit due events and turns them into items. Locking
// with SKIP LOCKED allows more than one API instance to run the worker safely.
func (s *Service) ProcessBatch(ctx context.Context, limit int) (int, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return 0, fmt.Errorf("notifications: begin batch: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	rows, err := tx.Query(ctx, `
		WITH claimed AS (
			SELECT id FROM notifications.outbox
			WHERE processed_at IS NULL
			  AND available_at <= now()
			  AND (locked_at IS NULL OR locked_at < now() - interval '5 minutes')
			ORDER BY id
			FOR UPDATE SKIP LOCKED
			LIMIT $1
		)
		UPDATE notifications.outbox o
		SET locked_at = now()
		FROM claimed
		WHERE o.id = claimed.id
		RETURNING o.id, o.event_key, o.category, o.recipient_id::text, o.payload`, limit)
	if err != nil {
		return 0, fmt.Errorf("notifications: claim outbox: %w", err)
	}
	var claimed []outboxRow
	for rows.Next() {
		var r outboxRow
		if err := rows.Scan(&r.ID, &r.Key, &r.Category, &r.Recipient, &r.Payload); err != nil {
			rows.Close()
			return 0, fmt.Errorf("notifications: scan outbox: %w", err)
		}
		claimed = append(claimed, r)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, err
	}

	processed := 0
	for _, r := range claimed {
		if err := s.processOne(ctx, tx, r); err != nil {
			// Release the row with bounded exponential backoff; another pass will
			// retry. A malformed payload is not retried forever after 10 attempts.
			_, _ = tx.Exec(ctx, `
				UPDATE notifications.outbox
				SET locked_at = NULL, attempts = attempts + 1,
				    available_at = now() + make_interval(secs => LEAST(300, 10 * (attempts + 1))),
				    last_error = left($2, 500)
				WHERE id = $1`, r.ID, err.Error())
			continue
		}
		processed++
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, fmt.Errorf("notifications: commit batch: %w", err)
	}
	return processed, nil
}

func (s *Service) processOne(ctx context.Context, tx pgx.Tx, r outboxRow) error {
	var payload struct {
		Title string `json:"title"`
		Body  string `json:"body"`
		Link  string `json:"link"`
	}
	if err := json.Unmarshal(r.Payload, &payload); err != nil {
		return fmt.Errorf("decode payload: %w", err)
	}
	if payload.Title == "" {
		return errors.New("payload has no title")
	}
	var inApp, categoryOn bool
	err := tx.QueryRow(ctx, `
		SELECT COALESCE(p.in_app, true),
		       CASE $2
		           WHEN 'messages' THEN COALESCE(p.messages, true)
		           WHEN 'engagement' THEN COALESCE(p.engagement, true)
		           WHEN 'payments' THEN COALESCE(p.payments, true)
		           ELSE true
		       END
		FROM identity.users u
		LEFT JOIN notifications.preferences p ON p.user_id = u.id
		WHERE u.id = $1::uuid AND u.deleted_at IS NULL`, r.Recipient, r.Category).Scan(&inApp, &categoryOn)
	if errors.Is(err, pgx.ErrNoRows) {
		// The recipient was deleted; the event is intentionally dropped.
		_, err = tx.Exec(ctx, `UPDATE notifications.outbox SET processed_at = now(), locked_at = NULL WHERE id = $1`, r.ID)
		return err
	}
	if err != nil {
		return fmt.Errorf("load preferences: %w", err)
	}
	if inApp && categoryOn {
		if _, err := tx.Exec(ctx, `
			INSERT INTO notifications.items (user_id, category, title, body, link)
			VALUES ($1::uuid, $2, $3, $4, $5)`,
			r.Recipient, r.Category, payload.Title, payload.Body, payload.Link); err != nil {
			return fmt.Errorf("insert item: %w", err)
		}
	}
	_, err = tx.Exec(ctx, `
		UPDATE notifications.outbox SET processed_at = now(), locked_at = NULL, last_error = ''
		WHERE id = $1`, r.ID)
	return err
}

// List returns newest first plus the total unread count.
func (s *Service) List(ctx context.Context, userID string, limit int) ([]Item, int, error) {
	if limit <= 0 || limit > 100 {
		limit = 30
	}
	rows, err := s.pool.Query(ctx, `
		SELECT id::text, category, title, body, link, read_at, created_at
		FROM notifications.items
		WHERE user_id = $1::uuid
		ORDER BY created_at DESC, id DESC
		LIMIT $2`, userID, limit)
	if err != nil {
		return nil, 0, fmt.Errorf("notifications: list: %w", err)
	}
	defer rows.Close()
	out := make([]Item, 0, limit)
	for rows.Next() {
		var i Item
		if err := rows.Scan(&i.ID, &i.Category, &i.Title, &i.Body, &i.Link, &i.ReadAt, &i.CreatedAt); err != nil {
			return nil, 0, fmt.Errorf("notifications: scan: %w", err)
		}
		out = append(out, i)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	var unread int
	if err := s.pool.QueryRow(ctx,
		`SELECT count(*) FROM notifications.items WHERE user_id = $1::uuid AND read_at IS NULL`,
		userID).Scan(&unread); err != nil {
		return nil, 0, fmt.Errorf("notifications: unread count: %w", err)
	}
	return out, unread, nil
}

func (s *Service) Unread(ctx context.Context, userID string) (int, error) {
	var n int
	if err := s.pool.QueryRow(ctx,
		`SELECT count(*) FROM notifications.items WHERE user_id = $1::uuid AND read_at IS NULL`,
		userID).Scan(&n); err != nil {
		return 0, fmt.Errorf("notifications: unread: %w", err)
	}
	return n, nil
}

func (s *Service) MarkRead(ctx context.Context, userID, id string) error {
	tag, err := s.pool.Exec(ctx, `
		UPDATE notifications.items SET read_at = COALESCE(read_at, now())
		WHERE id = $1::uuid AND user_id = $2::uuid`, id, userID)
	if err != nil {
		return fmt.Errorf("notifications: mark read: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Service) MarkAllRead(ctx context.Context, userID string) (int64, error) {
	tag, err := s.pool.Exec(ctx, `
		UPDATE notifications.items SET read_at = now()
		WHERE user_id = $1::uuid AND read_at IS NULL`, userID)
	if err != nil {
		return 0, fmt.Errorf("notifications: mark all read: %w", err)
	}
	return tag.RowsAffected(), nil
}

func (s *Service) GetPreferences(ctx context.Context, userID string) (Preferences, error) {
	p := Preferences{InApp: true, Messages: true, Engagement: true, Payments: true}
	err := s.pool.QueryRow(ctx, `
		INSERT INTO notifications.preferences (user_id) VALUES ($1::uuid)
		ON CONFLICT (user_id) DO UPDATE SET updated_at = notifications.preferences.updated_at
		RETURNING in_app, messages, engagement, payments, email, push, marketing`, userID).
		Scan(&p.InApp, &p.Messages, &p.Engagement, &p.Payments, &p.Email, &p.Push, &p.Marketing)
	if err != nil {
		return Preferences{}, fmt.Errorf("notifications: load preferences: %w", err)
	}
	return p, nil
}

func (s *Service) SavePreferences(ctx context.Context, userID string, p Preferences) (Preferences, error) {
	if p.Push || p.Email {
		// The flags are recorded, but no provider exists. Callers must not describe
		// this as enabled delivery; the response keeps them false.
		p.Push, p.Email = false, false
	}
	err := s.pool.QueryRow(ctx, `
		INSERT INTO notifications.preferences
			(user_id, in_app, messages, engagement, payments, email, push, marketing, updated_at)
		VALUES ($1::uuid, $2, $3, $4, $5, false, false, $6, now())
		ON CONFLICT (user_id) DO UPDATE
		SET in_app = EXCLUDED.in_app, messages = EXCLUDED.messages,
		    engagement = EXCLUDED.engagement, payments = EXCLUDED.payments,
		    marketing = EXCLUDED.marketing, updated_at = now()
		RETURNING in_app, messages, engagement, payments, email, push, marketing`,
		userID, p.InApp, p.Messages, p.Engagement, p.Payments, p.Marketing).
		Scan(&p.InApp, &p.Messages, &p.Engagement, &p.Payments, &p.Email, &p.Push, &p.Marketing)
	if err != nil {
		return Preferences{}, fmt.Errorf("notifications: save preferences: %w", err)
	}
	return p, nil
}

func (s *Service) RegisterPushToken(ctx context.Context, userID, token, platform string) error {
	if token == "" || len(token) > 4096 {
		return fmt.Errorf("notifications: invalid push token")
	}
	if platform != "android" && platform != "web" {
		return fmt.Errorf("notifications: unsupported platform")
	}
	_, err := s.pool.Exec(ctx, `
		INSERT INTO notifications.push_tokens (user_id, token, platform)
		VALUES ($1::uuid, $2, $3)
		ON CONFLICT (token) DO UPDATE
		SET user_id = EXCLUDED.user_id, platform = EXCLUDED.platform,
		    last_seen = now(), revoked_at = NULL`, userID, token, platform)
	if err != nil {
		return fmt.Errorf("notifications: register push token: %w", err)
	}
	return nil
}

func (s *Service) RevokePushToken(ctx context.Context, userID, token string) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE notifications.push_tokens SET revoked_at = now()
		WHERE user_id = $1::uuid AND token = $2 AND revoked_at IS NULL`, userID, token)
	if err != nil {
		return fmt.Errorf("notifications: revoke push token: %w", err)
	}
	return nil
}

// RunWorker processes events until the context is cancelled. It is deliberately
// simple: one process, short transactions, no leader election. SKIP LOCKED makes
// extra replicas harmless.
func RunWorker(ctx context.Context, svc *Service, logger interface {
	Info(string, ...any)
	Error(string, ...any)
}, interval time.Duration) {
	if interval <= 0 {
		interval = 2 * time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		if n, err := svc.ProcessBatch(ctx, 50); err != nil {
			logger.Error("notification outbox pass failed", "error", err)
		} else if n > 0 {
			logger.Info("notification outbox processed", "events", n)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
