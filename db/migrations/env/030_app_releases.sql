
-- +goose Up

CREATE TABLE app_releases (
  platform   text        PRIMARY KEY,
  channel_id bigint      NOT NULL,
  message_id bigint      NOT NULL,
  version    text        NOT NULL DEFAULT '',
  file_name  text        NOT NULL DEFAULT '',
  posted_at  timestamptz NOT NULL DEFAULT now()
);

-- +goose Down

DROP TABLE IF EXISTS app_releases;
