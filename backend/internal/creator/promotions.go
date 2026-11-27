package creator

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/jawuu187-ops/content-platform/backend/internal/identity"
)

type Promotion struct {
	ID             string     `json:"id"`
	CreatorID      string     `json:"creator_id"`
	Code           string     `json:"code"`
	PercentOff     int        `json:"percent_off"`
	MaxRedemptions int        `json:"max_redemptions"`
	RedeemedCount  int        `json:"redeemed_count"`
	StartsAt       time.Time  `json:"starts_at"`
	ExpiresAt      *time.Time `json:"expires_at,omitempty"`
	Active         bool       `json:"active"`
}

type PromotionInput struct {
	Code           string     `json:"code"`
	PercentOff     int        `json:"percent_off"`
	MaxRedemptions int        `json:"max_redemptions"`
	ExpiresAt      *time.Time `json:"expires_at"`
	Active         *bool      `json:"active"`
}

func (s *Service) CreatePromotion(ctx context.Context, p identity.Principal, in PromotionInput) (Promotion, error) {
	code := strings.ToUpper(strings.TrimSpace(in.Code))
	if len(code) < 3 || len(code) > 24 {
		return Promotion{}, fmt.Errorf("%w: code must be 3-24 characters", ErrInvalidInput)
	}
	if in.PercentOff < 1 || in.PercentOff > 100 {
		return Promotion{}, fmt.Errorf("%w: percent off must be between 1 and 100", ErrInvalidInput)
	}
	if in.MaxRedemptions <= 0 {
		in.MaxRedemptions = 100
	}
	if in.ExpiresAt != nil && !in.ExpiresAt.After(s.now().UTC()) {
		return Promotion{}, fmt.Errorf("%w: expiry must be in the future", ErrInvalidInput)
	}
	var promo Promotion
	var expires *time.Time
	err := s.pool.QueryRow(ctx, `
		INSERT INTO creator.promotions (creator_id, code, percent_off, max_redemptions, expires_at)
		VALUES ($1::uuid, $2, $3, $4, $5)
		RETURNING id::text, creator_id::text, code::text, percent_off, max_redemptions,
		          redeemed_count, starts_at, expires_at, is_active`,
		p.UserID, code, in.PercentOff, in.MaxRedemptions, in.ExpiresAt,
	).Scan(&promo.ID, &promo.CreatorID, &promo.Code, &promo.PercentOff, &promo.MaxRedemptions,
		&promo.RedeemedCount, &promo.StartsAt, &expires, &promo.Active)
	if err != nil {
		return Promotion{}, fmt.Errorf("creator: create promotion: %w", err)
	}
	promo.ExpiresAt = expires
	return promo, nil
}

func (s *Service) Promotions(ctx context.Context, p identity.Principal) ([]Promotion, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id::text, creator_id::text, code::text, percent_off, max_redemptions,
		       redeemed_count, starts_at, expires_at, is_active
		FROM creator.promotions WHERE creator_id = $1::uuid
		ORDER BY created_at DESC LIMIT 100`, p.UserID)
	if err != nil {
		return nil, fmt.Errorf("creator: list promotions: %w", err)
	}
	defer rows.Close()
	out := []Promotion{}
	for rows.Next() {
		var p Promotion
		var expires *time.Time
		if err := rows.Scan(&p.ID, &p.CreatorID, &p.Code, &p.PercentOff, &p.MaxRedemptions,
			&p.RedeemedCount, &p.StartsAt, &expires, &p.Active); err != nil {
			return nil, err
		}
		p.ExpiresAt = expires
		out = append(out, p)
	}
	return out, rows.Err()
}

func (s *Service) SetPromotionActive(ctx context.Context, p identity.Principal, id string, active bool) (Promotion, error) {
	var promo Promotion
	var expires *time.Time
	err := s.pool.QueryRow(ctx, `
		UPDATE creator.promotions SET is_active = $3
		WHERE id = $1::uuid AND creator_id = $2::uuid
		RETURNING id::text, creator_id::text, code::text, percent_off, max_redemptions,
		          redeemed_count, starts_at, expires_at, is_active`, id, p.UserID, active).
		Scan(&promo.ID, &promo.CreatorID, &promo.Code, &promo.PercentOff, &promo.MaxRedemptions,
			&promo.RedeemedCount, &promo.StartsAt, &expires, &promo.Active)
	if errors.Is(err, pgx.ErrNoRows) {
		return Promotion{}, ErrNotFound
	}
	if err != nil {
		return Promotion{}, fmt.Errorf("creator: update promotion: %w", err)
	}
	promo.ExpiresAt = expires
	return promo, nil
}

// SubscribeWithOffer applies a trial first, then a promotion code. It never
// charges a card: simulated payment activation is the only activation path.
func (s *Service) SubscribeWithOffer(ctx context.Context, p identity.Principal, handle, tierID, promoCode, requestID string) (Subscription, error) {
	profile, err := s.profileByHandle(ctx, NormalizeHandle(handle))
	if err != nil {
		return Subscription{}, err
	}
	if !profile.Published {
		return Subscription{}, ErrNotFound
	}
	if profile.UserID == p.UserID {
		return Subscription{}, ErrSelfSubscription
	}
	tier, err := s.tierByID(ctx, tierID)
	if err != nil {
		return Subscription{}, err
	}
	if tier.CreatorID != profile.UserID {
		return Subscription{}, ErrNotFound
	}
	if !tier.Active {
		return Subscription{}, ErrTierInactive
	}

	var prior int
	_ = s.pool.QueryRow(ctx, `
		SELECT count(*) FROM creator.subscriptions WHERE fan_id = $1::uuid AND creator_id = $2::uuid`,
		p.UserID, profile.UserID).Scan(&prior)
	trial := prior == 0 && tier.TrialDays > 0
	price, discount, code := tier.PriceMinor, int64(0), ""
	periodDays := tier.PeriodDays
	var promotionID string
	if trial {
		price, discount = 0, tier.PriceMinor
		periodDays = tier.TrialDays
	} else if strings.TrimSpace(promoCode) != "" {
		var percent int
		var max, redeemed int
		var expires *time.Time
		var active bool
		err := s.pool.QueryRow(ctx, `
			SELECT id::text, percent_off, max_redemptions, redeemed_count, expires_at, is_active
			FROM creator.promotions
			WHERE creator_id = $1::uuid AND lower(code) = lower($2)`, profile.UserID, strings.TrimSpace(promoCode)).
			Scan(&promotionID, &percent, &max, &redeemed, &expires, &active)
		if errors.Is(err, pgx.ErrNoRows) {
			return Subscription{}, fmt.Errorf("%w: promotion code not found", ErrInvalidInput)
		}
		if err != nil {
			return Subscription{}, err
		}
		if !active || (expires != nil && !expires.After(s.now().UTC())) || redeemed >= max {
			return Subscription{}, fmt.Errorf("%w: promotion is not available", ErrInvalidInput)
		}
		var used bool
		_ = s.pool.QueryRow(ctx, `
			SELECT EXISTS (SELECT 1 FROM creator.promotion_redemptions
			WHERE promotion_id = $1::uuid AND fan_id = $2::uuid)`, promotionID, p.UserID).Scan(&used)
		if used {
			return Subscription{}, fmt.Errorf("%w: promotion already used", ErrInvalidInput)
		}
		discount = (tier.PriceMinor * int64(percent)) / 100
		if discount < 0 {
			discount = 0
		}
		price = tier.PriceMinor - discount
		code = strings.ToUpper(strings.TrimSpace(promoCode))
	}

	sub, err := s.insertSubscriptionOffer(ctx, p.UserID, tier, price, discount, code, trial)
	if err != nil {
		return Subscription{}, err
	}
	s.audit(ctx, p.UserID, "creator.subscription_started", "creator_subscription", sub.ID, requestID)
	if s.fakePayments {
		active, err := s.activateWithPeriod(ctx, sub.ID, s.now().UTC(), periodDays)
		if err != nil {
			return Subscription{}, err
		}
		if promotionID != "" {
			_, _ = s.pool.Exec(ctx, `
				INSERT INTO creator.promotion_redemptions (promotion_id, fan_id, subscription_id)
				VALUES ($1::uuid, $2::uuid, $3::uuid) ON CONFLICT DO NOTHING`,
				promotionID, p.UserID, sub.ID)
			_, _ = s.pool.Exec(ctx, `
				UPDATE creator.promotions SET redeemed_count = redeemed_count + 1
				WHERE id = $1::uuid AND redeemed_count < max_redemptions`, promotionID)
		}
		s.audit(ctx, p.UserID, "creator.subscription_activated", "creator_subscription", active.ID, requestID)
		return active, nil
	}
	return sub, nil
}
