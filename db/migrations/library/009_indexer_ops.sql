
-- +goose Up

CREATE TABLE indexer_settings (
  id               smallint PRIMARY KEY CHECK (id = 1),
  indexer_enabled  boolean     NOT NULL DEFAULT true,
  reader_enabled   boolean     NOT NULL DEFAULT true,
  batch            int         NOT NULL DEFAULT 5
                               CHECK (batch BETWEEN 1 AND 50),
  pass_interval_s  int         NOT NULL DEFAULT 45
                               CHECK (pass_interval_s BETWEEN 15 AND 600),
  copy_pace_ms     int         NOT NULL DEFAULT 2000
                               CHECK (copy_pace_ms BETWEEN 1000 AND 15000),
  updated_at       timestamptz NOT NULL DEFAULT now(),
  updated_by       bigint      NOT NULL DEFAULT 0
);

INSERT INTO indexer_settings (id) VALUES (1);

-- +goose Down

DROP TABLE IF EXISTS indexer_settings;
