
-- +goose Up

ALTER TABLE items ADD COLUMN IF NOT EXISTS title_fa text NOT NULL DEFAULT '';

CREATE INDEX IF NOT EXISTS items_title_fa_idx ON items USING gin (title_fa gin_trgm_ops);

-- +goose Down

DROP INDEX items_title_fa_idx;
ALTER TABLE items DROP COLUMN title_fa;
