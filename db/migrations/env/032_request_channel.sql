
-- +goose Up

ALTER TABLE content_requests
  ADD COLUMN channel_message_id bigint;

CREATE INDEX content_requests_channel_msg_idx
  ON content_requests (channel_message_id)
  WHERE channel_message_id IS NOT NULL;

-- +goose Down

DROP INDEX IF EXISTS content_requests_channel_msg_idx;
ALTER TABLE content_requests DROP COLUMN IF EXISTS channel_message_id;
