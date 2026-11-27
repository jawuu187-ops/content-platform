-- 0027: 24-hour creator stories.
--
-- A story is an image or short video owned by the creator. It expires rather
-- than being deleted by a client. Access uses the same subscriber entitlement as
-- a subscriber post; there is no public anonymous story feed.

CREATE TABLE IF NOT EXISTS creator.stories (
    id         uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    creator_id uuid        NOT NULL REFERENCES creator.profiles (user_id) ON DELETE CASCADE,
    media_id   uuid        NOT NULL REFERENCES media.objects (id) ON DELETE CASCADE,
    caption    text        NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL DEFAULT now(),
    expires_at timestamptz NOT NULL,
    CONSTRAINT stories_caption CHECK (char_length(caption) <= 200),
    CONSTRAINT stories_expiry CHECK (expires_at > created_at)
);
CREATE INDEX IF NOT EXISTS stories_active_idx
    ON creator.stories (creator_id, expires_at DESC);
CREATE INDEX IF NOT EXISTS stories_expiry_idx ON creator.stories (expires_at);

DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'content_api') THEN
        EXECUTE 'GRANT SELECT, INSERT, DELETE ON creator.stories TO content_api';
    END IF;
END $$;
