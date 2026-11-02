
-- +goose Up

CREATE TABLE items (
  id          bigint      NOT NULL PRIMARY KEY,
  tmdb_id     bigint      NOT NULL,
  kind        smallint    NOT NULL,
  title       text        NOT NULL,
  year        int         NOT NULL DEFAULT 0,
  rating      smallint    NOT NULL DEFAULT 0,
  poster      text        NOT NULL DEFAULT '',
  overview    text        NOT NULL DEFAULT '',
  released_on date,
  updated_seq bigint      NOT NULL,
  created_at  timestamptz NOT NULL DEFAULT now(),
  updated_at  timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX items_released_idx ON items (released_on DESC NULLS LAST, id);
CREATE INDEX items_seq_idx      ON items (updated_seq);
CREATE INDEX items_title_idx    ON items USING gin (title gin_trgm_ops);
CREATE UNIQUE INDEX items_tmdb_idx ON items (tmdb_id, kind);

CREATE TABLE episodes (
  id          bigint      NOT NULL PRIMARY KEY,
  item_id     bigint      NOT NULL,
  season_no   int         NOT NULL DEFAULT 0,
  episode_no  int         NOT NULL DEFAULT 0,
  title       text        NOT NULL DEFAULT '',
  overview    text        NOT NULL DEFAULT '',
  updated_seq bigint      NOT NULL,
  created_at  timestamptz NOT NULL DEFAULT now(),
  updated_at  timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX episodes_item_idx ON episodes (item_id, season_no, episode_no);
CREATE INDEX episodes_seq_idx  ON episodes (updated_seq);
CREATE UNIQUE INDEX episodes_unit_idx ON episodes (item_id, season_no, episode_no);

CREATE TABLE files (
  id            bigint      NOT NULL PRIMARY KEY,
  episode_id    bigint      NOT NULL,
  quality_label text        NOT NULL DEFAULT '',
  height        int         NOT NULL DEFAULT 0,
  audio_langs   text        NOT NULL DEFAULT '',
  sub_langs     text        NOT NULL DEFAULT '',
  size_bytes    bigint      NOT NULL DEFAULT 0,
  duration_s    int         NOT NULL DEFAULT 0,
  state         smallint    NOT NULL DEFAULT 2,
  updated_seq   bigint      NOT NULL,
  created_at    timestamptz NOT NULL DEFAULT now(),
  updated_at    timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX files_episode_idx ON files (episode_id);
CREATE INDEX files_seq_idx  ON files (updated_seq);

CREATE TABLE file_locations (
  file_id    bigint      NOT NULL,
  role       text        NOT NULL,
  channel_id bigint      NOT NULL,
  message_id bigint      NOT NULL,
  copied_at  timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (file_id, role)
);

CREATE INDEX file_locations_role_idx ON file_locations (role);


-- +goose Down

DROP TABLE IF EXISTS file_locations, files, episodes, items;
