
-- +goose Up

ALTER TABLE files ADD COLUMN stream_probed boolean NOT NULL DEFAULT false;

-- +goose Down

ALTER TABLE files DROP COLUMN stream_probed;
