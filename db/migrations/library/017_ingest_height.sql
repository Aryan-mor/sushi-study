
-- +goose Up

ALTER TABLE ingest_queue
  ADD COLUMN height int NOT NULL DEFAULT 0;

-- +goose Down

ALTER TABLE ingest_queue DROP COLUMN IF EXISTS height;
