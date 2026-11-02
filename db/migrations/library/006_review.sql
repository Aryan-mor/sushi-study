
-- +goose Up

CREATE TABLE review_queue (
  id               bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  feed             text        NOT NULL CHECK (feed IN ('audit', 'held', 'error')),
  ingest_id        bigint      NOT NULL,   -- the ingest_queue row this is about
  file_id          bigint,                 -- set once a file row exists (audit; accepted held)
  title            text        NOT NULL DEFAULT '',
  tmdb_id          bigint      NOT NULL DEFAULT 0,
  kind             smallint    NOT NULL DEFAULT 0,
  season           int         NOT NULL DEFAULT 0,
  episode          int         NOT NULL DEFAULT 0,
  reason           text        NOT NULL DEFAULT '',
  src_channel_id   bigint      NOT NULL DEFAULT 0,
  src_msg_id       bigint      NOT NULL DEFAULT 0,
  status           text        NOT NULL DEFAULT 'open' CHECK (status IN ('open', 'resolved')),
  review_msg_id    bigint,
  created_at       timestamptz NOT NULL DEFAULT now(),
  updated_at       timestamptz NOT NULL DEFAULT now(),

  UNIQUE (feed, ingest_id)
);

CREATE INDEX review_queue_unposted_idx ON review_queue (id) WHERE review_msg_id IS NULL AND status = 'open';

-- +goose Down

DROP TABLE IF EXISTS review_queue;
