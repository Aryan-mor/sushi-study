
-- +goose Up

CREATE TABLE browse_people (
  tmdb_id    bigint      PRIMARY KEY,
  name       text        NOT NULL DEFAULT '',
  profile    text        NOT NULL DEFAULT '',
  biography  text        NOT NULL DEFAULT '',
  movies     jsonb       NOT NULL DEFAULT '[]',
  series     jsonb       NOT NULL DEFAULT '[]',
  fetched_at timestamptz NOT NULL DEFAULT now()
);

-- +goose Down

DROP TABLE IF EXISTS browse_people;
