package newsroom

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/Aryan-mor/sushi-study/be/internal/core/catalog"
)

func quietLog() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

type queued struct {
	debut         Debut
	announceAfter time.Time
}

type fakeStore struct {
	movies   []Debut
	episodes []Debut
	due      []Announcement
	admins   []Admin

	moviesErr  error
	queueErr   error
	dueErr     error
	adminsErr  error
	alreadyHad bool

	queuedRows []queued
	staleRows  []queued
	approved   []approval
	skipped    []int64

	// movieArgs / episodeArgs record what discover asked for, so the freshness rule can be asserted.
	movieArgs   *movieQuery
	episodeArgs *episodeQuery
}

type movieQuery struct {
	since          time.Time
	premieredAfter time.Time
	limit          int32
}

type episodeQuery struct {
	since          time.Time
	premieredAfter time.Time
	limit          int32
}

type approval struct{ id, chatID, messageID int64 }

func (f *fakeStore) MovieDebuts(_ context.Context, since, premieredAfter time.Time, limit int32) ([]Debut, error) {
	f.movieArgs = &movieQuery{since, premieredAfter, limit}
	return f.movies, f.moviesErr
}

func (f *fakeStore) EpisodeDebuts(_ context.Context, since, premieredAfter time.Time, limit int32) ([]Debut, error) {
	f.episodeArgs = &episodeQuery{since, premieredAfter, limit}
	return f.episodes, nil
}

func (f *fakeStore) QueueAnnouncement(_ context.Context, d Debut, announceAfter time.Time) (bool, error) {
	if f.queueErr != nil {
		return false, f.queueErr
	}
	f.queuedRows = append(f.queuedRows, queued{d, announceAfter})
	return !f.alreadyHad, nil
}

func (f *fakeStore) QueueStaleSeriesEpisode(_ context.Context, d Debut, announceAfter time.Time) (bool, error) {
	if f.queueErr != nil {
		return false, f.queueErr
	}
	f.staleRows = append(f.staleRows, queued{d, announceAfter})
	return !f.alreadyHad, nil
}

func (f *fakeStore) DueAnnouncements(context.Context, int32) ([]Announcement, error) {
	return f.due, f.dueErr
}

func (f *fakeStore) MarkAwaitingApproval(_ context.Context, id, chatID, messageID int64) error {
	f.approved = append(f.approved, approval{id, chatID, messageID})
	return nil
}

func (f *fakeStore) MarkSkipped(_ context.Context, id int64) error {
	f.skipped = append(f.skipped, id)
	return nil
}

func (f *fakeStore) NewsAdminRecipients(context.Context) ([]Admin, error) {
	return f.admins, f.adminsErr
}

type sent struct {
	chatID  int64
	preview StaffPreview
}

type fakeMessenger struct {
	sends  []sent
	nextID int64
	// failFor makes the send to one chat fail, so failover can be asserted.
	failFor int64
	failAll bool
}

func (m *fakeMessenger) SendPreview(_ context.Context, chatID int64, p StaffPreview) (int64, error) {
	if m.failAll || (m.failFor != 0 && chatID == m.failFor) {
		return 0, errors.New("telegram said no")
	}
	m.sends = append(m.sends, sent{chatID, p})
	m.nextID++
	return 1000 + m.nextID, nil
}

func newTestReconciler(store Store, dm Messenger) *Reconciler {
	r := New(store, dm, quietLog(), time.Minute, "exampleMainBot")
	r.now = func() time.Time { return time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC) }
	return r
}

func TestDiscoverQueuesMoviesAndEpisodes(t *testing.T) {
	t.Parallel()

	store := &fakeStore{
		movies: []Debut{{
			TMDBID: 603, Kind: catalog.KindMovie, Title: "Sinners", Year: 2026,
			PremiereOn: time.Date(2026, 8, 20, 0, 0, 0, 0, time.UTC),
		}},
		episodes: []Debut{{
			TMDBID: 1396, Kind: catalog.KindSeries, Season: 5, Episode: 14, Year: 2026,
			PremiereOn: time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC),
		}},
	}
	dm := &fakeMessenger{}
	newTestReconciler(store, dm).Tick(context.Background())

	if len(store.queuedRows) != 1 || store.queuedRows[0].debut.Kind != catalog.KindMovie {
		t.Fatalf("queued %d movie rows, want the movie only", len(store.queuedRows))
	}
	if len(store.staleRows) != 1 {
		t.Fatalf("series hold got %d rows, want the episode", len(store.staleRows))
	}
	// The delay is what stops a bulk import being reviewed mid-flight (ADR 0017 §4).
	want := time.Date(2026, 9, 5, 12, 1, 0, 0, time.UTC)
	if got := store.queuedRows[0].announceAfter; !got.Equal(want) {
		t.Errorf("announce_after = %s, want now + the delay (%s)", got, want)
	}
}

// The two halves of the freshness rule, as the store is asked for them.
func TestDiscoverAsksForTheFreshnessWindow(t *testing.T) {
	t.Parallel()

	store := &fakeStore{}
	newTestReconciler(store, &fakeMessenger{}).Tick(context.Background())

	if store.movieArgs == nil {
		t.Fatal("MovieDebuts was never called")
	}
	if got, want := store.movieArgs.since, time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC); !got.Equal(want) {
		t.Errorf("since = %s, want 48h back (%s)", got, want)
	}
	if got, want := store.movieArgs.premieredAfter, time.Date(2026, 6, 7, 12, 0, 0, 0, time.UTC); !got.Equal(want) {
		t.Errorf("premieredAfter = %s, want 90 days back (%s)", got, want)
	}
	if store.episodeArgs == nil {
		t.Fatal("EpisodeDebuts was never called")
	}
	wantPremiere := time.Date(2026, 6, 7, 12, 0, 0, 0, time.UTC)
	if !store.episodeArgs.premieredAfter.Equal(wantPremiere) {
		t.Errorf("episode premieredAfter = %s, want 90 days back (%s)", store.episodeArgs.premieredAfter, wantPremiere)
	}
}

// A failing movie query must not stop episodes being discovered, and vice versa: one broken half of
// the catalog should not silence the channel entirely.
func TestDiscoverStillQueuesEpisodesWhenTheMovieQueryFails(t *testing.T) {
	t.Parallel()

	store := &fakeStore{
		moviesErr: errors.New("boom"),
		episodes: []Debut{{
			TMDBID: 1396, Kind: catalog.KindSeries, Season: 1, Episode: 1, Year: 2026,
			PremiereOn: time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC),
		}},
	}
	newTestReconciler(store, &fakeMessenger{}).Tick(context.Background())

	if len(store.staleRows) != 1 {
		t.Fatalf("series hold got %d rows, want the episode", len(store.staleRows))
	}
}

// Indexing several episodes of one series, even one that premiered this week, uses the hold
// that keeps only the latest episode. Staff should see E10, not one preview per episode.
func TestDiscoverHoldsEverySeriesEpisode(t *testing.T) {
	t.Parallel()

	store := &fakeStore{
		episodes: []Debut{
			{
				TMDBID: 99, Kind: catalog.KindSeries, Season: 1, Episode: 3, Title: "Against the Current",
				PremiereOn: time.Date(2026, 9, 2, 0, 0, 0, 0, time.UTC),
			},
			{
				TMDBID: 99, Kind: catalog.KindSeries, Season: 1, Episode: 10, Title: "Against the Current",
				PremiereOn: time.Date(2026, 9, 2, 0, 0, 0, 0, time.UTC),
			},
		},
	}
	newTestReconciler(store, &fakeMessenger{}).Tick(context.Background())

	if len(store.queuedRows) != 0 {
		t.Fatalf("fresh queue got %d rows, want none", len(store.queuedRows))
	}
	if len(store.staleRows) != 2 {
		t.Fatalf("stale queue got %d rows, want both episodes", len(store.staleRows))
	}
	want := time.Date(2026, 9, 5, 12, 1, 0, 0, time.UTC)
	for _, row := range store.staleRows {
		if !row.announceAfter.Equal(want) {
			t.Errorf("S%02dE%02d announce_after = %s, want %s", row.debut.Season, row.debut.Episode, row.announceAfter, want)
		}
	}
}

func TestPreviewGoesToEveryAdminInTheirOwnLanguage(t *testing.T) {
	t.Parallel()

	store := &fakeStore{
		due: []Announcement{{
			ID: 7, TMDBID: 603, Kind: catalog.KindMovie, Title: "Sinners", Year: 2026, Poster: "/m.jpg",
			PremiereOn: time.Date(2026, 8, 20, 0, 0, 0, 0, time.UTC),
		}},
		admins: []Admin{
			{TelegramID: 111, Locale: "fa"},
			{TelegramID: 222, Locale: "en"},
		},
	}
	dm := &fakeMessenger{}
	newTestReconciler(store, dm).Tick(context.Background())

	if len(dm.sends) != 2 {
		t.Fatalf("sent %d previews, want one per admin", len(dm.sends))
	}
	if dm.sends[0].preview.Locale != "fa" || dm.sends[1].preview.Locale != "en" {
		t.Error("each admin should be sent their own locale")
	}
	if dm.sends[0].preview.PhotoURL != "https://image.tmdb.org/t/p/w500/m.jpg" {
		t.Errorf("poster url = %q", dm.sends[0].preview.PhotoURL)
	}
	if !strings.Contains(dm.sends[0].preview.Rich, "@exampleMainBot") {
		t.Errorf("staff preview should stamp the main-bot handle:\n%s", dm.sends[0].preview.Rich)
	}
	// The row records the first preview that landed, because that is the message approve edits.
	if len(store.approved) != 1 {
		t.Fatalf("marked %d rows awaiting approval, want 1", len(store.approved))
	}
	if got := store.approved[0]; got.id != 7 || got.chatID != 111 || got.messageID == 0 {
		t.Errorf("recorded %+v, want announcement 7 in the first admin's chat", got)
	}
}

// If the first admin's DM fails, the row must point at one that actually exists — otherwise approve
// would try to edit a message that was never sent.
func TestPreviewRecordsTheFirstPreviewThatLanded(t *testing.T) {
	t.Parallel()

	store := &fakeStore{
		due: []Announcement{{
			ID: 9, TMDBID: 1, Kind: catalog.KindMovie, Title: "X", Year: 2026,
			PremiereOn: time.Date(2026, 8, 20, 0, 0, 0, 0, time.UTC),
		}},
		admins: []Admin{{TelegramID: 111}, {TelegramID: 222}},
	}
	dm := &fakeMessenger{failFor: 111}
	newTestReconciler(store, dm).Tick(context.Background())

	if len(store.approved) != 1 || store.approved[0].chatID != 222 {
		t.Errorf("recorded %+v, want the admin whose DM succeeded", store.approved)
	}
}

// Every send failing is a Telegram outage, not a bad row: leave it queued so the next tick retries.
func TestPreviewLeavesTheRowQueuedWhenEverySendFails(t *testing.T) {
	t.Parallel()

	store := &fakeStore{
		due: []Announcement{{
			ID: 9, TMDBID: 1, Kind: catalog.KindMovie, Title: "X", Year: 2026,
			PremiereOn: time.Date(2026, 8, 20, 0, 0, 0, 0, time.UTC),
		}},
		admins: []Admin{{TelegramID: 111}},
	}
	newTestReconciler(store, &fakeMessenger{failAll: true}).Tick(context.Background())

	if len(store.approved) != 0 {
		t.Error("marked awaiting approval with no preview sent")
	}
	if len(store.skipped) != 0 {
		t.Error("skipped a row over a send failure; it should be retried")
	}
}

// A catalog resync rewrites files.created_at, so old titles land in the queue. They must be
// parked, not DMed — that is the flood this test exists to stop.
func TestPreviewSkipsStaleQueuedRows(t *testing.T) {
	t.Parallel()

	store := &fakeStore{
		due: []Announcement{{
			ID: 11, TMDBID: 121, Kind: catalog.KindMovie, Title: "The Matrix", Year: 1999,
			PremiereOn: time.Date(1999, 3, 31, 0, 0, 0, 0, time.UTC),
		}},
		admins: []Admin{{TelegramID: 111}},
	}
	dm := &fakeMessenger{}
	newTestReconciler(store, dm).Tick(context.Background())

	if len(dm.sends) != 0 {
		t.Error("sent a preview for a 1999 movie")
	}
	if len(store.skipped) != 1 || store.skipped[0] != 11 {
		t.Errorf("skipped = %v, want the stale row parked", store.skipped)
	}
}

// Staff asked for this title. Age does not park it.
func TestPreviewSendsManualStaleRow(t *testing.T) {
	t.Parallel()

	store := &fakeStore{
		due: []Announcement{{
			ID: 12, TMDBID: 121, Kind: catalog.KindMovie, Title: "The Matrix", Year: 1999,
			PremiereOn: time.Date(1999, 3, 31, 0, 0, 0, 0, time.UTC),
			Manual:     true,
		}},
		admins: []Admin{{TelegramID: 111, Locale: "fa"}},
	}
	dm := &fakeMessenger{}
	newTestReconciler(store, dm).Tick(context.Background())

	if len(dm.sends) != 1 {
		t.Fatalf("sent %d previews, want the manual draft", len(dm.sends))
	}
	if len(store.skipped) != 0 {
		t.Errorf("skipped = %v, want none", store.skipped)
	}
}

// With nobody to approve it, a due row would re-list every 45 seconds forever.
func TestPreviewSkipsWhenThereAreNoAdmins(t *testing.T) {
	t.Parallel()

	store := &fakeStore{due: []Announcement{{ID: 5, TMDBID: 1, Kind: catalog.KindMovie, Title: "X", Year: 2026}}}
	dm := &fakeMessenger{}
	newTestReconciler(store, dm).Tick(context.Background())

	if len(dm.sends) != 0 {
		t.Error("sent a preview with no admins configured")
	}
	if len(store.skipped) != 1 || store.skipped[0] != 5 {
		t.Errorf("skipped = %v, want the row parked", store.skipped)
	}
}

// A title already covered by a live row is normal (two overlapping ticks), not an error.
func TestDiscoverToleratesAnAlreadyQueuedDebut(t *testing.T) {
	t.Parallel()

	store := &fakeStore{
		alreadyHad: true,
		movies: []Debut{{
			TMDBID: 603, Kind: catalog.KindMovie, Title: "Sinners", Year: 2026,
			PremiereOn: time.Date(2026, 8, 20, 0, 0, 0, 0, time.UTC),
		}},
	}
	newTestReconciler(store, &fakeMessenger{}).Tick(context.Background())

	if len(store.skipped) != 0 {
		t.Error("a duplicate debut should not park anything")
	}
}

func TestRunStopsOnContextCancel(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := newTestReconciler(&fakeStore{}, &fakeMessenger{}).Run(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Run returned %v, want context.Canceled", err)
	}
}

func TestPreviewFillsCountryFromTMDBWhenDetailEmpty(t *testing.T) {
	t.Parallel()

	store := &fakeStore{
		due: []Announcement{{
			ID: 7, TMDBID: 1752176, Kind: catalog.KindMovie, Title: "Blackmail", Year: 2026,
			PremiereOn: time.Date(2026, 8, 20, 0, 0, 0, 0, time.UTC),
		}},
		admins: []Admin{{TelegramID: 111, Locale: "fa"}},
	}
	dm := &fakeMessenger{}
	newTestReconciler(store, dm).WithCountries(&fakeCountries{code: "NZ"}).Tick(context.Background())

	if len(dm.sends) != 1 {
		t.Fatalf("sent %d", len(dm.sends))
	}
	if !strings.Contains(dm.sends[0].preview.Rich, "کشور تولید") {
		t.Errorf("preview missing country row:\n%s", dm.sends[0].preview.Rich)
	}
	if !strings.Contains(dm.sends[0].preview.Rich, "نیوزیلند") {
		t.Errorf("preview missing NZ name:\n%s", dm.sends[0].preview.Rich)
	}
}

func TestFillCountrySkipsWhenAlreadySet(t *testing.T) {
	t.Parallel()
	lookup := &fakeCountries{code: "US"}
	got := FillCountry(context.Background(), lookup, Announcement{TMDBID: 1, Country: "NZ"})
	if got.Country != "NZ" {
		t.Fatalf("kept %q", got.Country)
	}
	if lookup.calls != 0 {
		t.Fatalf("calls %d", lookup.calls)
	}
}

type fakeCountries struct {
	code  string
	calls int
}

func (f *fakeCountries) OriginCountry(context.Context, uint64, catalog.Kind) (string, error) {
	f.calls++
	return f.code, nil
}

func TestPreviewFillsNetworkFromTMDBWhenDetailEmpty(t *testing.T) {
	t.Parallel()

	store := &fakeStore{
		due: []Announcement{{
			ID: 8, TMDBID: 66732, Kind: catalog.KindSeries, Title: "Stranger Things", Year: 2016,
			PremiereOn: time.Date(2026, 8, 20, 0, 0, 0, 0, time.UTC),
		}},
		admins: []Admin{{TelegramID: 111, Locale: "fa"}},
	}
	dm := &fakeMessenger{}
	newTestReconciler(store, dm).WithNetworks(&fakeNetworks{name: "Netflix"}).Tick(context.Background())

	if len(dm.sends) != 1 {
		t.Fatalf("sent %d", len(dm.sends))
	}
	if !strings.Contains(dm.sends[0].preview.Rich, ">شبکه</td>") || !strings.Contains(dm.sends[0].preview.Rich, "Netflix") {
		t.Errorf("preview missing network row:\n%s", dm.sends[0].preview.Rich)
	}
}

func TestFillNetworkSkipsWhenAlreadySet(t *testing.T) {
	t.Parallel()
	lookup := &fakeNetworks{name: "Netflix"}
	got := FillNetwork(context.Background(), lookup, Announcement{TMDBID: 1, Network: "Disney+"})
	if got.Network != "Disney+" {
		t.Fatalf("kept %q", got.Network)
	}
	if lookup.calls != 0 {
		t.Fatalf("calls %d", lookup.calls)
	}
}

type fakeNetworks struct {
	name  string
	calls int
}

func (f *fakeNetworks) Network(context.Context, uint64, catalog.Kind) (string, error) {
	f.calls++
	return f.name, nil
}

// A zero delay must not mean "show staff a half-imported series immediately".
func TestNewFallsBackToTheDefaultDelay(t *testing.T) {
	t.Parallel()

	if got := New(&fakeStore{}, &fakeMessenger{}, quietLog(), 0, "").delay; got != defaultAnnounceDelay {
		t.Errorf("delay = %s, want the %s default", got, defaultAnnounceDelay)
	}
}
