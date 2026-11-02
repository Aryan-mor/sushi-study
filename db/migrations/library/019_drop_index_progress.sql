
-- +goose Up

ALTER PUBLICATION sushi_catalog DROP TABLE index_progress;
DROP TABLE IF EXISTS index_progress;

-- +goose Down

CREATE TABLE index_progress (
  source      text        NOT NULL PRIMARY KEY,
  ox_eligible bigint      NOT NULL,
  indexed     bigint      NOT NULL,
  updated_at  timestamptz NOT NULL DEFAULT now()
);

ALTER PUBLICATION sushi_catalog ADD TABLE index_progress;
