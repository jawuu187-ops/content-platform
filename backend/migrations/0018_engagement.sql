-- 0018: post engagement (likes, comments, bookmarks).
--
-- Engagement rows are per account and post. Comments keep the author id and the
-- text only; display names are resolved from the creator profile at read time so
-- a fan's email address is never exposed.

CREATE TABLE IF NOT EXISTS creator.post_likes (
    post_id    uuid        NOT NULL REFERENCES creator.posts (id) ON DELETE CASCADE,
    user_id    uuid        NOT NULL REFERENCES identity.users (id) ON DELETE CASCADE,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (post_id, user_id)
);

CREATE INDEX IF NOT EXISTS post_likes_user_idx ON creator.post_likes (user_id, created_at DESC);

CREATE TABLE IF NOT EXISTS creator.post_bookmarks (
    post_id    uuid        NOT NULL REFERENCES creator.posts (id) ON DELETE CASCADE,
    user_id    uuid        NOT NULL REFERENCES identity.users (id) ON DELETE CASCADE,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (post_id, user_id)
);

CREATE INDEX IF NOT EXISTS post_bookmarks_user_idx ON creator.post_bookmarks (user_id, created_at DESC);

CREATE TABLE IF NOT EXISTS creator.post_comments (
    id         uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    post_id    uuid        NOT NULL REFERENCES creator.posts (id) ON DELETE CASCADE,
    user_id    uuid        NOT NULL REFERENCES identity.users (id) ON DELETE CASCADE,
    body       text        NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    deleted_at timestamptz,
    CONSTRAINT post_comments_body CHECK (char_length(btrim(body)) BETWEEN 1 AND 500)
);

CREATE INDEX IF NOT EXISTS post_comments_post_idx
    ON creator.post_comments (post_id, created_at) WHERE deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS post_comments_user_idx ON creator.post_comments (user_id, created_at DESC);

GRANT SELECT, INSERT, DELETE ON creator.post_likes TO content_api;
GRANT SELECT, INSERT, DELETE ON creator.post_bookmarks TO content_api;
GRANT SELECT, INSERT, UPDATE ON creator.post_comments TO content_api;
