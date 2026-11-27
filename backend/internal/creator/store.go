package creator

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/jawuu187-ops/content-platform/backend/internal/identity"
)

// Service is the creator use-case layer. It holds no HTTP knowledge.
type Service struct {
	pool *pgxpool.Pool
	now  func() time.Time
	// fakePayments activates a subscription without a payment provider. It exists
	// so the entitlement machinery can be tested end to end, and it is refused
	// outside local/test environments by configuration.
	fakePayments bool
	// adult is the age-verification gate. Nil means "not configured", which
	// denies paid scopes rather than allowing them.
	adult AdultGate
}

// Option configures the service.
type Option func(*Service)

// WithClock overrides the clock for deterministic tests.
func WithClock(now func() time.Time) Option { return func(s *Service) { s.now = now } }

// WithAgeGate installs the age-verification gate that paid scopes depend on.
func WithAgeGate(g AdultGate) Option { return func(s *Service) { s.adult = g } }

// WithFakePayments enables immediate activation without a payment provider.
func WithFakePayments(enabled bool) Option {
	return func(s *Service) { s.fakePayments = enabled }
}

// New builds the service on an existing pool.
func New(pool *pgxpool.Pool, opts ...Option) *Service {
	s := &Service{pool: pool, now: time.Now}
	for _, o := range opts {
		o(s)
	}
	return s
}

func (s *Service) audit(ctx context.Context, actorID, action, targetType, targetID, requestID string) {
	_, _ = s.pool.Exec(ctx, `
		SELECT ops.append_audit('user', $1, $2, $3, $4, NULL, '{}'::jsonb, NULLIF($5, ''))`,
		actorID, action, targetType, targetID, requestID)
}

// insertProfile creates the profile for an account that is becoming a creator.
func (s *Service) insertProfile(ctx context.Context, userID string, in ProfileInput) (Profile, error) {
	var p Profile
	err := s.pool.QueryRow(ctx, `
		INSERT INTO creator.profiles (user_id, handle, display_name, bio, category, avatar_url, cover_url, is_published)
		VALUES ($1::uuid, $2, $3, $4, $5, NULLIF($6, ''), NULLIF($7, ''), $8)
		RETURNING user_id::text, handle::text, display_name, bio, category,
		          COALESCE(avatar_url, ''), COALESCE(cover_url, ''), is_published, created_at, updated_at`,
		userID, in.Handle, in.DisplayName, in.Bio, in.Category, in.AvatarURL, in.CoverURL, in.publishValue(),
	).Scan(&p.UserID, &p.Handle, &p.DisplayName, &p.Bio, &p.Category,
		&p.AvatarURL, &p.CoverURL, &p.Published, &p.CreatedAt, &p.UpdatedAt)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return Profile{}, ErrHandleTaken
		}
		return Profile{}, fmt.Errorf("creator: insert profile: %w", err)
	}
	return p, nil
}

func (s *Service) updateProfile(ctx context.Context, userID string, in ProfileInput) (Profile, error) {
	var p Profile
	err := s.pool.QueryRow(ctx, `
		UPDATE creator.profiles
		SET display_name = COALESCE(NULLIF($2, ''), display_name),
		    bio = $3,
		    category = COALESCE(NULLIF($4, ''), category),
		    avatar_url = NULLIF($5, ''),
		    cover_url = NULLIF($6, ''),
		    is_published = COALESCE($7, is_published),
		    updated_at = now()
		WHERE user_id = $1::uuid
		RETURNING user_id::text, handle::text, display_name, bio, category,
		          COALESCE(avatar_url, ''), COALESCE(cover_url, ''), is_published, created_at, updated_at`,
		userID, in.DisplayName, in.Bio, in.Category, in.AvatarURL, in.CoverURL, in.Publish,
	).Scan(&p.UserID, &p.Handle, &p.DisplayName, &p.Bio, &p.Category,
		&p.AvatarURL, &p.CoverURL, &p.Published, &p.CreatedAt, &p.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Profile{}, ErrNotFound
	}
	if err != nil {
		return Profile{}, fmt.Errorf("creator: update profile: %w", err)
	}
	return p, nil
}

// profileByHandle loads any profile, published or not. Visibility is decided by
// the caller so the owner can see their own draft.
func (s *Service) profileByHandle(ctx context.Context, handle string) (Profile, error) {
	var p Profile
	err := s.pool.QueryRow(ctx, `
		SELECT user_id::text, handle::text, display_name, bio, category,
		       COALESCE(avatar_url, ''), COALESCE(cover_url, ''), is_published, created_at, updated_at
		FROM creator.profiles WHERE handle = $1`, handle,
	).Scan(&p.UserID, &p.Handle, &p.DisplayName, &p.Bio, &p.Category,
		&p.AvatarURL, &p.CoverURL, &p.Published, &p.CreatedAt, &p.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Profile{}, ErrNotFound
	}
	if err != nil {
		return Profile{}, fmt.Errorf("creator: load profile: %w", err)
	}
	return p, nil
}

func (s *Service) insertTier(ctx context.Context, creatorID string, in TierInput) (Tier, error) {
	name := strings.TrimSpace(in.Name)
	price := *in.PriceMinor
	periodDays := 30
	if in.PeriodDays != nil {
		periodDays = *in.PeriodDays
	}
	sortOrder := 0
	if in.SortOrder != nil {
		sortOrder = *in.SortOrder
	}
	trialDays := 0
	if in.TrialDays != nil {
		trialDays = *in.TrialDays
	}
	if trialDays < 0 || trialDays > 30 {
		return Tier{}, fmt.Errorf("%w: trial days must be between 0 and 30", ErrInvalidInput)
	}
	var t Tier
	err := s.pool.QueryRow(ctx, `
		INSERT INTO creator.tiers (creator_id, name, description, price_minor, currency, period_days, trial_days, perks, sort_order)
		VALUES ($1::uuid, $2, $3, $4, $5, $6, $7, $8, $9)
		RETURNING id::text, creator_id::text, name, description, price_minor, currency,
		          period_days, trial_days, perks, is_active, sort_order, created_at`,
		creatorID, name, in.Description, price, in.Currency, periodDays, trialDays, in.Perks, sortOrder,
	).Scan(&t.ID, &t.CreatorID, &t.Name, &t.Description, &t.PriceMinor, &t.Currency,
		&t.PeriodDays, &t.TrialDays, &t.Perks, &t.Active, &t.SortOrder, &t.CreatedAt)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return Tier{}, fmt.Errorf("%w: a tier with that name already exists", ErrInvalidInput)
		}
		return Tier{}, fmt.Errorf("creator: insert tier: %w", err)
	}
	return t, nil
}

func (s *Service) updateTier(ctx context.Context, creatorID, tierID string, in TierInput) (Tier, error) {
	var t Tier
	err := s.pool.QueryRow(ctx, `
		UPDATE creator.tiers
		SET name = COALESCE(NULLIF($3, ''), name),
		    description = $4,
		    price_minor = COALESCE($5, price_minor),
		    period_days = COALESCE($6, period_days),
		    trial_days = COALESCE($7, trial_days),
		    perks = COALESCE($8, perks),
		    sort_order = COALESCE($9, sort_order),
		    is_active = COALESCE($10, is_active),
		    updated_at = now()
		WHERE id = $1::uuid AND creator_id = $2::uuid
		RETURNING id::text, creator_id::text, name, description, price_minor, currency,
		          period_days, trial_days, perks, is_active, sort_order, created_at`,
		tierID, creatorID, in.Name, in.Description, in.PriceMinor, in.PeriodDays, in.TrialDays,
		in.Perks, in.SortOrder, in.Active,
	).Scan(&t.ID, &t.CreatorID, &t.Name, &t.Description, &t.PriceMinor, &t.Currency,
		&t.PeriodDays, &t.TrialDays, &t.Perks, &t.Active, &t.SortOrder, &t.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Tier{}, ErrNotFound
	}
	if err != nil {
		return Tier{}, fmt.Errorf("creator: update tier: %w", err)
	}
	return t, nil
}

func (s *Service) tiersOf(ctx context.Context, creatorID string, activeOnly bool) ([]Tier, error) {
	query := `
		SELECT id::text, creator_id::text, name, description, price_minor, currency,
		       period_days, trial_days, perks, is_active, sort_order, created_at
		FROM creator.tiers WHERE creator_id = $1::uuid`
	if activeOnly {
		query += ` AND is_active`
	}
	query += ` ORDER BY sort_order, created_at`
	rows, err := s.pool.Query(ctx, query, creatorID)
	if err != nil {
		return nil, fmt.Errorf("creator: list tiers: %w", err)
	}
	defer rows.Close()
	var out []Tier
	for rows.Next() {
		var t Tier
		if err := rows.Scan(&t.ID, &t.CreatorID, &t.Name, &t.Description, &t.PriceMinor, &t.Currency,
			&t.PeriodDays, &t.TrialDays, &t.Perks, &t.Active, &t.SortOrder, &t.CreatedAt); err != nil {
			return nil, fmt.Errorf("creator: scan tier: %w", err)
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func (s *Service) tierByID(ctx context.Context, tierID string) (Tier, error) {
	var t Tier
	err := s.pool.QueryRow(ctx, `
		SELECT id::text, creator_id::text, name, description, price_minor, currency,
		       period_days, trial_days, perks, is_active, sort_order, created_at
		FROM creator.tiers WHERE id = $1::uuid`, tierID,
	).Scan(&t.ID, &t.CreatorID, &t.Name, &t.Description, &t.PriceMinor, &t.Currency,
		&t.PeriodDays, &t.TrialDays, &t.Perks, &t.Active, &t.SortOrder, &t.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Tier{}, ErrNotFound
	}
	if err != nil {
		return Tier{}, fmt.Errorf("creator: load tier: %w", err)
	}
	return t, nil
}

func (s *Service) insertSubscriptionOffer(ctx context.Context, fanID string, t Tier, price, discount int64,
	promoCode string, trial bool) (Subscription, error) {
	var sub Subscription
	err := s.pool.QueryRow(ctx, `
		INSERT INTO creator.subscriptions
			(fan_id, creator_id, tier_id, status, price_minor, currency, discount_minor, promotion_code, is_trial)
		VALUES ($1::uuid, $2::uuid, $3::uuid, 'pending', $4, $5, $6, $7, $8)
		RETURNING id::text, fan_id::text, creator_id::text, tier_id::text, status, price_minor, currency,
		          current_period_start, current_period_end, cancel_at_period_end, discount_minor,
		          promotion_code, is_trial, created_at`,
		fanID, t.CreatorID, t.ID, price, t.Currency, discount, promoCode, trial,
	).Scan(&sub.ID, &sub.FanID, &sub.CreatorID, &sub.TierID, &sub.Status, &sub.PriceMinor, &sub.Currency,
		&sub.PeriodStart, &sub.PeriodEnd, &sub.CancelAtPeriodEnd, &sub.DiscountMinor,
		&sub.PromotionCode, &sub.IsTrial, &sub.CreatedAt)
	if err != nil {
		return Subscription{}, fmt.Errorf("creator: insert subscription offer: %w", err)
	}
	return sub, nil
}

func (s *Service) activateWithPeriod(ctx context.Context, subscriptionID string, start time.Time, periodDays int) (Subscription, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Subscription{}, fmt.Errorf("creator: begin activate: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	end := start.AddDate(0, 0, periodDays)
	var sub Subscription
	err = tx.QueryRow(ctx, `
		UPDATE creator.subscriptions
		SET status = 'active', current_period_start = $2, current_period_end = $3, updated_at = now()
		WHERE id = $1::uuid AND status = 'pending'
		RETURNING id::text, fan_id::text, creator_id::text, tier_id::text, status, price_minor, currency,
		          current_period_start, current_period_end, cancel_at_period_end, discount_minor,
		          promotion_code, is_trial, created_at`, subscriptionID, start, end).
		Scan(&sub.ID, &sub.FanID, &sub.CreatorID, &sub.TierID, &sub.Status, &sub.PriceMinor, &sub.Currency,
			&sub.PeriodStart, &sub.PeriodEnd, &sub.CancelAtPeriodEnd, &sub.DiscountMinor,
			&sub.PromotionCode, &sub.IsTrial, &sub.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Subscription{}, ErrAlreadySubscribed
	}
	if err != nil {
		return Subscription{}, fmt.Errorf("creator: activate offer: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO creator.entitlements (fan_id, creator_id, scope, source, subscription_id, granted_at, expires_at)
		VALUES ($1::uuid, $2::uuid, 'subscriber', 'subscription', $3::uuid, $4, $5)
		ON CONFLICT (fan_id, creator_id, scope) WHERE revoked_at IS NULL
		DO UPDATE SET expires_at = EXCLUDED.expires_at`, sub.FanID, sub.CreatorID, sub.ID, start, end); err != nil {
		return Subscription{}, fmt.Errorf("creator: grant offer entitlement: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return Subscription{}, err
	}
	return sub, nil
}

func (s *Service) insertSubscription(ctx context.Context, fanID string, t Tier, status string) (Subscription, error) {
	var sub Subscription
	err := s.pool.QueryRow(ctx, `
		INSERT INTO creator.subscriptions (fan_id, creator_id, tier_id, status, price_minor, currency)
		VALUES ($1::uuid, $2::uuid, $3::uuid, $4, $5, $6)
		RETURNING id::text, fan_id::text, creator_id::text, tier_id::text, status, price_minor, currency,
		          current_period_start, current_period_end, cancel_at_period_end, created_at`,
		fanID, t.CreatorID, t.ID, status, t.PriceMinor, t.Currency,
	).Scan(&sub.ID, &sub.FanID, &sub.CreatorID, &sub.TierID, &sub.Status, &sub.PriceMinor, &sub.Currency,
		&sub.PeriodStart, &sub.PeriodEnd, &sub.CancelAtPeriodEnd, &sub.CreatedAt)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return Subscription{}, ErrAlreadySubscribed
		}
		return Subscription{}, fmt.Errorf("creator: insert subscription: %w", err)
	}
	return sub, nil
}

// activate moves a pending subscription to active and grants the entitlement in
// the same transaction, so a subscription can never be active without access or
// access without a subscription.
func (s *Service) activate(ctx context.Context, subscriptionID string, start time.Time, periodDays int) (Subscription, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Subscription{}, fmt.Errorf("creator: begin activate: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	end := start.AddDate(0, 0, periodDays)
	var sub Subscription
	err = tx.QueryRow(ctx, `
		UPDATE creator.subscriptions
		SET status = 'active', current_period_start = $2, current_period_end = $3, updated_at = now()
		WHERE id = $1::uuid AND status = 'pending'
		RETURNING id::text, fan_id::text, creator_id::text, tier_id::text, status, price_minor, currency,
		          current_period_start, current_period_end, cancel_at_period_end, created_at`,
		subscriptionID, start, end,
	).Scan(&sub.ID, &sub.FanID, &sub.CreatorID, &sub.TierID, &sub.Status, &sub.PriceMinor, &sub.Currency,
		&sub.PeriodStart, &sub.PeriodEnd, &sub.CancelAtPeriodEnd, &sub.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		// Not pending: either already active (idempotent) or cancelled.
		return s.subscriptionByIDTx(ctx, tx, subscriptionID)
	}
	if err != nil {
		return Subscription{}, fmt.Errorf("creator: activate subscription: %w", err)
	}

	_, err = tx.Exec(ctx, `
		INSERT INTO creator.entitlements (fan_id, creator_id, scope, source, subscription_id, expires_at)
		VALUES ($1::uuid, $2::uuid, 'subscriber', 'subscription', $3::uuid, $4)
		ON CONFLICT (fan_id, creator_id, scope) WHERE revoked_at IS NULL
		DO UPDATE SET expires_at = EXCLUDED.expires_at, subscription_id = EXCLUDED.subscription_id`,
		sub.FanID, sub.CreatorID, sub.ID, end)
	if err != nil {
		return Subscription{}, fmt.Errorf("creator: grant entitlement: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return Subscription{}, fmt.Errorf("creator: commit activate: %w", err)
	}
	return sub, nil
}

func (s *Service) subscriptionByIDTx(ctx context.Context, tx pgx.Tx, id string) (Subscription, error) {
	var sub Subscription
	err := tx.QueryRow(ctx, `
		SELECT id::text, fan_id::text, creator_id::text, tier_id::text, status, price_minor, currency,
		       current_period_start, current_period_end, cancel_at_period_end, created_at
		FROM creator.subscriptions WHERE id = $1::uuid`, id,
	).Scan(&sub.ID, &sub.FanID, &sub.CreatorID, &sub.TierID, &sub.Status, &sub.PriceMinor, &sub.Currency,
		&sub.PeriodStart, &sub.PeriodEnd, &sub.CancelAtPeriodEnd, &sub.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Subscription{}, ErrNotFound
	}
	if err != nil {
		return Subscription{}, fmt.Errorf("creator: load subscription: %w", err)
	}
	return sub, nil
}

func (s *Service) subscriptionByID(ctx context.Context, id string) (Subscription, error) {
	var sub Subscription
	err := s.pool.QueryRow(ctx, `
		SELECT id::text, fan_id::text, creator_id::text, tier_id::text, status, price_minor, currency,
		       current_period_start, current_period_end, cancel_at_period_end, created_at
		FROM creator.subscriptions WHERE id = $1::uuid`, id,
	).Scan(&sub.ID, &sub.FanID, &sub.CreatorID, &sub.TierID, &sub.Status, &sub.PriceMinor, &sub.Currency,
		&sub.PeriodStart, &sub.PeriodEnd, &sub.CancelAtPeriodEnd, &sub.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Subscription{}, ErrNotFound
	}
	if err != nil {
		return Subscription{}, fmt.Errorf("creator: load subscription: %w", err)
	}
	return sub, nil
}

func (s *Service) subscriptionsOfFan(ctx context.Context, fanID string) ([]Subscription, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id::text, fan_id::text, creator_id::text, tier_id::text, status, price_minor, currency,
		       current_period_start, current_period_end, cancel_at_period_end, created_at
		FROM creator.subscriptions WHERE fan_id = $1::uuid
		ORDER BY created_at DESC`, fanID)
	if err != nil {
		return nil, fmt.Errorf("creator: list subscriptions: %w", err)
	}
	defer rows.Close()
	var out []Subscription
	for rows.Next() {
		var sub Subscription
		if err := rows.Scan(&sub.ID, &sub.FanID, &sub.CreatorID, &sub.TierID, &sub.Status, &sub.PriceMinor,
			&sub.Currency, &sub.PeriodStart, &sub.PeriodEnd, &sub.CancelAtPeriodEnd, &sub.CreatedAt); err != nil {
			return nil, fmt.Errorf("creator: scan subscription: %w", err)
		}
		out = append(out, sub)
	}
	return out, rows.Err()
}

// liveEntitlement returns the live entitlement for a scope and, when that
// entitlement came from a subscription, the subscription it depends on.
func (s *Service) liveEntitlement(ctx context.Context, fanID, creatorID, scope string, now time.Time) (bool, *string, *time.Time, error) {
	var expires *time.Time
	var subID *string
	err := s.pool.QueryRow(ctx, `
		SELECT expires_at, subscription_id::text FROM creator.entitlements
		WHERE fan_id = $1::uuid AND creator_id = $2::uuid
		  AND revoked_at IS NULL
		  AND scope = ANY($3::text[])
		  AND (expires_at IS NULL OR expires_at > $4)
		ORDER BY granted_at DESC
		LIMIT 1`, fanID, creatorID, []string{scope, ScopeAll}, now).Scan(&expires, &subID)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil, nil, nil
	}
	if err != nil {
		return false, nil, nil, fmt.Errorf("creator: load entitlement: %w", err)
	}
	return true, subID, expires, nil
}

// paidAccess is the single paid-content decision. An entitlement is required,
// and when it was granted by a subscription that subscription must still be in
// period: neither half alone opens content (D-035).
func (s *Service) paidAccess(ctx context.Context, fanID, creatorID, scope string, now time.Time) (Access, error) {
	ok, subID, expires, err := s.liveEntitlement(ctx, fanID, creatorID, scope, now)
	if err != nil {
		return Access{}, err
	}
	if !ok {
		return Access{Allowed: false, Scope: scope, Reason: "entitlement_required"}, nil
	}
	if subID != nil {
		active, err := s.subscriptionInPeriod(ctx, *subID, now)
		if err != nil {
			return Access{}, err
		}
		if !active {
			// The entitlement outlived its subscription: refuse rather than serve.
			return Access{Allowed: false, Scope: scope, Reason: "entitlement_required"}, nil
		}
	}
	return Access{Allowed: true, Scope: scope, Reason: "entitlement", ExpiresAt: expires}, nil
}

// subscriptionInPeriod reports whether one subscription is active and paid for
// the current period.
func (s *Service) subscriptionInPeriod(ctx context.Context, subscriptionID string, now time.Time) (bool, error) {
	var exists bool
	err := s.pool.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM creator.subscriptions
			WHERE id = $1::uuid AND status = 'active'
			  AND current_period_start <= $2 AND current_period_end > $2
		)`, subscriptionID, now).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("creator: check subscription period: %w", err)
	}
	return exists, nil
}

// expireDue ends subscriptions whose period is over and revokes the entitlement
// they granted. It returns how many subscriptions were expired.
func (s *Service) expireDue(ctx context.Context, now time.Time) (int64, error) {
	tag, err := s.pool.Exec(ctx, `
		WITH due AS (
			UPDATE creator.subscriptions
			SET status = 'expired', updated_at = now()
			WHERE status = 'active' AND current_period_end <= $1
			RETURNING id
		)
		UPDATE creator.entitlements e
		SET revoked_at = now()
		FROM due
		WHERE e.subscription_id = due.id AND e.revoked_at IS NULL`, now)
	if err != nil {
		return 0, fmt.Errorf("creator: expire subscriptions: %w", err)
	}
	return tag.RowsAffected(), nil
}

// cancel marks a subscription to end at the period boundary, or cancels a
// pending one immediately.
func (s *Service) cancel(ctx context.Context, sub Subscription) (Subscription, error) {
	var out Subscription
	var err error
	if sub.Status == StatusPending {
		err = s.pool.QueryRow(ctx, `
			UPDATE creator.subscriptions SET status = 'cancelled', updated_at = now()
			WHERE id = $1::uuid AND status = 'pending'
			RETURNING id::text, fan_id::text, creator_id::text, tier_id::text, status, price_minor, currency,
			          current_period_start, current_period_end, cancel_at_period_end, created_at`,
			sub.ID).Scan(&out.ID, &out.FanID, &out.CreatorID, &out.TierID, &out.Status, &out.PriceMinor,
			&out.Currency, &out.PeriodStart, &out.PeriodEnd, &out.CancelAtPeriodEnd, &out.CreatedAt)
	} else {
		err = s.pool.QueryRow(ctx, `
			UPDATE creator.subscriptions SET cancel_at_period_end = true, updated_at = now()
			WHERE id = $1::uuid AND status = 'active'
			RETURNING id::text, fan_id::text, creator_id::text, tier_id::text, status, price_minor, currency,
			          current_period_start, current_period_end, cancel_at_period_end, created_at`,
			sub.ID).Scan(&out.ID, &out.FanID, &out.CreatorID, &out.TierID, &out.Status, &out.PriceMinor,
			&out.Currency, &out.PeriodStart, &out.PeriodEnd, &out.CancelAtPeriodEnd, &out.CreatedAt)
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return Subscription{}, fmt.Errorf("%w: subscription cannot be cancelled in its current state", ErrInvalidInput)
	}
	if err != nil {
		return Subscription{}, fmt.Errorf("creator: cancel subscription: %w", err)
	}
	return out, nil
}

// SetProfileMedia replaces the avatar or cover with an image the creator owns.
// The media object is checked here, not trusted from the client.
func (s *Service) SetProfileMedia(ctx context.Context, p identity.Principal, kind, mediaID, requestID string) (string, error) {
	column := ""
	switch kind {
	case "avatar":
		column = "avatar_media_id"
	case "cover":
		column = "cover_media_id"
	default:
		return "", fmt.Errorf("%w: kind must be avatar or cover", ErrInvalidInput)
	}
	if mediaID == "" {
		if _, err := s.pool.Exec(ctx, `UPDATE creator.profiles SET `+column+` = NULL, updated_at = now() WHERE user_id = $1::uuid`, p.UserID); err != nil {
			return "", fmt.Errorf("creator: clear profile media: %w", err)
		}
		return "", nil
	}
	var ok bool
	if err := s.pool.QueryRow(ctx, `
		SELECT EXISTS (SELECT 1 FROM media.objects
		WHERE id = $1::uuid AND owner_id = $2::uuid AND kind = 'image' AND status = 'ready')`,
		mediaID, p.UserID).Scan(&ok); err != nil {
		return "", fmt.Errorf("creator: profile media lookup: %w", err)
	}
	if !ok {
		return "", fmt.Errorf("%w: that image is not owned by this creator", ErrForbidden)
	}
	tag, err := s.pool.Exec(ctx, `UPDATE creator.profiles SET `+column+` = $2::uuid, updated_at = now() WHERE user_id = $1::uuid`, p.UserID, mediaID)
	if err != nil {
		return "", fmt.Errorf("creator: set profile media: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return "", ErrNotFound
	}
	s.audit(ctx, p.UserID, "creator.profile_media", "user", p.UserID, requestID)
	return mediaID, nil
}

// ProfileMedia returns the media id for a public profile image. A draft profile
// is visible only to its owner. The caller streams the bytes.
func (s *Service) ProfileMedia(ctx context.Context, p identity.Principal, handle, kind string) (string, error) {
	column := ""
	switch kind {
	case "avatar":
		column = "avatar_media_id"
	case "cover":
		column = "cover_media_id"
	default:
		return "", ErrNotFound
	}
	var id string
	var published bool
	var owner string
	err := s.pool.QueryRow(ctx, `
		SELECT COALESCE(`+column+`::text, ''), is_published, user_id::text
		FROM creator.profiles WHERE handle = $1`, NormalizeHandle(handle)).Scan(&id, &published, &owner)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", fmt.Errorf("creator: profile media: %w", err)
	}
	if !published && owner != p.UserID {
		return "", ErrNotFound
	}
	if id == "" {
		return "", ErrNotFound
	}
	return id, nil
}
