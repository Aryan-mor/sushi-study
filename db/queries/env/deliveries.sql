-- Delivery bookkeeping (docs/05-media-delivery.md, docs/06 §3). Written by apid (enqueue/ack) and
-- deliveryd (claim/complete); nobody else.
--
-- kind is part of every key: 1 = catalog file, 2 = personal video (ADR 0037). The two id spaces
-- overlap, so a query that leaves kind out lets one answer for the other.

-- name: GetDelivery :one
SELECT reader_chat_id, file_id, bot_id, message_id, locator, delivered_at, last_ok_at
FROM deliveries
WHERE reader_chat_id = $1 AND kind = $2 AND file_id = $3;

-- name: PendingCountForReader :one
-- R-SEC-11: the concurrent-copy-jobs cap, per identity.
SELECT count(*) FROM deliveries WHERE reader_chat_id = $1 AND message_id IS NULL;

-- name: CountStaleUnclaimedPendingForReader :one
-- A pending row that deliveryd has never claimed, older than its idle cap, means the copier
-- is not running. Distinct from R-SEC-11: the concurrent cap is abuse control, this is a
-- dead worker (docs/05).
SELECT count(*) FROM deliveries
WHERE reader_chat_id = sqlc.arg(reader_chat_id)
  AND message_id IS NULL
  AND claimed_at IS NULL
  AND enqueued_at < now() - make_interval(secs => sqlc.arg(older_than_secs)::int);

-- name: CountStaleUnclaimedPending :one
-- Cluster-wide form for apid's watchdog: ops hears once, not per /play.
SELECT count(*) FROM deliveries
WHERE message_id IS NULL
  AND claimed_at IS NULL
  AND enqueued_at < now() - make_interval(secs => sqlc.arg(older_than_secs)::int);

-- name: DeliveredCountForReaderSince :one
-- R-SEC-11: the per-hour deliveries cap, per identity.
--
-- `delivered_at` is NULL until deliveryd actually lands the copy (env/014), so this counts
-- deliveries and not, as it used to, anything merely enqueued within the window. The IS NOT NULL is
-- redundant against the comparison and present so the planner reaches deliveries_delivered_idx.
SELECT count(*) FROM deliveries
WHERE reader_chat_id = $1 AND delivered_at IS NOT NULL AND delivered_at > $2;

-- name: EnqueuePending :exec
-- The row IS the job: deliveryd finds it by message_id IS NULL. A second /play for the same file
-- before the first copy finishes must not enqueue twice, hence ON CONFLICT DO NOTHING rather than
-- an error -- the existing pending row already covers it.
INSERT INTO deliveries (reader_chat_id, kind, file_id, locator)
VALUES ($1, $2, $3, $4)
ON CONFLICT (reader_chat_id, kind, file_id) DO NOTHING;

-- name: ForceRedeliver :exec
-- docs/05 §4 "broken row": the client could not find what it was told exists. Delete unconditionally
-- rather than trying to distinguish pending from delivered -- either way, start over.
DELETE FROM deliveries WHERE reader_chat_id = $1 AND kind = $2 AND file_id = $3;

-- name: TouchAck :exec
-- The id the client actually read (receiver-side TDLib numbering). copyMessage returns the
-- sender's id, which is a different sequence in a private chat, so CompleteDelivery's value
-- cannot be getMessages'd by a bot-login client. Overwrite it. A stale ack after force can
-- still land here; the next getMessages miss takes the broken-row path (docs/05 §4/§6).
UPDATE deliveries SET last_ok_at = now(), message_id = $4
WHERE reader_chat_id = $1 AND kind = $2 AND file_id = $3;

-- name: ClaimPendingDeliveries :many
-- One short statement, not a held transaction: the actual copy is a slow network call and must
-- never happen while a row lock is open. FOR UPDATE SKIP LOCKED lets several bot goroutines claim
-- disjoint batches with no coordination between them. A claim older than stale_secs is available
-- again -- the worker that made it is presumed dead.
--
-- Oldest *enqueued* first, which is not what ordering by `delivered_at` meant once CompleteDelivery
-- had moved it: a row re-enqueued after a force re-delivery sorted as if it had just arrived, ahead
-- of jobs that had been waiting longer.
UPDATE deliveries SET claimed_by = sqlc.arg(bot_id), claimed_at = now()
WHERE (reader_chat_id, kind, file_id) IN (
  SELECT reader_chat_id, kind, file_id FROM deliveries
  WHERE message_id IS NULL
    AND (claimed_at IS NULL OR claimed_at < now() - make_interval(secs => sqlc.arg(stale_secs)::int))
  ORDER BY enqueued_at
  LIMIT sqlc.arg(batch_size)
  FOR UPDATE SKIP LOCKED
)
RETURNING reader_chat_id, kind, file_id, locator;

-- name: CompleteDelivery :exec
-- Guarded by message_id IS NULL so a delayed retry of an already-completed job cannot overwrite a
-- real message id with a second, different one.
UPDATE deliveries SET bot_id = $1, message_id = $2, delivered_at = now()
WHERE reader_chat_id = $3 AND kind = $4 AND file_id = $5 AND message_id IS NULL;
