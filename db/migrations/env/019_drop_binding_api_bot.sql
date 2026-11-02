
-- +goose Up

DROP INDEX IF EXISTS bindings_api_bot_idx;
ALTER TABLE bindings DROP COLUMN api_bot_id;

-- +goose Down

ALTER TABLE bindings ADD COLUMN api_bot_id int REFERENCES bots (id);
CREATE INDEX bindings_api_bot_idx ON bindings (api_bot_id) WHERE superseded_at IS NULL;
