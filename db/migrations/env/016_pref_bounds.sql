
-- +goose Up

ALTER TABLE user_prefs DROP CONSTRAINT IF EXISTS user_prefs_max_height_check;
ALTER TABLE user_prefs ADD CONSTRAINT user_prefs_max_height_check
  CHECK (max_height = 0 OR max_height BETWEEN 144 AND 4320);

-- +goose Down

ALTER TABLE user_prefs DROP CONSTRAINT IF EXISTS user_prefs_max_height_check;
ALTER TABLE user_prefs ADD CONSTRAINT user_prefs_max_height_check
  CHECK (max_height IN (0, 480, 720, 1080, 2160));
