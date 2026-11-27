-- 0020: metadata-only creator identity verification.
--
-- Privacy rule: no identity document, selfie, scan, image, blob or face
-- template is accepted or stored. The reviewer establishes identity in the
-- existing live/in-person process; this table records the outcome and the
-- minimum eligibility metadata needed to decide it.

ALTER TABLE creator.profiles
    ADD COLUMN IF NOT EXISTS kyc_status text NOT NULL DEFAULT 'unverified';

ALTER TABLE creator.profiles DROP CONSTRAINT IF EXISTS profiles_kyc_status;
ALTER TABLE creator.profiles ADD CONSTRAINT profiles_kyc_status
    CHECK (kyc_status IN ('unverified', 'pending', 'verified', 'rejected', 'expired'));

CREATE TABLE IF NOT EXISTS creator.verification_requests (
    id            uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id       uuid        NOT NULL REFERENCES creator.profiles (user_id) ON DELETE CASCADE,
    status        text        NOT NULL DEFAULT 'pending',
    legal_name    text        NOT NULL,
    country_code  char(2)     NOT NULL,
    date_of_birth date        NOT NULL,
    attestation   boolean     NOT NULL DEFAULT false,
    reviewer_id   uuid        REFERENCES identity.users (id) ON DELETE SET NULL,
    reason_code   text        NOT NULL DEFAULT '',
    created_at    timestamptz NOT NULL DEFAULT now(),
    decided_at    timestamptz,
    expires_at    timestamptz,
    CONSTRAINT creator_verify_status CHECK (status IN ('pending', 'approved', 'rejected', 'revoked')),
    CONSTRAINT creator_verify_name CHECK (char_length(btrim(legal_name)) BETWEEN 2 AND 120),
    CONSTRAINT creator_verify_country CHECK (country_code ~ '^[A-Z]{2}$'),
    CONSTRAINT creator_verify_attest CHECK (attestation),
    CONSTRAINT creator_verify_shape CHECK (
        (status = 'pending' AND decided_at IS NULL AND reviewer_id IS NULL AND reason_code = '')
        OR (status <> 'pending' AND decided_at IS NOT NULL AND reviewer_id IS NOT NULL)
    ),
    CONSTRAINT creator_verify_reason CHECK (reason_code IN (
        '', 'identity_mismatch', 'age_not_established', 'sanctions_or_fraud',
        'incomplete_presentation', 'other'
    ))
);

CREATE UNIQUE INDEX IF NOT EXISTS creator_verify_one_pending_idx
    ON creator.verification_requests (user_id) WHERE status = 'pending';
CREATE INDEX IF NOT EXISTS creator_verify_queue_idx
    ON creator.verification_requests (status, created_at);
CREATE INDEX IF NOT EXISTS creator_verify_user_idx
    ON creator.verification_requests (user_id, created_at DESC);

-- Fail the migration if a document-bearing column is ever introduced into the
-- verification table. This protects the approved no-ID-storage decision.
DO $$
DECLARE offending text;
BEGIN
    SELECT string_agg(column_name, ', ') INTO offending
    FROM information_schema.columns
    WHERE table_schema = 'creator' AND table_name = 'verification_requests'
      AND (data_type = 'bytea'
           OR column_name ~* '(document|selfie|image|photo|scan|attachment|blob|portrait)');
    IF offending IS NOT NULL THEN
        RAISE EXCEPTION '0020: creator verification must not store documents: %', offending;
    END IF;
END $$;

DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'content_api') THEN
        EXECUTE 'GRANT SELECT, INSERT, UPDATE ON creator.verification_requests TO content_api';
    END IF;
END $$;
