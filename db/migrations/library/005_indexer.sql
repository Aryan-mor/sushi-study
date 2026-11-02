
-- +goose Up

CREATE TABLE source_channels (
  id            bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  tg_channel_id bigint      NOT NULL UNIQUE,
  title         text        NOT NULL DEFAULT '',
  enabled       boolean     NOT NULL DEFAULT true,
  tier          smallint    NOT NULL DEFAULT 0,
  last_msg_id   bigint      NOT NULL DEFAULT 0,
  added_at      timestamptz NOT NULL DEFAULT now(),
  updated_at    timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE ingest_queue (
  id              bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  src_channel_id  bigint      NOT NULL,          -- Bot API form
  src_msg_id      bigint      NOT NULL,
  tg_uid          text        NOT NULL,          -- Telegram file_unique_id; cross-path dedup key
  caption         text        NOT NULL DEFAULT '',
  file_name       text        NOT NULL DEFAULT '',
  mime_type       text        NOT NULL DEFAULT '',
  duration_s      int         NOT NULL DEFAULT 0,
  size_bytes      bigint      NOT NULL DEFAULT 0,
  state           text        NOT NULL DEFAULT 'new'
                    CHECK (state IN ('new', 'approved', 'held', 'rejected', 'done', 'error')),
  verdict         jsonb,
  storage_caption text        NOT NULL DEFAULT '',
  file_id         bigint,
  note            text        NOT NULL DEFAULT '',  -- last error / drop reason, for the ops eye
  created_at      timestamptz NOT NULL DEFAULT now(),
  updated_at      timestamptz NOT NULL DEFAULT now(),
  UNIQUE (src_channel_id, src_msg_id)
);

CREATE INDEX ingest_queue_state_idx ON ingest_queue (state, id);

-- +goose Down

DROP TABLE IF EXISTS ingest_queue, source_channels;
