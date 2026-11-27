package messaging

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
)

type MessagePurchase struct {
	MessageID  string
	CreatorID  string
	FanID      string
	PriceMinor int64
	Currency   string
}

var ErrAlreadyUnlocked = errors.New("message already unlocked")

// MessageForPurchase reads the server-owned price for one locked message. The
// caller must be the fan participant and the message must still be locked.
func (s *Service) MessageForPurchase(ctx context.Context, messageID, fanID string) (MessagePurchase, error) {
	var p MessagePurchase
	var price *int64
	var currency *string
	err := s.pool.QueryRow(ctx, `
		SELECT m.id::text, c.creator_id::text, c.fan_id::text, m.price_minor, m.currency::text
		FROM messaging.messages m
		JOIN messaging.conversations c ON c.id = m.conversation_id
		WHERE m.id=$1::uuid AND c.fan_id=$2::uuid AND m.unlock_required AND c.status='accepted'`,
		messageID, fanID).Scan(&p.MessageID, &p.CreatorID, &p.FanID, &price, &currency)
	if errors.Is(err, pgx.ErrNoRows) {
		return MessagePurchase{}, ErrNotFound
	}
	if err != nil {
		return MessagePurchase{}, err
	}
	if price == nil || currency == nil {
		return MessagePurchase{}, ErrNotFound
	}
	var live bool
	_ = s.pool.QueryRow(ctx, `
		SELECT EXISTS (SELECT 1 FROM messaging.message_unlocks
		WHERE message_id=$1::uuid AND fan_id=$2::uuid AND revoked_at IS NULL)`,
		messageID, fanID).Scan(&live)
	if live {
		return MessagePurchase{}, ErrAlreadyUnlocked
	}
	p.PriceMinor, p.Currency = *price, *currency
	return p, nil
}

// UnlockMessagePaid records the unlock after the simulated payment settles. The
// partial unique index prevents two live unlocks; the intent unique index
// prevents one payment from unlocking twice.
func (s *Service) UnlockMessagePaid(ctx context.Context, fanID, messageID, intentID string) error {
	tag, err := s.pool.Exec(ctx, `
		INSERT INTO messaging.message_unlocks (message_id, fan_id, payment_intent_id)
		VALUES ($1::uuid, $2::uuid, $3::uuid)
		ON CONFLICT (message_id, fan_id) WHERE revoked_at IS NULL DO NOTHING`,
		messageID, fanID, intentID)
	if err != nil {
		return fmt.Errorf("messaging: unlock message: %w", err)
	}
	if tag.RowsAffected() == 0 {
		var sameIntent bool
		_ = s.pool.QueryRow(ctx, `
			SELECT EXISTS (SELECT 1 FROM messaging.message_unlocks
			WHERE message_id=$1::uuid AND fan_id=$2::uuid AND payment_intent_id=$3::uuid)`,
			messageID, fanID, intentID).Scan(&sameIntent)
		if sameIntent {
			return nil
		}
		return ErrAlreadyUnlocked
	}
	return nil
}

// RevokeMessagePaid is called only after a refund is recorded and its ledger
// posting succeeds. Revocation history is retained.
func (s *Service) RevokeMessagePaid(ctx context.Context, intentID string) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE messaging.message_unlocks SET revoked_at = now()
		WHERE payment_intent_id=$1::uuid AND revoked_at IS NULL`, intentID)
	if err != nil {
		return fmt.Errorf("messaging: revoke message unlock: %w", err)
	}
	return nil
}
