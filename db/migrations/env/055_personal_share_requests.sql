
-- +goose Up

CREATE TABLE personal_share_requests (
  id         bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  user_id    bigint      NOT NULL REFERENCES users (id) ON DELETE CASCADE,
  video_id   bigint      NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  sent_at    timestamptz,
  attempts   int         NOT NULL DEFAULT 0,
  last_error text        NOT NULL DEFAULT ''
);

CREATE UNIQUE INDEX personal_share_requests_pending_idx
  ON personal_share_requests (user_id, video_id) WHERE sent_at IS NULL;

-- +goose Down

DROP TABLE IF EXISTS personal_share_requests;
