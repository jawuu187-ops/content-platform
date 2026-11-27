-- 0030: paid messages, unlock history and message requests.
--
-- History is append-only: a message_unlock row is never deleted or reused. A
-- partial unique index allows one live unlock but any number of revoked rows, so
-- a refunded message can be bought again with a new intent.

ALTER TABLE creator.profiles
    ADD COLUMN IF NOT EXISTS dm_request_policy text NOT NULL DEFAULT 'subscribers',
    ADD COLUMN IF NOT EXISTS paid_messages_enabled boolean NOT NULL DEFAULT false,
    ADD COLUMN IF NOT EXISTS default_message_price_minor bigint,
    ADD COLUMN IF NOT EXISTS message_currency char(3) NOT NULL DEFAULT 'USD';
ALTER TABLE creator.profiles DROP CONSTRAINT IF EXISTS profiles_dm_policy;
ALTER TABLE creator.profiles ADD CONSTRAINT profiles_dm_policy
    CHECK (dm_request_policy IN ('subscribers', 'everyone', 'none'));
ALTER TABLE creator.profiles DROP CONSTRAINT IF EXISTS profiles_message_price;
ALTER TABLE creator.profiles ADD CONSTRAINT profiles_message_price
    CHECK (default_message_price_minor IS NULL OR default_message_price_minor BETWEEN 100 AND 100000);
ALTER TABLE creator.profiles DROP CONSTRAINT IF EXISTS profiles_message_currency;
ALTER TABLE creator.profiles ADD CONSTRAINT profiles_message_currency
    CHECK (message_currency ~ '^[A-Z]{3}$');

ALTER TABLE messaging.conversations
    ADD COLUMN IF NOT EXISTS status text NOT NULL DEFAULT 'accepted',
    ADD COLUMN IF NOT EXISTS initiated_by uuid REFERENCES identity.users (id) ON DELETE SET NULL,
    ADD COLUMN IF NOT EXISTS requested_at timestamptz,
    ADD COLUMN IF NOT EXISTS responded_at timestamptz;
ALTER TABLE messaging.conversations DROP CONSTRAINT IF EXISTS conversations_status;
ALTER TABLE messaging.conversations ADD CONSTRAINT conversations_status
    CHECK (status IN ('requested', 'accepted', 'rejected', 'blocked'));

CREATE INDEX IF NOT EXISTS conversations_request_idx
    ON messaging.conversations (creator_id, requested_at DESC) WHERE status = 'requested';

CREATE TABLE IF NOT EXISTS messaging.blocks (
    blocker_id uuid NOT NULL REFERENCES identity.users (id) ON DELETE CASCADE,
    blocked_id uuid NOT NULL REFERENCES identity.users (id) ON DELETE CASCADE,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (blocker_id, blocked_id),
    CONSTRAINT blocks_no_self CHECK (blocker_id <> blocked_id)
);

ALTER TABLE messaging.messages
    ADD COLUMN IF NOT EXISTS price_minor bigint,
    ADD COLUMN IF NOT EXISTS currency char(3),
    ADD COLUMN IF NOT EXISTS unlock_required boolean NOT NULL DEFAULT false;
ALTER TABLE messaging.messages DROP CONSTRAINT IF EXISTS messages_price;
ALTER TABLE messaging.messages ADD CONSTRAINT messages_price CHECK (
    (unlock_required AND price_minor BETWEEN 100 AND 100000 AND currency IS NOT NULL)
    OR (NOT unlock_required AND price_minor IS NULL AND currency IS NULL)
);

CREATE TABLE IF NOT EXISTS messaging.message_unlocks (
    id                uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    message_id        uuid        NOT NULL REFERENCES messaging.messages (id) ON DELETE CASCADE,
    fan_id            uuid        NOT NULL REFERENCES identity.users (id) ON DELETE CASCADE,
    payment_intent_id uuid        NOT NULL REFERENCES payments.intents (id) ON DELETE RESTRICT,
    created_at        timestamptz NOT NULL DEFAULT now(),
    revoked_at        timestamptz,
    CONSTRAINT message_unlocks_intent UNIQUE (payment_intent_id)
);
CREATE UNIQUE INDEX IF NOT EXISTS message_unlocks_live_idx
    ON messaging.message_unlocks (message_id, fan_id) WHERE revoked_at IS NULL;
CREATE INDEX IF NOT EXISTS message_unlocks_history_idx
    ON messaging.message_unlocks (message_id, created_at DESC);

CREATE TABLE IF NOT EXISTS messaging.request_attachments (
    message_id  uuid        PRIMARY KEY REFERENCES messaging.messages (id) ON DELETE CASCADE,
    media_id    uuid        NOT NULL REFERENCES media.objects (id) ON DELETE RESTRICT,
    purge_after timestamptz,
    created_at  timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS request_attachment_purge_idx
    ON messaging.request_attachments (purge_after) WHERE purge_after IS NOT NULL;

ALTER TABLE payments.intents
    ADD COLUMN IF NOT EXISTS message_id uuid REFERENCES messaging.messages (id) ON DELETE SET NULL;
ALTER TABLE payments.intents DROP CONSTRAINT IF EXISTS intents_purpose;
ALTER TABLE payments.intents ADD CONSTRAINT intents_purpose CHECK (
    purpose IN ('subscription', 'ppv', 'topup', 'tip', 'message'));
ALTER TABLE payments.intents DROP CONSTRAINT IF EXISTS intents_target;
ALTER TABLE payments.intents ADD CONSTRAINT intents_target CHECK (
    (purpose = 'subscription' AND tier_id IS NOT NULL AND post_id IS NULL AND message_id IS NULL) OR
    (purpose = 'ppv' AND post_id IS NOT NULL AND tier_id IS NULL AND message_id IS NULL) OR
    (purpose = 'topup' AND tier_id IS NULL AND post_id IS NULL AND message_id IS NULL AND creator_id IS NULL) OR
    (purpose = 'tip' AND tier_id IS NULL AND post_id IS NULL AND message_id IS NULL AND creator_id IS NOT NULL) OR
    (purpose = 'message' AND message_id IS NOT NULL AND tier_id IS NULL AND post_id IS NULL AND creator_id IS NOT NULL)
);
CREATE INDEX IF NOT EXISTS intents_message_idx ON payments.intents (message_id) WHERE message_id IS NOT NULL;

DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'content_api') THEN
        EXECUTE 'GRANT SELECT, INSERT, UPDATE ON messaging.message_unlocks TO content_api';
        EXECUTE 'GRANT SELECT, INSERT, UPDATE, DELETE ON messaging.request_attachments TO content_api';
        EXECUTE 'GRANT SELECT, INSERT, DELETE ON messaging.blocks TO content_api';
    END IF;
END $$;
