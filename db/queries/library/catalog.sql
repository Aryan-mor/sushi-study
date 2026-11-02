-- Writes indexerd/reader make while filling the catalog (ADR 0003).

-- name: UpsertItem :one
-- Everything here is already normalised: the TMDB id is parsed, the poster path stripped, and
-- unmatched videos dropped before calling this.
INSERT INTO items (tmdb_id, kind, title, year, rating, poster, overview, released_on, detail, updated_seq)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, nextval('catalog_seq'))
ON CONFLICT (tmdb_id, kind) DO UPDATE SET
  title = EXCLUDED.title,
  year = EXCLUDED.year,
  rating = EXCLUDED.rating,
  poster = EXCLUDED.poster,
  overview = EXCLUDED.overview,
  -- The Persian synopsis (library/022) is a translation of `overview`: keep it while the English
  -- is unchanged, drop it the moment TMDB rewrites the text so a post never shows a stale one.
  overview_fa = CASE WHEN items.overview = EXCLUDED.overview THEN items.overview_fa ELSE '' END,
  released_on = EXCLUDED.released_on,
  detail = EXCLUDED.detail,
  updated_seq = nextval('catalog_seq'),
  updated_at = now()
RETURNING id;

-- name: UpsertEpisode :one
-- The watchable unit. A movie gets exactly one, at season 0 episode 0.
INSERT INTO episodes (item_id, season_no, episode_no, title, overview, updated_seq)
VALUES ($1, $2, $3, $4, $5, nextval('catalog_seq'))
ON CONFLICT (item_id, season_no, episode_no) DO UPDATE SET
  title = EXCLUDED.title,
  overview = EXCLUDED.overview,
  overview_fa = CASE WHEN episodes.overview = EXCLUDED.overview THEN episodes.overview_fa ELSE '' END,
  updated_seq = nextval('catalog_seq'),
  updated_at = now()
RETURNING id;

-- name: FileBySourceRef :one
-- What we already took from a source, so a re-run copies nothing twice.
SELECT file_id FROM source_refs WHERE source = $1 AND ext_key = $2;

-- name: TGDocsForFile :many
-- Byte identities already glued to this catalog file. Prepare refuses to reuse the file when the
-- incoming Telegram file_unique_id is not among them: a shared post address is not the same bytes.
SELECT ext_key FROM source_refs WHERE source = 'tg_doc' AND file_id = $1 ORDER BY ext_key;

-- name: InsertFileWithSourceRef :one
-- The pending file row and the source ref that makes it findable again, in one statement.
--
-- They used to be two, and a crash in the gap between them -- a long copy pass against a
-- rate-limited API, so the gap gets hit -- left a `files` row that nothing could ever reach:
-- FileBySourceRef would not find it on the next pass, so the variant was indexed again as a new
-- file, and the original sat at state 2 ('pending copy') for ever, counted by CatalogCounts and
-- fixed by nothing.
--
-- One statement is one implicit transaction, which is all the atomicity this needs -- no explicit
-- BEGIN, no transaction plumbed through the Runner, and the invariant "a files row exists only if a
-- source_ref points at it" holds by construction.
WITH inserted AS (
  INSERT INTO files (episode_id, quality_label, height,
                     audio_langs, sub_langs, size_bytes, duration_s, state, updated_seq)
  VALUES (
    sqlc.arg(episode_id), sqlc.arg(quality_label), sqlc.arg(height),
    sqlc.arg(audio_langs), sqlc.arg(sub_langs), sqlc.arg(size_bytes), sqlc.arg(duration_s),
    2, nextval('catalog_seq')
  )
  RETURNING id
), ref AS (
  INSERT INTO source_refs (source, ext_key, file_id)
  SELECT sqlc.arg(source)::text, sqlc.arg(ext_key)::text, inserted.id FROM inserted
  ON CONFLICT (source, ext_key) DO UPDATE SET file_id = EXCLUDED.file_id, synced_at = now()
)
SELECT id FROM inserted;

-- name: SetFileLocation :exec
-- Our channel, our message id. From here on the prototype's channel is never read again.
--
-- Keyed by role, not by file: one file has a clone copy and one copy per backup channel, and a
-- re-run must update the row for the role it just wrote rather than overwrite a different one.
INSERT INTO file_locations (file_id, role, channel_id, message_id) VALUES ($1, $2, $3, $4)
ON CONFLICT (file_id, role) DO UPDATE SET
  channel_id = EXCLUDED.channel_id, message_id = EXCLUDED.message_id, copied_at = now();

-- name: MarkFileReady :exec
UPDATE files SET state = 1, updated_seq = nextval('catalog_seq'), updated_at = now() WHERE id = $1;

-- name: MarkFileUnavailable :exec
UPDATE files SET state = 3, updated_seq = nextval('catalog_seq'), updated_at = now() WHERE id = $1;

-- name: CloneLocationsMissingRole :many
-- Clone coordinates that backupd has not yet copied into this role's channel.
--
-- Keyset by file id, not offset, and no longer ordered by copied_at. Both changes exist for the
-- same reason: a row that can never be copied -- its clone message deleted, the bot removed from a
-- channel -- must not sit at the head of every page forever. backupd walks file_id upward within
-- one pass and steps over what fails, so a broken row costs one attempt per pass instead of
-- blocking the whole role behind it.
--
-- It is also cheaper: (file_id, role) is the primary key, so this is a range scan and the
-- NOT EXISTS is a key lookup. Ordering by copied_at sorted every clone row on every page.
SELECT fl.file_id, fl.channel_id, fl.message_id
FROM file_locations fl
WHERE fl.role = 'clone'
  AND fl.file_id > sqlc.arg(after_file_id)
  AND NOT EXISTS (
    SELECT 1 FROM file_locations b
    WHERE b.file_id = fl.file_id AND b.role = sqlc.arg(role)
  )
ORDER BY fl.file_id
LIMIT sqlc.arg(page_size);

-- name: CloneLocationForFile :one
-- Where deliveryd copies from. Same principle as backupd: trust the coordinates the row actually
-- recorded, never CHANNELS's current clone id -- the two stop agreeing the moment a clone is
-- replaced.
SELECT channel_id, message_id FROM file_locations WHERE file_id = $1 AND role = 'clone';

-- name: CatalogCounts :one
SELECT
  (SELECT count(*) FROM items)::bigint AS items,
  (SELECT count(*) FROM episodes)::bigint AS episodes,
  (SELECT count(*) FROM files WHERE state = 1)::bigint AS ready_files,
  (SELECT count(*) FROM files WHERE state = 2)::bigint AS pending_files;

-- name: ListItemKeys :many
SELECT tmdb_id, kind FROM items;

-- name: GetSyncCursor :one
-- Where a source walk left off, so a re-run resumes instead of re-walking from the start
-- every time (doc 06 §2, "what makes a re-run cheap").
SELECT cursor FROM sync_cursors WHERE source = $1;

-- name: SetSyncCursor :exec
INSERT INTO sync_cursors (source, cursor, updated_at) VALUES ($1, $2, now())
ON CONFLICT (source) DO UPDATE SET cursor = EXCLUDED.cursor, updated_at = now();

-- name: ListItemDetail :many
SELECT tmdb_id, kind, detail FROM items;

-- name: UpdateItemDetail :exec
UPDATE items
SET detail = $3, updated_seq = nextval('catalog_seq'), updated_at = now()
WHERE tmdb_id = $1 AND kind = $2;

-- name: SetItemBackdrops :exec
-- Extra TMDB backdrop stems for the news slideshow. jsonb_set leaves the rest of detail alone.
-- Replicated to env so the next approve reads them without another TMDB call.
UPDATE items
SET detail = jsonb_set(
      COALESCE(detail, '{}'::jsonb),
      '{backdrops}',
      sqlc.arg(backdrops)::jsonb,
      true
    ),
    updated_seq = nextval('catalog_seq'),
    updated_at = now()
WHERE tmdb_id = sqlc.arg(tmdb_id) AND kind = sqlc.arg(kind);

-- name: RenameItem :exec
-- A superadmin's cosmetic title fix from the main-bot probe card (ADR 0026), not a fetcherd write:
-- no TMDB re-fetch, just the displayed text.
UPDATE items SET title = $2, updated_seq = nextval('catalog_seq'), updated_at = now() WHERE id = $1;

-- name: SetItemTitleFa :exec
-- Persian search/display alias (library/029). UpsertItem does not touch it.
UPDATE items
SET title_fa = sqlc.arg(title_fa), updated_seq = nextval('catalog_seq'), updated_at = now()
WHERE tmdb_id = sqlc.arg(tmdb_id) AND kind = sqlc.arg(kind);

-- name: SetItemOverviewFa :execrows
-- main-bot's Persian synopsis for a news post (ADR 0032). Guarded on the English it translated:
-- if a hydrate rewrote `overview` while the LLM was thinking, the answer is for text we no longer
-- have and is dropped rather than stored against the new one.
UPDATE items
SET overview_fa = sqlc.arg(overview_fa), updated_seq = nextval('catalog_seq'), updated_at = now()
WHERE tmdb_id = sqlc.arg(tmdb_id) AND kind = sqlc.arg(kind)
  AND overview = sqlc.arg(overview) AND overview <> '';

-- name: SetEpisodeOverviewFa :execrows
-- Same, for an episode debut captioned with the episode's own synopsis.
UPDATE episodes e
SET overview_fa = sqlc.arg(overview_fa), updated_seq = nextval('catalog_seq'), updated_at = now()
FROM items i
WHERE i.id = e.item_id AND i.tmdb_id = sqlc.arg(tmdb_id) AND i.kind = 2
  AND e.season_no = sqlc.arg(season_no) AND e.episode_no = sqlc.arg(episode_no)
  AND e.overview = sqlc.arg(overview) AND e.overview <> '';

-- name: SetFileAudioLangs :exec
-- Same probe-card correction, for the audio_langs label only.
UPDATE files SET audio_langs = $2, updated_seq = nextval('catalog_seq'), updated_at = now() WHERE id = $1;

-- name: SetFileSubLangs :exec
-- Same probe-card correction, for the sub_langs label only.
UPDATE files SET sub_langs = $2, updated_seq = nextval('catalog_seq'), updated_at = now() WHERE id = $1;

-- name: SetFileDisplayName :exec
-- Probe-card file label. Empty clears it so the app falls back to quality_label.
-- quality_label stays: classify, hardsub hiding, and delivery still read that.
UPDATE files SET display_name = $2, updated_seq = nextval('catalog_seq'), updated_at = now() WHERE id = $1;

-- name: RelocateFileEpisode :exec
-- Reclassify can change season/episode after the file row already exists (talk-show air date).
-- FileBySourceRef is the idempotency key, so Prepare must move the existing file onto the new
-- episode rather than leave it on S00E00.
UPDATE files
SET episode_id = sqlc.arg(episode_id),
    updated_seq = nextval('catalog_seq'),
    updated_at = now()
WHERE id = sqlc.arg(id) AND episode_id IS DISTINCT FROM sqlc.arg(episode_id);

