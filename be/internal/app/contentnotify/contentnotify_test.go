package contentnotify

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

type fakeStore struct {
	alerts       []AdminAlert
	admins       []Admin
	adminMarks   []int64
	channelMarks [][2]int64
	settledPosts []ChannelCard
	cleared      []int64

	targets       []Target
	serveable     map[uint64]bool
	fulfilledFor  []Target
	fulfilledRows int64

	notices        []RequesterNotice
	requesterMarks []int64

	followSeries  []FollowedSeries
	maxEp         map[uint64][2]int32
	bumped        []FollowedSeries
	followNotices []FollowNotice
	advanced      []FollowNotice
	syncedFollows [][2]int64

	err error
}

func (f *fakeStore) PendingRequestAdminAlerts(context.Context, int32) ([]AdminAlert, error) {
	return f.alerts, f.err
}
func (f *fakeStore) AdminRecipients(context.Context) ([]Admin, error) { return f.admins, nil }
func (f *fakeStore) MarkRequestAdminsNotified(_ context.Context, id int64) error {
	f.adminMarks = append(f.adminMarks, id)
	return nil
}

func (f *fakeStore) MarkRequestChannelPosted(_ context.Context, id, messageID int64) error {
	f.channelMarks = append(f.channelMarks, [2]int64{id, messageID})
	return nil
}

func (f *fakeStore) SettledRequestChannelPosts(context.Context, int32) ([]ChannelCard, error) {
	return f.settledPosts, nil
}

func (f *fakeStore) ClearRequestChannelMessage(_ context.Context, id int64) error {
	f.cleared = append(f.cleared, id)
	return nil
}

func (f *fakeStore) FulfillPlayableRequests(context.Context, int32) (int, error) {
	if f.err != nil && len(f.targets) == 0 && len(f.alerts) == 0 {
		return 0, f.err
	}
	var n int
	for _, t := range f.targets {
		if f.serveable[t.TMDBID] {
			f.fulfilledFor = append(f.fulfilledFor, t)
			n += int(f.fulfilledRows)
		}
	}
	return n, nil
}

func (f *fakeStore) FulfilledRequestsToNotify(context.Context, int32) ([]RequesterNotice, error) {
	return f.notices, f.err
}

func (f *fakeStore) MarkRequesterNotified(_ context.Context, id int64) error {
	f.requesterMarks = append(f.requesterMarks, id)
	return nil
}

func (f *fakeStore) FollowedSeriesToScan(context.Context, int32) ([]FollowedSeries, error) {
	return f.followSeries, nil
}

func (f *fakeStore) SeriesMaxServeableEpisode(_ context.Context, tmdbID uint64) (int32, int32, error) {
	m := f.maxEp[tmdbID]
	return m[0], m[1], nil
}

func (f *fakeStore) BumpFollowedSeriesMax(_ context.Context, tmdbID uint64, s, e int32) (bool, error) {
	f.bumped = append(f.bumped, FollowedSeries{TMDBID: tmdbID, MaxSeason: s, MaxEp: e})
	return true, nil
}

func (f *fakeStore) FollowNoticesDue(context.Context, time.Time, int32) ([]FollowNotice, error) {
	return f.followNotices, nil
}

func (f *fakeStore) AdvanceFollowSeen(_ context.Context, userID int64, tmdbID uint64, s, e int32) error {
	f.advanced = append(f.advanced, FollowNotice{UserID: userID, TMDBID: tmdbID, MaxSeason: s, MaxEp: e})
	return nil
}

func (f *fakeStore) SyncFollowSeenToSeriesMax(_ context.Context, userID int64, tmdbID uint64) error {
	f.syncedFollows = append(f.syncedFollows, [2]int64{userID, int64(tmdbID)})
	return nil
}

type fakeMessenger struct {
	sent         []int64
	threads      []int64
	texts        []string
	photos       []string
	closed       []int64
	closeThreads []int64
	failTo       map[int64]bool
	noPhoto      bool // PostAlert skips the photo, still sends the caption
	nextMsgID    int64
}

func (m *fakeMessenger) SendHTML(_ context.Context, chatID int64, text string) error {
	if m.failTo[chatID] {
		return errors.New("blocked the bot")
	}
	m.sent = append(m.sent, chatID)
	m.texts = append(m.texts, text)
	return nil
}

func (m *fakeMessenger) SendPhotoHTML(_ context.Context, chatID int64, photoURL, caption string) error {
	if m.noPhoto || m.failTo[chatID] {
		return errors.New("no photo")
	}
	m.sent = append(m.sent, chatID)
	m.photos = append(m.photos, photoURL)
	m.texts = append(m.texts, caption)
	return nil
}

func (m *fakeMessenger) PostAlert(_ context.Context, p AlertPost) (int64, error) {
	if m.failTo[p.ChatID] {
		return 0, errors.New("blocked the bot")
	}
	m.sent = append(m.sent, p.ChatID)
	m.threads = append(m.threads, p.ThreadID)
	m.texts = append(m.texts, p.Caption)
	if p.PhotoURL != "" && !m.noPhoto {
		m.photos = append(m.photos, p.PhotoURL)
	}
	m.nextMsgID++
	return m.nextMsgID, nil
}

func (m *fakeMessenger) CloseAlert(_ context.Context, _, messageID int64, outcome string, threadID int64) error {
	m.closed = append(m.closed, messageID)
	m.closeThreads = append(m.closeThreads, threadID)
	m.texts = append(m.texts, outcome)
	return nil
}

func newReconciler(s Store, m Messenger) *Reconciler {
	return New(s, m, slog.New(slog.NewTextHandler(io.Discard, nil)), 15*time.Minute, 0)
}

func TestAlertAdminsDMsEveryAdminOnceAndMarks(t *testing.T) {
	t.Parallel()

	store := &fakeStore{
		alerts: []AdminAlert{{ID: 7, TMDBID: 42, Kind: catalog.KindMovie, Title: "Dune", RequesterTG: 99}},
		admins: []Admin{{TelegramID: 100}, {TelegramID: 200, Locale: "fa"}},
	}
	msg := &fakeMessenger{}
	newReconciler(store, msg).tick(context.Background())

	if len(msg.sent) != 2 {
		t.Errorf("DMs sent to %v, want both admins", msg.sent)
	}
	if len(store.adminMarks) != 1 || store.adminMarks[0] != 7 {
		t.Errorf("admin marks = %v, want [7]", store.adminMarks)
	}
}

func TestAlertAdminsSendsThePosterWhenThereIsOne(t *testing.T) {
	t.Parallel()

	store := &fakeStore{
		alerts: []AdminAlert{{
			ID: 7, TMDBID: 42, Kind: catalog.KindMovie, Title: "Dune",
			Poster: "/dune.jpg", IMDBID: "tt1160419",
			RequesterTG: 99, RequesterName: "Ada", RequesterUsername: "ada_l",
		}},
		admins: []Admin{{TelegramID: 100}},
	}
	msg := &fakeMessenger{}
	newReconciler(store, msg).tick(context.Background())

	if len(msg.photos) != 1 || msg.photos[0] != "https://image.tmdb.org/t/p/w500/dune.jpg" {
		t.Errorf("photos = %v, want the w500 TMDB url", msg.photos)
	}
	dm := msg.texts[0]
	for _, want := range []string{"Dune", "42", "tt1160419", "Ada", "https://t.me/ada_l", "@ada_l", "99"} {
		if !strings.Contains(dm, want) {
			t.Errorf("caption missing %q:\n%s", want, dm)
		}
	}
}

func TestAdminAlertRequesterLineDegradesWithoutNameOrUsername(t *testing.T) {
	t.Parallel()

	store := &fakeStore{
		alerts: []AdminAlert{{ID: 1, TMDBID: 5, RequesterTG: 777}},
		admins: []Admin{{TelegramID: 100}},
	}
	msg := &fakeMessenger{}
	newReconciler(store, msg).tick(context.Background())

	dm := msg.texts[0]
	if !strings.Contains(dm, "777") {
		t.Errorf("caption should still carry the id: %s", dm)
	}
	if strings.Contains(dm, "t.me") || strings.Contains(dm, "<b></b>") {
		t.Errorf("caption should not have an empty username link or empty bold name: %s", dm)
	}
}

func TestAlertAdminsFallsBackToTextWhenThePhotoSendFails(t *testing.T) {
	t.Parallel()

	store := &fakeStore{
		alerts: []AdminAlert{{ID: 7, TMDBID: 42, Title: "Dune", Poster: "/broken.jpg"}},
		admins: []Admin{{TelegramID: 100}},
	}
	msg := &fakeMessenger{noPhoto: true}
	newReconciler(store, msg).tick(context.Background())

	if len(msg.photos) != 0 {
		t.Errorf("photos = %v, want none (photo send fails)", msg.photos)
	}
	if len(msg.sent) != 1 || len(store.adminMarks) != 1 {
		t.Errorf("text fallback did not land: sent=%v marks=%v", msg.sent, store.adminMarks)
	}
}

func TestAlertAdminsMarksEvenWithNoRecipients(t *testing.T) {
	t.Parallel()

	store := &fakeStore{alerts: []AdminAlert{{ID: 3}}, admins: nil}
	newReconciler(store, &fakeMessenger{}).tick(context.Background())

	if len(store.adminMarks) != 1 {
		t.Errorf("row not marked with no admins configured; it would re-list every tick")
	}
}

func TestAlertAdminsDoesNotMarkWhenEverySendFails(t *testing.T) {
	t.Parallel()

	store := &fakeStore{alerts: []AdminAlert{{ID: 5}}, admins: []Admin{{TelegramID: 100}}}
	msg := &fakeMessenger{failTo: map[int64]bool{100: true}}
	newReconciler(store, msg).tick(context.Background())

	if len(store.adminMarks) != 0 {
		t.Errorf("marked despite the only send failing; the alert would be lost")
	}
}

func TestDetectFulfilmentOnlyMarksServeableTargets(t *testing.T) {
	t.Parallel()

	store := &fakeStore{
		targets: []Target{
			{TMDBID: 1, Kind: catalog.KindMovie},
			{TMDBID: 2, Kind: catalog.KindSeries},
		},
		serveable:     map[uint64]bool{2: true},
		fulfilledRows: 4,
	}
	newReconciler(store, &fakeMessenger{}).tick(context.Background())

	if len(store.fulfilledFor) != 1 || store.fulfilledFor[0].TMDBID != 2 {
		t.Errorf("fulfilled = %v, want only the serveable target 2", store.fulfilledFor)
	}
}

func TestNotifyRequestersDMsAndMarks(t *testing.T) {
	t.Parallel()

	store := &fakeStore{notices: []RequesterNotice{
		{ID: 11, RequesterTG: 500, TMDBID: 42, Kind: catalog.KindMovie, Title: "Dune"},
	}}
	msg := &fakeMessenger{}
	newReconciler(store, msg).tick(context.Background())

	if len(msg.sent) != 1 || msg.sent[0] != 500 {
		t.Errorf("DM sent to %v, want [500]", msg.sent)
	}
	if len(store.requesterMarks) != 1 || store.requesterMarks[0] != 11 {
		t.Errorf("requester marks = %v, want [11]", store.requesterMarks)
	}
}

func TestNotifyRequestersDoesNotMarkOnSendFailure(t *testing.T) {
	t.Parallel()

	store := &fakeStore{notices: []RequesterNotice{{ID: 12, RequesterTG: 500}}}
	msg := &fakeMessenger{failTo: map[int64]bool{500: true}}
	newReconciler(store, msg).tick(context.Background())

	if len(store.requesterMarks) != 0 {
		t.Errorf("marked despite the DM failing; the requester would never hear")
	}
}

// A fulfilled series request catches its auto-follow up so the follow flush does not double-DM.
func TestNotifyRequestersSyncsTheLinkedSeriesFollow(t *testing.T) {
	t.Parallel()

	store := &fakeStore{notices: []RequesterNotice{
		{ID: 1, UserID: 8, RequesterTG: 500, TMDBID: 77, Kind: catalog.KindSeries},
		{ID: 2, UserID: 9, RequesterTG: 501, TMDBID: 42, Kind: catalog.KindMovie},
	}}
	newReconciler(store, &fakeMessenger{}).tick(context.Background())

	if len(store.syncedFollows) != 1 || store.syncedFollows[0] != [2]int64{8, 77} {
		t.Errorf("synced follows = %v, want only the series row [{8 77}]", store.syncedFollows)
	}
}

func TestFollowScanBumpsMaxWhenAnEpisodeArrives(t *testing.T) {
	t.Parallel()

	store := &fakeStore{
		followSeries: []FollowedSeries{{TMDBID: 5, MaxSeason: 1, MaxEp: 3}},
		maxEp:        map[uint64][2]int32{5: {1, 8}},
	}
	newReconciler(store, &fakeMessenger{}).tick(context.Background())

	if len(store.bumped) != 1 || store.bumped[0] != (FollowedSeries{TMDBID: 5, MaxSeason: 1, MaxEp: 8}) {
		t.Errorf("bumped = %v, want [{5 1 8}]", store.bumped)
	}
}

func TestFollowScanIsIdleWhenNothingNew(t *testing.T) {
	t.Parallel()

	store := &fakeStore{
		followSeries: []FollowedSeries{{TMDBID: 5, MaxSeason: 2, MaxEp: 4}},
		maxEp:        map[uint64][2]int32{5: {2, 4}},
	}
	newReconciler(store, &fakeMessenger{}).tick(context.Background())

	if len(store.bumped) != 0 {
		t.Errorf("bumped %v with no new episode", store.bumped)
	}
}

// The whole point of the debounce: 20 episodes landing at once is ONE DM naming the latest.
func TestFollowFlushCoalescesToTheLatestEpisode(t *testing.T) {
	t.Parallel()

	store := &fakeStore{followNotices: []FollowNotice{
		{UserID: 8, RequesterTG: 500, TMDBID: 5, MaxSeason: 2, MaxEp: 20, Title: "The Show", NewCount: 20},
	}}
	msg := &fakeMessenger{}
	newReconciler(store, msg).tick(context.Background())

	if len(msg.sent) != 1 {
		t.Fatalf("sent %d DMs, want exactly 1 for a 20-episode drop", len(msg.sent))
	}
	if !strings.Contains(msg.texts[0], "S02E20") || !strings.Contains(msg.texts[0], "+19") {
		t.Errorf("DM %q, want it to name S02E20 and +19 more", msg.texts[0])
	}
	if len(store.advanced) != 1 || store.advanced[0].MaxEp != 20 {
		t.Errorf("advanced = %v, want the follow moved to episode 20", store.advanced)
	}
}

func TestFollowFlushDoesNotAdvanceOnSendFailure(t *testing.T) {
	t.Parallel()

	store := &fakeStore{followNotices: []FollowNotice{{UserID: 8, RequesterTG: 500, TMDBID: 5, MaxEp: 1}}}
	msg := &fakeMessenger{failTo: map[int64]bool{500: true}}
	newReconciler(store, msg).tick(context.Background())

	if len(store.advanced) != 0 {
		t.Errorf("advanced despite the DM failing; the follower would skip that episode forever")
	}
}

func TestAlertPostsOnceToTheRequestChannel(t *testing.T) {
	t.Parallel()

	const channel int64 = -1009900000002
	store := &fakeStore{
		alerts: []AdminAlert{{ID: 7, TMDBID: 42, Kind: catalog.KindMovie, Title: "Dune"}},
		admins: []Admin{{TelegramID: 100}, {TelegramID: 200}},
	}
	msg := &fakeMessenger{}
	New(store, msg, slog.New(slog.NewTextHandler(io.Discard, nil)), 15*time.Minute, channel).
		tick(context.Background())

	if len(msg.sent) != 1 || msg.sent[0] != channel {
		t.Errorf("sent to %v, want one post to the requests channel", msg.sent)
	}
	if len(store.channelMarks) != 1 || store.channelMarks[0] != [2]int64{7, 1} {
		t.Errorf("channel marks = %v, want [{7 1}]", store.channelMarks)
	}
	if len(store.adminMarks) != 0 {
		t.Errorf("also DMed admins: %v", store.adminMarks)
	}
}

func TestAlertChannelSendFailureDoesNotMark(t *testing.T) {
	t.Parallel()

	const channel int64 = -1009900000002
	store := &fakeStore{alerts: []AdminAlert{{ID: 5}}}
	msg := &fakeMessenger{failTo: map[int64]bool{channel: true}}
	New(store, msg, slog.New(slog.NewTextHandler(io.Discard, nil)), 15*time.Minute, channel).
		tick(context.Background())

	if len(store.channelMarks) != 0 {
		t.Errorf("marked despite the channel post failing")
	}
}

func TestCloseSettledChannelPosts(t *testing.T) {
	t.Parallel()

	const channel int64 = -1009900000002
	store := &fakeStore{settledPosts: []ChannelCard{
		{ID: 9, MessageID: 44, Status: 2, Title: "Dune", TMDBID: 42, Kind: catalog.KindMovie},
	}}
	msg := &fakeMessenger{}
	New(store, msg, slog.New(slog.NewTextHandler(io.Discard, nil)), 15*time.Minute, channel).
		tick(context.Background())

	if len(msg.closed) != 1 || msg.closed[0] != 44 {
		t.Errorf("closed = %v, want [44]", msg.closed)
	}
	if len(store.cleared) != 1 || store.cleared[0] != 9 {
		t.Errorf("cleared = %v, want [9]", store.cleared)
	}
	if !strings.Contains(msg.texts[len(msg.texts)-1], closedAdded) {
		t.Errorf("close caption missing added mark: %q", msg.texts[len(msg.texts)-1])
	}
	if msg.closeThreads[0] != 0 {
		t.Errorf("close thread = %d, want in-place edit when the forum is off", msg.closeThreads[0])
	}
}

func TestForumPostsOpenAndMovesSettledCards(t *testing.T) {
	t.Parallel()

	const channel int64 = -1009900000002
	topics := ForumTopics{Open: 11, Reject: 22, Done: 33}
	store := &fakeStore{
		alerts: []AdminAlert{{ID: 7, TMDBID: 42, Kind: catalog.KindMovie, Title: "Dune"}},
		settledPosts: []ChannelCard{
			{ID: 3, MessageID: 30, Status: statusUnavailable, Title: "Nope", TMDBID: 1, Kind: catalog.KindMovie},
			{ID: 4, MessageID: 40, Status: statusFulfilled, Title: "Dune", TMDBID: 42, Kind: catalog.KindMovie},
		},
	}
	msg := &fakeMessenger{}
	New(store, msg, slog.New(slog.NewTextHandler(io.Discard, nil)), 15*time.Minute, channel).
		WithForum(topics).
		tick(context.Background())

	if len(msg.threads) != 1 || msg.threads[0] != topics.Open {
		t.Errorf("post threads = %v, want open %d", msg.threads, topics.Open)
	}
	if len(msg.closeThreads) != 2 || msg.closeThreads[0] != topics.Reject || msg.closeThreads[1] != topics.Done {
		t.Errorf("close threads = %v, want reject then done", msg.closeThreads)
	}
}
