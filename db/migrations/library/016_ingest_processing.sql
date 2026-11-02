
-- +goose Up

ALTER TABLE ingest_queue DROP CONSTRAINT ingest_queue_state_check;
ALTER TABLE ingest_queue ADD CONSTRAINT ingest_queue_state_check
  CHECK (state IN ('new', 'processing', 'approved', 'held', 'rejected', 'done', 'error'));

-- +goose Down

UPDATE ingest_queue SET state = 'new', updated_at = now() WHERE state = 'processing';
ALTER TABLE ingest_queue DROP CONSTRAINT ingest_queue_state_check;
ALTER TABLE ingest_queue ADD CONSTRAINT ingest_queue_state_check
  CHECK (state IN ('new', 'approved', 'held', 'rejected', 'done', 'error'));
