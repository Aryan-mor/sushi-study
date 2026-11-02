
-- +goose Up

ALTER TABLE ingest_queue
  ADD COLUMN forced boolean NOT NULL DEFAULT false;

CREATE INDEX ingest_queue_tg_uid_idx ON ingest_queue (tg_uid);

-- +goose Down

DROP INDEX IF EXISTS ingest_queue_tg_uid_idx;
ALTER TABLE ingest_queue DROP COLUMN IF EXISTS forced;
