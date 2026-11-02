
-- +goose Up

CREATE TABLE browse_titles (
  tmdb_id     bigint      NOT NULL,
  kind        smallint    NOT NULL,
  title       text        NOT NULL DEFAULT '',
  year        int         NOT NULL DEFAULT 0,
  rating      smallint    NOT NULL DEFAULT 0,
  poster      text        NOT NULL DEFAULT '',
  overview    text        NOT NULL DEFAULT '',
  released_on date,
  detail      jsonb       NOT NULL DEFAULT '{}',
  fetched_at  timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (tmdb_id, kind),
  CONSTRAINT browse_titles_kind_check   CHECK (kind IN (1, 2)),
  CONSTRAINT browse_titles_rating_check CHECK (rating BETWEEN 0 AND 100)
);

CREATE TABLE playlists (
  id         bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  user_id    bigint      NOT NULL REFERENCES users (id) ON DELETE CASCADE,
  name       text        NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX playlists_user_idx ON playlists (user_id);

CREATE TABLE playlist_items (
  playlist_id bigint      NOT NULL REFERENCES playlists (id) ON DELETE CASCADE,
  tmdb_id     bigint      NOT NULL,
  kind        smallint    NOT NULL,
  rank        int         NOT NULL DEFAULT 0,
  added_at    timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (playlist_id, tmdb_id, kind),
  CONSTRAINT playlist_items_kind_check CHECK (kind IN (1, 2))
);

CREATE INDEX playlist_items_playlist_idx ON playlist_items (playlist_id, rank);

ALTER TABLE user_item_state ADD COLUMN tmdb_id bigint;
ALTER TABLE user_item_state ADD COLUMN kind smallint;

UPDATE user_item_state u
SET tmdb_id = i.tmdb_id, kind = i.kind
FROM items i
WHERE u.item_id = i.id AND u.tmdb_id IS NULL;

DELETE FROM user_item_state WHERE tmdb_id IS NULL OR kind IS NULL;

ALTER TABLE user_item_state DROP CONSTRAINT user_item_state_pkey;
ALTER TABLE user_item_state DROP COLUMN item_id;
ALTER TABLE user_item_state ALTER COLUMN tmdb_id SET NOT NULL;
ALTER TABLE user_item_state ALTER COLUMN kind SET NOT NULL;
ALTER TABLE user_item_state
  ADD CONSTRAINT user_item_state_kind_check CHECK (kind IN (1, 2)),
  ADD PRIMARY KEY (user_id, tmdb_id, kind);

-- +goose Down

-- +goose StatementBegin
DO $$
BEGIN
  RAISE EXCEPTION 'env/009 is forward-only: user_item_state.item_id cannot be reconstructed'
    USING HINT = 'roll forward with a new migration, or restore the environment from a backup';
END
$$;
-- +goose StatementEnd
