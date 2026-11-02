
-- +goose Up

ALTER TABLE review_queue
  ADD COLUMN year int NOT NULL DEFAULT 0;

-- +goose Down

ALTER TABLE review_queue DROP COLUMN IF EXISTS year;
