
-- +goose Up

CREATE SEQUENCE user_episode_state_seq;

ALTER TABLE user_episode_state
  ADD COLUMN seq bigint NOT NULL DEFAULT nextval('user_episode_state_seq');

CREATE INDEX user_episode_state_seq_idx ON user_episode_state (user_id, seq);

-- +goose Down

DROP INDEX user_episode_state_seq_idx;

ALTER TABLE user_episode_state DROP COLUMN seq;

DROP SEQUENCE user_episode_state_seq;
