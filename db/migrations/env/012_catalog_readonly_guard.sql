
-- +goose Up

-- +goose StatementBegin
CREATE FUNCTION sushi_catalog_readonly() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
  RAISE EXCEPTION
    'R-LIB-3: % is replicated from the library and is read-only in an environment', TG_TABLE_NAME
    USING HINT = 'write it in sushi_library (fetcherd/backupd); it arrives here by replication';
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER items_readonly BEFORE INSERT OR UPDATE OR DELETE OR TRUNCATE ON items
  FOR EACH STATEMENT EXECUTE FUNCTION sushi_catalog_readonly();
CREATE TRIGGER episodes_readonly BEFORE INSERT OR UPDATE OR DELETE OR TRUNCATE ON episodes
  FOR EACH STATEMENT EXECUTE FUNCTION sushi_catalog_readonly();
CREATE TRIGGER files_readonly BEFORE INSERT OR UPDATE OR DELETE OR TRUNCATE ON files
  FOR EACH STATEMENT EXECUTE FUNCTION sushi_catalog_readonly();
CREATE TRIGGER file_locations_readonly BEFORE INSERT OR UPDATE OR DELETE OR TRUNCATE ON file_locations
  FOR EACH STATEMENT EXECUTE FUNCTION sushi_catalog_readonly();

-- +goose Down

DROP TRIGGER IF EXISTS file_locations_readonly ON file_locations;
DROP TRIGGER IF EXISTS files_readonly ON files;
DROP TRIGGER IF EXISTS episodes_readonly ON episodes;
DROP TRIGGER IF EXISTS items_readonly ON items;
DROP FUNCTION IF EXISTS sushi_catalog_readonly();
