
-- +goose Up

ALTER TABLE user_prefs
  ADD COLUMN IF NOT EXISTS subs text NOT NULL DEFAULT '';

ALTER TABLE user_prefs
  DROP CONSTRAINT IF EXISTS user_prefs_subs_check;

ALTER TABLE user_prefs
  ADD CONSTRAINT user_prefs_subs_check
  CHECK (subs IN ('', 'on', 'off'));

-- +goose Down

ALTER TABLE user_prefs DROP CONSTRAINT IF EXISTS user_prefs_subs_check;
ALTER TABLE user_prefs DROP COLUMN IF EXISTS subs;
