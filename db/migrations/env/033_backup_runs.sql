
-- +goose Up

CREATE TABLE backup_runs (
  id           bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  status       text        NOT NULL CHECK (status IN ('queued', 'running', 'done', 'failed')),
  requested_by bigint      REFERENCES users (id) ON DELETE SET NULL,
  backup_name  text,
  created_at   timestamptz NOT NULL DEFAULT now(),
  started_at   timestamptz,
  finished_at  timestamptz
);

CREATE UNIQUE INDEX backup_runs_one_open
  ON backup_runs ((true))
  WHERE status IN ('queued', 'running');

-- +goose Down

DROP TABLE IF EXISTS backup_runs;
