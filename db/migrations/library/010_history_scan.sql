
-- +goose Up

ALTER TABLE source_channels
  ADD COLUMN history_msg_id bigint NOT NULL DEFAULT 0;

-- +goose Down

ALTER TABLE source_channels DROP COLUMN IF EXISTS history_msg_id;
