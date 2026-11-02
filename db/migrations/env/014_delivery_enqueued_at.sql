
-- +goose Up

ALTER TABLE deliveries ADD COLUMN enqueued_at timestamptz NOT NULL DEFAULT now();

UPDATE deliveries SET enqueued_at = delivered_at;

ALTER TABLE deliveries ALTER COLUMN delivered_at DROP NOT NULL;
ALTER TABLE deliveries ALTER COLUMN delivered_at DROP DEFAULT;
UPDATE deliveries SET delivered_at = NULL WHERE message_id IS NULL;

DROP INDEX IF EXISTS deliveries_pending_idx;
CREATE INDEX deliveries_pending_idx ON deliveries (enqueued_at) WHERE message_id IS NULL;

CREATE INDEX deliveries_delivered_idx ON deliveries (reader_chat_id, delivered_at)
  WHERE delivered_at IS NOT NULL;

-- +goose Down

DROP INDEX IF EXISTS deliveries_delivered_idx;
DROP INDEX IF EXISTS deliveries_pending_idx;
CREATE INDEX deliveries_pending_idx ON deliveries (delivered_at) WHERE message_id IS NULL;

UPDATE deliveries SET delivered_at = enqueued_at WHERE delivered_at IS NULL;
ALTER TABLE deliveries ALTER COLUMN delivered_at SET DEFAULT now();
ALTER TABLE deliveries ALTER COLUMN delivered_at SET NOT NULL;
ALTER TABLE deliveries DROP COLUMN enqueued_at;
