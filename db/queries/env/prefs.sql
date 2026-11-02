-- Onboarding locale (main-bot) + taste / playback prefs (app wizard).

-- name: GetPrefs :one
SELECT COALESCE(p.locale, '')::text AS locale,
       COALESCE(p.max_height, 0)::int AS max_height,
       COALESCE(p.audio, '')::text AS audio,
       COALESCE(p.subtitle_lang, '')::text AS subtitle_lang,
       COALESCE(p.subs, '')::text AS subs
FROM users u
LEFT JOIN user_prefs p ON p.user_id = u.id
WHERE u.id = $1;

-- name: GetTaste :one
SELECT COALESCE(p.watch_kind, '')::text AS watch_kind,
       COALESCE(p.genres, '{}')::text[] AS genres,
       COALESCE(p.liked_titles, '[]'::jsonb) AS liked_titles,
       COALESCE(p.liked_people, '{}')::bigint[] AS liked_people,
       COALESCE(p.max_height, 0)::int AS max_height,
       COALESCE(p.audio, '')::text AS audio,
       COALESCE(p.subtitle_lang, '')::text AS subtitle_lang,
       COALESCE(p.subs, '')::text AS subs,
       p.taste_completed_at
FROM users u
LEFT JOIN user_prefs p ON p.user_id = u.id
WHERE u.id = $1;

-- name: SetLocale :exec
INSERT INTO user_prefs (user_id, locale) VALUES ($1, $2)
ON CONFLICT (user_id) DO UPDATE SET locale = EXCLUDED.locale, updated_at = now();

-- name: SetMaxHeight :exec
INSERT INTO user_prefs (user_id, max_height) VALUES ($1, $2)
ON CONFLICT (user_id) DO UPDATE SET max_height = EXCLUDED.max_height, updated_at = now();

-- name: SetAudio :exec
INSERT INTO user_prefs (user_id, audio) VALUES ($1, $2)
ON CONFLICT (user_id) DO UPDATE SET audio = EXCLUDED.audio, updated_at = now();

-- name: SetTaste :exec
INSERT INTO user_prefs (
  user_id, watch_kind, genres, liked_titles, liked_people,
  max_height, audio, subtitle_lang, subs, taste_completed_at
) VALUES (
  $1, $2, $3, $4, $5, $6, $7, $8, $9, now()
)
ON CONFLICT (user_id) DO UPDATE SET
  watch_kind = EXCLUDED.watch_kind,
  genres = EXCLUDED.genres,
  liked_titles = EXCLUDED.liked_titles,
  liked_people = EXCLUDED.liked_people,
  max_height = EXCLUDED.max_height,
  audio = EXCLUDED.audio,
  subtitle_lang = EXCLUDED.subtitle_lang,
  subs = EXCLUDED.subs,
  taste_completed_at = now(),
  updated_at = now();

-- name: GetGeminiKeyEnc :one
SELECT gemini_api_key_enc FROM user_prefs WHERE user_id = $1;

-- name: SetGeminiKeyEnc :exec
INSERT INTO user_prefs (user_id, gemini_api_key_enc) VALUES ($1, $2)
ON CONFLICT (user_id) DO UPDATE SET gemini_api_key_enc = EXCLUDED.gemini_api_key_enc, updated_at = now();

-- name: ClearGeminiKeyEnc :exec
UPDATE user_prefs SET gemini_api_key_enc = NULL, updated_at = now() WHERE user_id = $1;
