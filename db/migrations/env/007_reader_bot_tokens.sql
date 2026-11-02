
-- +goose Up

CREATE TABLE reader_bot_tokens (
  user_id    bigint      PRIMARY KEY REFERENCES users (id) ON DELETE CASCADE,
  token      text        NOT NULL,
  username   text        NOT NULL DEFAULT '',
  updated_at timestamptz NOT NULL DEFAULT now()
);

-- +goose Down

DROP TABLE IF EXISTS reader_bot_tokens;
