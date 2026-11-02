
-- +goose Up

CREATE TABLE users (
  id           bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  tg_user_id   bigint      NOT NULL UNIQUE,
  created_at   timestamptz NOT NULL DEFAULT now(),
  last_seen_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE user_prefs (
  user_id    bigint      PRIMARY KEY REFERENCES users (id) ON DELETE CASCADE,
  locale     text        NOT NULL DEFAULT '',
  max_height int         NOT NULL DEFAULT 0,
  audio      text        NOT NULL DEFAULT '',
  updated_at timestamptz NOT NULL DEFAULT now(),

  CONSTRAINT user_prefs_locale_check     CHECK (locale IN ('', 'en', 'fa')),
  CONSTRAINT user_prefs_max_height_check CHECK (max_height IN (0, 480, 720, 1080, 2160)),
  CONSTRAINT user_prefs_audio_check      CHECK (audio IN ('', 'original', 'dub'))
);

CREATE TABLE user_item_state (
  user_id     bigint      NOT NULL REFERENCES users (id) ON DELETE CASCADE,
  item_id     bigint      NOT NULL,
  favorite    boolean     NOT NULL DEFAULT false,
  watch_later boolean     NOT NULL DEFAULT false,
  seen_at     timestamptz,
  updated_at  timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (user_id, item_id)
);

CREATE TABLE user_episode_state (
  user_id      bigint      NOT NULL REFERENCES users (id) ON DELETE CASCADE,
  episode_id   bigint      NOT NULL,
  position_s   int         NOT NULL DEFAULT 0,
  done         boolean     NOT NULL DEFAULT false,
  last_file_id bigint,
  played_at    timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (user_id, episode_id)
);

CREATE INDEX user_episode_state_recent_idx
  ON user_episode_state (user_id, played_at DESC) WHERE NOT done;

CREATE TABLE play_events (
  id         bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  user_id    bigint      NOT NULL REFERENCES users (id) ON DELETE CASCADE,
  item_id    bigint      NOT NULL,
  episode_id bigint,
  file_id    bigint,
  kind       smallint    NOT NULL,
  at         timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX play_events_recent_idx ON play_events (at DESC);
CREATE INDEX play_events_item_idx   ON play_events (item_id, at DESC);

CREATE TABLE bots (
  id         int GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  username   text        NOT NULL UNIQUE,
  role       smallint    NOT NULL,
  state      smallint    NOT NULL DEFAULT 1,
  health_at  timestamptz,
  created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE bindings (
  id             bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  user_id        bigint      NOT NULL REFERENCES users (id) ON DELETE CASCADE,
  reader_chat_id bigint      NOT NULL,
  api_bot_id     int         NOT NULL REFERENCES bots (id),
  provider_id    int         NOT NULL DEFAULT 1,
  binding_token  bytea       NOT NULL,
  epoch          int         NOT NULL DEFAULT 1,
  created_at     timestamptz NOT NULL DEFAULT now(),
  superseded_at  timestamptz
);

CREATE UNIQUE INDEX bindings_live_chat ON bindings (reader_chat_id) WHERE superseded_at IS NULL;
CREATE INDEX bindings_live_user ON bindings (user_id) WHERE superseded_at IS NULL;

CREATE TABLE deliveries (
  reader_chat_id bigint      NOT NULL,
  file_id        bigint      NOT NULL,
  bot_id         int         NOT NULL REFERENCES bots (id),
  message_id     bigint      NOT NULL,
  locator        text        NOT NULL,
  delivered_at   timestamptz NOT NULL DEFAULT now(),
  last_ok_at     timestamptz,
  PRIMARY KEY (reader_chat_id, file_id)
);

CREATE TABLE rails (
  kind    smallint NOT NULL,
  tab     smallint NOT NULL,
  rank    smallint NOT NULL,
  item_id bigint   NOT NULL,
  PRIMARY KEY (kind, tab, rank)
);

CREATE TABLE rail_state (
  only_row boolean     NOT NULL DEFAULT true,
  seq      bigint      NOT NULL DEFAULT 1,
  built_at timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT rail_state_singleton PRIMARY KEY (only_row),
  CONSTRAINT rail_state_only_one CHECK (only_row)
);

INSERT INTO rail_state (only_row) VALUES (true) ON CONFLICT DO NOTHING;

CREATE TABLE req_cache (
  chat_id    bigint      NOT NULL,
  corr       bigint      NOT NULL,
  response   text        NOT NULL,
  expires_at timestamptz NOT NULL,
  PRIMARY KEY (chat_id, corr)
);

CREATE INDEX req_cache_expiry_idx ON req_cache (expires_at);

CREATE TABLE event_seqs (
  chat_id  bigint NOT NULL PRIMARY KEY,
  last_seq bigint NOT NULL
);

-- +goose Down

DROP TABLE IF EXISTS event_seqs, req_cache, rail_state, rails, deliveries, bindings, bots,
  play_events, user_episode_state, user_item_state, user_prefs, users;
