package messaging

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/jawuu187-ops/content-platform/backend/internal/identity"
)

// Request opens a pending conversation for a non-entitled fan when the creator
// allows everyone. It writes exactly one message; attachments stay hidden until
// the creator accepts.
func (s *Service) Request(ctx context.Context, p identity.Principal, creatorID, raw, mediaID string) (Conversation, Message, error) {
	creatorID = strings.TrimSpace(creatorID)
	if creatorID == "" || creatorID == p.UserID {
		return Conversation{}, Message{}, ErrInvalidInput
	}
	body, err := NormalizeBody(raw)
	if err != nil {
		return Conversation{}, Message{}, err
	}
	var policy string
	if err := s.pool.QueryRow(ctx, `
		SELECT dm_request_policy FROM creator.profiles WHERE user_id = $1::uuid`, creatorID).Scan(&policy); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Conversation{}, Message{}, ErrNotFound
		}
		return Conversation{}, Message{}, err
	}
	if policy != "everyone" {
		return Conversation{}, Message{}, ErrRequestPolicy
	}
	var blocked bool
	if err := s.pool.QueryRow(ctx, `
		SELECT EXISTS (SELECT 1 FROM messaging.blocks
		WHERE (blocker_id = $1::uuid AND blocked_id = $2::uuid)
		   OR (blocker_id = $2::uuid AND blocked_id = $1::uuid))`, creatorID, p.UserID).Scan(&blocked); err != nil {
		return Conversation{}, Message{}, err
	}
	if blocked {
		return Conversation{}, Message{}, ErrForbidden
	}
	if mediaID != "" {
		var ok bool
		if err := s.pool.QueryRow(ctx, `
			SELECT EXISTS (SELECT 1 FROM media.objects
			WHERE id=$1::uuid AND owner_id=$2::uuid AND status='ready')`, mediaID, p.UserID).Scan(&ok); err != nil || !ok {
			return Conversation{}, Message{}, ErrForbidden
		}
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Conversation{}, Message{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var conv Conversation
	var existingMessage bool
	err = tx.QueryRow(ctx, `
		SELECT id::text, creator_id::text, fan_id::text, created_at, last_message_at,
		       status, COALESCE(initiated_by::text, ''), requested_at, responded_at,
		       EXISTS (SELECT 1 FROM messaging.messages WHERE conversation_id = messaging.conversations.id)
		FROM messaging.conversations
		WHERE creator_id=$1::uuid AND fan_id=$2::uuid FOR UPDATE`, creatorID, p.UserID).
		Scan(&conv.ID, &conv.CreatorID, &conv.FanID, &conv.CreatedAt, &conv.LastMessageAt,
			&conv.Status, &conv.InitiatedBy, &conv.RequestedAt, &conv.RespondedAt, &existingMessage)
	if errors.Is(err, pgx.ErrNoRows) {
		err = tx.QueryRow(ctx, `
			INSERT INTO messaging.conversations
				(creator_id, fan_id, status, initiated_by, requested_at)
			VALUES ($1::uuid, $2::uuid, 'requested', $2::uuid, now())
			RETURNING id::text, creator_id::text, fan_id::text, created_at, last_message_at,
			          status, COALESCE(initiated_by::text, ''), requested_at, responded_at`,
			creatorID, p.UserID).
			Scan(&conv.ID, &conv.CreatorID, &conv.FanID, &conv.CreatedAt, &conv.LastMessageAt,
				&conv.Status, &conv.InitiatedBy, &conv.RequestedAt, &conv.RespondedAt)
	} else if err == nil {
		if conv.Status == "requested" {
			return Conversation{}, Message{}, ErrRequestPending
		}
		if conv.Status != "accepted" {
			return Conversation{}, Message{}, ErrForbidden
		}
		return Conversation{}, Message{}, fmt.Errorf("%w: conversation already accepted; send normally", ErrInvalidInput)
	}
	if err != nil {
		return Conversation{}, Message{}, fmt.Errorf("messaging: request conversation: %w", err)
	}

	var m Message
	err = tx.QueryRow(ctx, `
		INSERT INTO messaging.messages (conversation_id, sender_id, body, media_id)
		VALUES ($1::uuid, $2::uuid, $3, NULLIF($4, '')::uuid)
		RETURNING id::text, conversation_id::text, sender_id::text, body,
		          COALESCE(media_id::text, ''), sent_at, read_at`,
		conv.ID, p.UserID, body, mediaID).
		Scan(&m.ID, &m.ConversationID, &m.SenderID, &m.Body, &m.MediaID, &m.SentAt, &m.ReadAt)
	if err != nil {
		return Conversation{}, Message{}, err
	}
	if mediaID != "" {
		if _, err := tx.Exec(ctx, `
			INSERT INTO messaging.request_attachments (message_id, media_id)
			VALUES ($1::uuid, $2::uuid)`, m.ID, mediaID); err != nil {
			return Conversation{}, Message{}, err
		}
	}
	if _, err := tx.Exec(ctx, `
		UPDATE messaging.conversations SET last_message_at = $2 WHERE id = $1::uuid`,
		conv.ID, m.SentAt); err != nil {
		return Conversation{}, Message{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Conversation{}, Message{}, err
	}
	m.Mine = true
	m.MediaID = ""
	conv.Status, conv.RequestPending, conv.CanReply = "requested", true, true
	conv.PeerName = s.peerName(ctx, p.UserID, conv)
	return conv, m, nil
}

func (s *Service) decideRequest(ctx context.Context, p identity.Principal, conversationID, status string, block bool) (Conversation, error) {
	if status != "accepted" && status != "rejected" && status != "blocked" {
		return Conversation{}, ErrInvalidInput
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Conversation{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var c Conversation
	err = tx.QueryRow(ctx, `
		SELECT id::text, creator_id::text, fan_id::text, created_at, last_message_at,
		       status, COALESCE(initiated_by::text, ''), requested_at, responded_at
		FROM messaging.conversations WHERE id=$1::uuid FOR UPDATE`, conversationID).
		Scan(&c.ID, &c.CreatorID, &c.FanID, &c.CreatedAt, &c.LastMessageAt,
			&c.Status, &c.InitiatedBy, &c.RequestedAt, &c.RespondedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Conversation{}, ErrNotFound
	}
	if err != nil {
		return Conversation{}, err
	}
	if c.CreatorID != p.UserID {
		return Conversation{}, ErrForbidden
	}
	if c.Status != "requested" {
		return Conversation{}, ErrAlreadyAccepted
	}
	_, err = tx.Exec(ctx, `
		UPDATE messaging.conversations SET status=$2, responded_at=now()
		WHERE id=$1::uuid`, conversationID, status)
	if err != nil {
		return Conversation{}, err
	}
	if status == "rejected" || status == "blocked" {
		if _, err := tx.Exec(ctx, `
			UPDATE messaging.request_attachments SET purge_after = now() + interval '30 days'
			WHERE message_id IN (SELECT id FROM messaging.messages WHERE conversation_id=$1::uuid)`,
			conversationID); err != nil {
			return Conversation{}, err
		}
	}
	if block {
		if _, err := tx.Exec(ctx, `
			INSERT INTO messaging.blocks (blocker_id, blocked_id) VALUES ($1::uuid,$2::uuid)
			ON CONFLICT DO NOTHING`, p.UserID, c.FanID); err != nil {
			return Conversation{}, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return Conversation{}, err
	}
	c.Status, c.RequestPending = status, false
	c.CanReply = status == "accepted"
	return c, nil
}

func (s *Service) AcceptRequest(ctx context.Context, p identity.Principal, conversationID string) (Conversation, error) {
	return s.decideRequest(ctx, p, conversationID, "accepted", false)
}

func (s *Service) RejectRequest(ctx context.Context, p identity.Principal, conversationID string) (Conversation, error) {
	return s.decideRequest(ctx, p, conversationID, "rejected", false)
}

func (s *Service) BlockRequest(ctx context.Context, p identity.Principal, conversationID string) (Conversation, error) {
	return s.decideRequest(ctx, p, conversationID, "blocked", true)
}

// PurgeRejectedAttachments deletes media for rejected/blocked requests after the
// 30-day safety window. The message remains as evidence; only bytes and media
// metadata are removed.
func (s *Service) PurgeRejectedAttachments(ctx context.Context, deleteMedia func(context.Context, string) error) (int, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT ra.message_id::text, ra.media_id::text
		FROM messaging.request_attachments ra
		JOIN messaging.messages m ON m.id = ra.message_id
		JOIN messaging.conversations c ON c.id = m.conversation_id
		WHERE ra.purge_after IS NOT NULL AND ra.purge_after <= now()
		  AND c.status IN ('rejected','blocked') LIMIT 50`)
	if err != nil {
		return 0, err
	}
	type item struct{ messageID, mediaID string }
	var items []item
	for rows.Next() {
		var it item
		if err := rows.Scan(&it.messageID, &it.mediaID); err != nil {
			rows.Close()
			return 0, err
		}
		items = append(items, it)
	}
	rows.Close()
	done := 0
	for _, it := range items {
		if err := deleteMedia(ctx, it.mediaID); err != nil {
			continue
		}
		if _, err := s.pool.Exec(ctx, `UPDATE messaging.messages SET media_id=NULL WHERE id=$1::uuid`, it.messageID); err != nil {
			continue
		}
		if _, err := s.pool.Exec(ctx, `DELETE FROM messaging.request_attachments WHERE message_id=$1::uuid`, it.messageID); err == nil {
			done++
		}
	}
	return done, nil
}
