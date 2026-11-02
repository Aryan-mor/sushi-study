
-- +goose Up

ALTER TABLE deliveries
  ALTER COLUMN bot_id DROP NOT NULL,
  ALTER COLUMN message_id DROP NOT NULL,
  ADD COLUMN claimed_by int REFERENCES bots (id),
  ADD COLUMN claimed_at timestamptz;

CREATE INDEX deliveries_pending_idx ON deliveries (delivered_at) WHERE message_id IS NULL;

-- +goose Down

DROP INDEX IF EXISTS deliveries_pending_idx;

ALTER TABLE deliveries
  DROP COLUMN IF EXISTS claimed_by,
  DROP COLUMN IF EXISTS claimed_at,
  ALTER COLUMN bot_id SET NOT NULL,
  ALTER COLUMN message_id SET NOT NULL;
