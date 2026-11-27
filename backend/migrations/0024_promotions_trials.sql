-- 0024: promotions, discounts and free trials.
--
-- A promotion is creator-scoped, single-use per fan, bounded by percentage and
-- redemption count. A trial is a zero-price subscription with an explicit flag;
-- simulated payments never treat it as collected revenue.

ALTER TABLE creator.tiers
    ADD COLUMN IF NOT EXISTS trial_days integer NOT NULL DEFAULT 0;
ALTER TABLE creator.tiers DROP CONSTRAINT IF EXISTS tiers_trial_days;
ALTER TABLE creator.tiers ADD CONSTRAINT tiers_trial_days
    CHECK (trial_days BETWEEN 0 AND 30);

ALTER TABLE creator.subscriptions
    ADD COLUMN IF NOT EXISTS discount_minor bigint NOT NULL DEFAULT 0;
ALTER TABLE creator.subscriptions
    ADD COLUMN IF NOT EXISTS promotion_code text NOT NULL DEFAULT '';
ALTER TABLE creator.subscriptions
    ADD COLUMN IF NOT EXISTS is_trial boolean NOT NULL DEFAULT false;

CREATE TABLE IF NOT EXISTS creator.promotions (
    id              uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    creator_id      uuid        NOT NULL REFERENCES creator.profiles (user_id) ON DELETE CASCADE,
    code            citext      NOT NULL,
    percent_off     integer     NOT NULL,
    max_redemptions integer     NOT NULL DEFAULT 100,
    redeemed_count  integer     NOT NULL DEFAULT 0,
    starts_at       timestamptz NOT NULL DEFAULT now(),
    expires_at      timestamptz,
    is_active       boolean     NOT NULL DEFAULT true,
    created_at      timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT promotions_percent CHECK (percent_off BETWEEN 1 AND 100),
    CONSTRAINT promotions_max CHECK (max_redemptions BETWEEN 1 AND 100000),
    CONSTRAINT promotions_redeemed CHECK (redeemed_count >= 0 AND redeemed_count <= max_redemptions),
    CONSTRAINT promotions_code CHECK (code ~ '^[A-Za-z0-9_-]{3,24}$'),
    CONSTRAINT promotions_window CHECK (expires_at IS NULL OR expires_at > starts_at)
);
CREATE UNIQUE INDEX IF NOT EXISTS promotions_creator_code_idx
    ON creator.promotions (creator_id, lower(code));
CREATE INDEX IF NOT EXISTS promotions_live_idx
    ON creator.promotions (creator_id) WHERE is_active;

CREATE TABLE IF NOT EXISTS creator.promotion_redemptions (
    promotion_id    uuid        NOT NULL REFERENCES creator.promotions (id) ON DELETE CASCADE,
    fan_id          uuid        NOT NULL REFERENCES identity.users (id) ON DELETE CASCADE,
    subscription_id uuid        NOT NULL REFERENCES creator.subscriptions (id) ON DELETE CASCADE,
    created_at      timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (promotion_id, fan_id)
);

DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'content_api') THEN
        EXECUTE 'GRANT SELECT, INSERT, UPDATE ON creator.promotions TO content_api';
        EXECUTE 'GRANT SELECT, INSERT ON creator.promotion_redemptions TO content_api';
    END IF;
END $$;
