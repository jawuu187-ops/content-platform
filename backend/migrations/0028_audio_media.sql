-- 0028: audio media objects for creator posts.
ALTER TABLE media.objects DROP CONSTRAINT IF EXISTS objects_kind;
ALTER TABLE media.objects ADD CONSTRAINT objects_kind
    CHECK (kind IN ('image', 'video', 'audio'));
