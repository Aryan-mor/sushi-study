
-- +goose Up

ALTER TABLE users ADD COLUMN tg_name     text NOT NULL DEFAULT '';
ALTER TABLE users ADD COLUMN tg_username text NOT NULL DEFAULT '';

-- +goose Down

ALTER TABLE users DROP COLUMN IF EXISTS tg_username;
ALTER TABLE users DROP COLUMN IF EXISTS tg_name;
