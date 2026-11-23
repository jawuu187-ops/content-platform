-- 0009_payments_ledger.sql
--
-- A double-entry ledger plus the payment-intent and payment-event records that
-- drive it.
--
-- Rules encoded here, not in application code:
--   * entries are immutable (no UPDATE, no DELETE);
--   * every transaction must balance to zero, checked by a deferred constraint
--     trigger so a multi-leg posting is validated at commit;
--   * (kind, reference) is unique, so replaying a settlement cannot double-post;
--   * a provider event is applied at most once (primary key on provider+event).

CREATE SCHEMA IF NOT EXISTS ledger;
CREATE SCHEMA IF NOT EXISTS payments;

CREATE TABLE IF NOT EXISTS ledger.accounts (
    id         uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    code       text        NOT NULL UNIQUE,
    kind       text        NOT NULL,
    owner_type text        NOT NULL,
    owner_id   uuid,
    currency   char(3)     NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT accounts_kind CHECK (kind IN ('asset', 'liability', 'revenue', 'expense')),
    CONSTRAINT accounts_owner CHECK (owner_type IN ('platform', 'user', 'creator')),
    CONSTRAINT accounts_owner_id CHECK ((owner_type = 'platform') = (owner_id IS NULL)),
    CONSTRAINT accounts_currency CHECK (currency ~ '^[A-Z]{3}$')
);

CREATE TABLE IF NOT EXISTS ledger.transactions (
    id         uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    kind       text        NOT NULL,
    reference  text        NOT NULL,
    currency   char(3)     NOT NULL,
    memo       text        NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT transactions_kind CHECK (kind IN ('subscription', 'ppv', 'refund', 'payout', 'adjustment')),
    -- One posting per (kind, reference): the database is the idempotency guard.
    CONSTRAINT transactions_reference UNIQUE (kind, reference)
);

CREATE TABLE IF NOT EXISTS ledger.entries (
    id             bigserial   PRIMARY KEY,
    transaction_id uuid        NOT NULL REFERENCES ledger.transactions (id) ON DELETE RESTRICT,
    account_id     uuid        NOT NULL REFERENCES ledger.accounts (id) ON DELETE RESTRICT,
    -- Debits are positive, credits negative, so a balanced transaction sums to 0.
    amount_minor   bigint      NOT NULL,
    created_at     timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT entries_nonzero CHECK (amount_minor <> 0)
);

CREATE INDEX IF NOT EXISTS entries_transaction_idx ON ledger.entries (transaction_id);
CREATE INDEX IF NOT EXISTS entries_account_idx ON ledger.entries (account_id, created_at DESC);

-- Ledger rows are append-only. Corrections are new transactions, never edits.
CREATE OR REPLACE FUNCTION ledger.deny_mutation() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'ledger rows are immutable (attempted % on %)', TG_OP, TG_TABLE_NAME;
END;
$$;

DROP TRIGGER IF EXISTS transactions_immutable ON ledger.transactions;
CREATE TRIGGER transactions_immutable
    BEFORE UPDATE OR DELETE ON ledger.transactions
    FOR EACH ROW EXECUTE FUNCTION ledger.deny_mutation();

DROP TRIGGER IF EXISTS entries_immutable ON ledger.entries;
CREATE TRIGGER entries_immutable
    BEFORE UPDATE OR DELETE ON ledger.entries
    FOR EACH ROW EXECUTE FUNCTION ledger.deny_mutation();

-- Balance is checked once per transaction, at commit time.
CREATE OR REPLACE FUNCTION ledger.assert_balanced() RETURNS trigger
LANGUAGE plpgsql AS $$
DECLARE total bigint;
BEGIN
    SELECT COALESCE(sum(amount_minor), 0) INTO total
    FROM ledger.entries WHERE transaction_id = NEW.transaction_id;
    IF total <> 0 THEN
        RAISE EXCEPTION 'ledger transaction % is unbalanced by % minor units',
            NEW.transaction_id, total;
    END IF;
    RETURN NULL;
END;
$$;

DROP TRIGGER IF EXISTS entries_balanced ON ledger.entries;
CREATE CONSTRAINT TRIGGER entries_balanced
    AFTER INSERT ON ledger.entries
    DEFERRABLE INITIALLY DEFERRED
    FOR EACH ROW EXECUTE FUNCTION ledger.assert_balanced();

-- Payment intents: what the buyer is trying to pay for. The amount and currency
-- come from our price source, never from the request body.
CREATE TABLE IF NOT EXISTS payments.intents (
    id              uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id         uuid        NOT NULL REFERENCES identity.users (id) ON DELETE CASCADE,
    purpose         text        NOT NULL,
    creator_id      uuid        REFERENCES creator.profiles (user_id) ON DELETE SET NULL,
    tier_id         uuid        REFERENCES creator.tiers (id) ON DELETE SET NULL,
    post_id         uuid        REFERENCES creator.posts (id) ON DELETE SET NULL,
    amount_minor    bigint      NOT NULL,
    currency        char(3)     NOT NULL,
    provider        text        NOT NULL,
    provider_ref    text,
    status          text        NOT NULL DEFAULT 'created',
    idempotency_key text        NOT NULL,
    checkout_url    text        NOT NULL DEFAULT '',
    failure_reason  text        NOT NULL DEFAULT '',
    created_at      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now(),
    settled_at      timestamptz,
    CONSTRAINT intents_purpose CHECK (purpose IN ('subscription', 'ppv', 'topup')),
    CONSTRAINT intents_status CHECK (status IN ('created', 'pending', 'processing', 'succeeded', 'failed', 'canceled', 'refunded')),
    CONSTRAINT intents_amount CHECK (amount_minor BETWEEN 100 AND 10000000),
    CONSTRAINT intents_currency CHECK (currency ~ '^[A-Z]{3}$'),
    CONSTRAINT intents_target CHECK (
        (purpose = 'subscription' AND tier_id IS NOT NULL AND post_id IS NULL) OR
        (purpose = 'ppv' AND post_id IS NOT NULL AND tier_id IS NULL) OR
        (purpose = 'topup' AND tier_id IS NULL AND post_id IS NULL)
    ),
    CONSTRAINT intents_idempotency UNIQUE (user_id, idempotency_key)
);

-- A provider reference identifies at most one intent, so a webhook cannot be
-- replayed against a different purchase.
CREATE UNIQUE INDEX IF NOT EXISTS intents_provider_ref_idx
    ON payments.intents (provider, provider_ref) WHERE provider_ref IS NOT NULL;
CREATE INDEX IF NOT EXISTS intents_user_idx ON payments.intents (user_id, created_at DESC);

CREATE TABLE IF NOT EXISTS payments.events (
    provider     text        NOT NULL,
    event_id     text        NOT NULL,
    kind         text        NOT NULL,
    intent_id    uuid        REFERENCES payments.intents (id) ON DELETE SET NULL,
    payload_hash text        NOT NULL,
    received_at  timestamptz NOT NULL DEFAULT now(),
    applied_at   timestamptz,
    PRIMARY KEY (provider, event_id)
);

CREATE TABLE IF NOT EXISTS payments.refunds (
    id           uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    intent_id    uuid        NOT NULL REFERENCES payments.intents (id) ON DELETE RESTRICT,
    amount_minor bigint      NOT NULL,
    reason       text        NOT NULL,
    actor_id     uuid        NOT NULL REFERENCES identity.users (id) ON DELETE RESTRICT,
    provider_ref text        NOT NULL DEFAULT '',
    created_at   timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT refunds_amount CHECK (amount_minor > 0)
);

CREATE INDEX IF NOT EXISTS refunds_intent_idx ON payments.refunds (intent_id);

-- Link purchases back to the grants they paid for, so a refund can revoke
-- exactly the entitlement it created and nothing else.
ALTER TABLE creator.subscriptions
    ADD COLUMN IF NOT EXISTS payment_intent_id uuid REFERENCES payments.intents (id) ON DELETE SET NULL;
CREATE UNIQUE INDEX IF NOT EXISTS subscriptions_intent_idx
    ON creator.subscriptions (payment_intent_id) WHERE payment_intent_id IS NOT NULL;

ALTER TABLE creator.post_unlocks
    ADD COLUMN IF NOT EXISTS payment_intent_id uuid REFERENCES payments.intents (id) ON DELETE SET NULL;
CREATE UNIQUE INDEX IF NOT EXISTS unlocks_intent_idx
    ON creator.post_unlocks (payment_intent_id) WHERE payment_intent_id IS NOT NULL;
