
-- +goose Up

ALTER TABLE files ADD COLUMN display_name text NOT NULL DEFAULT '';

-- +goose Down

ALTER TABLE files DROP COLUMN display_name;
