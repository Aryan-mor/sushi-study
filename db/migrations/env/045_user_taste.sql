
-- +goose Up

ALTER TABLE user_prefs
  ADD COLUMN IF NOT EXISTS subtitle_lang text NOT NULL DEFAULT '',
  ADD COLUMN IF NOT EXISTS watch_kind text NOT NULL DEFAULT '',
  ADD COLUMN IF NOT EXISTS genres text[] NOT NULL DEFAULT '{}',
  ADD COLUMN IF NOT EXISTS liked_titles jsonb NOT NULL DEFAULT '[]',
  ADD COLUMN IF NOT EXISTS liked_people bigint[] NOT NULL DEFAULT '{}',
  ADD COLUMN IF NOT EXISTS taste_completed_at timestamptz;

ALTER TABLE user_prefs
  DROP CONSTRAINT IF EXISTS user_prefs_watch_kind_check;

ALTER TABLE user_prefs
  ADD CONSTRAINT user_prefs_watch_kind_check
  CHECK (watch_kind IN ('', 'movie', 'series', 'both'));

-- +goose Down

ALTER TABLE user_prefs DROP CONSTRAINT IF EXISTS user_prefs_watch_kind_check;
ALTER TABLE user_prefs
  DROP COLUMN IF EXISTS taste_completed_at,
  DROP COLUMN IF EXISTS liked_people,
  DROP COLUMN IF EXISTS liked_titles,
  DROP COLUMN IF EXISTS genres,
  DROP COLUMN IF EXISTS watch_kind,
  DROP COLUMN IF EXISTS subtitle_lang;
