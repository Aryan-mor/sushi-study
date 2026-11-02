
-- +goose Up

CREATE TABLE file_backup_cursor (
  source          text        PRIMARY KEY,
  backed_up_until timestamptz NOT NULL,
  updated_at      timestamptz NOT NULL DEFAULT now()
);

-- +goose Down

DROP TABLE IF EXISTS file_backup_cursor;
