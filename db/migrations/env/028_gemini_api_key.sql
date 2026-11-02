
-- +goose Up

ALTER TABLE user_prefs
  ADD COLUMN gemini_api_key_enc bytea;

-- +goose Down

ALTER TABLE user_prefs
  DROP COLUMN gemini_api_key_enc;
