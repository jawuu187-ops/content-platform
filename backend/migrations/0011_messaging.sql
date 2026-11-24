-- Direct messages.
--
-- A conversation is per (creator, fan) pair and is created lazily, so there is no
-- "inbox" row for someone who never wrote. A message belongs to exactly one
-- conversation, carries its sender explicitly (the participant is not inferred from
-- the conversation), and can never be edited or deleted: messages are evidence.

CREATE SCHEMA IF NOT EXISTS messaging;

CREATE TABLE IF NOT EXISTS messaging.conversations (
    id              uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    creator_id      uuid        NOT NULL REFERENCES creator.profiles (user_id) ON DELETE CASCADE,
    fan_id          uuid        NOT NULL REFERENCES identity.users (id) ON DELETE CASCADE,
    created_at      timestamptz NOT NULL DEFAULT now(),
    last_message_at timestamptz,
    CONSTRAINT conversations_no_self CHECK (creator_id <> fan_id),
    -- One thread per pair: opening twice returns the same row instead of forking a
    -- conversation the other side will never see.
    CONSTRAINT conversations_pair UNIQUE (creator_id, fan_id)
);

CREATE TABLE IF NOT EXISTS messaging.messages (
    id              uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    conversation_id uuid        NOT NULL REFERENCES messaging.conversations (id) ON DELETE CASCADE,
    sender_id       uuid        NOT NULL REFERENCES identity.users (id) ON DELETE CASCADE,
    body            text        NOT NULL,
    sent_at         timestamptz NOT NULL DEFAULT now(),
    read_at         timestamptz,
    CONSTRAINT messages_body_length CHECK (char_length(body) BETWEEN 1 AND 4000)
);

-- The only read pattern is "newest N in this thread", and "newest threads first".
CREATE INDEX IF NOT EXISTS messages_thread_idx
    ON messaging.messages (conversation_id, sent_at DESC);

CREATE INDEX IF NOT EXISTS conversations_creator_idx
    ON messaging.conversations (creator_id, last_message_at DESC NULLS LAST);

CREATE INDEX IF NOT EXISTS conversations_fan_idx
    ON messaging.conversations (fan_id, last_message_at DESC NULLS LAST);

-- A message is immutable. Marking it read is allowed; rewriting or deleting it is
-- not, because a message is what a moderation or harassment report is judged on.
CREATE OR REPLACE FUNCTION messaging.deny_message_mutation() RETURNS trigger AS $$
BEGIN
    IF NEW.body <> OLD.body
        OR NEW.sender_id <> OLD.sender_id
        OR NEW.conversation_id <> OLD.conversation_id
        OR NEW.sent_at <> OLD.sent_at THEN
        RAISE EXCEPTION 'messages are append-only: body, sender, conversation and time cannot change';
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS messages_immutable ON messaging.messages;
CREATE TRIGGER messages_immutable
    BEFORE UPDATE ON messaging.messages
    FOR EACH ROW EXECUTE FUNCTION messaging.deny_message_mutation();

CREATE OR REPLACE FUNCTION messaging.deny_message_delete() RETURNS trigger AS $$
BEGIN
    RAISE EXCEPTION 'messages are append-only and cannot be deleted';
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS messages_no_delete ON messaging.messages;
CREATE TRIGGER messages_no_delete
    BEFORE DELETE ON messaging.messages
    FOR EACH ROW EXECUTE FUNCTION messaging.deny_message_delete();
