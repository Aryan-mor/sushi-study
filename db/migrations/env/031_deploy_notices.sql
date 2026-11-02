
-- +goose Up

CREATE TABLE deploy_notices (
  service     text        PRIMARY KEY,
  tag         text        NOT NULL,
  notified_at timestamptz NOT NULL DEFAULT now()
);

-- +goose Down

DROP TABLE IF EXISTS deploy_notices;
