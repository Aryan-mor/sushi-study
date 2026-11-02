package catalog

import (
	"bytes"
	"encoding/json"
	"strconv"
	"strings"
)

const (
	maxPeople     = 20
	maxSimilarIDs = 16
)

// Person is one cast/crew name on a title page.
type Person struct {
	Name    string `json:"name,omitempty"`
	Role    string `json:"role,omitempty"`
	Profile string `json:"profile,omitempty"`
	// TMDBID is the TMDB person id; zero means unknown (older detail JSON).
	TMDBID uint64 `json:"tmdbId,omitempty"`
}

// Detail is the title-page extras docs/12 §4 lists: logo, backdrop, genres, runtime, cast.
// Related TMDB ids and a collection id are stored so Item() can join against our catalog.
type Detail struct {
	Logo           string
	Backdrop       string
	Genres         string
	RuntimeS       int32
	People         []Person
	SimilarTMDB    []uint64
	CollectionID   uint64
	CollectionName string
	RelatedPreview []Row
	// Country is ISO 3166-1 alpha-2 origin (US, GB, IR). CountryNone means TMDB was checked and
	// had none. Empty still means unknown so DetailNeedsHydrate re-fetches. News reads this for
	// the meta table (ADR 0017); it is not on the wire.
	Country string
	// LastAirDate is TMDB last_episode_to_air.air_date / last_air_date (YYYY-MM-DD). Series only.
	// Browse YEAR sort uses this so a 1989 show with a 2026 episode ranks by the episode, not
	// first-air and not our ingest clock.
	LastAirDate string
	// TrailerKey is a YouTube video id (TMDB videos, type=Trailer/Teaser, site=YouTube).
	// TrailerNone means TMDB was checked and had none. Empty still means unknown/not hydrated.
	TrailerKey string
	// Network is the broadcast or streaming name (Netflix, Disney+), comma-joined when there are
	// a few. NetworkNone means TMDB was checked and had none. Empty still means unknown.
	// News prints the row only when a real name is stored.
	Network string
	// Backdrops are extra TMDB backdrop stems (not the primary Backdrop) for the news slideshow.
	// Nil means not stored yet. An empty slice means TMDB was checked and had no extras.
	Backdrops []string
}

type storedDetail struct {
	Logo     string `json:"logo,omitempty"`
	Backdrop string `json:"backdrop,omitempty"`
	Genres   string `json:"genres,omitempty"`
	// GenreList is Genres split back into names, stored so browse can filter with jsonb
	// containment over an index instead of a substring match over the sentence above -- which
	// matched "Fantasy" inside "Sci-Fi & Fantasy" and could not use an index at all
	// (db/migrations/library/004, db/queries/env/browse.sql). It is derived, never read back:
	// Genres stays the single display value and the only one on the wire.
	GenreList      []string    `json:"genreList,omitempty"`
	RuntimeS       int32       `json:"runtimeS,omitempty"`
	People         []Person    `json:"people,omitempty"`
	SimilarTMDB    []uint64    `json:"similarTmdb,omitempty"`
	CollectionID   uint64      `json:"collectionId,omitempty"`
	CollectionName string      `json:"collectionName,omitempty"`
	RelatedPreview []storedRow `json:"relatedPreview,omitempty"`
	Country        string      `json:"country,omitempty"`
	LastAirDate    string      `json:"lastAirDate,omitempty"`
	TrailerKey     string      `json:"trailerKey,omitempty"`
	Backdrops      []string    `json:"backdrops,omitempty"`
	Network        string      `json:"network,omitempty"`
}

type storedRow struct {
	TMDBID uint64 `json:"tmdbId,omitempty"`
	Kind   uint8  `json:"kind,omitempty"`
	Title  string `json:"title,omitempty"`
	Year   uint32 `json:"year,omitempty"`
	Rating uint32 `json:"rating,omitempty"`
	Poster string `json:"poster,omitempty"`
}

// MarshalDetail stores extras on items.detail. Empty detail is "{}".
func MarshalDetail(d Detail) []byte {
	if d.Logo == "" && d.Backdrop == "" && d.Genres == "" && d.RuntimeS == 0 &&
		len(d.People) == 0 && len(d.SimilarTMDB) == 0 && d.CollectionID == 0 &&
		len(d.RelatedPreview) == 0 && d.Country == "" && d.LastAirDate == "" && d.TrailerKey == "" &&
		len(d.Backdrops) == 0 && d.Network == "" {
		return []byte("{}")
	}
	b, err := json.Marshal(storedDetail{
		Logo:           d.Logo,
		Backdrop:       d.Backdrop,
		Genres:         d.Genres,
		GenreList:      SplitGenres(d.Genres),
		RuntimeS:       d.RuntimeS,
		People:         d.People,
		SimilarTMDB:    d.SimilarTMDB,
		CollectionID:   d.CollectionID,
		CollectionName: d.CollectionName,
		RelatedPreview: storeRows(d.RelatedPreview),
		Country:        d.Country,
		LastAirDate:    d.LastAirDate,
		TrailerKey:     d.TrailerKey,
		Backdrops:      d.Backdrops,
		Network:        d.Network,
	})
	if err != nil {
		return []byte("{}")
	}
	return b
}

// ParseStoredBackdrops reads items.detail->>'backdrops'.
// An empty raw string means the key was never stored. "[]" means TMDB was checked and had no extras.
func ParseStoredBackdrops(raw string) (stems []string, known bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, false
	}
	var list []string
	if json.Unmarshal([]byte(raw), &list) != nil {
		return nil, false
	}
	out := make([]string, 0, len(list))
	seen := map[string]struct{}{}
	for _, s := range list {
		s = strings.TrimSpace(s)
		if s == "" || s == "-" {
			continue
		}
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	return out, true
}

// UnmarshalDetail reads items.detail. Invalid JSON is treated as empty (R-META-2).
func UnmarshalDetail(raw []byte) Detail {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || bytes.Equal(raw, []byte("{}")) || bytes.Equal(raw, []byte("null")) {
		return Detail{}
	}
	var s storedDetail
	if json.Unmarshal(raw, &s) != nil {
		return Detail{}
	}
	return Detail{
		Logo:           s.Logo,
		Backdrop:       s.Backdrop,
		Genres:         s.Genres,
		RuntimeS:       s.RuntimeS,
		People:         s.People,
		SimilarTMDB:    s.SimilarTMDB,
		CollectionID:   s.CollectionID,
		CollectionName: s.CollectionName,
		RelatedPreview: loadRows(s.RelatedPreview),
		Country:        s.Country,
		LastAirDate:    s.LastAirDate,
		TrailerKey:     s.TrailerKey,
		Backdrops:      s.Backdrops,
		Network:        s.Network,
	}
}

// SplitGenres turns the display string back into the names it was joined from.
//
// Tolerant of both separators the writers use -- detail.joinGenres and tmdb.joinNamed both join on
// ", ", but a name is never allowed to contain a comma, so splitting on the comma alone is enough
// and survives either. Empty pieces are dropped, so "" yields no genres rather than one blank one.
func SplitGenres(genres string) []string {
	if strings.TrimSpace(genres) == "" {
		return nil
	}
	parts := strings.Split(genres, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func storeRows(rows []Row) []storedRow {
	out := make([]storedRow, 0, len(rows))
	for _, r := range rows {
		if r.TMDBID == 0 {
			continue
		}
		out = append(out, storedRow{
			TMDBID: r.TMDBID,
			Kind:   uint8(r.Kind),
			Title:  r.Title,
			Year:   r.Year,
			Rating: r.Rating,
			Poster: r.Poster,
		})
	}
	return out
}

func loadRows(rows []storedRow) []Row {
	out := make([]Row, 0, len(rows))
	for _, r := range rows {
		if r.TMDBID == 0 {
			continue
		}
		out = append(out, Row{
			TMDBID: r.TMDBID,
			Kind:   Kind(r.Kind),
			Title:  r.Title,
			Year:   r.Year,
			Rating: r.Rating,
			Poster: r.Poster,
		})
	}
	return out
}

// ParseOxplayerMetadata pulls title-page fields from the prototype catalog_items.metadata_json.
func ParseOxplayerMetadata(raw []byte) Detail {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || bytes.Equal(raw, []byte("{}")) || bytes.Equal(raw, []byte("null")) {
		return Detail{}
	}
	var root map[string]json.RawMessage
	if json.Unmarshal(raw, &root) != nil {
		return Detail{}
	}
	d := Detail{
		Logo:           PosterPath(jsonString(root["logoUrl"])),
		Backdrop:       PosterPath(jsonString(root["backdropUrl"])),
		Genres:         joinGenres(root["genres"], root["genreItems"]),
		RuntimeS:       jsonInt32(root["runtimeSeconds"]),
		People:         parsePeople(firstRaw(root, "people", "People")),
		SimilarTMDB:    parseSimilar(root["tmdbSimilar"]),
		CollectionID:   jsonUint64(root["tmdbCollectionId"]),
		CollectionName: strings.TrimSpace(jsonString(root["tmdbCollectionName"])),
		Country: OriginCountryJSON(
			firstRaw(root, "originCountry", "origin_country", "OriginCountry"),
			firstRaw(root, "productionCountries", "production_countries", "ProductionCountries"),
		),
	}
	return d
}

func jsonString(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var s string
	if json.Unmarshal(raw, &s) != nil {
		return ""
	}
	return s
}

func jsonInt32(raw json.RawMessage) int32 {
	if len(raw) == 0 {
		return 0
	}
	var n float64
	if json.Unmarshal(raw, &n) != nil || n <= 0 {
		return 0
	}
	return int32(n)
}

func jsonUint64(raw json.RawMessage) uint64 {
	if len(raw) == 0 {
		return 0
	}
	var n float64
	if json.Unmarshal(raw, &n) == nil && n > 0 {
		return uint64(n)
	}
	var s string
	if json.Unmarshal(raw, &s) != nil {
		return 0
	}
	v, err := strconv.ParseUint(strings.TrimSpace(s), 10, 64)
	if err != nil {
		return 0
	}
	return v
}

func joinGenres(genresRaw, itemsRaw json.RawMessage) string {
	names := jsonStringSlice(genresRaw)
	if len(names) == 0 {
		names = genreMapNames(itemsRaw)
	}
	return strings.Join(nonEmptyTrimmed(names), ", ")
}

func jsonStringSlice(raw json.RawMessage) []string {
	var asStrings []string
	if json.Unmarshal(raw, &asStrings) == nil {
		return asStrings
	}
	return nil
}

func genreMapNames(itemsRaw json.RawMessage) []string {
	if len(itemsRaw) == 0 {
		return nil
	}
	var items []map[string]string
	if json.Unmarshal(itemsRaw, &items) != nil {
		return nil
	}
	var names []string
	for _, g := range items {
		n := strings.TrimSpace(g["Name"])
		if n == "" {
			n = strings.TrimSpace(g["name"])
		}
		if n != "" {
			names = append(names, n)
		}
	}
	return names
}

func nonEmptyTrimmed(names []string) []string {
	out := make([]string, 0, len(names))
	for _, n := range names {
		n = strings.TrimSpace(n)
		if n != "" {
			out = append(out, n)
		}
	}
	return out
}

func parsePeople(raw json.RawMessage) []Person {
	if len(raw) == 0 {
		return nil
	}
	var rows []map[string]json.RawMessage
	if json.Unmarshal(raw, &rows) != nil {
		return nil
	}
	out := make([]Person, 0, min(len(rows), maxPeople))
	for _, row := range rows {
		if len(out) >= maxPeople {
			break
		}
		name := firstString(row, "Name", "name")
		if name == "" {
			continue
		}
		role := firstString(row, "Role", "role", "character", "Character", "type", "Type")
		profile := PosterPath(firstString(row, "profilePath", "imageUrl", "ImageUrl", "profile"))
		tmdbID := personTMDBID(row)
		out = append(out, Person{Name: name, Role: role, Profile: profile, TMDBID: tmdbID})
	}
	return out
}

// personTMDBID prefers explicit tmdb fields / ProviderIds.Tmdb. Plain "id" is used only when it
// parses as a positive integer (TMDB person id); Jellyfin GUIDs stay zero.
func personTMDBID(row map[string]json.RawMessage) uint64 {
	if id := jsonUint64(row["tmdbId"]); id > 0 {
		return id
	}
	if id := jsonUint64(row["tmdb_id"]); id > 0 {
		return id
	}
	if id := providerTMDB(row, "ProviderIds", "providerIds"); id > 0 {
		return id
	}
	return jsonUint64(row["id"])
}

func providerTMDB(row map[string]json.RawMessage, keys ...string) uint64 {
	for _, k := range keys {
		raw := bytes.TrimSpace(row[k])
		if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
			continue
		}
		var m map[string]json.RawMessage
		if json.Unmarshal(raw, &m) != nil {
			continue
		}
		for _, pk := range []string{"Tmdb", "tmdb", "TMDB", "tmdbId"} {
			if id := jsonUint64(m[pk]); id > 0 {
				return id
			}
		}
	}
	return 0
}

func firstRaw(root map[string]json.RawMessage, keys ...string) json.RawMessage {
	for _, k := range keys {
		raw := bytes.TrimSpace(root[k])
		if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
			continue
		}
		return raw
	}
	return nil
}

func firstString(row map[string]json.RawMessage, keys ...string) string {
	for _, k := range keys {
		if s := strings.TrimSpace(jsonString(row[k])); s != "" {
			return s
		}
	}
	return ""
}

func parseSimilar(raw json.RawMessage) []uint64 {
	if len(raw) == 0 {
		return nil
	}
	var wrap struct {
		Results []struct {
			TmdbID uint64 `json:"tmdbId"`
			ID     uint64 `json:"id"`
		} `json:"results"`
	}
	if json.Unmarshal(raw, &wrap) != nil {
		return nil
	}
	out := make([]uint64, 0, min(len(wrap.Results), maxSimilarIDs))
	seen := map[uint64]struct{}{}
	for _, r := range wrap.Results {
		id := r.TmdbID
		if id == 0 {
			id = r.ID
		}
		if id == 0 {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
		if len(out) >= maxSimilarIDs {
			break
		}
	}
	return out
}
