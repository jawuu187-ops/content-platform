-- 0005_webauthn.sql
--
-- Passkeys (WebAuthn). Two tables:
--
--   webauthn_credentials  one row per registered authenticator. The credential id
--                         is globally unique because an authenticator scopes
--                         credentials per RP, and we must be able to resolve a
--                         discoverable login (no email supplied) from the id.
--   webauthn_challenges   short-lived, single-use challenges. A challenge row is
--                         consumed exactly once under a row lock, which is what
--                         makes replay impossible.

CREATE TABLE IF NOT EXISTS identity.webauthn_credentials (
    id              uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id         uuid        NOT NULL REFERENCES identity.users (id) ON DELETE CASCADE,
    credential_id   bytea       NOT NULL UNIQUE,
    public_key_cose bytea       NOT NULL,
    alg             integer     NOT NULL,
    sign_count      bigint      NOT NULL DEFAULT 0,
    aaguid          uuid,
    transports      text[]      NOT NULL DEFAULT '{}',
    label           text        NOT NULL DEFAULT '',
    created_at      timestamptz NOT NULL DEFAULT now(),
    last_used_at    timestamptz,
    revoked_at      timestamptz,
    CONSTRAINT webauthn_credentials_id_len CHECK (octet_length(credential_id) BETWEEN 16 AND 1023)
);

CREATE INDEX IF NOT EXISTS webauthn_credentials_user_idx
    ON identity.webauthn_credentials (user_id)
    WHERE revoked_at IS NULL;

CREATE TABLE IF NOT EXISTS identity.webauthn_challenges (
    id          uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    kind        text        NOT NULL CHECK (kind IN ('register', 'login')),
    user_id     uuid        REFERENCES identity.users (id) ON DELETE CASCADE,
    challenge   bytea       NOT NULL,
    expires_at  timestamptz NOT NULL,
    consumed_at timestamptz,
    created_at  timestamptz NOT NULL DEFAULT now(),
    -- A registration challenge is always bound to a user; a login challenge may
    -- be unbound so a discoverable credential can identify the account.
    CONSTRAINT webauthn_challenges_binding CHECK (kind = 'login' OR user_id IS NOT NULL)
);

CREATE INDEX IF NOT EXISTS webauthn_challenges_live_idx
    ON identity.webauthn_challenges (id)
    WHERE consumed_at IS NULL;
