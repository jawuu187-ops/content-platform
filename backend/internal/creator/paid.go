package creator

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// Purchase pricing.
//
// The price of a purchase is always read from the creator's own records. A
// client can choose *what* to buy and nothing else; the amount sent to a payment
// provider comes from here.

// TierForPurchase loads an active tier and refuses inactive or unknown ones.
func (s *Service) TierForPurchase(ctx context.Context, tierID string) (Tier, error) {
	tier, err := s.tierByID(ctx, tierID)
	if err != nil {
		return Tier{}, err
	}
	if !tier.Active {
		return Tier{}, ErrTierInactive
	}
	profile, err := s.profileByCreatorID(ctx, tier.CreatorID)
	if err != nil {
		return Tier{}, err
	}
	if !profile.Published {
		return Tier{}, ErrNotFound
	}
	return tier, nil
}

// PostForPurchase loads a published pay-per-view post.
func (s *Service) PostForPurchase(ctx context.Context, postID string) (Post, error) {
	post, err := s.postByID(ctx, postID)
	if err != nil {
		return Post{}, err
	}
	if post.Status != PostPublished || post.Visibility != VisibilityPPV || post.PriceMinor == nil {
		return Post{}, ErrNotFound
	}
	return post, nil
}

// ActivatePaidSubscription grants a paid membership after a settled payment.
//
// It is idempotent on the payment intent: replaying the settlement returns the
// subscription that already exists instead of creating a second one. If the fan
// already holds an active subscription to the same creator, the payment extends
// the current period rather than starting a new membership.
func (s *Service) ActivatePaidSubscription(ctx context.Context, fanID, creatorID, tierID, intentID string) (Subscription, error) {
	tier, err := s.tierByID(ctx, tierID)
	if err != nil {
		return Subscription{}, err
	}
	if tier.CreatorID != creatorID {
		return Subscription{}, ErrNotFound
	}
	if fanID == creatorID {
		return Subscription{}, ErrSelfSubscription
	}
	now := s.now().UTC()

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Subscription{}, fmt.Errorf("creator: begin paid activation: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// Replay of this settlement: return what the first attempt created.
	var existing Subscription
	err = tx.QueryRow(ctx, `
		SELECT id::text, fan_id::text, creator_id::text, tier_id::text, status, price_minor, currency,
		       current_period_start, current_period_end, cancel_at_period_end, created_at
		FROM creator.subscriptions WHERE payment_intent_id = $1::uuid`, intentID).
		Scan(&existing.ID, &existing.FanID, &existing.CreatorID, &existing.TierID, &existing.Status,
			&existing.PriceMinor, &existing.Currency, &existing.PeriodStart, &existing.PeriodEnd,
			&existing.CancelAtPeriodEnd, &existing.CreatedAt)
	if err == nil {
		return existing, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return Subscription{}, fmt.Errorf("creator: lookup paid subscription: %w", err)
	}

	// Renewal: extend the live membership instead of stacking a second one.
	var live Subscription
	err = tx.QueryRow(ctx, `
		SELECT id::text, fan_id::text, creator_id::text, tier_id::text, status, price_minor, currency,
		       current_period_start, current_period_end, cancel_at_period_end, created_at
		FROM creator.subscriptions
		WHERE fan_id = $1::uuid AND creator_id = $2::uuid
		  AND status IN ('active', 'pending')
		ORDER BY created_at DESC LIMIT 1
		FOR UPDATE`, fanID, creatorID).
		Scan(&live.ID, &live.FanID, &live.CreatorID, &live.TierID, &live.Status, &live.PriceMinor,
			&live.Currency, &live.PeriodStart, &live.PeriodEnd, &live.CancelAtPeriodEnd, &live.CreatedAt)
	switch {
	case err == nil:
		base := now
		if live.PeriodEnd != nil && live.PeriodEnd.After(base) {
			base = live.PeriodEnd.UTC()
		}
		end := base.AddDate(0, 0, tier.PeriodDays)
		if err := tx.QueryRow(ctx, `
			UPDATE creator.subscriptions
			SET status = 'active', tier_id = $2::uuid, current_period_start = COALESCE(current_period_start, $3),
			    current_period_end = $4, cancel_at_period_end = false, payment_intent_id = $5::uuid,
			    updated_at = now()
			WHERE id = $1::uuid
			RETURNING id::text, fan_id::text, creator_id::text, tier_id::text, status, price_minor, currency,
			          current_period_start, current_period_end, cancel_at_period_end, created_at`,
			live.ID, tier.ID, now, end, intentID,
		).Scan(&live.ID, &live.FanID, &live.CreatorID, &live.TierID, &live.Status, &live.PriceMinor,
			&live.Currency, &live.PeriodStart, &live.PeriodEnd, &live.CancelAtPeriodEnd, &live.CreatedAt); err != nil {
			return Subscription{}, fmt.Errorf("creator: renew subscription: %w", err)
		}
		if err := grantSubscriptionEntitlement(ctx, tx, live.FanID, live.CreatorID, live.ID, live.PeriodEnd); err != nil {
			return Subscription{}, err
		}
		if err := tx.Commit(ctx); err != nil {
			return Subscription{}, fmt.Errorf("creator: commit renewal: %w", err)
		}
		return live, nil
	case errors.Is(err, pgx.ErrNoRows):
		// fall through to a new subscription
	default:
		return Subscription{}, fmt.Errorf("creator: lookup live subscription: %w", err)
	}

	end := now.AddDate(0, 0, tier.PeriodDays)
	var sub Subscription
	err = tx.QueryRow(ctx, `
		INSERT INTO creator.subscriptions
		    (fan_id, creator_id, tier_id, status, price_minor, currency, current_period_start, current_period_end, payment_intent_id)
		VALUES ($1::uuid, $2::uuid, $3::uuid, 'active', $4, $5, $6, $7, $8::uuid)
		RETURNING id::text, fan_id::text, creator_id::text, tier_id::text, status, price_minor, currency,
		          current_period_start, current_period_end, cancel_at_period_end, created_at`,
		fanID, creatorID, tier.ID, tier.PriceMinor, tier.Currency, now, end, intentID).
		Scan(&sub.ID, &sub.FanID, &sub.CreatorID, &sub.TierID, &sub.Status, &sub.PriceMinor, &sub.Currency,
			&sub.PeriodStart, &sub.PeriodEnd, &sub.CancelAtPeriodEnd, &sub.CreatedAt)
	if err != nil {
		return Subscription{}, fmt.Errorf("creator: create paid subscription: %w", err)
	}
	if err := grantSubscriptionEntitlement(ctx, tx, fanID, creatorID, sub.ID, sub.PeriodEnd); err != nil {
		return Subscription{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Subscription{}, fmt.Errorf("creator: commit paid activation: %w", err)
	}
	return sub, nil
}

func grantSubscriptionEntitlement(ctx context.Context, tx pgx.Tx, fanID, creatorID, subscriptionID string, expires *time.Time) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO creator.entitlements (fan_id, creator_id, scope, source, subscription_id, expires_at)
		VALUES ($1::uuid, $2::uuid, 'subscriber', 'subscription', $3::uuid, $4)
		ON CONFLICT (fan_id, creator_id, scope) WHERE revoked_at IS NULL
		DO UPDATE SET expires_at = EXCLUDED.expires_at, subscription_id = EXCLUDED.subscription_id,
		              revoked_at = NULL`,
		fanID, creatorID, subscriptionID, expires)
	if err != nil {
		return fmt.Errorf("creator: grant subscription entitlement: %w", err)
	}
	return nil
}

// UnlockPostPaid opens one post after a settled payment. Idempotent on the
// payment intent.
func (s *Service) UnlockPostPaid(ctx context.Context, fanID, postID, intentID string) (Unlock, error) {
	var u Unlock
	err := s.pool.QueryRow(ctx, `
		SELECT id::text, post_id::text, source, granted_at, expires_at
		FROM creator.post_unlocks WHERE payment_intent_id = $1::uuid`, intentID).
		Scan(&u.ID, &u.PostID, &u.Source, &u.GrantedAt, &u.ExpiresAt)
	if err == nil {
		return u, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return Unlock{}, fmt.Errorf("creator: lookup paid unlock: %w", err)
	}
	err = s.pool.QueryRow(ctx, `
		INSERT INTO creator.post_unlocks (post_id, fan_id, source, payment_intent_id)
		VALUES ($1::uuid, $2::uuid, 'purchase', $3::uuid)
		ON CONFLICT (post_id, fan_id) WHERE revoked_at IS NULL
		DO UPDATE SET payment_intent_id = EXCLUDED.payment_intent_id, granted_at = now(), revoked_at = NULL
		RETURNING id::text, post_id::text, source, granted_at, expires_at`,
		postID, fanID, intentID).
		Scan(&u.ID, &u.PostID, &u.Source, &u.GrantedAt, &u.ExpiresAt)
	if err != nil {
		return Unlock{}, fmt.Errorf("creator: grant paid unlock: %w", err)
	}
	return u, nil
}

// RevokePaidAccess withdraws everything a payment intent granted: the
// subscription it started or extended, its entitlement, and any post unlock.
// A refund calls this so a chargeback cannot leave access behind.
func (s *Service) RevokePaidAccess(ctx context.Context, intentID string) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("creator: begin revoke paid access: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx, `
		UPDATE creator.entitlements e
		SET revoked_at = now()
		FROM creator.subscriptions s
		WHERE e.subscription_id = s.id AND s.payment_intent_id = $1::uuid AND e.revoked_at IS NULL`,
		intentID); err != nil {
		return fmt.Errorf("creator: revoke paid entitlement: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		UPDATE creator.subscriptions SET status = 'cancelled', updated_at = now()
		WHERE payment_intent_id = $1::uuid AND status IN ('pending', 'active')`, intentID); err != nil {
		return fmt.Errorf("creator: cancel paid subscription: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		UPDATE creator.post_unlocks SET revoked_at = now()
		WHERE payment_intent_id = $1::uuid AND revoked_at IS NULL`, intentID); err != nil {
		return fmt.Errorf("creator: revoke paid unlock: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("creator: commit revoke paid access: %w", err)
	}
	return nil
}

// profileByCreatorID loads a creator profile by the account that owns it.
func (s *Service) profileByCreatorID(ctx context.Context, creatorID string) (Profile, error) {
	var p Profile
	err := s.pool.QueryRow(ctx, `
		SELECT user_id::text, handle::text, display_name, bio, category,
		       COALESCE(avatar_url, ''), COALESCE(cover_url, ''), is_published, created_at, updated_at
		FROM creator.profiles WHERE user_id = $1::uuid`, creatorID).
		Scan(&p.UserID, &p.Handle, &p.DisplayName, &p.Bio, &p.Category,
			&p.AvatarURL, &p.CoverURL, &p.Published, &p.CreatedAt, &p.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Profile{}, ErrNotFound
	}
	if err != nil {
		return Profile{}, fmt.Errorf("creator: load profile by id: %w", err)
	}
	return p, nil
}
