
-- +goose Up

ALTER TABLE source_channels ADD COLUMN content_kind text NOT NULL DEFAULT 'video'
  CHECK (content_kind IN ('video', 'subtitle'));

ALTER TABLE ingest_queue ADD COLUMN content_kind text NOT NULL DEFAULT 'video'
  CHECK (content_kind IN ('video', 'subtitle'));


-- +goose Down

ALTER TABLE ingest_queue DROP COLUMN IF EXISTS content_kind;
ALTER TABLE source_channels DROP COLUMN IF EXISTS content_kind;
