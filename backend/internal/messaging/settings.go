package messaging

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jawuu187-ops/content-platform/backend/internal/identity"
)

type Settings struct {
	RequestPolicy     string `json:"dm_request_policy"`
	PaidMessages      bool   `json:"paid_messages_enabled"`
	DefaultPriceMinor *int64 `json:"default_message_price_minor,omitempty"`
	Currency          string `json:"message_currency"`
}

func (s *Service) Settings(ctx context.Context, p identity.Principal) (Settings, error) {
	var st Settings
	err := s.pool.QueryRow(ctx, `
		SELECT dm_request_policy, paid_messages_enabled, default_message_price_minor, message_currency::text
		FROM creator.profiles WHERE user_id=$1::uuid`, p.UserID).
		Scan(&st.RequestPolicy, &st.PaidMessages, &st.DefaultPriceMinor, &st.Currency)
	if errors.Is(err, pgx.ErrNoRows) {
		return Settings{}, ErrNotFound
	}
	return st, err
}

func (s *Service) UpdateSettings(ctx context.Context, p identity.Principal, in Settings, requestID string) (Settings, error) {
	policy := strings.TrimSpace(in.RequestPolicy)
	switch policy {
	case "subscribers", "everyone", "none":
	default:
		return Settings{}, fmt.Errorf("%w: request policy must be subscribers, everyone or none", ErrInvalidInput)
	}
	currency := strings.ToUpper(strings.TrimSpace(in.Currency))
	if currency == "" {
		currency = "USD"
	}
	if len(currency) != 3 {
		return Settings{}, fmt.Errorf("%w: currency must be three letters", ErrInvalidInput)
	}
	if in.DefaultPriceMinor != nil && (*in.DefaultPriceMinor < 100 || *in.DefaultPriceMinor > 100000) {
		return Settings{}, fmt.Errorf("%w: default price must be 100-100000 minor units", ErrInvalidInput)
	}
	var out Settings
	err := s.pool.QueryRow(ctx, `
		UPDATE creator.profiles
		SET dm_request_policy=$2, paid_messages_enabled=$3,
		    default_message_price_minor=$4, message_currency=$5, updated_at=now()
		WHERE user_id=$1::uuid
		RETURNING dm_request_policy, paid_messages_enabled, default_message_price_minor, message_currency::text`,
		p.UserID, policy, in.PaidMessages, in.DefaultPriceMinor, currency).
		Scan(&out.RequestPolicy, &out.PaidMessages, &out.DefaultPriceMinor, &out.Currency)
	if errors.Is(err, pgx.ErrNoRows) {
		return Settings{}, ErrNotFound
	}
	if err != nil {
		return Settings{}, err
	}
	_, _ = s.pool.Exec(ctx, `
		SELECT ops.append_audit('user', $1::text, 'messaging.settings_updated', 'user', $1::text,
		NULL, jsonb_build_object('policy', $2::text), NULLIF($3::text, ''))`,
		p.UserID, policy, requestID)
	return out, nil
}
