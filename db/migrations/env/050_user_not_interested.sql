
-- +goose Up

ALTER TABLE user_item_state
  ADD COLUMN not_interested boolean NOT NULL DEFAULT false;

-- +goose Down

ALTER TABLE user_item_state DROP COLUMN not_interested;
