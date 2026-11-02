
-- +goose Up

DROP TRIGGER IF EXISTS index_progress_readonly ON index_progress;
DROP TABLE IF EXISTS index_progress;

-- +goose Down

CREATE TABLE index_progress (
  source      text        NOT NULL PRIMARY KEY,
  ox_eligible bigint      NOT NULL,
  indexed     bigint      NOT NULL,
  updated_at  timestamptz NOT NULL DEFAULT now()
);

CREATE TRIGGER index_progress_readonly BEFORE INSERT OR UPDATE OR DELETE OR TRUNCATE ON index_progress
  FOR EACH STATEMENT EXECUTE FUNCTION sushi_catalog_readonly();
