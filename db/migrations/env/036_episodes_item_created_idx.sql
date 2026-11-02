
-- +goose Up

CREATE INDEX episodes_item_created_idx ON episodes (item_id, created_at DESC);

-- +goose Down

DROP INDEX IF EXISTS episodes_item_created_idx;
