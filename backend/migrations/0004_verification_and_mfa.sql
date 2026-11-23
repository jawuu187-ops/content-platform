-- 0004_verification_and_mfa.sql
--
-- Adds attempt accounting to email verification codes and a table for MFA
-- recovery codes.
--
-- Recovery codes are single-use, stored hashed and marked used rather than
-- deleted, so an account owner can see when a code was consumed.

ALTER TABLE identity.email_verifications
    ADD COLUMN IF NOT EXISTS attempts integer NOT NULL DEFAULT 0;

-- A code is only usable while it is unconsumed, unexpired and under the attempt
-- cap. The partial index keeps the lookup cheap without indexing dead rows.
CREATE INDEX IF NOT EXISTS email_verifications_live_idx
    ON identity.email_verifications (user_id, expires_at)
    WHERE consumed_at IS NULL;

CREATE TABLE IF NOT EXISTS identity.recovery_codes (
    id         uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id    uuid        NOT NULL REFERENCES identity.users (id) ON DELETE CASCADE,
    code_hash  bytea       NOT NULL,
    used_at    timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (user_id, code_hash)
);

CREATE INDEX IF NOT EXISTS recovery_codes_user_idx
    ON identity.recovery_codes (user_id) WHERE used_at IS NULL;

-- A TOTP factor created for a user must not silently duplicate: one confirmed
-- factor per kind keeps "which secret do I check" unambiguous.
CREATE UNIQUE INDEX IF NOT EXISTS mfa_factors_one_confirmed_totp
    ON identity.mfa_factors (user_id)
    WHERE kind = 'totp' AND confirmed_at IS NOT NULL;
