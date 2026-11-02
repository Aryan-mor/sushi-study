package catalog

import (
	"bytes"
	"encoding/json"
)

// PlaylistMeta is one user playlist card for /list PLAYLISTS scope (ADR 0009).
type PlaylistMeta struct {
	ID        int64
	Name      string
	ItemCount uint32
}

// BoxsetMeta is one TMDB collection card for /list BOXSETS scope — the collection's own identity,
// never a member movie's (ADR 0009).
type BoxsetMeta struct {
	CollectionID uint64
	Name         string
	Poster       string
	ItemCount    uint32
}

// PersonPage is TMDB person identity plus filmography preview rows.
type PersonPage struct {
	TMDBID    uint64
	Name      string
	Profile   string
	Biography string
	Movies    []Row
	Series    []Row
}

// MarshalRows stores compact rows as JSON for browse_people movies/series columns.
func MarshalRows(rows []Row) []byte {
	stored := storeRows(rows)
	if len(stored) == 0 {
		return []byte("[]")
	}
	b, err := json.Marshal(stored)
	if err != nil {
		return []byte("[]")
	}
	return b
}

// UnmarshalPersonPage rebuilds a PersonPage from browse_people columns.
func UnmarshalPersonPage(tmdbID uint64, name, profile, biography string, moviesRaw, seriesRaw []byte) PersonPage {
	return PersonPage{
		TMDBID:    tmdbID,
		Name:      name,
		Profile:   profile,
		Biography: biography,
		Movies:    loadRows(decodeStoredRows(moviesRaw)),
		Series:    loadRows(decodeStoredRows(seriesRaw)),
	}
}

func decodeStoredRows(raw []byte) []storedRow {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) || bytes.Equal(raw, []byte("[]")) {
		return nil
	}
	var rows []storedRow
	if json.Unmarshal(raw, &rows) != nil {
		return nil
	}
	return rows
}
