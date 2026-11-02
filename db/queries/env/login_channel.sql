-- Bot-login nonce + channel outbox (ADR 0013).

-- name: UpsertLoginNonce :exec
INSERT INTO login_nonces (user_id, nonce, created_at)
VALUES ($1, $2, now())
ON CONFLICT (user_id) DO UPDATE SET nonce = EXCLUDED.nonce, created_at = now();

-- name: LoginNonceByUser :one
SELECT nonce FROM login_nonces WHERE user_id = $1;

-- name: DeleteLoginNonce :exec
DELETE FROM login_nonces WHERE user_id = $1;

-- name: EnqueueLoginPost :exec
INSERT INTO login_channel_outbox (blob, delete_after) VALUES ($1, $2);

-- name: PendingLoginPosts :many
SELECT id, blob FROM login_channel_outbox WHERE posted_at IS NULL ORDER BY id LIMIT 20;

-- name: MarkLoginPostPosted :exec
UPDATE login_channel_outbox SET posted_at = now(), channel_message_id = $2 WHERE id = $1;

-- name: LoginPostsToDelete :many
SELECT id, channel_message_id FROM login_channel_outbox
WHERE channel_message_id IS NOT NULL AND delete_after <= now();

-- name: DeleteLoginPost :exec
DELETE FROM login_channel_outbox WHERE id = $1;
