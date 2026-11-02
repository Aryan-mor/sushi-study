
-- +goose Up

ALTER TABLE bindings ADD COLUMN reader_username text;

-- +goose Down

ALTER TABLE bindings DROP COLUMN IF EXISTS reader_username;
