
-- +goose Up

CREATE INDEX search_events_at_idx ON search_events (at);
CREATE INDEX play_events_at_idx ON play_events (at);
CREATE INDEX browse_titles_fetched_idx ON browse_titles (fetched_at);
CREATE INDEX browse_people_fetched_idx ON browse_people (fetched_at);

CREATE INDEX deliveries_chat_idx ON deliveries (reader_chat_id);

-- +goose Down

DROP INDEX IF EXISTS deliveries_chat_idx;
DROP INDEX IF EXISTS browse_people_fetched_idx;
DROP INDEX IF EXISTS browse_titles_fetched_idx;
DROP INDEX IF EXISTS play_events_at_idx;
DROP INDEX IF EXISTS search_events_at_idx;
