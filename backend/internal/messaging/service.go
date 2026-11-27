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

// Open returns an existing conversation or creates an accepted one for an
// entitled fan. A request conversation is returned as-is; a rejected/blocked one
// is refused.
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

	if c, err, found := s.existingConversation(ctx, p, creatorID); err != nil {
		return Conversation{}, err
	} else if found {
		return c, nil
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
		INSERT INTO messaging.conversations (creator_id, fan_id, status, initiated_by)
		VALUES ($1::uuid, $2::uuid, 'accepted', $2::uuid)
		ON CONFLICT (creator_id, fan_id) DO UPDATE SET creator_id = EXCLUDED.creator_id
		RETURNING id::text, creator_id::text, fan_id::text, created_at, last_message_at,
		          status, COALESCE(initiated_by::text, ''), requested_at, responded_at`,
		creatorID, p.UserID).
		Scan(&out.ID, &out.CreatorID, &out.FanID, &out.CreatedAt, &out.LastMessageAt,
			&out.Status, &out.InitiatedBy, &out.RequestedAt, &out.RespondedAt)
	if err != nil {
		return Conversation{}, fmt.Errorf("messaging: open conversation: %w", err)
	}
	out.PeerName = s.peerName(ctx, p.UserID, out)
	out.CanReply = true
	return out, nil
}

func (s *Service) existingConversation(ctx context.Context, p identity.Principal, creatorID string) (Conversation, error, bool) {
	var c Conversation
	err := s.pool.QueryRow(ctx, `
		SELECT id::text, creator_id::text, fan_id::text, created_at, last_message_at,
		       status, COALESCE(initiated_by::text, ''), requested_at, responded_at
		FROM messaging.conversations WHERE creator_id = $1::uuid AND fan_id = $2::uuid`,
		creatorID, p.UserID).
		Scan(&c.ID, &c.CreatorID, &c.FanID, &c.CreatedAt, &c.LastMessageAt,
			&c.Status, &c.InitiatedBy, &c.RequestedAt, &c.RespondedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Conversation{}, nil, false
	}
	if err != nil {
		return Conversation{}, fmt.Errorf("messaging: existing conversation: %w", err), false
	}
	if c.Status == "rejected" || c.Status == "blocked" {
		return Conversation{}, ErrForbidden, false
	}
	c.PeerName = s.peerName(ctx, p.UserID, c)
	c.CanReply = c.Status == "accepted"
	c.RequestPending = c.Status == "requested"
	return c, nil, true
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
		       COALESCE(u.unread, 0),
		       c.status, COALESCE(c.initiated_by::text, ''), c.requested_at, c.responded_at,
		       COALESCE(m.unlock_required, false),
		       EXISTS (
		           SELECT 1 FROM messaging.message_unlocks u2
		           WHERE u2.message_id = m.id AND u2.fan_id = $1::uuid AND u2.revoked_at IS NULL
		       )
		FROM messaging.conversations c
		LEFT JOIN creator.profiles p ON p.user_id = c.creator_id
		LEFT JOIN LATERAL (
		    SELECT id, body, unlock_required FROM messaging.messages
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
		var lastLocked, lastUnlocked bool
		if err := rows.Scan(&c.ID, &c.CreatorID, &c.FanID, &c.CreatedAt, &c.LastMessageAt,
			&creatorName, &entitled, &c.LastMessageBody, &c.UnreadCount,
			&c.Status, &c.InitiatedBy, &c.RequestedAt, &c.RespondedAt, &lastLocked, &lastUnlocked); err != nil {
			return nil, fmt.Errorf("messaging: scan conversation: %w", err)
		}
		if lastLocked && !lastUnlocked && p.UserID != c.CreatorID {
			c.LastMessageBody = "Locked paid message"
		} else {
			c.LastMessageBody = preview(c.LastMessageBody)
		}
		c.RequestPending = c.Status == "requested"
		if p.UserID == c.CreatorID {
			if len(c.FanID) >= 6 {
				c.PeerName = "Member " + strings.ToUpper(c.FanID[:6])
			} else {
				c.PeerName = "Member"
			}
			c.CanReply = c.Status == "accepted"
		} else {
			c.PeerName = creatorName
			c.CanReply = c.Status == "accepted" && (entitled || c.RequestedAt != nil)
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// Send appends a message. The participant check and the entitlement check both run
// here, not only in Open: a fan whose subscription expired an hour ago must not be
// able to keep writing because their client still holds a conversation id.
func (s *Service) Send(ctx context.Context, p identity.Principal, conversationID, raw string) (Message, error) {
	return s.SendWithMedia(ctx, p, conversationID, raw, "")
}

// SendWithMedia appends a text message with an optional image attachment. The
// media object must belong to the sender and be ready.
func (s *Service) SendWithMedia(ctx context.Context, p identity.Principal, conversationID, raw, mediaID string) (Message, error) {
	return s.send(ctx, p, conversationID, raw, mediaID, nil)
}

// SendPaid lets a creator attach a price. The fan unlock is a separate payment.
func (s *Service) SendPaid(ctx context.Context, p identity.Principal, conversationID, raw, mediaID string, priceMinor *int64) (Message, error) {
	return s.send(ctx, p, conversationID, raw, mediaID, priceMinor)
}

func (s *Service) send(ctx context.Context, p identity.Principal, conversationID, raw, mediaID string, priceMinor *int64) (Message, error) {
	body := ""
	var err error
	if strings.TrimSpace(raw) != "" || mediaID == "" {
		body, err = NormalizeBody(raw)
		if err != nil {
			return Message{}, err
		}
	}
	if strings.TrimSpace(raw) == "" && mediaID == "" {
		return Message{}, errors.New("message body is empty")
	}
	if mediaID != "" {
		var ok bool
		if err := s.pool.QueryRow(ctx, `
			SELECT EXISTS (SELECT 1 FROM media.objects
			WHERE id = $1::uuid AND owner_id = $2::uuid AND kind = 'image' AND status = 'ready')`,
			mediaID, p.UserID).Scan(&ok); err != nil {
			return Message{}, fmt.Errorf("messaging: attachment lookup: %w", err)
		}
		if !ok {
			return Message{}, fmt.Errorf("%w: attachment is not an image you own", ErrForbidden)
		}
	}

	var creatorID, fanID, status string
	var requestedAt *time.Time
	err = s.pool.QueryRow(ctx, `
		SELECT creator_id::text, fan_id::text, status, requested_at
		FROM messaging.conversations WHERE id = $1::uuid`,
		conversationID).Scan(&creatorID, &fanID, &status, &requestedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Message{}, ErrNotFound
	}
	if err != nil {
		return Message{}, fmt.Errorf("messaging: load conversation: %w", err)
	}
	if p.UserID != creatorID && p.UserID != fanID {
		return Message{}, ErrForbidden
	}
	if status == "rejected" || status == "blocked" {
		return Message{}, ErrForbidden
	}
	if p.UserID == creatorID {
		if status != "accepted" {
			return Message{}, ErrRequestPending
		}
	} else if status == "requested" {
		// The initial request is written by Request, not this endpoint.
		return Message{}, ErrRequestPending
	}
	if p.UserID == fanID && status == "accepted" && requestedAt == nil {
		allowed, err := s.liveEntitlement(ctx, fanID, creatorID)
		if err != nil {
			return Message{}, err
		}
		if !allowed {
			return Message{}, ErrNotEntitled
		}
	}
	if priceMinor != nil {
		if p.UserID != creatorID || status != "accepted" {
			return Message{}, ErrForbidden
		}
		if *priceMinor < 100 || *priceMinor > 100000 {
			return Message{}, fmt.Errorf("%w: price must be between 100 and 100000 minor units", ErrInvalidInput)
		}
	}
	var currency *string
	if priceMinor != nil {
		c := "USD"
		currency = &c
	}

	var m Message
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Message{}, fmt.Errorf("messaging: begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if err := tx.QueryRow(ctx, `
		INSERT INTO messaging.messages
			(conversation_id, sender_id, body, media_id, price_minor, currency, unlock_required)
		VALUES ($1::uuid, $2::uuid, $3, NULLIF($4, '')::uuid, $5::bigint, $6::char(3), $5::bigint IS NOT NULL)
		RETURNING id::text, conversation_id::text, sender_id::text, body,
		          COALESCE(media_id::text, ''), sent_at, read_at, unlock_required,
		          price_minor, COALESCE(currency::text, '')`,
		conversationID, p.UserID, body, mediaID, priceMinor, currency).
		Scan(&m.ID, &m.ConversationID, &m.SenderID, &m.Body, &m.MediaID, &m.SentAt, &m.ReadAt,
			&m.Locked, &m.PriceMinor, &m.Currency); err != nil {
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
	m.Locked = false
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
		SELECT m.id::text, m.conversation_id::text, m.sender_id::text, m.body,
		       COALESCE(m.media_id::text, ''), m.sent_at, m.read_at, m.unlock_required,
		       m.price_minor, COALESCE(m.currency::text, ''),
		       EXISTS (
		           SELECT 1 FROM messaging.message_unlocks u
		           WHERE u.message_id = m.id AND u.fan_id = $2::uuid AND u.revoked_at IS NULL
		       ),
		       EXISTS (
		           SELECT 1 FROM messaging.request_attachments ra
		           WHERE ra.message_id = m.id
		       )
		FROM messaging.messages m
		JOIN messaging.conversations c ON c.id = m.conversation_id
		WHERE m.conversation_id = $1::uuid
		ORDER BY m.sent_at DESC, m.id DESC
		LIMIT $3`, conversationID, p.UserID, MaxThreadSize)
	if err != nil {
		return nil, fmt.Errorf("messaging: read thread: %w", err)
	}
	defer rows.Close()

	desc := make([]Message, 0, MaxThreadSize)
	for rows.Next() {
		var m Message
		var unlockRequired, unlocked, requestAttachment bool
		if err := rows.Scan(&m.ID, &m.ConversationID, &m.SenderID, &m.Body, &m.MediaID,
			&m.SentAt, &m.ReadAt, &unlockRequired, &m.PriceMinor, &m.Currency,
			&unlocked, &requestAttachment); err != nil {
			return nil, fmt.Errorf("messaging: scan message: %w", err)
		}
		var convStatus string
		_ = s.pool.QueryRow(ctx, `SELECT status FROM messaging.conversations WHERE id=$1::uuid`, conversationID).Scan(&convStatus)
		if requestAttachment && convStatus != "accepted" {
			m.MediaID = ""
		}
		if unlockRequired && p.UserID != creatorID && !unlocked {
			m.Locked = true
			m.Body = ""
			m.MediaID = ""
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

// MessageMedia returns the sender-owned object attached to a message after the
// caller proves they are a participant.
func (s *Service) MessageMedia(ctx context.Context, p identity.Principal, messageID string) (string, error) {
	var mediaID, creatorID, fanID, convStatus string
	var unlockRequired bool
	err := s.pool.QueryRow(ctx, `
		SELECT COALESCE(m.media_id::text, ''), c.creator_id::text, c.fan_id::text,
		       c.status, m.unlock_required
		FROM messaging.messages m JOIN messaging.conversations c ON c.id = m.conversation_id
		WHERE m.id = $1::uuid`, messageID).Scan(&mediaID, &creatorID, &fanID, &convStatus, &unlockRequired)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", fmt.Errorf("messaging: load attachment: %w", err)
	}
	if p.UserID != creatorID && p.UserID != fanID {
		return "", ErrForbidden
	}
	if convStatus != "accepted" {
		return "", ErrForbidden
	}
	if unlockRequired && p.UserID != creatorID {
		var unlocked bool
		_ = s.pool.QueryRow(ctx, `
			SELECT EXISTS (SELECT 1 FROM messaging.message_unlocks
			WHERE message_id=$1::uuid AND fan_id=$2::uuid AND revoked_at IS NULL)`,
			messageID, p.UserID).Scan(&unlocked)
		if !unlocked {
			return "", ErrForbidden
		}
	}
	if mediaID == "" {
		return "", ErrNotFound
	}
	return mediaID, nil
}

// Broadcast sends one creator message to every fan with a live entitlement and
// returns the number of recipients. It is bounded to 1000 recipients per call.
func (s *Service) Broadcast(ctx context.Context, p identity.Principal, raw string) (int, error) {
	body, err := NormalizeBody(raw)
	if err != nil {
		return 0, err
	}
	var isCreator bool
	if err := s.pool.QueryRow(ctx, `
		SELECT EXISTS (SELECT 1 FROM creator.profiles WHERE user_id = $1::uuid)`, p.UserID).Scan(&isCreator); err != nil {
		return 0, err
	}
	if !isCreator {
		return 0, ErrForbidden
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	rows, err := tx.Query(ctx, `
		SELECT e.fan_id::text FROM creator.entitlements e
		WHERE e.creator_id = $1::uuid AND e.revoked_at IS NULL
		  AND (e.expires_at IS NULL OR e.expires_at > now())
		GROUP BY e.fan_id LIMIT 1000`, p.UserID)
	if err != nil {
		return 0, fmt.Errorf("messaging: broadcast recipients: %w", err)
	}
	var fans []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return 0, err
		}
		fans = append(fans, id)
	}
	rows.Close()
	if len(fans) == 0 {
		return 0, errors.New("no entitled recipients")
	}
	broadcastID := ""
	if err := tx.QueryRow(ctx, `SELECT gen_random_uuid()::text`).Scan(&broadcastID); err != nil {
		return 0, err
	}
	sent := 0
	for _, fanID := range fans {
		var convID string
		if err := tx.QueryRow(ctx, `
			INSERT INTO messaging.conversations (creator_id, fan_id)
			VALUES ($1::uuid, $2::uuid)
			ON CONFLICT (creator_id, fan_id) DO UPDATE SET creator_id = EXCLUDED.creator_id
			RETURNING id::text`, p.UserID, fanID).Scan(&convID); err != nil {
			return 0, err
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO messaging.messages (conversation_id, sender_id, body, broadcast_id)
			VALUES ($1::uuid, $2::uuid, $3, $4::uuid)`, convID, p.UserID, body, broadcastID); err != nil {
			return 0, err
		}
		if _, err := tx.Exec(ctx, `
			UPDATE messaging.conversations SET last_message_at = now() WHERE id = $1::uuid`, convID); err != nil {
			return 0, err
		}
		sent++
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, err
	}
	return sent, nil
}

// MessageByID returns one message with the same redaction rules as Thread.
func (s *Service) MessageByID(ctx context.Context, p identity.Principal, messageID string) (Message, error) {
	var m Message
	var creatorID, fanID, convStatus string
	var unlockRequired, requestAttachment, unlocked bool
	err := s.pool.QueryRow(ctx, `
		SELECT m.id::text, m.conversation_id::text, m.sender_id::text, m.body,
		       COALESCE(m.media_id::text, ''), m.sent_at, m.read_at, m.unlock_required,
		       m.price_minor, COALESCE(m.currency::text, ''),
		       c.creator_id::text, c.fan_id::text, c.status,
		       EXISTS (SELECT 1 FROM messaging.message_unlocks u
		               WHERE u.message_id=m.id AND u.fan_id=$2::uuid AND u.revoked_at IS NULL),
		       EXISTS (SELECT 1 FROM messaging.request_attachments ra WHERE ra.message_id=m.id)
		FROM messaging.messages m JOIN messaging.conversations c ON c.id=m.conversation_id
		WHERE m.id=$1::uuid`, messageID, p.UserID).
		Scan(&m.ID, &m.ConversationID, &m.SenderID, &m.Body, &m.MediaID, &m.SentAt, &m.ReadAt,
			&unlockRequired, &m.PriceMinor, &m.Currency, &creatorID, &fanID, &convStatus,
			&unlocked, &requestAttachment)
	if errors.Is(err, pgx.ErrNoRows) {
		return Message{}, ErrNotFound
	}
	if err != nil {
		return Message{}, err
	}
	if p.UserID != creatorID && p.UserID != fanID {
		return Message{}, ErrForbidden
	}
	if requestAttachment && convStatus != "accepted" {
		m.MediaID = ""
	}
	if unlockRequired && p.UserID != creatorID && !unlocked {
		m.Locked = true
		m.Body = ""
		m.MediaID = ""
	}
	m.Mine = m.SenderID == p.UserID
	return m, nil
}
