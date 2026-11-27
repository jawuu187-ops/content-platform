-- 0019: notification outbox, items and preferences.
--
-- Events are written to the outbox by triggers in the same statement that writes
-- the comment, like, message, subscription or payment. The API does not have to
-- remember to publish them, and a crash between the business write and the
-- notification cannot lose the event. A worker claims rows with SKIP LOCKED.

CREATE SCHEMA IF NOT EXISTS notifications;

CREATE TABLE IF NOT EXISTS notifications.outbox (
    id           bigserial   PRIMARY KEY,
    event_key    text        NOT NULL UNIQUE,
    category     text        NOT NULL,
    recipient_id uuid        NOT NULL REFERENCES identity.users (id) ON DELETE CASCADE,
    payload      jsonb       NOT NULL,
    attempts     integer     NOT NULL DEFAULT 0,
    available_at timestamptz NOT NULL DEFAULT now(),
    locked_at    timestamptz,
    processed_at timestamptz,
    last_error   text        NOT NULL DEFAULT '',
    created_at   timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT outbox_category CHECK (category IN ('engagement', 'messages', 'payments', 'system'))
);

CREATE INDEX IF NOT EXISTS outbox_pending_idx
    ON notifications.outbox (available_at, id) WHERE processed_at IS NULL;

CREATE TABLE IF NOT EXISTS notifications.items (
    id         uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id    uuid        NOT NULL REFERENCES identity.users (id) ON DELETE CASCADE,
    category   text        NOT NULL,
    title      text        NOT NULL,
    body       text        NOT NULL,
    link       text        NOT NULL DEFAULT '',
    read_at    timestamptz,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS items_user_idx
    ON notifications.items (user_id, created_at DESC);
CREATE INDEX IF NOT EXISTS items_unread_idx
    ON notifications.items (user_id) WHERE read_at IS NULL;

CREATE TABLE IF NOT EXISTS notifications.preferences (
    user_id            uuid        PRIMARY KEY REFERENCES identity.users (id) ON DELETE CASCADE,
    in_app             boolean     NOT NULL DEFAULT true,
    messages           boolean     NOT NULL DEFAULT true,
    engagement         boolean     NOT NULL DEFAULT true,
    payments           boolean     NOT NULL DEFAULT true,
    email              boolean     NOT NULL DEFAULT false,
    push               boolean     NOT NULL DEFAULT false,
    marketing          boolean     NOT NULL DEFAULT false,
    updated_at         timestamptz NOT NULL DEFAULT now()
);

-- A push token is only an integration point until a provider exists. It is
-- scoped to the user and platform and can be revoked independently.
CREATE TABLE IF NOT EXISTS notifications.push_tokens (
    id         uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id    uuid        NOT NULL REFERENCES identity.users (id) ON DELETE CASCADE,
    token      text        NOT NULL,
    platform   text        NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    last_seen  timestamptz NOT NULL DEFAULT now(),
    revoked_at timestamptz,
    CONSTRAINT push_platform CHECK (platform IN ('android', 'web')),
    CONSTRAINT push_token_unique UNIQUE (token)
);

CREATE OR REPLACE FUNCTION notifications.enqueue(
    p_key text, p_category text, p_recipient uuid, p_payload jsonb
) RETURNS void LANGUAGE plpgsql AS $$
BEGIN
    INSERT INTO notifications.outbox (event_key, category, recipient_id, payload)
    VALUES (p_key, p_category, p_recipient, p_payload)
    ON CONFLICT (event_key) DO NOTHING;
END;
$$;

-- Engagement: comments and likes notify the post creator.
CREATE OR REPLACE FUNCTION notifications.on_comment() RETURNS trigger
LANGUAGE plpgsql AS $$
DECLARE
    creator uuid;
    actor text;
BEGIN
    SELECT creator_id INTO creator FROM creator.posts WHERE id = NEW.post_id;
    SELECT COALESCE(NULLIF(display_name, ''), 'A member') INTO actor
      FROM creator.profiles WHERE user_id = NEW.user_id;
    actor := COALESCE(actor, 'A member');
    PERFORM notifications.enqueue(
        'comment:' || NEW.id, 'engagement', creator,
        jsonb_build_object(
            'title', 'New comment',
            'body', actor || ': ' || left(NEW.body, 120),
            'link', '/posts/' || NEW.post_id));
    RETURN NEW;
END $$;

DROP TRIGGER IF EXISTS post_comment_notify ON creator.post_comments;
CREATE TRIGGER post_comment_notify AFTER INSERT ON creator.post_comments
FOR EACH ROW EXECUTE FUNCTION notifications.on_comment();

CREATE OR REPLACE FUNCTION notifications.on_like() RETURNS trigger
LANGUAGE plpgsql AS $$
DECLARE
    creator uuid;
    actor text;
BEGIN
    SELECT creator_id INTO creator FROM creator.posts WHERE id = NEW.post_id;
    SELECT COALESCE(NULLIF(display_name, ''), 'A member') INTO actor
      FROM creator.profiles WHERE user_id = NEW.user_id;
    actor := COALESCE(actor, 'A member');
    PERFORM notifications.enqueue(
        'like:' || NEW.post_id || ':' || NEW.user_id, 'engagement', creator,
        jsonb_build_object(
            'title', 'New like',
            'body', actor || ' liked your post.',
            'link', '/posts/' || NEW.post_id));
    RETURN NEW;
END $$;

DROP TRIGGER IF EXISTS post_like_notify ON creator.post_likes;
CREATE TRIGGER post_like_notify AFTER INSERT ON creator.post_likes
FOR EACH ROW EXECUTE FUNCTION notifications.on_like();

-- Messages notify the other participant, never the sender.
CREATE OR REPLACE FUNCTION notifications.on_message() RETURNS trigger
LANGUAGE plpgsql AS $$
DECLARE
    creator uuid;
    fan uuid;
    recipient uuid;
BEGIN
    SELECT creator_id, fan_id INTO creator, fan
      FROM messaging.conversations WHERE id = NEW.conversation_id;
    recipient := CASE WHEN NEW.sender_id = fan THEN creator ELSE fan END;
    IF recipient IS NULL OR recipient = NEW.sender_id THEN
        RETURN NEW;
    END IF;
    PERFORM notifications.enqueue(
        'message:' || NEW.id, 'messages', recipient,
        jsonb_build_object(
            'title', 'New message',
            'body', left(NEW.body, 160),
            'link', '/messages/' || NEW.conversation_id));
    RETURN NEW;
END $$;

DROP TRIGGER IF EXISTS message_notify ON messaging.messages;
CREATE TRIGGER message_notify AFTER INSERT ON messaging.messages
FOR EACH ROW EXECUTE FUNCTION notifications.on_message();

-- Subscriptions notify both sides when the row becomes active.
CREATE OR REPLACE FUNCTION notifications.on_subscription() RETURNS trigger
LANGUAGE plpgsql AS $$
DECLARE
    creator_name text;
BEGIN
    IF NEW.status <> 'active' OR (TG_OP = 'UPDATE' AND OLD.status = 'active') THEN
        RETURN NEW;
    END IF;
    SELECT COALESCE(NULLIF(display_name, ''), 'a creator') INTO creator_name
      FROM creator.profiles WHERE user_id = NEW.creator_id;
    creator_name := COALESCE(creator_name, 'a creator');
    PERFORM notifications.enqueue(
        'subscription:creator:' || NEW.id, 'payments', NEW.creator_id,
        jsonb_build_object(
            'title', 'New member',
            'body', 'A member joined ' || creator_name || '.',
            'link', '/studio'));
    PERFORM notifications.enqueue(
        'subscription:fan:' || NEW.id, 'payments', NEW.fan_id,
        jsonb_build_object(
            'title', 'Membership active',
            'body', 'Your membership to ' || creator_name || ' is active.',
            'link', '/memberships'));
    RETURN NEW;
END $$;

DROP TRIGGER IF EXISTS subscription_notify ON creator.subscriptions;
CREATE TRIGGER subscription_notify AFTER INSERT OR UPDATE OF status ON creator.subscriptions
FOR EACH ROW EXECUTE FUNCTION notifications.on_subscription();

-- A successful intent notifies the buyer and, when there is a creator, the
-- creator. Simulated money is identified as simulated in the body.
CREATE OR REPLACE FUNCTION notifications.on_payment() RETURNS trigger
LANGUAGE plpgsql AS $$
DECLARE
    amount text;
    simulated text;
BEGIN
    IF NEW.status <> 'succeeded' OR (TG_OP = 'UPDATE' AND OLD.status = 'succeeded') THEN
        RETURN NEW;
    END IF;
    amount := NEW.currency || ' ' || to_char(NEW.amount_minor / 100.0, 'FM999999990.00');
    simulated := CASE WHEN NEW.provider = 'fake' THEN ' (simulated; no real money moved)' ELSE '' END;
    PERFORM notifications.enqueue(
        'payment:user:' || NEW.id || ':succeeded', 'payments', NEW.user_id,
        jsonb_build_object(
            'title', 'Payment successful',
            'body', amount || ' recorded for ' || NEW.purpose || simulated || '.',
            'link', '/payments'));
    IF NEW.creator_id IS NOT NULL AND NEW.creator_id <> NEW.user_id THEN
        PERFORM notifications.enqueue(
            'payment:creator:' || NEW.id || ':succeeded', 'payments', NEW.creator_id,
            jsonb_build_object(
                'title', 'New payment',
                'body', amount || ' ' || NEW.purpose || simulated || '.',
                'link', '/studio'));
    END IF;
    RETURN NEW;
END $$;

DROP TRIGGER IF EXISTS payment_notify ON payments.intents;
CREATE TRIGGER payment_notify AFTER INSERT OR UPDATE OF status ON payments.intents
FOR EACH ROW EXECUTE FUNCTION notifications.on_payment();

DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'content_api') THEN
        EXECUTE 'GRANT USAGE ON SCHEMA notifications TO content_api';
        EXECUTE 'GRANT SELECT, INSERT, UPDATE ON notifications.outbox TO content_api';
        EXECUTE 'GRANT SELECT, INSERT, UPDATE ON notifications.items TO content_api';
        EXECUTE 'GRANT SELECT, INSERT, UPDATE ON notifications.preferences TO content_api';
        EXECUTE 'GRANT SELECT, INSERT, UPDATE ON notifications.push_tokens TO content_api';
        EXECUTE 'GRANT USAGE, SELECT ON SEQUENCE notifications.outbox_id_seq TO content_api';
    END IF;
END $$;
