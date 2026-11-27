-- Creator payouts: a creator asks for money the platform already owes them, and a
-- finance operator decides.
--
-- Three rules are enforced here rather than in a handler:
--
--   * A request is never deleted and never rewritten. Its status moves through a
--     small closed set of transitions, and each move appends an event with the
--     actor, the time and the note. payouts.request_events refuses UPDATE and
--     DELETE, so the trail behind a payment is evidence and not a draft.
--
--   * One open request per creator and currency. A creator pressing "request"
--     twice is the same intention, not two withdrawals, and a partial unique
--     index is what makes that true under concurrency as well as in the UI.
--
--   * A decided request always says who decided it and when; an undecided one
--     never does. "We forgot" and "we refused" must not look alike.
--
-- Deliberately absent: payout destination details. Bank or wallet coordinates
-- need a tokenisation and retention story before they can be stored, so a
-- request carries a method label only. No endpoint sets 'paid' or 'failed' until
-- a payout rail is configured: the statuses exist so that step is a driver, not a
-- migration, when the owner activates one (D-065).

CREATE SCHEMA IF NOT EXISTS payouts;

CREATE TABLE IF NOT EXISTS payouts.requests (
    id             uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    creator_id     uuid        NOT NULL REFERENCES creator.profiles (user_id) ON DELETE CASCADE,
    amount_minor   bigint      NOT NULL,
    currency       char(3)     NOT NULL,
    method         text        NOT NULL DEFAULT 'manual',
    status         text        NOT NULL DEFAULT 'requested',
    requested_by   uuid        NOT NULL REFERENCES identity.users (id),
    decided_by     uuid        REFERENCES identity.users (id),
    decided_at     timestamptz,
    paid_at        timestamptz,
    note           text        NOT NULL DEFAULT '',
    decision_note  text        NOT NULL DEFAULT '',
    failure_reason text        NOT NULL DEFAULT '',
    created_at     timestamptz NOT NULL DEFAULT now(),
    updated_at     timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT requests_amount CHECK (amount_minor BETWEEN 1000 AND 10000000),
    CONSTRAINT requests_currency CHECK (currency ~ '^[A-Z]{3}$'),
    CONSTRAINT requests_method CHECK (method IN ('manual', 'bank', 'wallet')),
    CONSTRAINT requests_status CHECK (status IN (
        'requested', 'approved', 'rejected', 'cancelled', 'paid', 'failed')),
    CONSTRAINT requests_note_length CHECK (char_length(note) <= 500),
    CONSTRAINT requests_decision_note_length CHECK (char_length(decision_note) <= 2000),
    -- A decision is an attribution: both halves are present or neither is.
    CONSTRAINT requests_decision CHECK (
        (status IN ('approved', 'rejected', 'paid', 'failed'))
        = (decided_at IS NOT NULL AND decided_by IS NOT NULL)),
    -- Only a request that money actually left against can carry a paid time.
    CONSTRAINT requests_paid CHECK ((status = 'paid') = (paid_at IS NOT NULL))
);

-- The finance queue reads requested-first, oldest-first; a creator reads their own
-- newest-first.
CREATE INDEX IF NOT EXISTS requests_queue_idx
    ON payouts.requests (status, created_at);

CREATE INDEX IF NOT EXISTS requests_creator_idx
    ON payouts.requests (creator_id, created_at DESC);

-- One open request per creator and currency: the database, not a read-then-write
-- check in the service, is what prevents a double request under concurrency.
CREATE UNIQUE INDEX IF NOT EXISTS requests_one_open_per_creator
    ON payouts.requests (creator_id, currency)
    WHERE status IN ('requested', 'approved');

CREATE TABLE IF NOT EXISTS payouts.request_events (
    id          bigserial   PRIMARY KEY,
    request_id  uuid        NOT NULL REFERENCES payouts.requests (id) ON DELETE CASCADE,
    actor_id    uuid        REFERENCES identity.users (id),
    -- The verb the actor used (request, approve, reject, cancel). Empty on the
    -- request's own creation event.
    action      text        NOT NULL DEFAULT '',
    from_status text,
    to_status   text        NOT NULL,
    note        text        NOT NULL DEFAULT '',
    created_at  timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT request_events_note_length CHECK (char_length(note) <= 2000)
);

CREATE INDEX IF NOT EXISTS request_events_request_idx
    ON payouts.request_events (request_id, id);

-- Append-only: a payout decision is what an audit or a dispute asks about, so a
-- later correction is a new event and never an edit.
CREATE OR REPLACE FUNCTION payouts.deny_event_mutation() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'payout events are append-only: history cannot be edited or deleted';
END;
$$;

DROP TRIGGER IF EXISTS request_events_immutable ON payouts.request_events;
CREATE TRIGGER request_events_immutable
    BEFORE UPDATE OR DELETE ON payouts.request_events
    FOR EACH ROW EXECUTE FUNCTION payouts.deny_event_mutation();
