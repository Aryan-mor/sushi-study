
-- +goose Up

ALTER TABLE items    ADD COLUMN overview_fa text NOT NULL DEFAULT '';
ALTER TABLE episodes ADD COLUMN overview_fa text NOT NULL DEFAULT '';

-- +goose Down

ALTER TABLE episodes DROP COLUMN overview_fa;
ALTER TABLE items    DROP COLUMN overview_fa;
