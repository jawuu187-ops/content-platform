-- 0016_age_review.sql
--
-- Manual age-verification review.
--
-- Privacy rule: this schema stores outcomes and references only. There is no
-- column for an identity document, a selfie, a scan, a face template or a date
-- of birth, and no upload endpoint exists. A reviewer sees a live presentation
-- in the app's workflow and records a decision code; nothing visual is retained.

CREATE TABLE IF NOT EXISTS verify.review_requests (
    id           uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id      uuid        NOT NULL REFERENCES identity.users (id) ON DELETE CASCADE,
    status       text        NOT NULL DEFAULT 'pending',
    method       text        NOT NULL DEFAULT 'manual_review',
    reviewer_id  uuid        REFERENCES identity.users (id) ON DELETE SET NULL,
    reason_code  text        NOT NULL DEFAULT '',
    check_id     uuid        REFERENCES verify.checks (id) ON DELETE SET NULL,
    audit_ref    text        NOT NULL DEFAULT '',
    requested_at timestamptz NOT NULL DEFAULT now(),
    decided_at   timestamptz,
    expires_at   timestamptz,
    CONSTRAINT review_status CHECK (status IN ('pending', 'approved', 'rejected', 'revoked')),
    CONSTRAINT review_method CHECK (method = 'manual_review'),
    CONSTRAINT review_shape CHECK (
        (status = 'pending' AND decided_at IS NULL AND reviewer_id IS NULL)
        OR (status <> 'pending' AND decided_at IS NOT NULL AND reviewer_id IS NOT NULL)
    ),
    CONSTRAINT review_reason CHECK (
        (status IN ('rejected', 'revoked') AND reason_code <> '')
        OR (status IN ('pending', 'approved') AND reason_code = '')
    ),
    CONSTRAINT review_reason_code CHECK (reason_code IN (
        '', 'age_not_established', 'identity_mismatch', 'presentation_failed',
        'suspected_fraud', 'other'
    ))
);

-- At most one open request per account. A closed request stays as history.
CREATE UNIQUE INDEX IF NOT EXISTS review_one_pending_idx
    ON verify.review_requests (user_id) WHERE status = 'pending';

CREATE INDEX IF NOT EXISTS review_queue_idx
    ON verify.review_requests (status, requested_at);

CREATE INDEX IF NOT EXISTS review_user_idx
    ON verify.review_requests (user_id, requested_at DESC);

-- No document-bearing columns may ever be added to the verification schema.
-- This guard fails the migration if someone introduces an image, blob or bytea
-- column into verify.*; the check runs after the tables above exist.
DO $$
DECLARE
    offending text;
BEGIN
    SELECT string_agg(table_name || '.' || column_name, ', ')
      INTO offending
      FROM information_schema.columns
     WHERE table_schema = 'verify'
       AND (data_type = 'bytea'
            OR column_name ~* '(document|selfie|image|photo|scan|attachment|blob|portrait)');
    IF offending IS NOT NULL THEN
        RAISE EXCEPTION '0016: verification schema must not store documents: %', offending;
    END IF;
END $$;
