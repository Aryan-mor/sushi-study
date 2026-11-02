
-- +goose Up

CREATE TABLE followed_series_state (
  tmdb_id        bigint      PRIMARY KEY,
  max_season_no  int         NOT NULL DEFAULT 0,
  max_episode_no int         NOT NULL DEFAULT 0,
  changed_at     timestamptz NOT NULL DEFAULT now()   -- last time (max_season_no, max_episode_no) rose
);

CREATE TABLE series_follows (
  user_id         bigint      NOT NULL REFERENCES users (id) ON DELETE CASCADE,
  tmdb_id         bigint      NOT NULL,
  created_at      timestamptz NOT NULL DEFAULT now(),
  seen_season_no  int         NOT NULL DEFAULT 0,     -- newest episode already DMed (coalescing high-water mark)
  seen_episode_no int         NOT NULL DEFAULT 0,
  PRIMARY KEY (user_id, tmdb_id)
);

CREATE INDEX series_follows_tmdb_idx ON series_follows (tmdb_id);

-- +goose Down

DROP TABLE IF EXISTS series_follows;
DROP TABLE IF EXISTS followed_series_state;
