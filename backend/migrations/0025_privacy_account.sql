-- 0025: privacy consents, account export support and scheduled deletion.
--
-- Deletion anonymizes personal fields instead of deleting ledger/payment history,
-- which must remain for financial audit. A seven-day grace period allows the
-- owner to cancel before the worker executes it.

CREATE SCHEMA IF NOT EXISTS privacy;

CREATE TABLE IF NOT EXISTS privacy.consents (
    user_id    uuid        NOT NULL REFERENCES identity.users (id) ON DELETE CASCADE,
    kind       text        NOT NULL,
    version    text        NOT NULL DEFAULT '2026-11',
    granted_at timestamptz NOT NULL DEFAULT now(),
    revoked_at timestamptz,
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, kind),
    CONSTRAINT consents_kind CHECK (kind IN ('terms', 'privacy', 'marketing', 'analytics')),
    CONSTRAINT consents_version CHECK (char_length(version) BETWEEN 1 AND 32)
);

CREATE TABLE IF NOT EXISTS privacy.deletion_requests (
    id           uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id      uuid        NOT NULL REFERENCES identity.users (id) ON DELETE CASCADE,
    status       text        NOT NULL DEFAULT 'scheduled',
    requested_at timestamptz NOT NULL DEFAULT now(),
    scheduled_for timestamptz NOT NULL,
    cancelled_at timestamptz,
    completed_at timestamptz,
    CONSTRAINT deletion_status CHECK (status IN ('scheduled', 'cancelled', 'completed')),
    CONSTRAINT deletion_shape CHECK (
        (status = 'scheduled' AND cancelled_at IS NULL AND completed_at IS NULL)
        OR (status = 'cancelled' AND cancelled_at IS NOT NULL AND completed_at IS NULL)
        OR (status = 'completed' AND completed_at IS NOT NULL)
    )
);
CREATE UNIQUE INDEX IF NOT EXISTS deletion_one_pending_idx
    ON privacy.deletion_requests (user_id) WHERE status = 'scheduled';
CREATE INDEX IF NOT EXISTS deletion_due_idx
    ON privacy.deletion_requests (scheduled_for) WHERE status = 'scheduled';

DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'content_api') THEN
        EXECUTE 'GRANT USAGE ON SCHEMA privacy TO content_api';
        EXECUTE 'GRANT SELECT, INSERT, UPDATE ON privacy.consents TO content_api';
        EXECUTE 'GRANT SELECT, INSERT, UPDATE ON privacy.deletion_requests TO content_api';
    END IF;
END $$;
