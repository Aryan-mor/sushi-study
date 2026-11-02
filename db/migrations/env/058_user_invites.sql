
-- +goose Up

CREATE TABLE user_invites (
  tg_user_id bigint      PRIMARY KEY,
  invited_by bigint      NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now()
);

-- +goose Down

DROP TABLE IF EXISTS user_invites;
