
-- +goose Up

CREATE TABLE items (
  id          bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
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
  updated_at  timestamptz NOT NULL DEFAULT now(),

  CONSTRAINT items_kind_check   CHECK (kind IN (1, 2)),
  CONSTRAINT items_rating_check CHECK (rating BETWEEN 0 AND 100),
  UNIQUE (tmdb_id, kind)
);

CREATE INDEX items_released_idx ON items (released_on DESC NULLS LAST, id);
CREATE INDEX items_seq_idx      ON items (updated_seq);
CREATE INDEX items_title_idx    ON items USING gin (title gin_trgm_ops);

CREATE TABLE episodes (
  id          bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  item_id     bigint      NOT NULL REFERENCES items (id) ON DELETE CASCADE,
  season_no   int         NOT NULL DEFAULT 0,
  episode_no  int         NOT NULL DEFAULT 0,
  title       text        NOT NULL DEFAULT '',
  overview    text        NOT NULL DEFAULT '',
  updated_seq bigint      NOT NULL,
  created_at  timestamptz NOT NULL DEFAULT now(),
  updated_at  timestamptz NOT NULL DEFAULT now(),

  CONSTRAINT episodes_season_check  CHECK (season_no >= 0),
  CONSTRAINT episodes_episode_check CHECK (episode_no >= 0),
  UNIQUE (item_id, season_no, episode_no)
);

CREATE INDEX episodes_item_idx ON episodes (item_id, season_no, episode_no);
CREATE INDEX episodes_seq_idx  ON episodes (updated_seq);

CREATE TABLE files (
  id            bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  episode_id    bigint      NOT NULL REFERENCES episodes (id) ON DELETE CASCADE,
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
  file_id    bigint      NOT NULL REFERENCES files (id) ON DELETE CASCADE,
  role       text        NOT NULL,
  channel_id bigint      NOT NULL,
  message_id bigint      NOT NULL,
  copied_at  timestamptz NOT NULL DEFAULT now(),

  PRIMARY KEY (file_id, role),
  UNIQUE (channel_id, message_id)
);

CREATE INDEX file_locations_role_idx ON file_locations (role);

CREATE TABLE source_refs (
  source    text        NOT NULL,
  ext_key   text        NOT NULL,
  file_id   bigint      NOT NULL REFERENCES files (id) ON DELETE CASCADE,
  synced_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (source, ext_key)
);

CREATE TABLE sync_cursors (
  source     text        NOT NULL PRIMARY KEY,
  cursor     text        NOT NULL DEFAULT '0',
  updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE SEQUENCE catalog_seq;

CREATE PUBLICATION sushi_catalog FOR TABLE items, episodes, files, file_locations;

-- +goose Down

DROP PUBLICATION IF EXISTS sushi_catalog;
DROP TABLE IF EXISTS sync_cursors, source_refs, file_locations, files, episodes, items;
DROP SEQUENCE IF EXISTS catalog_seq;
