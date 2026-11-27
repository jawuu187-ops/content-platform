// Package mailsim is the simulated email transport. It persists messages in
// PostgreSQL for inspection by the local mail CLI and never contacts a network.
package mailsim

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/jawuu187-ops/content-platform/backend/internal/platform/mail"
)

// Sender stores each message in mail.simulated_messages.
type Sender struct {
	pool *pgxpool.Pool
}

// New returns a simulated sender. The pool must already be migrated.
func New(pool *pgxpool.Pool) *Sender { return &Sender{pool: pool} }

// Send records the message. The body is never written to application logs.
func (s *Sender) Send(ctx context.Context, m mail.Message) error {
	kind := m.Kind
	if kind == "" {
		kind = "transactional"
	}
	_, err := s.pool.Exec(ctx, `
		INSERT INTO mail.simulated_messages (recipient, subject, body, kind)
		VALUES ($1, $2, $3, $4)`, m.To, m.Subject, m.Text, kind)
	if err != nil {
		return fmt.Errorf("mailsim: store message: %w", err)
	}
	return nil
}

// Message is one stored simulated email.
type Message struct {
	ID        string
	Recipient string
	Subject   string
	Body      string
	Kind      string
	CreatedAt time.Time
	ReadAt    *time.Time
}

// Latest returns the newest messages first.
func Latest(ctx context.Context, pool *pgxpool.Pool, limit int) ([]Message, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	rows, err := pool.Query(ctx, `
		SELECT id::text, recipient, subject, body, kind, created_at, read_at
		FROM mail.simulated_messages
		ORDER BY created_at DESC
		LIMIT $1`, limit)
	if err != nil {
		return nil, fmt.Errorf("mailsim: list messages: %w", err)
	}
	defer rows.Close()
	var out []Message
	for rows.Next() {
		var m Message
		if err := rows.Scan(&m.ID, &m.Recipient, &m.Subject, &m.Body, &m.Kind, &m.CreatedAt, &m.ReadAt); err != nil {
			return nil, fmt.Errorf("mailsim: scan message: %w", err)
		}
		out = append(out, m)
	}
	return out, rows.Err()
}
