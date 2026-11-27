-- 0023: scheduled posts.
--
-- A scheduled post is not published: it has no published_at and is invisible in
-- the public feed until the scheduler moves it to published.

ALTER TABLE creator.posts ADD COLUMN IF NOT EXISTS scheduled_at timestamptz;
ALTER TABLE creator.posts DROP CONSTRAINT IF EXISTS posts_status;
ALTER TABLE creator.posts ADD CONSTRAINT posts_status
    CHECK (status IN ('draft', 'scheduled', 'published', 'removed'));
ALTER TABLE creator.posts DROP CONSTRAINT IF EXISTS posts_published_shape;
ALTER TABLE creator.posts ADD CONSTRAINT posts_published_shape
    CHECK ((status = 'published') = (published_at IS NOT NULL));
ALTER TABLE creator.posts DROP CONSTRAINT IF EXISTS posts_scheduled_shape;
ALTER TABLE creator.posts ADD CONSTRAINT posts_scheduled_shape
    CHECK ((status = 'scheduled') = (scheduled_at IS NOT NULL));

CREATE INDEX IF NOT EXISTS posts_due_idx
    ON creator.posts (scheduled_at) WHERE status = 'scheduled';
