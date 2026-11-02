
-- +goose Up

CREATE VIEW title_cards AS
SELECT
  COALESCE(i.tmdb_id, b.tmdb_id)             AS tmdb_id,
  COALESCE(i.kind, b.kind)                   AS kind,
  COALESCE(i.title, b.title, '')             AS title,
  COALESCE(i.year, b.year, 0)                AS year,
  COALESCE(i.rating, b.rating, 0)::smallint  AS rating,
  COALESCE(i.poster, b.poster, '')           AS poster
FROM items i
FULL JOIN browse_titles b ON b.tmdb_id = i.tmdb_id AND b.kind = i.kind;

-- +goose Down

DROP VIEW IF EXISTS title_cards;
