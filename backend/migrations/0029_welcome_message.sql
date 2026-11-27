-- 0029: creator welcome message shown on the profile.
ALTER TABLE creator.profiles ADD COLUMN IF NOT EXISTS welcome_message text NOT NULL DEFAULT '';
ALTER TABLE creator.profiles DROP CONSTRAINT IF EXISTS profiles_welcome_len;
ALTER TABLE creator.profiles ADD CONSTRAINT profiles_welcome_len
    CHECK (char_length(welcome_message) <= 500);
