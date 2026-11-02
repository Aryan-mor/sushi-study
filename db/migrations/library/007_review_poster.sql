
-- +goose Up
ALTER TABLE review_queue ADD COLUMN poster text NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE review_queue DROP COLUMN poster;
