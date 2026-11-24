package messaging

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

// Service owns conversations and messages. It checks entitlement itself rather than
// trusting a caller: the HTTP layer is not the only possible caller.
type Service struct {
	pool *pgxpool.Pool
	now  func() time.Time
}

// New builds the service on a migrated database.
func New(pool *pgxpool.Pool) *Service {
	return &Service{pool: pool, now: time.Now}
}

// liveEntitlement reports whether the fan may message this creator right now: an
// active subscription or an unlock that has not been revoked or expired. Checking in
// SQL keeps "still allowed?" and "write the row" in one round trip's worth of truth
// instead of two decisions that can disagree.
func (s *Service) liveEntitlement(ctx context.Context, fanID, creatorID string) (bool, error) {
	var ok bool
	err := s.pool.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM creator.entitlements
			WHERE fan_id = $1::uuid AND creator_id = $2::uuid
			  AND revoked_at IS NULL
			  AND (expires_at IS NULL OR expires_at > now())
		)`, fanID, creatorID).Scan(&ok)
	if err != nil {
		return false, fmt.Errorf("messaging: entitlement check: %w", err)
	}
	return ok, nil
}

// Open returns the conversation between the caller and a creator, creating it on
// first contact. It refuses a fan with no live entitlement: a thread that can never
// be read is not a feature, it is a support ticket.
func (s *Service) Open(ctx context.Context, p identity.Principal, creatorID string) (Conversation, error) {
	creatorID = strings.TrimSpace(creatorID)
	if creatorID == "" {
		return Conversation{}, fmt.Errorf("%w: creator_id is required", ErrInvalidInput)
	}
	if creatorID == p.UserID {
		return Conversation{}, ErrSelf
	}

	var exists bool
	if err := s.pool.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM creator.profiles WHERE user_id = $1::uuid)`, creatorID).Scan(&exists); err != nil {
		return Conversation{}, fmt.Errorf("messaging: creator lookup: %w", err)
	}
	if !exists {
		return Conversation{}, ErrNotFound
	}

	allowed, err := s.liveEntitlement(ctx, p.UserID, creatorID)
	if err != nil {
		return Conversation{}, err
	}
	if !allowed {
		return Conversation{}, ErrNotEntitled
	}

	var out Conversation
	err = s.pool.QueryRow(ctx, `
		INSERT INTO messaging.conversations (creator_id, fan_id)
		VALUES ($1::uuid, $2::uuid)
		ON CONFLICT (creator_id, fan_id) DO UPDATE SET creator_id = EXCLUDED.creator_id
		RETURNING id::text, creator_id::text, fan_id::text, created_at, last_message_at`,
		creatorID, p.UserID).
		Scan(&out.ID, &out.CreatorID, &out.FanID, &out.CreatedAt, &out.LastMessageAt)
	if err != nil {
		return Conversation{}, fmt.Errorf("messaging: open conversation: %w", err)
	}
	out.PeerName = s.peerName(ctx, p.UserID, out)
	out.CanReply = true
	return out, nil
}

// peerName resolves the other participant's label. A creator shows their display
// name; a fan is labelled by a short handle until fan profiles exist, because their
// email is not ours to publish.
func (s *Service) peerName(ctx context.Context, viewerID string, c Conversation) string {
	if viewerID == c.CreatorID {
		if len(c.FanID) >= 6 {
			return "Member " + strings.ToUpper(c.FanID[:6])
		}
		return "Member"
	}
	var name string
	if err := s.pool.QueryRow(ctx,
		`SELECT display_name FROM creator.profiles WHERE user_id = $1::uuid`, c.CreatorID).Scan(&name); err != nil {
		return ""
	}
	return name
}

// List returns the caller's threads, newest activity first.
func (s *Service) List(ctx context.Context, p identity.Principal) ([]Conversation, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT c.id::text, c.creator_id::text, c.fan_id::text, c.created_at, c.last_message_at,
		       COALESCE(p.display_name, ''),
		       EXISTS (
		           SELECT 1 FROM creator.entitlements e
		           WHERE e.fan_id = c.fan_id AND e.creator_id = c.creator_id
		             AND e.revoked_at IS NULL
		             AND (e.expires_at IS NULL OR e.expires_at > now())
		       ),
		       COALESCE(m.body, ''),
		       COALESCE(u.unread, 0)
		FROM messaging.conversations c
		LEFT JOIN creator.profiles p ON p.user_id = c.creator_id
		LEFT JOIN LATERAL (
		    SELECT body FROM messaging.messages
		    WHERE conversation_id = c.id
		    ORDER BY sent_at DESC, id DESC
		    LIMIT 1
		) m ON true
		LEFT JOIN LATERAL (
		    SELECT count(*)::int AS unread FROM messaging.messages
		    WHERE conversation_id = c.id AND sender_id <> $1::uuid AND read_at IS NULL
		) u ON true
		WHERE c.creator_id = $1::uuid OR c.fan_id = $1::uuid
		ORDER BY c.last_message_at DESC NULLS LAST, c.created_at DESC
		LIMIT 50`, p.UserID)
	if err != nil {
		return nil, fmt.Errorf("messaging: list conversations: %w", err)
	}
	defer rows.Close()

	out := make([]Conversation, 0, 8)
	for rows.Next() {
		var c Conversation
		var creatorName string
		var entitled bool
		if err := rows.Scan(&c.ID, &c.CreatorID, &c.FanID, &c.CreatedAt, &c.LastMessageAt,
			&creatorName, &entitled, &c.LastMessageBody, &c.UnreadCount); err != nil {
			return nil, fmt.Errorf("messaging: scan conversation: %w", err)
		}
		c.LastMessageBody = preview(c.LastMessageBody)
		if p.UserID == c.CreatorID {
			if len(c.FanID) >= 6 {
				c.PeerName = "Member " + strings.ToUpper(c.FanID[:6])
			} else {
				c.PeerName = "Member"
			}
			// A creator can always reply; the fan's reply depends on their own side.
			c.CanReply = true
		} else {
			c.PeerName = creatorName
			c.CanReply = entitled
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// Send appends a message. The participant check and the entitlement check both run
// here, not only in Open: a fan whose subscription expired an hour ago must not be
// able to keep writing because their client still holds a conversation id.
func (s *Service) Send(ctx context.Context, p identity.Principal, conversationID, raw string) (Message, error) {
	body, err := NormalizeBody(raw)
	if err != nil {
		return Message{}, err
	}

	var creatorID, fanID string
	err = s.pool.QueryRow(ctx, `
		SELECT creator_id::text, fan_id::text FROM messaging.conversations WHERE id = $1::uuid`,
		conversationID).Scan(&creatorID, &fanID)
	if errors.Is(err, pgx.ErrNoRows) {
		return Message{}, ErrNotFound
	}
	if err != nil {
		return Message{}, fmt.Errorf("messaging: load conversation: %w", err)
	}
	if p.UserID != creatorID && p.UserID != fanID {
		return Message{}, ErrForbidden
	}
	if p.UserID == fanID {
		allowed, err := s.liveEntitlement(ctx, fanID, creatorID)
		if err != nil {
			return Message{}, err
		}
		if !allowed {
			return Message{}, ErrNotEntitled
		}
	}

	var m Message
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Message{}, fmt.Errorf("messaging: begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if err := tx.QueryRow(ctx, `
		INSERT INTO messaging.messages (conversation_id, sender_id, body)
		VALUES ($1::uuid, $2::uuid, $3)
		RETURNING id::text, conversation_id::text, sender_id::text, body, sent_at, read_at`,
		conversationID, p.UserID, body).
		Scan(&m.ID, &m.ConversationID, &m.SenderID, &m.Body, &m.SentAt, &m.ReadAt); err != nil {
		return Message{}, fmt.Errorf("messaging: insert message: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		UPDATE messaging.conversations SET last_message_at = $2 WHERE id = $1::uuid`,
		conversationID, m.SentAt); err != nil {
		return Message{}, fmt.Errorf("messaging: touch conversation: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return Message{}, fmt.Errorf("messaging: commit: %w", err)
	}
	m.Mine = true
	return m, nil
}

// Thread returns the newest messages in a conversation, oldest first, and marks the
// other side's messages read. Reading is what the participant already did by opening
// the thread, so there is no separate receipt endpoint to forget to call.
func (s *Service) Thread(ctx context.Context, p identity.Principal, conversationID string) ([]Message, error) {
	var creatorID, fanID string
	err := s.pool.QueryRow(ctx, `
		SELECT creator_id::text, fan_id::text FROM messaging.conversations WHERE id = $1::uuid`,
		conversationID).Scan(&creatorID, &fanID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("messaging: load conversation: %w", err)
	}
	if p.UserID != creatorID && p.UserID != fanID {
		return nil, ErrForbidden
	}

	rows, err := s.pool.Query(ctx, `
		SELECT id::text, conversation_id::text, sender_id::text, body, sent_at, read_at
		FROM messaging.messages
		WHERE conversation_id = $1::uuid
		ORDER BY sent_at DESC, id DESC
		LIMIT $2`, conversationID, MaxThreadSize)
	if err != nil {
		return nil, fmt.Errorf("messaging: read thread: %w", err)
	}
	defer rows.Close()

	desc := make([]Message, 0, MaxThreadSize)
	for rows.Next() {
		var m Message
		if err := rows.Scan(&m.ID, &m.ConversationID, &m.SenderID, &m.Body, &m.SentAt, &m.ReadAt); err != nil {
			return nil, fmt.Errorf("messaging: scan message: %w", err)
		}
		m.Mine = m.SenderID == p.UserID
		desc = append(desc, m)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	rows.Close()

	// Oldest first is what a chat renders; the query is newest first so the limit
	// keeps the recent end of a long thread rather than the beginning.
	out := make([]Message, 0, len(desc))
	for i := len(desc) - 1; i >= 0; i-- {
		out = append(out, desc[i])
	}

	if _, err := s.pool.Exec(ctx, `
		UPDATE messaging.messages SET read_at = now()
		WHERE conversation_id = $1::uuid AND sender_id <> $2::uuid AND read_at IS NULL`,
		conversationID, p.UserID); err != nil {
		return nil, fmt.Errorf("messaging: mark read: %w", err)
	}
	return out, nil
}
