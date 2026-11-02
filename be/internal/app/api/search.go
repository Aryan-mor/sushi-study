package api

import (
	"context"
	"fmt"
	"time"

	"github.com/Aryan-mor/sushi-study/be/internal/core/catalog"
	sushiv1 "github.com/Aryan-mor/sushi-study/be/internal/protocol/sushi/v1"
)

// searchPageSize is one page of results: ~1.2 KB encoded, comfortably one message (docs/12 §6).
const searchPageSize = 20

// searchMissingCap is how many TMDB-only cards ride on the first page (ADR 0010).
const searchMissingCap = 5

// searchTMDBTimeout bounds the missing-strip call so a slow TMDB never blocks catalog rows.
const searchTMDBTimeout = 2 * time.Second

// searchRetryAfter is the hint for a tripped R-SEC-11 search cap. Unlike a delivery's concurrent-job
// cap, nothing here clears early -- the hourly window is the only thing that does, so the hint is
// the same order of magnitude as Play's hourly one.
const searchRetryAfter = 5 * time.Minute

// SearchStore is what Search needs from storage. Declared here, by the consumer, and small
// (R-GO-5).
type SearchStore interface {
	SearchItems(ctx context.Context, query string, offset, limit uint32) ([]catalog.Row, error)
	RecordSearch(ctx context.Context, userID int64) error
	SearchCountSince(ctx context.Context, userID int64, since time.Time) (int, error)
	CatalogKeysByTMDB(ctx context.Context, ids []uint64) ([]catalog.RowKey, error)
	CatalogRowsByTMDBAnyKind(ctx context.Context, ids []uint64) ([]catalog.Row, error)
}

// TitleSearcher loads TMDB multi-search compact rows (ADR 0010).
type TitleSearcher interface {
	SearchTitles(ctx context.Context, query string) ([]catalog.Row, error)
}

// Search answers one committed query with a page of compact rows, ranked by title similarity
// (docs/12 §6). Debouncing and "committed, not per-keystroke" are the client's job (R-META-3); the
// server only enforces the volume cap (R-SEC-11, 60/hour). First page may append TMDB-missing rows.
func (a *API) Search(ctx context.Context, p Principal, req *sushiv1.SearchReq) (*sushiv1.SearchRes, error) {
	if req.GetQuery() == "" {
		// Defaulting to "everything" would hide a client bug behind an answer that looks right, the
		// same reasoning Home and Item apply to their own required fields.
		return nil, fmt.Errorf("search: query not specified")
	}

	// Scoped to the person, not the reader chat: unlike Play's caps (docs/06 §3, genuinely
	// per-reader because message bookkeeping is), a search costs nothing reader-specific, so scoping
	// it to ReaderChatID would just let one person double their budget across a session and a
	// personal bot.
	if err := a.checkSearchLimit(ctx, p.UserID); err != nil {
		return nil, err
	}

	rows, err := a.searches.SearchItems(ctx, req.GetQuery(), req.GetCursor(), searchPageSize)
	if err != nil {
		return nil, fmt.Errorf("search: %w", err)
	}
	if req.GetCursor() == 0 && len(rows) == 0 {
		rows = a.searchCatalogViaTMDB(ctx, req.GetQuery())
	}

	var cursor uint32
	if len(rows) == searchPageSize {
		// A full page might not be the last one; a short page always is. This can occasionally send
		// one extra, empty-handed request at the exact boundary, which costs a message but never a
		// wrong answer -- cheaper than a second COUNT query on every search.
		cursor = req.GetCursor() + searchPageSize
	}

	missing := a.searchMissing(ctx, req.GetQuery(), req.GetCursor())
	return &sushiv1.SearchRes{Rows: protoRows(rows), Cursor: cursor, Missing: protoRows(missing)}, nil
}

// searchCatalogViaTMDB returns catalog rows when pg_trgm missed but TMDB matched an in-catalog
// title (e.g. Persian «بامداد خمار» vs English items.title). Fail-open; first page only.
func (a *API) searchCatalogViaTMDB(ctx context.Context, query string) []catalog.Row {
	if a.searcher == nil {
		return nil
	}
	tctx, cancel := context.WithTimeout(ctx, searchTMDBTimeout)
	defer cancel()
	hits, err := a.searcher.SearchTitles(tctx, query)
	if err != nil || len(hits) == 0 {
		return nil
	}
	inCatalog, err := a.catalogKeySet(ctx, tmdbIDsFromRows(hits))
	if err != nil || len(inCatalog) == 0 {
		return nil
	}
	wantIDs := catalogTMDBIDsFromHits(hits, inCatalog, searchPageSize)
	if len(wantIDs) == 0 {
		return nil
	}
	rows, err := a.searches.CatalogRowsByTMDBAnyKind(ctx, wantIDs)
	if err != nil {
		return nil
	}
	return rows
}

func tmdbIDsFromRows(rows []catalog.Row) []uint64 {
	ids := make([]uint64, 0, len(rows))
	for _, h := range rows {
		if h.TMDBID > 0 {
			ids = append(ids, h.TMDBID)
		}
	}
	return ids
}

func (a *API) catalogKeySet(ctx context.Context, ids []uint64) (map[catalog.RowKey]struct{}, error) {
	keys, err := a.searches.CatalogKeysByTMDB(ctx, ids)
	if err != nil {
		return nil, err
	}
	out := map[catalog.RowKey]struct{}{}
	for _, k := range keys {
		out[k] = struct{}{}
	}
	return out, nil
}

func catalogTMDBIDsFromHits(hits []catalog.Row, inCatalog map[catalog.RowKey]struct{}, maxN int) []uint64 {
	wantIDs := make([]uint64, 0, maxN)
	seen := map[uint64]struct{}{}
	for _, h := range hits {
		if _, ok := inCatalog[catalog.RowKey{TMDBID: h.TMDBID, Kind: h.Kind}]; !ok {
			continue
		}
		if _, dup := seen[h.TMDBID]; dup {
			continue
		}
		seen[h.TMDBID] = struct{}{}
		wantIDs = append(wantIDs, h.TMDBID)
		if len(wantIDs) >= maxN {
			break
		}
	}
	return wantIDs
}

// searchMissing returns ≤5 TMDB titles not in catalog. Fail-open; only on first page.
func (a *API) searchMissing(ctx context.Context, query string, cursor uint32) []catalog.Row {
	if cursor != 0 || a.searcher == nil {
		return nil
	}
	tctx, cancel := context.WithTimeout(ctx, searchTMDBTimeout)
	defer cancel()
	hits, err := a.searcher.SearchTitles(tctx, query)
	if err != nil || len(hits) == 0 {
		return nil
	}
	ids := make([]uint64, 0, len(hits))
	for _, h := range hits {
		ids = append(ids, h.TMDBID)
	}
	keys, err := a.searches.CatalogKeysByTMDB(ctx, ids)
	if err != nil {
		return nil
	}
	inCatalog := map[catalog.RowKey]struct{}{}
	for _, k := range keys {
		inCatalog[k] = struct{}{}
	}
	out := make([]catalog.Row, 0, searchMissingCap)
	for _, h := range hits {
		if len(out) >= searchMissingCap {
			break
		}
		if _, ok := inCatalog[catalog.RowKey{TMDBID: h.TMDBID, Kind: h.Kind}]; ok {
			continue
		}
		out = append(out, h)
	}
	return out
}

// checkSearchLimit enforces R-SEC-11 before spending a query on the catalog. Per identity, not per
// request: somebody already at the cap gets ErrRateLimited on every further attempt until the hour
// rolls over -- the same shape as Play's checkPlayLimits.
func (a *API) checkSearchLimit(ctx context.Context, userID int64) error {
	n, err := a.searches.SearchCountSince(ctx, userID, time.Now().Add(-time.Hour))
	if err != nil {
		return fmt.Errorf("search: rate check: %w", err)
	}
	if n >= a.limits.SearchPerHour {
		return &RateLimitError{RetryAfter: searchRetryAfter}
	}
	if err := a.searches.RecordSearch(ctx, userID); err != nil {
		return fmt.Errorf("search: record: %w", err)
	}
	return nil
}
