package newsroom

import (
	"context"
	"encoding/json"
	"log/slog"

	"github.com/Aryan-mor/sushi-study/be/internal/core/catalog"
)

// BackdropLookup loads extra TMDB backdrop stems when items.detail has not stored any yet.
type BackdropLookup interface {
	ExtraBackdrops(ctx context.Context, tmdbID uint64, kind catalog.Kind, primary string) ([]string, error)
}

// BackdropStore writes those stems onto items.detail so the next post skips TMDB.
type BackdropStore interface {
	SaveBackdrops(ctx context.Context, tmdbID uint64, kind catalog.Kind, stems []string) error
}

// FillBackdrops attaches gallery stills. A stored list (even empty) wins. A miss asks TMDB
// once and saves the answer, including an empty list so a title with no extras is not fetched
// on every preview.
func FillBackdrops(
	ctx context.Context, lookup BackdropLookup, store BackdropStore, log *slog.Logger, a Announcement,
) Announcement {
	if a.BackdropsKnown || lookup == nil || a.TMDBID == 0 {
		return a
	}
	stems, err := lookup.ExtraBackdrops(ctx, a.TMDBID, a.Kind, a.Backdrop)
	if err != nil {
		if log != nil {
			log.Warn("newsroom: backdrops", "err", err, "tmdb", a.TMDBID, "kind", a.Kind)
		}
		return a
	}
	// nil with no error means the lookup declined (no API key). Do not persist that as "none".
	if stems == nil {
		return a
	}
	a.Backdrops = stems
	a.BackdropsKnown = true
	if store != nil {
		if err := store.SaveBackdrops(ctx, a.TMDBID, a.Kind, stems); err != nil && log != nil {
			log.Warn("newsroom: save backdrops", "err", err, "tmdb", a.TMDBID, "kind", a.Kind)
		}
	}
	return a
}

// BackdropJSON is the jsonb value SetItemBackdrops stores. An empty list is "[]", not omitted,
// so a later read can tell "checked" from "never stored".
func BackdropJSON(stems []string) []byte {
	if stems == nil {
		stems = []string{}
	}
	b, err := json.Marshal(stems)
	if err != nil {
		return []byte("[]")
	}
	return b
}
