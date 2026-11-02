
-- +goose Up

DROP INDEX IF EXISTS episodes_item_created_idx;

-- +goose Down

CREATE INDEX episodes_item_created_idx ON episodes (item_id, created_at DESC);
