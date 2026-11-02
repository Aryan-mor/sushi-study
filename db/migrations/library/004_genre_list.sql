
-- +goose Up

UPDATE items
SET detail = jsonb_set(
      detail,
      '{genreList}',
      to_jsonb(string_to_array(detail->>'genres', ', '))
    ),
    updated_seq = nextval('catalog_seq'),
    updated_at = now()
WHERE COALESCE(detail->>'genres', '') <> ''
  AND NOT (detail ? 'genreList');

-- +goose Down

UPDATE items SET detail = detail - 'genreList' WHERE detail ? 'genreList';
