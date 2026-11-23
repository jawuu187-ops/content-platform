-- 0007_age_verification.sql
--
-- Age assurance, provider independent.
--
-- Privacy rule encoded here: we store the *result* of a check, never the
-- evidence. There is no column for a document image, a selfie, a face template
-- or an extracted date of birth, because none of those are needed to answer
-- "is this account old enough". The vendor keeps the evidence; we keep a
-- reference and an assurance level.

CREATE SCHEMA IF NOT EXISTS verify;

CREATE TABLE IF NOT EXISTS verify.checks (
    id            uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id       uuid        NOT NULL REFERENCES identity.users (id) ON DELETE CASCADE,
    status        text        NOT NULL,
    method        text        NOT NULL,
    provider      text        NOT NULL DEFAULT '',
    provider_ref  text        NOT NULL DEFAULT '',
    -- Free-text assurance label from the provider (for example a documented
    -- age-estimation level). Kept for audit, never used as an access rule.
    assurance     text        NOT NULL DEFAULT '',
    min_age       integer     NOT NULL DEFAULT 18,
    verified_at   timestamptz,
    expires_at    timestamptz,
    revoked_at    timestamptz,
    failure_reason text       NOT NULL DEFAULT '',
    created_at    timestamptz NOT NULL DEFAULT now(),
    updated_at    timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT checks_status CHECK (status IN ('pending', 'verified', 'failed', 'expired', 'revoked')),
    CONSTRAINT checks_method CHECK (method IN ('provider', 'manual_review')),
    CONSTRAINT checks_min_age CHECK (min_age BETWEEN 18 AND 21),
    -- A verified row must know when it was verified. verified_at is kept after a
    -- revocation or expiry: it records when the check passed, which is what an
    -- investigation needs.
    CONSTRAINT checks_verified_shape CHECK (status <> 'verified' OR verified_at IS NOT NULL),
    CONSTRAINT checks_revoked_shape CHECK ((status = 'revoked') = (revoked_at IS NOT NULL))
);

-- One live verified check per account. Re-verification supersedes the previous
-- row instead of stacking, so "is this account verified" is a single lookup.
CREATE UNIQUE INDEX IF NOT EXISTS checks_live_verified_idx
    ON verify.checks (user_id) WHERE status = 'verified' AND revoked_at IS NULL;

CREATE INDEX IF NOT EXISTS checks_user_idx
    ON verify.checks (user_id, created_at DESC);

-- Provider references are unique per provider: a duplicated webhook cannot
-- create a second check, and a reference cannot be shared by two accounts.
CREATE UNIQUE INDEX IF NOT EXISTS checks_provider_ref_idx
    ON verify.checks (provider, provider_ref) WHERE provider_ref <> '';

-- Every state transition attempt, for rate limiting and for investigations.
CREATE TABLE IF NOT EXISTS verify.attempts (
    id         bigserial   PRIMARY KEY,
    user_id    uuid        REFERENCES identity.users (id) ON DELETE CASCADE,
    outcome    text        NOT NULL,
    reason     text        NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT attempts_outcome CHECK (outcome IN
        ('started', 'verified', 'failed', 'expired', 'revoked', 'rejected'))
);

CREATE INDEX IF NOT EXISTS attempts_user_idx
    ON verify.attempts (user_id, created_at DESC);

-- Webhook replay protection. A provider event is applied at most once.
CREATE TABLE IF NOT EXISTS verify.callbacks (
    provider    text        NOT NULL,
    event_id    text        NOT NULL,
    received_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (provider, event_id)
);

-- Moderators and support need a readable trail of who revoked what.
CREATE TABLE IF NOT EXISTS verify.revocations (
    id         bigserial   PRIMARY KEY,
    user_id    uuid        NOT NULL REFERENCES identity.users (id) ON DELETE CASCADE,
    check_id   uuid        REFERENCES verify.checks (id) ON DELETE SET NULL,
    actor_id   uuid        REFERENCES identity.users (id) ON DELETE SET NULL,
    reason     text        NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);
