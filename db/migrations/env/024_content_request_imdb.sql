
-- +goose Up

ALTER TABLE content_requests ADD COLUMN imdb_id text NOT NULL DEFAULT '';

-- +goose Down

ALTER TABLE content_requests DROP COLUMN IF EXISTS imdb_id;
