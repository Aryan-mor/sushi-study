package api_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/Aryan-mor/sushi-study/be/internal/app/api"
	"github.com/Aryan-mor/sushi-study/be/internal/core/catalog"
	sushiv1 "github.com/Aryan-mor/sushi-study/be/internal/protocol/sushi/v1"
)

// fakeSearch is the in-process store Search runs against: a fixed result set (paged in memory, the
// way SearchItems' LIMIT/OFFSET would) plus a per-user event count, mirroring search_events closely
// enough to exercise the R-SEC-11 cap (R-API-3).
type fakeSearch struct {
	mu          sync.Mutex
	rows        []catalog.Row
	catalogKeys []catalog.RowKey
	catalogRows []catalog.Row
	events      map[int64][]time.Time
	err         error
	queries     []string
}

func (f *fakeSearch) SearchItems(_ context.Context, query string, offset, limit uint32) ([]catalog.Row, error) {
	if f.err != nil {
		return nil, f.err
	}
	f.mu.Lock()
	f.queries = append(f.queries, query)
	f.mu.Unlock()

	if int(offset) >= len(f.rows) {
		return nil, nil
	}
	end := int(offset) + int(limit)
	if end > len(f.rows) {
		end = len(f.rows)
	}
	return f.rows[offset:end], nil
}

func (f *fakeSearch) RecordSearch(_ context.Context, userID int64) error {
	if f.err != nil {
		return f.err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.events == nil {
		f.events = map[int64][]time.Time{}
	}
	f.events[userID] = append(f.events[userID], time.Now())
	return nil
}

func (f *fakeSearch) SearchCountSince(_ context.Context, userID int64, since time.Time) (int, error) {
	if f.err != nil {
		return 0, f.err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, at := range f.events[userID] {
		if at.After(since) {
			n++
		}
	}
	return n, nil
}

func (f *fakeSearch) CatalogKeysByTMDB(_ context.Context, ids []uint64) ([]catalog.RowKey, error) {
	if f.err != nil {
		return nil, f.err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	want := map[uint64]struct{}{}
	for _, id := range ids {
		want[id] = struct{}{}
	}
	var out []catalog.RowKey
	for _, k := range f.catalogKeys {
		if _, ok := want[k.TMDBID]; ok {
			out = append(out, k)
		}
	}
	return out, nil
}

func (f *fakeSearch) CatalogRowsByTMDBAnyKind(_ context.Context, ids []uint64) ([]catalog.Row, error) {
	if f.err != nil {
		return nil, f.err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	want := map[uint64]struct{}{}
	for _, id := range ids {
		want[id] = struct{}{}
	}
	var out []catalog.Row
	for _, r := range f.catalogRows {
		if _, ok := want[r.TMDBID]; ok {
			out = append(out, r)
		}
	}
	return out, nil
}

func searchAPI(s *fakeSearch, limits api.RateLimits) *api.API {
	return api.New(&fakeRails{}, &fakeDeliveries{}, &fakeCatalog{}, s, limits)
}

func TestSearchReturnsAPageOfRows(t *testing.T) {
	t.Parallel()

	s := &fakeSearch{rows: []catalog.Row{row(1, "Tenet"), row(2, "Tenacious D")}}
	res, err := searchAPI(s, api.RateLimits{SearchPerHour: 60}).
		Search(context.Background(), api.Principal{UserID: 9}, &sushiv1.SearchReq{Query: "ten"})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(res.GetRows()) != 2 {
		t.Fatalf("got %d rows, want 2", len(res.GetRows()))
	}
	if res.GetRows()[0].GetTitle() != "Tenet" {
		t.Errorf("first row = %q, want Tenet", res.GetRows()[0].GetTitle())
	}
	if res.GetCursor() != 0 {
		t.Errorf("cursor = %d, want 0 (a short page is the last one)", res.GetCursor())
	}
}

func TestSearchCursorAdvancesOnAFullPage(t *testing.T) {
	t.Parallel()

	rows := make([]catalog.Row, 20)
	for i := range rows {
		rows[i] = row(uint64(i+1), "x")
	}
	s := &fakeSearch{rows: rows}
	res, err := searchAPI(s, api.RateLimits{SearchPerHour: 60}).
		Search(context.Background(), api.Principal{UserID: 9}, &sushiv1.SearchReq{Query: "x"})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(res.GetRows()) != 20 {
		t.Fatalf("got %d rows, want 20", len(res.GetRows()))
	}
	if res.GetCursor() != 20 {
		t.Errorf("cursor = %d, want 20 (a full page may have more)", res.GetCursor())
	}
}

func TestSearchRejectsAnEmptyQuery(t *testing.T) {
	t.Parallel()

	_, err := searchAPI(&fakeSearch{}, api.RateLimits{SearchPerHour: 60}).
		Search(context.Background(), api.Principal{UserID: 9}, &sushiv1.SearchReq{})
	if err == nil {
		t.Fatal("want an error; an empty query is a client bug, not \"search for everything\"")
	}
}

func TestSearchRejectsAtTheHourlyCap(t *testing.T) {
	t.Parallel()

	s := &fakeSearch{}
	ctx := context.Background()
	a := searchAPI(s, api.RateLimits{SearchPerHour: 2})

	for i := 0; i < 2; i++ {
		if _, err := a.Search(ctx, api.Principal{UserID: 9}, &sushiv1.SearchReq{Query: "x"}); err != nil {
			t.Fatalf("search %d: %v", i, err)
		}
	}

	_, err := a.Search(ctx, api.Principal{UserID: 9}, &sushiv1.SearchReq{Query: "x"})
	var rl *api.RateLimitError
	if !errors.As(err, &rl) {
		t.Fatalf("err = %v, want a *api.RateLimitError (R-SEC-11: 60 search/hour default, 2 here)", err)
	}
	if rl.RetryAfter <= 0 {
		t.Error("RetryAfter is zero; docs/02 §6 treats zero as \"do not retry\", never send it for ERR_RATE")
	}
}

func TestSearchLimitIsScopedPerUserNotReaderChat(t *testing.T) {
	t.Parallel()

	// Unlike Play's caps, a search costs nothing reader-specific: one person must not double their
	// budget by asking from a session and a personal bot (docs/06 §3 explains the axis; this is the
	// opposite choice, deliberately).
	s := &fakeSearch{}
	ctx := context.Background()
	a := searchAPI(s, api.RateLimits{SearchPerHour: 1})

	if _, err := a.Search(ctx, api.Principal{UserID: 9, ReaderChatID: 111}, &sushiv1.SearchReq{Query: "x"}); err != nil {
		t.Fatalf("first search: %v", err)
	}
	_, err := a.Search(ctx, api.Principal{UserID: 9, ReaderChatID: 222}, &sushiv1.SearchReq{Query: "x"})
	var rl *api.RateLimitError
	if !errors.As(err, &rl) {
		t.Fatalf("err = %v, want a *api.RateLimitError -- same user, a different reader chat must not reset the cap", err)
	}
}

func TestSearchSurfacesStoreFailures(t *testing.T) {
	t.Parallel()

	want := errors.New("database is down")
	_, err := searchAPI(&fakeSearch{err: want}, api.RateLimits{SearchPerHour: 60}).
		Search(context.Background(), api.Principal{UserID: 9}, &sushiv1.SearchReq{Query: "x"})
	if !errors.Is(err, want) {
		t.Errorf("err = %v, want it to wrap %v", err, want)
	}
}

type fakeTitleSearch struct {
	rows []catalog.Row
	err  error
	n    int
}

func (f *fakeTitleSearch) SearchTitles(_ context.Context, _ string) ([]catalog.Row, error) {
	f.n++
	if f.err != nil {
		return nil, f.err
	}
	return f.rows, nil
}

func TestSearchAppendsMissingFromTMDB(t *testing.T) {
	t.Parallel()

	s := &fakeSearch{
		rows:        []catalog.Row{row(1, "In Catalog")},
		catalogKeys: []catalog.RowKey{{TMDBID: 99, Kind: catalog.KindMovie}},
	}
	tm := &fakeTitleSearch{rows: []catalog.Row{
		{TMDBID: 99, Kind: catalog.KindMovie, Title: "Already Have"},
		{TMDBID: 100, Kind: catalog.KindMovie, Title: "Ghost"},
		{TMDBID: 101, Kind: catalog.KindSeries, Title: "Ghost Show"},
	}}
	a := searchAPI(s, api.RateLimits{SearchPerHour: 60}).WithTitleSearch(tm)
	res, err := a.Search(context.Background(), api.Principal{UserID: 9}, &sushiv1.SearchReq{Query: "ghost"})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(res.GetRows()) != 1 {
		t.Fatalf("catalog rows = %d, want 1", len(res.GetRows()))
	}
	if len(res.GetMissing()) != 2 {
		t.Fatalf("missing = %d, want 2 (deduped catalog hit)", len(res.GetMissing()))
	}
	if res.GetMissing()[0].GetTitle() != "Ghost" {
		t.Errorf("first missing = %q", res.GetMissing()[0].GetTitle())
	}
}

func TestSearchMissingSkippedOnLaterPage(t *testing.T) {
	t.Parallel()

	tm := &fakeTitleSearch{rows: []catalog.Row{{TMDBID: 1, Kind: catalog.KindMovie, Title: "X"}}}
	a := searchAPI(&fakeSearch{}, api.RateLimits{SearchPerHour: 60}).WithTitleSearch(tm)
	res, err := a.Search(context.Background(), api.Principal{UserID: 9}, &sushiv1.SearchReq{Query: "x", Cursor: 20})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if tm.n != 0 {
		t.Fatalf("TMDB called %d times on cursor!=0, want 0", tm.n)
	}
	if len(res.GetMissing()) != 0 {
		t.Fatalf("missing = %d, want 0", len(res.GetMissing()))
	}
}

func TestSearchMissingFailOpenOnTMDBError(t *testing.T) {
	t.Parallel()

	s := &fakeSearch{rows: []catalog.Row{row(1, "Tenet")}}
	tm := &fakeTitleSearch{err: errors.New("tmdb down")}
	a := searchAPI(s, api.RateLimits{SearchPerHour: 60}).WithTitleSearch(tm)
	res, err := a.Search(context.Background(), api.Principal{UserID: 9}, &sushiv1.SearchReq{Query: "ten"})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(res.GetRows()) != 1 {
		t.Fatalf("rows = %d, want catalog still returned", len(res.GetRows()))
	}
	if len(res.GetMissing()) != 0 {
		t.Fatalf("missing = %d, want empty on TMDB error", len(res.GetMissing()))
	}
}

func TestSearchPersianAliasViaTMDBWhenTrigramMisses(t *testing.T) {
	t.Parallel()

	const tmdb = uint64(303463)
	s := &fakeSearch{
		catalogKeys: []catalog.RowKey{{TMDBID: tmdb, Kind: catalog.KindSeries}},
		catalogRows: []catalog.Row{{TMDBID: tmdb, Kind: catalog.KindSeries, Title: "Drunkard Morning"}},
	}
	tm := &fakeTitleSearch{rows: []catalog.Row{{TMDBID: tmdb, Kind: catalog.KindSeries, Title: "بامداد خمار"}}}
	a := searchAPI(s, api.RateLimits{SearchPerHour: 60}).WithTitleSearch(tm)
	res, err := a.Search(context.Background(), api.Principal{UserID: 9}, &sushiv1.SearchReq{Query: "بامداد خمار"})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(res.GetRows()) != 1 || res.GetRows()[0].GetTmdbId() != tmdb {
		t.Fatalf("rows = %+v, want catalog series %d", res.GetRows(), tmdb)
	}
	if len(res.GetMissing()) != 0 {
		t.Fatalf("missing = %d, want 0 (already in catalog)", len(res.GetMissing()))
	}
}

func TestSearchMissingCapsAtFive(t *testing.T) {
	t.Parallel()

	hits := make([]catalog.Row, 8)
	for i := range hits {
		hits[i] = catalog.Row{TMDBID: uint64(i + 1), Kind: catalog.KindMovie, Title: "M"}
	}
	tm := &fakeTitleSearch{rows: hits}
	a := searchAPI(&fakeSearch{}, api.RateLimits{SearchPerHour: 60}).WithTitleSearch(tm)
	res, err := a.Search(context.Background(), api.Principal{UserID: 9}, &sushiv1.SearchReq{Query: "m"})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(res.GetMissing()) != 5 {
		t.Fatalf("missing = %d, want 5", len(res.GetMissing()))
	}
}
