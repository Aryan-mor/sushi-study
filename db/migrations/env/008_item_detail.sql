
-- +goose Up

ALTER TABLE items ADD COLUMN detail jsonb NOT NULL DEFAULT '{}';

-- +goose Down

ALTER TABLE items DROP COLUMN detail;
