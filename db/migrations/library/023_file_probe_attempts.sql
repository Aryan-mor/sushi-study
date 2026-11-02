
-- +goose Up

CREATE TABLE file_probe_attempts (
  file_id     bigint      NOT NULL REFERENCES files (id) ON DELETE CASCADE,
  pass        text        NOT NULL CHECK (pass IN ('meta', 'stream')),
  tries       int         NOT NULL DEFAULT 0,
  next_try_at timestamptz NOT NULL DEFAULT now(),
  last_error  text        NOT NULL DEFAULT '',
  PRIMARY KEY (file_id, pass)
);

UPDATE files AS f
SET state = 3, updated_seq = nextval('catalog_seq'), updated_at = now()
WHERE f.state = 1
  AND f.size_bytes > 0 AND f.size_bytes < 300 * 1024 * 1024
  AND (
    f.quality_label ~* '(^|[^a-z])(trailer|teaser|fragman)([^a-z]|$)'
    OR f.quality_label ~ '(تریلر|تیزر|آنونس)'
    OR EXISTS (
      SELECT 1 FROM ingest_queue q
      WHERE q.file_id = f.id
        AND (q.file_name ~* '(^|[^a-z])(trailer|teaser|fragman)([^a-z]|$)'
             OR q.file_name ~ '(تریلر|تیزر|آنونس)')
    )
  );

-- +goose Down

DROP TABLE IF EXISTS file_probe_attempts;
