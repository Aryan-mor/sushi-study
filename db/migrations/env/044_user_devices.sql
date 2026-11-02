
-- +goose Up

CREATE TABLE user_devices (
  user_id  bigint      NOT NULL REFERENCES users (id) ON DELETE CASCADE,
  platform text        NOT NULL,
  seen_at  timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (user_id, platform)
);

CREATE INDEX user_devices_seen_idx ON user_devices (seen_at DESC);

-- +goose Down

DROP TABLE IF EXISTS user_devices;
