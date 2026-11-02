
-- +goose Up

ALTER TABLE deliveries ADD COLUMN kind smallint NOT NULL DEFAULT 1;
ALTER TABLE deliveries ADD CONSTRAINT deliveries_kind_check CHECK (kind IN (1, 2));
ALTER TABLE deliveries DROP CONSTRAINT deliveries_pkey;
ALTER TABLE deliveries ADD PRIMARY KEY (reader_chat_id, kind, file_id);

ALTER TABLE user_episode_state ADD COLUMN kind smallint NOT NULL DEFAULT 1;
ALTER TABLE user_episode_state ADD CONSTRAINT user_episode_state_kind_check CHECK (kind IN (1, 2));
ALTER TABLE user_episode_state DROP CONSTRAINT user_episode_state_pkey;
ALTER TABLE user_episode_state ADD PRIMARY KEY (user_id, kind, episode_id);

-- +goose Down

DELETE FROM user_episode_state WHERE kind = 2;
ALTER TABLE user_episode_state DROP CONSTRAINT user_episode_state_pkey;
ALTER TABLE user_episode_state ADD PRIMARY KEY (user_id, episode_id);
ALTER TABLE user_episode_state DROP CONSTRAINT user_episode_state_kind_check;
ALTER TABLE user_episode_state DROP COLUMN kind;

DELETE FROM deliveries WHERE kind = 2;
ALTER TABLE deliveries DROP CONSTRAINT deliveries_pkey;
ALTER TABLE deliveries ADD PRIMARY KEY (reader_chat_id, file_id);
ALTER TABLE deliveries DROP CONSTRAINT deliveries_kind_check;
ALTER TABLE deliveries DROP COLUMN kind;
