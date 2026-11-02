
-- +goose Up

DROP INDEX IF EXISTS episodes_item_idx;

DROP INDEX IF EXISTS file_locations_role_idx;
CREATE INDEX file_locations_clone_idx ON file_locations (file_id) WHERE role = 'clone';

CREATE INDEX items_genres_idx ON items USING gin ((detail->'genreList') jsonb_path_ops);

CREATE INDEX items_collection_idx
  ON items (((detail->>'collectionId')::bigint), year DESC NULLS LAST)
  WHERE detail ? 'collectionId';

CREATE INDEX bindings_api_bot_idx ON bindings (api_bot_id) WHERE superseded_at IS NULL;
CREATE INDEX deliveries_bot_idx ON deliveries (bot_id);
CREATE INDEX deliveries_claimed_by_idx ON deliveries (claimed_by);
CREATE INDEX play_events_user_idx ON play_events (user_id);

-- +goose Down

DROP INDEX IF EXISTS play_events_user_idx;
DROP INDEX IF EXISTS deliveries_claimed_by_idx;
DROP INDEX IF EXISTS deliveries_bot_idx;
DROP INDEX IF EXISTS bindings_api_bot_idx;
DROP INDEX IF EXISTS items_collection_idx;
DROP INDEX IF EXISTS items_genres_idx;
DROP INDEX IF EXISTS file_locations_clone_idx;
CREATE INDEX file_locations_role_idx ON file_locations (role);
CREATE INDEX episodes_item_idx ON episodes (item_id, season_no, episode_no);
