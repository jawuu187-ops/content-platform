-- 0006_creators.sql
--
-- Creator profiles, subscription tiers, subscriptions and entitlements.
--
-- Money rules that this schema encodes:
--   * prices are integer minor units with an explicit ISO-4217 code, never
--     floats and never a currency-less number;
--   * a subscription stores the price and currency it was sold at, so a later
--     price change cannot rewrite history;
--   * an entitlement is the only thing that grants access, and a subscription
--     entitlement is derived from a paid, in-period subscription, never from the
--     presence of a subscription row alone.

CREATE SCHEMA IF NOT EXISTS creator;

CREATE TABLE IF NOT EXISTS creator.profiles (
    user_id      uuid        PRIMARY KEY REFERENCES identity.users (id) ON DELETE CASCADE,
    handle       citext      NOT NULL UNIQUE,
    display_name text        NOT NULL,
    bio          text        NOT NULL DEFAULT '',
    category     text        NOT NULL DEFAULT 'other',
    avatar_url   text,
    cover_url    text,
    is_published boolean     NOT NULL DEFAULT false,
    created_at   timestamptz NOT NULL DEFAULT now(),
    updated_at   timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT profiles_handle_format CHECK (handle ~ '^[a-z0-9_]{3,30}$'),
    CONSTRAINT profiles_display_name_len CHECK (char_length(display_name) BETWEEN 1 AND 60),
    CONSTRAINT profiles_bio_len CHECK (char_length(bio) <= 600),
    CONSTRAINT profiles_category CHECK (category IN
        ('art', 'music', 'fitness', 'lifestyle', 'photography', 'writing', 'education', 'other'))
);

CREATE INDEX IF NOT EXISTS profiles_published_idx
    ON creator.profiles (is_published) WHERE is_published;

CREATE TABLE IF NOT EXISTS creator.tiers (
    id           uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    creator_id   uuid        NOT NULL REFERENCES creator.profiles (user_id) ON DELETE CASCADE,
    name         text        NOT NULL,
    description  text        NOT NULL DEFAULT '',
    price_minor  bigint      NOT NULL,
    currency     char(3)     NOT NULL,
    period_days  integer     NOT NULL DEFAULT 30,
    perks        text[]      NOT NULL DEFAULT '{}',
    is_active    boolean     NOT NULL DEFAULT true,
    sort_order   integer     NOT NULL DEFAULT 0,
    created_at   timestamptz NOT NULL DEFAULT now(),
    updated_at   timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT tiers_price_positive CHECK (price_minor BETWEEN 100 AND 1000000),
    CONSTRAINT tiers_period CHECK (period_days BETWEEN 1 AND 366),
    CONSTRAINT tiers_currency CHECK (currency ~ '^[A-Z]{3}$'),
    CONSTRAINT tiers_name_len CHECK (char_length(name) BETWEEN 1 AND 40),
    UNIQUE (creator_id, name)
);

CREATE INDEX IF NOT EXISTS tiers_creator_active_idx
    ON creator.tiers (creator_id, sort_order) WHERE is_active;

CREATE TABLE IF NOT EXISTS creator.subscriptions (
    id                   uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    fan_id               uuid        NOT NULL REFERENCES identity.users (id) ON DELETE CASCADE,
    creator_id           uuid        NOT NULL REFERENCES creator.profiles (user_id) ON DELETE CASCADE,
    tier_id              uuid        NOT NULL REFERENCES creator.tiers (id),
    status               text        NOT NULL,
    price_minor          bigint      NOT NULL,
    currency             char(3)     NOT NULL,
    current_period_start timestamptz,
    current_period_end   timestamptz,
    cancel_at_period_end boolean     NOT NULL DEFAULT false,
    created_at           timestamptz NOT NULL DEFAULT now(),
    updated_at           timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT subscriptions_status CHECK (status IN
        ('pending', 'active', 'cancelled', 'expired', 'past_due')),
    CONSTRAINT subscriptions_no_self CHECK (fan_id <> creator_id),
    -- An active subscription must know its period; a pending one must not pretend
    -- to have one.
    CONSTRAINT subscriptions_period_present CHECK (
        (status = 'active' AND current_period_start IS NOT NULL AND current_period_end IS NOT NULL)
        OR status <> 'active'),
    CONSTRAINT subscriptions_period_order CHECK (
        current_period_end IS NULL OR current_period_start IS NULL
        OR current_period_end > current_period_start)
);

-- One live subscription per fan and creator. Two rows would mean double billing
-- and two entitlements, so the database refuses it instead of the service.
CREATE UNIQUE INDEX IF NOT EXISTS subscriptions_one_live_idx
    ON creator.subscriptions (fan_id, creator_id)
    WHERE status IN ('pending', 'active');

CREATE INDEX IF NOT EXISTS subscriptions_creator_idx
    ON creator.subscriptions (creator_id, status);

CREATE TABLE IF NOT EXISTS creator.entitlements (
    id              uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    fan_id          uuid        NOT NULL REFERENCES identity.users (id) ON DELETE CASCADE,
    creator_id      uuid        NOT NULL REFERENCES creator.profiles (user_id) ON DELETE CASCADE,
    scope           text        NOT NULL,
    source          text        NOT NULL,
    subscription_id uuid        REFERENCES creator.subscriptions (id) ON DELETE CASCADE,
    granted_at      timestamptz NOT NULL DEFAULT now(),
    expires_at      timestamptz,
    revoked_at      timestamptz,
    CONSTRAINT entitlements_scope CHECK (scope IN ('subscriber', 'ppv', 'all')),
    CONSTRAINT entitlements_source CHECK (source IN ('subscription', 'grant', 'admin')),
    CONSTRAINT entitlements_subscription_source CHECK (
        (source = 'subscription') = (subscription_id IS NOT NULL))
);

-- One live entitlement per fan, creator and scope. A subscription upgrade
-- replaces the entitlement rather than stacking duplicates.
CREATE UNIQUE INDEX IF NOT EXISTS entitlements_live_idx
    ON creator.entitlements (fan_id, creator_id, scope)
    WHERE revoked_at IS NULL;

CREATE INDEX IF NOT EXISTS entitlements_lookup_idx
    ON creator.entitlements (fan_id, creator_id)
    WHERE revoked_at IS NULL;
