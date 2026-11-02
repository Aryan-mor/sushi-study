
-- +goose Up

CREATE TABLE personal_video_shares (
  token         text        NOT NULL PRIMARY KEY,
  video_id      bigint      NOT NULL UNIQUE REFERENCES personal_videos (id) ON DELETE CASCADE,
  title         text        NOT NULL,
  created_by_tg bigint      NOT NULL,
  created_at    timestamptz NOT NULL DEFAULT now(),

  CONSTRAINT personal_video_shares_token_check CHECK (token ~ '^[A-Za-z0-9_-]{16,48}$')
);

-- +goose Down

DROP TABLE IF EXISTS personal_video_shares;
