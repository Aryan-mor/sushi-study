
-- +goose Up

ALTER TABLE announcements
  ADD COLUMN manual boolean NOT NULL DEFAULT false;

-- +goose Down

ALTER TABLE announcements DROP COLUMN IF EXISTS manual;
