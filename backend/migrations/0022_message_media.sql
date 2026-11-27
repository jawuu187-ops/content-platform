-- 0022: message attachments and creator broadcasts.
--
-- An attachment is a media object owned by the sender. The message stores only
-- the reference; the recipient never receives a public storage path.

ALTER TABLE messaging.messages ADD COLUMN IF NOT EXISTS media_id uuid
    REFERENCES media.objects (id) ON DELETE SET NULL;

ALTER TABLE messaging.messages DROP CONSTRAINT IF EXISTS messages_body_length;
ALTER TABLE messaging.messages ADD CONSTRAINT messages_body_length CHECK (
    (char_length(body) BETWEEN 1 AND 4000) OR media_id IS NOT NULL
);

CREATE INDEX IF NOT EXISTS messages_media_idx
    ON messaging.messages (media_id) WHERE media_id IS NOT NULL;

-- A broadcast is one creator-authored message copied to entitled fans. The
-- batch id makes the operation auditable and bounded.
ALTER TABLE messaging.messages ADD COLUMN IF NOT EXISTS broadcast_id uuid;
CREATE INDEX IF NOT EXISTS messages_broadcast_idx
    ON messaging.messages (broadcast_id) WHERE broadcast_id IS NOT NULL;

-- Attachment-only messages should not produce an empty notification body.
CREATE OR REPLACE FUNCTION notifications.on_message() RETURNS trigger
LANGUAGE plpgsql AS $$
DECLARE
    creator uuid;
    fan uuid;
    recipient uuid;
    text_body text;
BEGIN
    SELECT creator_id, fan_id INTO creator, fan
      FROM messaging.conversations WHERE id = NEW.conversation_id;
    recipient := CASE WHEN NEW.sender_id = fan THEN creator ELSE fan END;
    IF recipient IS NULL OR recipient = NEW.sender_id THEN
        RETURN NEW;
    END IF;
    text_body := CASE WHEN NEW.body = '' THEN 'Sent an attachment' ELSE left(NEW.body, 160) END;
    PERFORM notifications.enqueue(
        'message:' || NEW.id, 'messages', recipient,
        jsonb_build_object(
            'title', 'New message',
            'body', text_body,
            'link', '/messages/' || NEW.conversation_id));
    RETURN NEW;
END $$;
