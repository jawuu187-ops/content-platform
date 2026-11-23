-- 0008_media_content.sql
--
-- Media objects, posts and post unlocks.
--
-- Storage keys are content hashes we generate, never user input: there is no
-- path a client can influence, and no public bucket path is ever exposed. A
-- delivery token is single use and short lived, and it is only a *reference* to a
-- decision; the access check runs again when the token is redeemed.

CREATE SCHEMA IF NOT EXISTS media;

CREATE TABLE IF NOT EXISTS media.objects (
    id           uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    owner_id     uuid        NOT NULL REFERENCES identity.users (id) ON DELETE CASCADE,
    kind         text        NOT NULL,
    storage_key  text        NOT NULL,
    content_type text        NOT NULL,
    byte_size    bigint      NOT NULL,
    sha256       text        NOT NULL,
    width        integer,
    height       integer,
    duration_ms  integer,
    status       text        NOT NULL DEFAULT 'ready',
    created_at   timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT objects_kind CHECK (kind IN ('image', 'video')),
    CONSTRAINT objects_status CHECK (status IN ('ready', 'quarantined', 'deleted')),
    CONSTRAINT objects_size CHECK (byte_size > 0),
    CONSTRAINT objects_hash CHECK (sha256 ~ '^[a-f0-9]{64}$'),
    CONSTRAINT objects_key CHECK (storage_key ~ '^[a-f0-9]{64}$')
);

CREATE INDEX IF NOT EXISTS objects_owner_idx
    ON media.objects (owner_id, created_at DESC);

-- Bytes are shared by content hash across the whole instance, but ownership and
-- metadata are per account: the same image uploaded twice by the same creator is
-- one row, and two creators uploading identical bytes get their own rows.
CREATE UNIQUE INDEX IF NOT EXISTS objects_owner_key_idx
    ON media.objects (owner_id, storage_key);

CREATE TABLE IF NOT EXISTS creator.posts (
    id           uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    creator_id   uuid        NOT NULL REFERENCES creator.profiles (user_id) ON DELETE CASCADE,
    body         text        NOT NULL DEFAULT '',
    visibility   text        NOT NULL,
    price_minor  bigint,
    currency     char(3),
    status       text        NOT NULL DEFAULT 'draft',
    published_at timestamptz,
    created_at   timestamptz NOT NULL DEFAULT now(),
    updated_at   timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT posts_visibility CHECK (visibility IN ('public', 'subscriber', 'ppv')),
    CONSTRAINT posts_status CHECK (status IN ('draft', 'published', 'removed')),
    CONSTRAINT posts_body_len CHECK (char_length(body) <= 2000),
    -- A pay-per-view post must carry a price; a non-ppv post must not.
    CONSTRAINT posts_ppv_price CHECK ((visibility = 'ppv') = (price_minor IS NOT NULL)),
    CONSTRAINT posts_price_positive CHECK (price_minor IS NULL OR price_minor BETWEEN 100 AND 1000000),
    CONSTRAINT posts_currency CHECK (currency IS NULL OR currency ~ '^[A-Z]{3}$'),
    CONSTRAINT posts_published_shape CHECK ((status = 'published') = (published_at IS NOT NULL))
);

CREATE INDEX IF NOT EXISTS posts_feed_idx
    ON creator.posts (creator_id, published_at DESC) WHERE status = 'published';

CREATE TABLE IF NOT EXISTS creator.post_media (
    post_id    uuid NOT NULL REFERENCES creator.posts (id) ON DELETE CASCADE,
    media_id   uuid NOT NULL REFERENCES media.objects (id) ON DELETE RESTRICT,
    sort_order integer NOT NULL DEFAULT 0,
    PRIMARY KEY (post_id, media_id)
);

-- One-off unlocks for a single post. A live row is the only thing that lets a
-- PPV post through, so revoking it closes access without touching the post.
CREATE TABLE IF NOT EXISTS creator.post_unlocks (
    id         uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    post_id    uuid        NOT NULL REFERENCES creator.posts (id) ON DELETE CASCADE,
    fan_id     uuid        NOT NULL REFERENCES identity.users (id) ON DELETE CASCADE,
    source     text        NOT NULL,
    granted_at timestamptz NOT NULL DEFAULT now(),
    expires_at timestamptz,
    revoked_at timestamptz,
    CONSTRAINT unlocks_source CHECK (source IN ('purchase', 'grant', 'admin'))
);

CREATE UNIQUE INDEX IF NOT EXISTS unlocks_live_idx
    ON creator.post_unlocks (post_id, fan_id) WHERE revoked_at IS NULL;

-- Delivery tokens: single use, viewer bound, expiring. The row is the control;
-- the signed string only avoids a database lookup for garbage input.
CREATE TABLE IF NOT EXISTS media.delivery_tokens (
    jti         uuid        PRIMARY KEY,
    media_id    uuid        NOT NULL REFERENCES media.objects (id) ON DELETE CASCADE,
    post_id     uuid        NOT NULL REFERENCES creator.posts (id) ON DELETE CASCADE,
    viewer_id   uuid        NOT NULL REFERENCES identity.users (id) ON DELETE CASCADE,
    issued_at   timestamptz NOT NULL DEFAULT now(),
    expires_at  timestamptz NOT NULL,
    redeemed_at timestamptz,
    CONSTRAINT tokens_expiry_after_issue CHECK (expires_at > issued_at)
);

CREATE INDEX IF NOT EXISTS delivery_tokens_expiry_idx
    ON media.delivery_tokens (expires_at) WHERE redeemed_at IS NULL;
