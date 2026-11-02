
-- +goose Up

ALTER TABLE user_prefs DROP CONSTRAINT IF EXISTS user_prefs_locale_check;
ALTER TABLE user_prefs ADD CONSTRAINT user_prefs_locale_check
  CHECK (locale IN ('', 'en', 'fa', 'ar', 'de', 'ru', 'uk', 'it', 'fr', 'tr'));

-- +goose Down

ALTER TABLE user_prefs DROP CONSTRAINT IF EXISTS user_prefs_locale_check;
ALTER TABLE user_prefs ADD CONSTRAINT user_prefs_locale_check
  CHECK (locale IN ('', 'en', 'fa'));
