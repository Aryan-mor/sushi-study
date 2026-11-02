
-- +goose Up

CREATE TABLE subtitles (
  id           bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  tmdb_id      bigint      NOT NULL,
  kind         smallint    NOT NULL,
  season_no    int         NOT NULL DEFAULT 0,
  episode_no   int         NOT NULL DEFAULT 0,
  lang         text        NOT NULL,
  provider     text        NOT NULL,
  ext          text        NOT NULL DEFAULT '.srt',
  channel_id   bigint      NOT NULL,
  message_id   bigint      NOT NULL,
  created_at   timestamptz NOT NULL DEFAULT now(),

  UNIQUE (tmdb_id, kind, season_no, episode_no, lang),
  UNIQUE (channel_id, message_id)
);

CREATE INDEX subtitles_tmdb_idx ON subtitles (tmdb_id, kind);

-- +goose Down

DROP TABLE subtitles;
