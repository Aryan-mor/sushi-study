
-- +goose Up

CREATE TABLE deleted_telegram_ids (
  tg_user_id bigint      PRIMARY KEY,
  deleted_at timestamptz NOT NULL DEFAULT now()
);

-- +goose Down

DROP TABLE IF EXISTS deleted_telegram_ids;
