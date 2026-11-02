
-- +goose Up

ALTER TABLE review_queue
  ADD COLUMN file_name  text NOT NULL DEFAULT '',
  ADD COLUMN confidence real NOT NULL DEFAULT 0,
  ADD COLUMN hint       text NOT NULL DEFAULT '';

-- +goose Down

ALTER TABLE review_queue
  DROP COLUMN IF EXISTS file_name,
  DROP COLUMN IF EXISTS confidence,
  DROP COLUMN IF EXISTS hint;
