
-- +goose Up

CREATE TABLE request_forum_topics (
    name text PRIMARY KEY,
    thread_id bigint NOT NULL,
    CONSTRAINT request_forum_topics_name_check CHECK (name IN ('open', 'reject', 'done')),
    CONSTRAINT request_forum_topics_thread_check CHECK (thread_id > 0)
);

-- +goose Down

DROP TABLE IF EXISTS request_forum_topics;
