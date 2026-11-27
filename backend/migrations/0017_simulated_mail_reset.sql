-- 0017: simulated email mailbox and password reset.
--
-- The mailbox is a development/test facility. It stores exactly what would have
-- been sent so a developer can complete verification or reset flows without a
-- provider, DNS change or real delivery. It is never mounted on a public API,
-- and the application refuses MAIL_PROVIDER=simulated outside local/demo.

CREATE SCHEMA IF NOT EXISTS mail;

CREATE TABLE IF NOT EXISTS mail.simulated_messages (
    id          uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    recipient   text        NOT NULL,
    subject     text        NOT NULL,
    body        text        NOT NULL,
    kind        text        NOT NULL DEFAULT 'transactional',
    created_at  timestamptz NOT NULL DEFAULT now(),
    read_at     timestamptz
);

CREATE INDEX IF NOT EXISTS simulated_messages_created_idx
    ON mail.simulated_messages (created_at DESC);

CREATE TABLE IF NOT EXISTS identity.password_resets (
    id          uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id     uuid        NOT NULL REFERENCES identity.users (id) ON DELETE CASCADE,
    token_hash  bytea       NOT NULL UNIQUE,
    expires_at  timestamptz NOT NULL,
    used_at     timestamptz,
    created_at  timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS password_resets_user_idx
    ON identity.password_resets (user_id, created_at DESC);

-- Only the latest token for an account can be live. Older rows remain as an
-- audit trail but are marked used by the request path.
CREATE UNIQUE INDEX IF NOT EXISTS password_resets_one_live_idx
    ON identity.password_resets (user_id) WHERE used_at IS NULL;

DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'content_api') THEN
        EXECUTE 'GRANT USAGE ON SCHEMA mail TO content_api';
        EXECUTE 'GRANT SELECT, INSERT, UPDATE ON mail.simulated_messages TO content_api';
        EXECUTE 'GRANT SELECT, INSERT, UPDATE ON identity.password_resets TO content_api';
    END IF;
END $$;
