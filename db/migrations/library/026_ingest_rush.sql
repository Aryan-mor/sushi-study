-- +goose Up

ALTER TABLE ingest_queue
  ADD COLUMN rush_at timestamptz;

CREATE TABLE ingest_rush_ping (
  ingest_id   bigint PRIMARY KEY REFERENCES ingest_queue (id) ON DELETE CASCADE,
  admin_tg_id bigint      NOT NULL,
  locale      text        NOT NULL,
  created_at  timestamptz NOT NULL DEFAULT now(),
  notified_at timestamptz
);

-- +goose Down

DROP TABLE IF EXISTS ingest_rush_ping;
ALTER TABLE ingest_queue DROP COLUMN IF EXISTS rush_at;
