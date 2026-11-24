-- Safety reports and the moderation queue.
--
-- A report is a person saying "this should be looked at". It is never deleted and it is
-- never edited: its status changes, and every change is appended to moderation.report_events
-- with who did it, when, and why. That trail is what an appeal, a regulator or a court asks
-- for, so the events table refuses UPDATE and DELETE at the database level.
--
-- Nothing here deletes the reported content. A moderator decides; a separate, audited
-- removal path takes content down, and this table only records the decision.

CREATE SCHEMA IF NOT EXISTS moderation;

CREATE TABLE IF NOT EXISTS moderation.reports (
    id            uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    reporter_id   uuid        NOT NULL REFERENCES identity.users (id) ON DELETE CASCADE,
    subject_type  text        NOT NULL,
    subject_id    uuid        NOT NULL,
    reason        text        NOT NULL,
    details       text        NOT NULL DEFAULT '',
    status        text        NOT NULL DEFAULT 'open',
    created_at    timestamptz NOT NULL DEFAULT now(),
    updated_at    timestamptz NOT NULL DEFAULT now(),
    resolved_at   timestamptz,
    resolved_by   uuid        REFERENCES identity.users (id),
    CONSTRAINT reports_subject_type CHECK (subject_type IN ('user', 'post', 'message')),
    CONSTRAINT reports_reason CHECK (reason IN (
        'harassment', 'nonconsensual', 'minor', 'impersonation', 'spam', 'payment', 'other')),
    CONSTRAINT reports_details_length CHECK (char_length(details) <= 2000),
    CONSTRAINT reports_status CHECK (status IN ('open', 'reviewing', 'actioned', 'dismissed')),
    -- A closed report always says when, and an open one never does. This is the
    -- difference between "we forgot" and "we decided".
    CONSTRAINT reports_resolution CHECK ((status IN ('actioned', 'dismissed')) = (resolved_at IS NOT NULL))
);

-- The queue reads oldest-open-first; a reporter reads their own newest-first.
CREATE INDEX IF NOT EXISTS reports_queue_idx
    ON moderation.reports (status, created_at);

CREATE INDEX IF NOT EXISTS reports_reporter_idx
    ON moderation.reports (reporter_id, created_at DESC);

CREATE INDEX IF NOT EXISTS reports_subject_idx
    ON moderation.reports (subject_type, subject_id);

-- One open report per person per subject: pressing "report" twice on the same message is
-- the same complaint, not a second one, and the submitter is told so.
CREATE UNIQUE INDEX IF NOT EXISTS reports_one_open_per_reporter
    ON moderation.reports (reporter_id, subject_type, subject_id)
    WHERE status IN ('open', 'reviewing');

CREATE TABLE IF NOT EXISTS moderation.report_events (
    id          bigserial   PRIMARY KEY,
    report_id   uuid        NOT NULL REFERENCES moderation.reports (id) ON DELETE CASCADE,
    actor_id    uuid        REFERENCES identity.users (id),
    from_status text,
    to_status   text        NOT NULL,
    note        text        NOT NULL DEFAULT '',
    created_at  timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT report_events_note_length CHECK (char_length(note) <= 2000)
);

CREATE INDEX IF NOT EXISTS report_events_report_idx
    ON moderation.report_events (report_id, id);

-- Append-only: the trail is evidence, so a later "correction" cannot rewrite it. A status
-- that was set wrongly is fixed by adding an event, never by editing one.
CREATE OR REPLACE FUNCTION moderation.deny_event_mutation() RETURNS trigger AS $$
BEGIN
    RAISE EXCEPTION 'report events are append-only: history cannot be edited or deleted';
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS report_events_immutable ON moderation.report_events;
CREATE TRIGGER report_events_immutable
    BEFORE UPDATE OR DELETE ON moderation.report_events
    FOR EACH ROW EXECUTE FUNCTION moderation.deny_event_mutation();
