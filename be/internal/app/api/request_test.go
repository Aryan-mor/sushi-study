package api_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Aryan-mor/sushi-study/be/internal/app/api"
	"github.com/Aryan-mor/sushi-study/be/internal/core/catalog"
	sushiv1 "github.com/Aryan-mor/sushi-study/be/internal/protocol/sushi/v1"
)

// fakeRequests is the in-process RequestStore. Each knob is a single value — the tests set only
// what they exercise.
type fakeRequests struct {
	serveable    bool
	serveableErr error

	used     int
	countErr error

	title, poster string
	snapErr       error

	created    bool
	insertErr  error
	insertArgs []api.NewRequest

	sinceSeen time.Time
}

func (f *fakeRequests) CatalogPlayable(_ context.Context, _ int64, _ uint64, _ catalog.Kind) (bool, error) {
	return f.serveable, f.serveableErr
}

func (f *fakeRequests) RequestCountSince(_ context.Context, _ int64, since time.Time) (int, error) {
	f.sinceSeen = since
	return f.used, f.countErr
}

func (f *fakeRequests) TitleSnapshot(_ context.Context, _ uint64, _ catalog.Kind) (string, string, error) {
	return f.title, f.poster, f.snapErr
}

func (f *fakeRequests) InsertRequest(_ context.Context, r api.NewRequest) (bool, error) {
	f.insertArgs = append(f.insertArgs, r)
	return f.created, f.insertErr
}

func requestAPI(r api.RequestStore, quota int) *api.API {
	return api.New(&fakeRails{}, &fakeDeliveries{}, &fakeCatalog{}, &fakeSearch{},
		api.RateLimits{RequestsPerDay: quota}).WithRequests(r)
}

// followRecorder is a fakeBrowse that also remembers SetFollow calls, so a series-request test can
// assert the auto-follow (ADR 0014 §D2).
type followRecorder struct {
	*fakeBrowse
	follows []uint64
}

func (f *followRecorder) SetFollow(_ context.Context, _ int64, tmdbID uint64, on bool) error {
	if on {
		f.follows = append(f.follows, tmdbID)
	}
	return nil
}

func movieReq(id uint64) *sushiv1.RequestReq {
	return &sushiv1.RequestReq{TmdbId: id, Kind: sushiv1.Kind_KIND_MOVIE}
}

func TestRequestOutcomes(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name          string
		store         *fakeRequests
		quota         int
		role          string
		wantOutcome   sushiv1.RequestOutcome
		wantRemaining uint32
		wantInsert    bool
	}{
		{
			name:        "already available short-circuits",
			store:       &fakeRequests{serveable: true, created: true},
			quota:       10,
			wantOutcome: sushiv1.RequestOutcome_REQUEST_OUTCOME_ALREADY_AVAILABLE,
			wantInsert:  false,
		},
		{
			name:          "accepted decrements the remaining count",
			store:         &fakeRequests{used: 3, created: true},
			quota:         10,
			wantOutcome:   sushiv1.RequestOutcome_REQUEST_OUTCOME_ACCEPTED,
			wantRemaining: 6, // 10 - 3 used, minus the row just written
			wantInsert:    true,
		},
		{
			name:          "accepted on the last available slot",
			store:         &fakeRequests{used: 9, created: true},
			quota:         10,
			wantOutcome:   sushiv1.RequestOutcome_REQUEST_OUTCOME_ACCEPTED,
			wantRemaining: 0,
			wantInsert:    true,
		},
		{
			name:        "duplicate when an open request already exists",
			store:       &fakeRequests{used: 2, created: false},
			quota:       10,
			wantOutcome: sushiv1.RequestOutcome_REQUEST_OUTCOME_DUPLICATE,
			wantInsert:  true, // insert is attempted; the partial unique index no-ops it
		},
		{
			name:        "quota exceeded before any write",
			store:       &fakeRequests{used: 10, created: true},
			quota:       10,
			wantOutcome: sushiv1.RequestOutcome_REQUEST_OUTCOME_QUOTA_EXCEEDED,
			wantInsert:  false,
		},
		{
			name:          "admin skips the quota entirely",
			store:         &fakeRequests{used: 999, created: true},
			quota:         10,
			role:          "admin",
			wantOutcome:   sushiv1.RequestOutcome_REQUEST_OUTCOME_ACCEPTED,
			wantRemaining: 0,
			wantInsert:    true,
		},
		{
			name:          "superadmin skips the quota entirely",
			store:         &fakeRequests{used: 999, created: true},
			quota:         10,
			role:          "superadmin",
			wantOutcome:   sushiv1.RequestOutcome_REQUEST_OUTCOME_ACCEPTED,
			wantRemaining: 0,
			wantInsert:    true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			p := api.Principal{UserID: 1, Role: tc.role}
			res, err := requestAPI(tc.store, tc.quota).Request(context.Background(), p, movieReq(42))
			if err != nil {
				t.Fatalf("Request: %v", err)
			}
			if res.GetOutcome() != tc.wantOutcome {
				t.Errorf("outcome = %v, want %v", res.GetOutcome(), tc.wantOutcome)
			}
			if res.GetRemainingToday() != tc.wantRemaining {
				t.Errorf("remaining_today = %d, want %d", res.GetRemainingToday(), tc.wantRemaining)
			}
			if got := len(tc.store.insertArgs) > 0; got != tc.wantInsert {
				t.Errorf("insert attempted = %v, want %v", got, tc.wantInsert)
			}
		})
	}
}

func TestRequestStoresTheTitleSnapshot(t *testing.T) {
	t.Parallel()

	store := &fakeRequests{created: true, title: "Dune", poster: "/dune.jpg"}
	_, err := requestAPI(store, 10).Request(context.Background(),
		api.Principal{UserID: 7}, &sushiv1.RequestReq{TmdbId: 438631, Kind: sushiv1.Kind_KIND_MOVIE})
	if err != nil {
		t.Fatalf("Request: %v", err)
	}
	if len(store.insertArgs) != 1 {
		t.Fatalf("insert calls = %d, want 1", len(store.insertArgs))
	}
	got := store.insertArgs[0]
	if got.UserID != 7 || got.TMDBID != 438631 || got.Kind != catalog.KindMovie {
		t.Errorf("insert arg identity = %+v", got)
	}
	if got.Title != "Dune" || got.Poster != "/dune.jpg" {
		t.Errorf("insert arg snapshot = %q / %q, want Dune / /dune.jpg", got.Title, got.Poster)
	}
}

func TestRequestForASeriesAlsoFollowsIt(t *testing.T) {
	t.Parallel()

	rec := &followRecorder{fakeBrowse: &fakeBrowse{}}
	a := api.New(&fakeRails{}, &fakeDeliveries{}, &fakeCatalog{}, &fakeSearch{},
		api.RateLimits{RequestsPerDay: 10}).WithRequests(&fakeRequests{created: true}).WithBrowse(rec)

	// a series request auto-follows
	if _, err := a.Request(context.Background(), api.Principal{UserID: 3},
		&sushiv1.RequestReq{TmdbId: 1399, Kind: sushiv1.Kind_KIND_SERIES}); err != nil {
		t.Fatalf("Request: %v", err)
	}
	// a movie request does not
	if _, err := a.Request(context.Background(), api.Principal{UserID: 3}, movieReq(603)); err != nil {
		t.Fatalf("Request: %v", err)
	}

	if len(rec.follows) != 1 || rec.follows[0] != 1399 {
		t.Errorf("follows = %v, want only the series [1399]", rec.follows)
	}
}

func TestRequestQuotaWindowIsRolling24h(t *testing.T) {
	t.Parallel()

	store := &fakeRequests{created: true}
	before := time.Now().Add(-24 * time.Hour)
	if _, err := requestAPI(store, 10).Request(context.Background(), api.Principal{UserID: 1}, movieReq(1)); err != nil {
		t.Fatalf("Request: %v", err)
	}
	after := time.Now().Add(-24 * time.Hour)
	if store.sinceSeen.Before(before.Add(-time.Second)) || store.sinceSeen.After(after.Add(time.Second)) {
		t.Errorf("quota counted since %v, want ~24h ago", store.sinceSeen)
	}
}

func TestRequestValidatesInput(t *testing.T) {
	t.Parallel()

	a := requestAPI(&fakeRequests{}, 10)
	if _, err := a.Request(context.Background(), api.Principal{UserID: 1}, movieReq(0)); err == nil {
		t.Error("Request with tmdb_id 0 should error")
	}
	if _, err := a.Request(context.Background(), api.Principal{UserID: 1},
		&sushiv1.RequestReq{TmdbId: 1, Kind: sushiv1.Kind_KIND_UNSPECIFIED}); err == nil {
		t.Error("Request with unspecified kind should error")
	}
}

func TestRequestErrorsWithoutAStore(t *testing.T) {
	t.Parallel()

	a := api.New(&fakeRails{}, &fakeDeliveries{}, &fakeCatalog{}, &fakeSearch{}, api.RateLimits{RequestsPerDay: 10})
	if _, err := a.Request(context.Background(), api.Principal{UserID: 1}, movieReq(1)); err == nil {
		t.Error("Request without WithRequests should error, not silently accept")
	}
}

func TestRequestSurfacesStoreErrors(t *testing.T) {
	t.Parallel()

	boom := errors.New("db down")
	cases := map[string]*fakeRequests{
		"serveable check": {serveableErr: boom},
		"quota check":     {countErr: boom},
		"title snapshot":  {snapErr: boom},
		"insert":          {insertErr: boom},
	}
	for name, store := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, err := requestAPI(store, 10).Request(context.Background(), api.Principal{UserID: 1}, movieReq(1))
			if !errors.Is(err, boom) {
				t.Errorf("err = %v, want it to wrap %v", err, boom)
			}
		})
	}
}
