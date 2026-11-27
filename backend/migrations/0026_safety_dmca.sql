-- 0026: CSAM reason, urgent escalation and copyright claims.
--
-- No evidence files are accepted. A report or claim stores text references only;
-- moderators use the existing immutable report/event trail. A CSAM report is
-- marked urgent at submission and notifies every owner immediately.

ALTER TABLE moderation.reports DROP CONSTRAINT IF EXISTS reports_reason;
ALTER TABLE moderation.reports ADD CONSTRAINT reports_reason CHECK (reason IN (
    'harassment', 'nonconsensual', 'minor', 'csam', 'impersonation', 'spam', 'payment', 'other'));

CREATE TABLE IF NOT EXISTS moderation.copyright_claims (
    id              uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    claimant_id     uuid        NOT NULL REFERENCES identity.users (id) ON DELETE CASCADE,
    target_type     text        NOT NULL,
    target_id       uuid        NOT NULL,
    work_description text       NOT NULL,
    original_url    text        NOT NULL DEFAULT '',
    status          text        NOT NULL DEFAULT 'open',
    counter_notice  text        NOT NULL DEFAULT '',
    reviewer_id     uuid        REFERENCES identity.users (id),
    decision_note   text        NOT NULL DEFAULT '',
    created_at      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now(),
    decided_at      timestamptz,
    CONSTRAINT copyright_target CHECK (target_type IN ('post', 'message')),
    CONSTRAINT copyright_status CHECK (status IN ('open', 'reviewing', 'takedown', 'rejected', 'counter_notice')),
    CONSTRAINT copyright_work CHECK (char_length(btrim(work_description)) BETWEEN 10 AND 2000),
    CONSTRAINT copyright_url CHECK (char_length(original_url) <= 500),
    CONSTRAINT copyright_counter CHECK (char_length(counter_notice) <= 2000),
    CONSTRAINT copyright_decision CHECK (char_length(decision_note) <= 2000),
    CONSTRAINT copyright_shape CHECK (
        (status IN ('open', 'reviewing') AND decided_at IS NULL)
        OR (status IN ('takedown', 'rejected') AND decided_at IS NOT NULL)
        OR status = 'counter_notice')
);
CREATE INDEX IF NOT EXISTS copyright_queue_idx ON moderation.copyright_claims (status, created_at);

-- Urgent CSAM reports notify every owner through the existing outbox. The
-- notification deliberately contains no reported content.
CREATE OR REPLACE FUNCTION notifications.on_csam_report() RETURNS trigger
LANGUAGE plpgsql AS $$
DECLARE owner_id uuid;
BEGIN
    IF NEW.reason <> 'csam' THEN
        RETURN NEW;
    END IF;
    FOR owner_id IN SELECT user_id FROM identity.role_grants WHERE role = 'owner' LOOP
        PERFORM notifications.enqueue(
            'csam:' || NEW.id || ':' || owner_id, 'system', owner_id,
            jsonb_build_object(
                'title', 'URGENT: possible CSAM report',
                'body', 'Open the safety queue immediately. Report ' || NEW.id,
                'link', '/moderation'));
    END LOOP;
    RETURN NEW;
END $$;
DROP TRIGGER IF EXISTS csam_report_urgent ON moderation.reports;
CREATE TRIGGER csam_report_urgent AFTER INSERT ON moderation.reports
FOR EACH ROW EXECUTE FUNCTION notifications.on_csam_report();

DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'content_api') THEN
        EXECUTE 'GRANT SELECT, INSERT, UPDATE ON moderation.copyright_claims TO content_api';
    END IF;
END $$;
