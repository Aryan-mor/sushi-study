
-- +goose Up

ALTER TABLE files ADD COLUMN stream_probed boolean NOT NULL DEFAULT false;

UPDATE files SET stream_probed = true
WHERE audio_langs <> '' OR sub_langs <> ''
   OR EXISTS (
     SELECT 1 FROM source_refs sr
     WHERE sr.file_id = files.id AND sr.source = 'ox'
   );

CREATE INDEX files_unprobed_idx ON files (id)
  WHERE NOT stream_probed AND state = 1;

-- +goose Down

DROP INDEX IF EXISTS files_unprobed_idx;
ALTER TABLE files DROP COLUMN stream_probed;
