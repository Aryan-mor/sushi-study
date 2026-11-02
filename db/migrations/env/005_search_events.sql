
-- +goose Up

CREATE TABLE search_events (
  user_id bigint      NOT NULL REFERENCES users (id) ON DELETE CASCADE,
  at      timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX search_events_user_idx ON search_events (user_id, at DESC);

-- +goose Down

DROP TABLE IF EXISTS search_events;
