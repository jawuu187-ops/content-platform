-- 0021: creator avatar and cover media references.
--
-- The bytes live in media.objects under the creator's ownership; the profile
-- stores only a reference. No external URL is trusted, so a profile cannot be
-- used to make every visitor load a third-party tracker.

ALTER TABLE creator.profiles
    ADD COLUMN IF NOT EXISTS avatar_media_id uuid REFERENCES media.objects (id) ON DELETE SET NULL;
ALTER TABLE creator.profiles
    ADD COLUMN IF NOT EXISTS cover_media_id uuid REFERENCES media.objects (id) ON DELETE SET NULL;
