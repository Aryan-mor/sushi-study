-- Staff request-inbox forum topics (ADR 0036).

-- name: ListRequestForumTopics :many
SELECT name, thread_id FROM request_forum_topics;

-- name: UpsertRequestForumTopic :exec
INSERT INTO request_forum_topics (name, thread_id)
VALUES ($1, $2)
ON CONFLICT (name) DO UPDATE SET thread_id = EXCLUDED.thread_id;
