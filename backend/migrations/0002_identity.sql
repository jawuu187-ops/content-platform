-- 0002_identity.sql
-- Identity, sessions, devices and roles.
--
-- Design notes
--   * Passwords are stored as a PHC-style string with the algorithm and its
--     parameters, so a future algorithm change can be rolled out per user
--     (verify -> rehash on next successful login). No plaintext or reversible
--     material is ever stored.
--   * Sessions are opaque: the client holds a random access token (15 min) and a
--     random refresh token (30 days). Only SHA-256 hashes are stored, so a
--     database leak does not hand out live sessions. Tokens are revoked by
--     flipping revoked_at; nothing is deleted.
--   * Refresh tokens belong to a family. Rotating a refresh token revokes the
--     old row and links it to its replacement. Presenting an already-rotated
--     token means the token leaked, so the whole family is revoked.
--   * Email is citext so case cannot create duplicate accounts; a unique index
--     enforces it in the database, not only in application code.

CREATE TABLE identity.users (
    id                uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    email             citext      NOT NULL UNIQUE,
    password_hash     text        NOT NULL,
    password_algo     text        NOT NULL DEFAULT 'pbkdf2-sha256',
    password_params   jsonb       NOT NULL DEFAULT '{}'::jsonb,
    email_verified_at timestamptz,
    status            text        NOT NULL DEFAULT 'active'
        CHECK (status IN ('active', 'suspended', 'deleted')),
    is_creator        boolean     NOT NULL DEFAULT false,
    created_at        timestamptz NOT NULL DEFAULT now(),
    updated_at        timestamptz NOT NULL DEFAULT now(),
    deleted_at        timestamptz
);

CREATE INDEX users_status_idx ON identity.users (status) WHERE deleted_at IS NULL;

DROP TRIGGER IF EXISTS users_updated_at ON identity.users;
CREATE TRIGGER users_updated_at
    BEFORE UPDATE ON identity.users
    FOR EACH ROW EXECUTE FUNCTION ops.set_updated_at();

-- Roles are explicit grants, never inferred from a user-editable column.
CREATE TABLE identity.role_grants (
    user_id    uuid        NOT NULL REFERENCES identity.users (id) ON DELETE CASCADE,
    role       text        NOT NULL
        CHECK (role IN ('fan', 'creator', 'creator_team', 'moderator', 'finance', 'support', 'admin')),
    granted_at timestamptz NOT NULL DEFAULT now(),
    granted_by text        NOT NULL DEFAULT 'system',
    PRIMARY KEY (user_id, role)
);

CREATE TABLE identity.devices (
    id            uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id       uuid        NOT NULL REFERENCES identity.users (id) ON DELETE CASCADE,
    fingerprint   text        NOT NULL,
    platform      text        NOT NULL DEFAULT 'unknown'
        CHECK (platform IN ('android', 'web', 'admin', 'unknown')),
    name          text,
    trusted       boolean     NOT NULL DEFAULT false,
    first_seen_at timestamptz NOT NULL DEFAULT now(),
    last_seen_at  timestamptz NOT NULL DEFAULT now(),
    UNIQUE (user_id, fingerprint)
);

CREATE TABLE identity.sessions (
    id                uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id           uuid        NOT NULL REFERENCES identity.users (id) ON DELETE CASCADE,
    family_id         uuid        NOT NULL,
    device_id         uuid        REFERENCES identity.devices (id) ON DELETE SET NULL,
    access_hash       bytea       NOT NULL UNIQUE,
    access_expires_at timestamptz NOT NULL,
    refresh_hash      bytea       NOT NULL UNIQUE,
    refresh_expires_at timestamptz NOT NULL,
    user_agent        text,
    ip                inet,
    created_at        timestamptz NOT NULL DEFAULT now(),
    last_used_at      timestamptz NOT NULL DEFAULT now(),
    revoked_at        timestamptz,
    revoked_reason    text,
    replaced_by       uuid        REFERENCES identity.sessions (id) ON DELETE SET NULL
);

CREATE INDEX sessions_user_idx ON identity.sessions (user_id, created_at DESC);
CREATE INDEX sessions_family_idx ON identity.sessions (family_id);
CREATE INDEX sessions_active_idx ON identity.sessions (user_id) WHERE revoked_at IS NULL;

-- One-time email verification codes. Only the hash is stored.
CREATE TABLE identity.email_verifications (
    id          uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id     uuid        NOT NULL REFERENCES identity.users (id) ON DELETE CASCADE,
    code_hash   bytea       NOT NULL UNIQUE,
    expires_at  timestamptz NOT NULL,
    consumed_at timestamptz,
    created_at  timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX email_verifications_user_idx ON identity.email_verifications (user_id);

-- TOTP secrets are stored encrypted by the application (AES-GCM with a key from
-- the secret store); the column never holds a plaintext shared secret.
CREATE TABLE identity.mfa_factors (
    id           uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id      uuid        NOT NULL REFERENCES identity.users (id) ON DELETE CASCADE,
    kind         text        NOT NULL CHECK (kind IN ('totp', 'webauthn')),
    label        text,
    secret_cipher bytea,
    credential   jsonb,
    confirmed_at timestamptz,
    last_used_at timestamptz,
    created_at   timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX mfa_factors_user_idx ON identity.mfa_factors (user_id);

-- Guard: a session may never be un-revoked. Revocation is final and auditable.
CREATE OR REPLACE FUNCTION identity.sessions_revocation_is_final() RETURNS trigger
    LANGUAGE plpgsql
AS $$
BEGIN
    IF OLD.revoked_at IS NOT NULL AND NEW.revoked_at IS NULL THEN
        RAISE EXCEPTION 'identity.sessions: revocation cannot be undone';
    END IF;
    RETURN NEW;
END;
$$;

DROP TRIGGER IF EXISTS sessions_revocation_final ON identity.sessions;
CREATE TRIGGER sessions_revocation_final
    BEFORE UPDATE ON identity.sessions
    FOR EACH ROW EXECUTE FUNCTION identity.sessions_revocation_is_final();
