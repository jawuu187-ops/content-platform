-- Tips: money a fan sends to a creator with nothing granted in return.
--
-- A tip has no tier and no post, so the amount cannot come from a price record the way a
-- subscription or an unlock does. It therefore carries the client's amount, bounded by the
-- service (100..500000 minor units) and by the existing intents_amount CHECK. Everything
-- else — intent, provider, settlement, ledger, simulated marking — is the same path a
-- subscription takes, so a tip is test data in exactly the same sense and gets the same
-- simulated stamp.
--
-- This migration is idempotent: every constraint is dropped before it is re-added, so the
-- migration runner can apply it twice (CI proves it).

ALTER TABLE payments.intents ADD COLUMN IF NOT EXISTS note text;

-- A tip note is shown to the creator on the receipt. It is not a message thread, so it is
-- short, and an empty note is stored as NULL rather than an empty string.
ALTER TABLE payments.intents DROP CONSTRAINT IF EXISTS intents_note;
ALTER TABLE payments.intents ADD CONSTRAINT intents_note
    CHECK (note IS NULL OR char_length(note) BETWEEN 1 AND 280);

ALTER TABLE payments.intents DROP CONSTRAINT IF EXISTS intents_purpose;
ALTER TABLE payments.intents ADD CONSTRAINT intents_purpose
    CHECK (purpose IN ('subscription', 'ppv', 'topup', 'tip'));

ALTER TABLE payments.intents DROP CONSTRAINT IF EXISTS intents_target;
ALTER TABLE payments.intents ADD CONSTRAINT intents_target CHECK (
    (purpose = 'subscription' AND tier_id IS NOT NULL AND post_id IS NULL) OR
    (purpose = 'ppv' AND post_id IS NOT NULL AND tier_id IS NULL) OR
    (purpose = 'topup' AND tier_id IS NULL AND post_id IS NULL AND creator_id IS NULL) OR
    (purpose = 'tip' AND tier_id IS NULL AND post_id IS NULL AND creator_id IS NOT NULL)
);

-- A tip is its own ledger kind, so an earnings query can total tips without guessing from
-- the reference. The check is replaced rather than extended in place because PostgreSQL
-- cannot alter a CHECK constraint.
ALTER TABLE ledger.transactions DROP CONSTRAINT IF EXISTS transactions_kind;
ALTER TABLE ledger.transactions ADD CONSTRAINT transactions_kind
    CHECK (kind IN ('subscription', 'ppv', 'tip', 'refund', 'payout', 'adjustment'));

-- Tips are read two ways: by creator over time, and by fan. Both indexes are partial on
-- purpose so they stay small next to the subscription and unlock history.
CREATE INDEX IF NOT EXISTS intents_tip_creator_idx
    ON payments.intents (creator_id, created_at DESC) WHERE purpose = 'tip';
CREATE INDEX IF NOT EXISTS intents_tip_fan_idx
    ON payments.intents (user_id, created_at DESC) WHERE purpose = 'tip';
