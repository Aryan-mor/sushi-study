
-- +goose Up

DROP TABLE IF EXISTS req_cache;
DROP TABLE IF EXISTS event_seqs;

-- +goose Down

CREATE TABLE req_cache (
  chat_id    bigint      NOT NULL,
  corr       bigint      NOT NULL,
  response   text        NOT NULL,
  expires_at timestamptz NOT NULL,
  PRIMARY KEY (chat_id, corr)
);

CREATE INDEX req_cache_expiry_idx ON req_cache (expires_at);

CREATE TABLE event_seqs (
  chat_id  bigint NOT NULL PRIMARY KEY,
  last_seq bigint NOT NULL
);
