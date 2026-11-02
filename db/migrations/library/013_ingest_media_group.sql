
-- +goose Up

ALTER TABLE ingest_queue
  ADD COLUMN media_group_id text NOT NULL DEFAULT '';

CREATE INDEX ingest_queue_media_group_idx
  ON ingest_queue (src_channel_id, media_group_id)
  WHERE media_group_id <> '';

-- +goose Down

DROP INDEX IF EXISTS ingest_queue_media_group_idx;
ALTER TABLE ingest_queue DROP COLUMN IF EXISTS media_group_id;
