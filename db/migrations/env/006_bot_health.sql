
-- +goose Up

ALTER TABLE bots ADD COLUMN quarantined_until timestamptz;

-- +goose Down

ALTER TABLE bots DROP COLUMN IF EXISTS quarantined_until;
