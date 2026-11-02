package delivery

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"google.golang.org/protobuf/proto"

	"github.com/Aryan-mor/sushi-study/be/internal/adapter/tg"
	"github.com/Aryan-mor/sushi-study/be/internal/core/catalog"
	sushiv1 "github.com/Aryan-mor/sushi-study/be/internal/protocol/sushi/v1"
	"github.com/Aryan-mor/sushi-study/be/internal/wire"
)

type cloneLoc struct{ channelID, messageID int64 }

type jobKey struct{ readerChatID, fileID int64 }

type job struct {
	readerChatID, fileID int64
	// kind zero reads as a catalog file, so every existing test stays a catalog delivery.
	kind      catalog.MediaKind
	locator   string
	claimedBy int32
	claimedAt time.Time
	done      bool
}

// fakeStore plays both databases deliveryd actually talks to: claim/complete (environment) and
// clone lookup (library). Claiming really removes a job from the claimable pool until its claim
// goes stale, the same trap CloneLocationsMissingRole/backupd's fakeStore exists to catch on the
// backup side -- a job that fails must come back, but never be worked twice at once.
type fakeStore struct {
	mu          sync.Mutex
	jobs        []*job
	cloneLoc    map[int64]cloneLoc
	cloneErr    map[int64]error
	personalLoc map[int64]cloneLoc
	completeErr map[jobKey]error
	claimErr    error
	// usernames is the bindings-table fallback lookup: set for a "bot-login" reader, absent for a
	// "session" one.
	usernames   map[int64]string
	usernameErr error
}

func newStore(jobs ...*job) *fakeStore {
	return &fakeStore{
		jobs:        jobs,
		cloneLoc:    map[int64]cloneLoc{},
		cloneErr:    map[int64]error{},
		personalLoc: map[int64]cloneLoc{},
		completeErr: map[jobKey]error{},
		usernames:   map[int64]string{},
	}
}

func (s *fakeStore) ReaderUsername(_ context.Context, readerChatID int64) (string, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.usernameErr != nil {
		return "", false, s.usernameErr
	}
	u, ok := s.usernames[readerChatID]
	return u, ok, nil
}

func (s *fakeStore) ClaimPendingDeliveries(
	_ context.Context, botID int32, batch int, staleAfter time.Duration,
) ([]PendingJob, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.claimErr != nil {
		return nil, s.claimErr
	}
	out := []PendingJob{}
	for _, j := range s.jobs {
		if j.done {
			continue
		}
		if !j.claimedAt.IsZero() && time.Since(j.claimedAt) < staleAfter {
			continue
		}
		j.claimedBy = botID
		j.claimedAt = time.Now()
		out = append(out, PendingJob{ReaderChatID: j.readerChatID, Kind: j.jobKind(), FileID: j.fileID, Locator: j.locator})
		if len(out) == batch {
			break
		}
	}
	return out, nil
}

func (s *fakeStore) CloneLocation(_ context.Context, fileID int64) (int64, int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.cloneErr[fileID]; err != nil {
		return 0, 0, err
	}
	loc := s.cloneLoc[fileID]
	return loc.channelID, loc.messageID, nil
}

func (s *fakeStore) PersonalLocation(_ context.Context, videoID int64) (int64, int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	loc, ok := s.personalLoc[videoID]
	if !ok {
		return 0, 0, errors.New("no personal location")
	}
	return loc.channelID, loc.messageID, nil
}

func (s *fakeStore) CompleteDelivery(
	_ context.Context, readerChatID int64, kind catalog.MediaKind, fileID int64, botID int32, messageID int64,
) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.completeErr[jobKey{readerChatID, fileID}]; err != nil {
		return err
	}
	for _, j := range s.jobs {
		if j.readerChatID == readerChatID && j.fileID == fileID && j.jobKind() == kind {
			j.done = true
			_ = botID
			_ = messageID
			return nil
		}
	}
	return errors.New("no such job")
}

func (s *fakeStore) doneJobs() map[jobKey]bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := map[jobKey]bool{}
	for _, j := range s.jobs {
		if j.done {
			out[jobKey{j.readerChatID, j.fileID}] = true
		}
	}
	return out
}

type call struct{ dest, source, messageID int64 }

type usernameCall struct {
	username          string
	source, messageID int64
	caption           string
}

// chatNotFound builds the exact shape Telegram returns for a chat id it cannot resolve --
// confirmed live 2026-08-26 against real bot-to-bot traffic.
func chatNotFound() *tg.Error {
	return &tg.Error{Code: http.StatusBadRequest, Description: "Bad Request: chat not found"}
}

// fakeBot answers CopyMessage from a script keyed by clone message id, and records every attempt
// including the caption it was given.
type fakeBot struct {
	mu            sync.Mutex
	calls         []call
	captions      []string
	fail          map[int64]error
	rateOnce      map[int64]*tg.Error
	usernameCalls []usernameCall
	usernameFail  map[string]error
	next          int64
}

func newBot() *fakeBot {
	return &fakeBot{
		fail:         map[int64]error{},
		rateOnce:     map[int64]*tg.Error{},
		usernameFail: map[string]error{},
		next:         9000,
	}
}

func (b *fakeBot) CopyProtectedMessage(_ context.Context, destChat, sourceChat, messageID int64, caption string) (int64, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.calls = append(b.calls, call{dest: destChat, source: sourceChat, messageID: messageID})
	b.captions = append(b.captions, caption)
	if e, ok := b.rateOnce[messageID]; ok {
		delete(b.rateOnce, messageID)
		return 0, e
	}
	if err, ok := b.fail[messageID]; ok {
		return 0, err
	}
	b.next++
	return b.next, nil
}

// CopyProtectedMessageToUsername is the first-contact fallback (docs/06 §3). The fake never fails
// it by default -- once a test puts a message id in fail, this is the only path that can still succeed.
func (b *fakeBot) CopyProtectedMessageToUsername(_ context.Context, username string, sourceChat, messageID int64, caption string) (int64, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.usernameCalls = append(b.usernameCalls, usernameCall{username: username, source: sourceChat, messageID: messageID, caption: caption})
	if err, ok := b.usernameFail[username]; ok {
		return 0, err
	}
	b.next++
	return b.next, nil
}

func (b *fakeBot) attempts() []call {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make([]call, len(b.calls))
	copy(out, b.calls)
	return out
}

func (b *fakeBot) usernameAttempts() []usernameCall {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make([]usernameCall, len(b.usernameCalls))
	copy(out, b.usernameCalls)
	return out
}

func quietLog() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func newJob(readerChatID, fileID int64, locator string) *job {
	return &job{readerChatID: readerChatID, fileID: fileID, locator: locator}
}

func (j *job) jobKind() catalog.MediaKind {
	if j.kind == 0 {
		return catalog.MediaCatalog
	}
	return j.kind
}

func newTestRunner(store Store, slots []Slot) (*Runner, *[]time.Duration) {
	var mu sync.Mutex
	waits := []time.Duration{}
	r := New(store, slots, nil, quietLog(), nil)
	r.sleep = func(_ context.Context, d time.Duration) error {
		mu.Lock()
		defer mu.Unlock()
		waits = append(waits, d)
		return nil
	}
	return r, &waits
}

func TestRunDeliversTheBacklog(t *testing.T) {
	t.Parallel()

	store := newStore(newJob(111, 1, "plm_1"), newJob(222, 2, "plm_2"))
	store.cloneLoc[1] = cloneLoc{channelID: -100111, messageID: 11}
	store.cloneLoc[2] = cloneLoc{channelID: -100111, messageID: 12}
	bot := newBot()
	r, _ := newTestRunner(store, []Slot{{BotID: 7, Username: "dl1_bot", Bot: bot}})

	if err := r.Run(t.Context(), true); err != nil {
		t.Fatalf("Run: %v", err)
	}
	done := store.doneJobs()
	if !done[jobKey{111, 1}] || !done[jobKey{222, 2}] {
		t.Fatalf("done = %v, want both jobs delivered", done)
	}
	if got := len(bot.attempts()); got != 2 {
		t.Fatalf("copied %d messages, want 2", got)
	}
}

// The locator is the caption here (unlike backupd's empty one): the client verifies it before
// trusting the copy (docs/05 §3).
func TestCopyUsesTheJobsLocatorAsTheCaption(t *testing.T) {
	t.Parallel()

	store := newStore(newJob(111, 1, "plm_4211"))
	store.cloneLoc[1] = cloneLoc{channelID: -100111, messageID: 11}
	bot := newBot()
	r, _ := newTestRunner(store, []Slot{{BotID: 7, Username: "dl1_bot", Bot: bot}})

	if err := r.Run(t.Context(), true); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(bot.captions) != 1 || bot.captions[0] != "plm_4211" {
		t.Fatalf("captions = %v, want [plm_4211]", bot.captions)
	}
}

func TestCopiesFromTheChannelTheCloneLocationRecords(t *testing.T) {
	t.Parallel()

	store := newStore(newJob(111, 1, "plm_1"))
	store.cloneLoc[1] = cloneLoc{channelID: -100999, messageID: 42}
	bot := newBot()
	r, _ := newTestRunner(store, []Slot{{BotID: 7, Username: "dl1_bot", Bot: bot}})

	if err := r.Run(t.Context(), true); err != nil {
		t.Fatalf("Run: %v", err)
	}
	attempts := bot.attempts()
	if len(attempts) != 1 {
		t.Fatalf("got %d copies, want 1", len(attempts))
	}
	if attempts[0].dest != 111 {
		t.Errorf("copied into chat %d, want the reader's chat 111", attempts[0].dest)
	}
	if attempts[0].source != -100999 || attempts[0].messageID != 42 {
		t.Errorf("copied from %d/%d, want the clone location -100999/42", attempts[0].source, attempts[0].messageID)
	}
}

// A file whose clone copy has not landed yet must not stop other jobs in the same pass.
func TestRunStepsOverAJobWithNoCloneLocationYet(t *testing.T) {
	t.Parallel()

	store := newStore(newJob(111, 1, "plm_1"), newJob(222, 2, "plm_2"))
	store.cloneErr[1] = errors.New("file 1 has no clone location yet")
	store.cloneLoc[2] = cloneLoc{channelID: -100111, messageID: 12}
	bot := newBot()
	r, _ := newTestRunner(store, []Slot{{BotID: 7, Username: "dl1_bot", Bot: bot}})

	err := r.Run(t.Context(), true)
	if err == nil {
		t.Fatal("Run reported success while a job could not be delivered")
	}
	done := store.doneJobs()
	if !done[jobKey{222, 2}] {
		t.Error("file 2 was not delivered; the broken job blocked the batch behind it")
	}
	if done[jobKey{111, 1}] {
		t.Error("file 1 recorded done despite having no clone location")
	}
}

// One dead bot's failing slot must not stop another slot's deliveries. Run() gives each slot its
// own goroutine and joins their errors afterward (no shared errgroup, deliberately -- the same
// reasoning and the same code as internal/app/backup.Runner.Run); this exercises that isolation
// with a store per slot so which job each one claims is deterministic rather than a race.
func TestOneBrokenSlotDoesNotStopTheOthers(t *testing.T) {
	t.Parallel()

	deadStore := newStore(newJob(111, 1, "plm_1"))
	deadStore.cloneLoc[1] = cloneLoc{channelID: -100111, messageID: 11}
	dead := newBot()
	dead.fail[11] = &tg.Error{Code: http.StatusForbidden, Description: "bot was kicked"}

	healthyStore := newStore(newJob(222, 2, "plm_2"))
	healthyStore.cloneLoc[2] = cloneLoc{channelID: -100111, messageID: 12}
	healthy := newBot()

	multi := multiStore{7: deadStore, 8: healthyStore}
	r, _ := newTestRunner(multi, []Slot{
		{BotID: 7, Username: "dead_bot", Bot: dead},
		{BotID: 8, Username: "healthy_bot", Bot: healthy},
	})

	err := r.Run(t.Context(), true)
	if err == nil {
		t.Fatal("Run reported success while the dead bot's slot failed")
	}
	if !healthyStore.doneJobs()[jobKey{222, 2}] {
		t.Error("the healthy slot's job was not delivered because the dead slot failed")
	}
}

// multiStore routes each bot to its own backing store, so two slots in one Runner never compete
// for the same claim -- letting the test above assert on each slot's outcome deterministically.
type multiStore map[int32]*fakeStore

func (m multiStore) ClaimPendingDeliveries(ctx context.Context, botID int32, batch int, stale time.Duration) ([]PendingJob, error) {
	return m[botID].ClaimPendingDeliveries(ctx, botID, batch, stale)
}

func (m multiStore) CloneLocation(ctx context.Context, fileID int64) (int64, int64, error) {
	for _, s := range m {
		if _, ok := s.cloneLoc[fileID]; ok {
			return s.CloneLocation(ctx, fileID)
		}
		if _, ok := s.cloneErr[fileID]; ok {
			return s.CloneLocation(ctx, fileID)
		}
	}
	return 0, 0, errors.New("file not found in any store")
}

func (m multiStore) PersonalLocation(ctx context.Context, videoID int64) (int64, int64, error) {
	for _, s := range m {
		if _, ok := s.personalLoc[videoID]; ok {
			return s.PersonalLocation(ctx, videoID)
		}
	}
	return 0, 0, errors.New("video not found in any store")
}

func (m multiStore) CompleteDelivery(
	ctx context.Context, readerChatID int64, kind catalog.MediaKind, fileID int64, botID int32, messageID int64,
) error {
	return m[botID].CompleteDelivery(ctx, readerChatID, kind, fileID, botID, messageID)
}

func (m multiStore) ReaderUsername(ctx context.Context, readerChatID int64) (string, bool, error) {
	for _, s := range m {
		if u, ok, err := s.ReaderUsername(ctx, readerChatID); ok || err != nil {
			return u, ok, err
		}
	}
	return "", false, nil
}

func TestRateLimitWithoutRetryAfterWaitsTheFloor(t *testing.T) {
	t.Parallel()

	store := newStore(newJob(111, 1, "plm_1"))
	store.cloneLoc[1] = cloneLoc{channelID: -100111, messageID: 11}
	bot := newBot()
	bot.rateOnce[11] = &tg.Error{Code: http.StatusTooManyRequests, Description: "Too Many Requests"}
	r, waits := newTestRunner(store, []Slot{{BotID: 7, Username: "dl1_bot", Bot: bot}})

	if err := r.Run(t.Context(), true); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(*waits) != 1 {
		t.Fatalf("waited %d times, want 1", len(*waits))
	}
	if (*waits)[0] < minRetryAfter {
		t.Errorf("waited %v after a 429 with no retry_after, want at least %v", (*waits)[0], minRetryAfter)
	}
	if !store.doneJobs()[jobKey{111, 1}] {
		t.Error("the retry did not land")
	}
}

func TestRateLimitHonoursTelegramsRetryAfter(t *testing.T) {
	t.Parallel()

	store := newStore(newJob(111, 1, "plm_1"))
	store.cloneLoc[1] = cloneLoc{channelID: -100111, messageID: 11}
	bot := newBot()
	bot.rateOnce[11] = &tg.Error{Code: http.StatusTooManyRequests, RetryAfter: 42 * time.Second}
	r, waits := newTestRunner(store, []Slot{{BotID: 7, Username: "dl1_bot", Bot: bot}})

	if err := r.Run(t.Context(), true); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(*waits) != 1 || (*waits)[0] != 42*time.Second {
		t.Errorf("waits = %v, want exactly [42s]", *waits)
	}
}

// A copy that lands while its row does not leaves a message nobody points at. It has to be named.
func TestOrphanedCopyIsReported(t *testing.T) {
	t.Parallel()

	store := newStore(newJob(111, 1, "plm_1"))
	store.cloneLoc[1] = cloneLoc{channelID: -100111, messageID: 11}
	store.completeErr[jobKey{111, 1}] = errors.New("connection reset")
	bot := newBot()
	r, _ := newTestRunner(store, []Slot{{BotID: 7, Username: "dl1_bot", Bot: bot}})

	err := r.Run(t.Context(), true)
	if err == nil {
		t.Fatal("Run reported success after the completion write failed")
	}
	if !strings.Contains(err.Error(), "could not be delivered") {
		t.Errorf("error does not report the failure: %v", err)
	}
}

func TestUnclaimableBacklogIsReportedNotFatal(t *testing.T) {
	t.Parallel()

	store := newStore(newJob(111, 1, "plm_1"))
	store.claimErr = errors.New("server closed the connection")
	bot := newBot()
	r, _ := newTestRunner(store, []Slot{{BotID: 7, Username: "dl1_bot", Bot: bot}})

	err := r.Run(t.Context(), true)
	if err == nil {
		t.Fatal("a failed claim was reported as success")
	}
	if !strings.Contains(err.Error(), "claim") {
		t.Errorf("error does not say what failed: %v", err)
	}
}

func TestRunStopsOnContextCancel(t *testing.T) {
	t.Parallel()

	store := newStore(newJob(111, 1, "plm_1"))
	store.cloneLoc[1] = cloneLoc{channelID: -100111, messageID: 11}
	bot := newBot()
	ctx, cancel := context.WithCancel(t.Context())

	r := New(store, []Slot{{BotID: 7, Username: "dl1_bot", Bot: bot}}, nil, quietLog(), nil)
	r.sleep = func(context.Context, time.Duration) error {
		cancel()
		return context.Canceled
	}

	done := make(chan error, 1)
	go func() { done <- r.Run(ctx, false) }()

	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Errorf("Run returned %v, want context.Canceled", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return after its context was cancelled")
	}
}

// Regression test for a bug a live smoke test caught: someone pressed play and deliveryd, having
// been idle, did not pick the job up for a full minute -- backoff had climbed toward the 5-minute
// ceiling copied from backupd, which is fine for a bulk backlog nobody is watching but not for a
// service serving somebody waiting on a play button. The workload here is inherently small
// (R-SEC-11 caps it at 2 concurrent jobs per user), so the ceiling must stay low.
func TestIdleBackoffStaysShort(t *testing.T) {
	t.Parallel()

	store := newStore() // never anything to claim
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	r := New(store, []Slot{{BotID: 7, Username: "dl1_bot"}}, nil, quietLog(), nil)
	var mu sync.Mutex
	var waits []time.Duration
	r.sleep = func(_ context.Context, d time.Duration) error {
		mu.Lock()
		waits = append(waits, d)
		n := len(waits)
		mu.Unlock()
		if n >= 8 {
			cancel()
			return context.Canceled
		}
		return nil
	}

	if err := r.Run(ctx, false); err != nil && !errors.Is(err, context.Canceled) {
		t.Fatalf("Run: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	for _, w := range waits {
		if w > 15*time.Second {
			t.Fatalf("idle backoff reached %v; a real /play could wait that long for deliveryd to notice it", w)
		}
	}
}

// activeBotStore wraps fakeStore to also satisfy activeBotChecker (docs/02 §7) -- kept separate
// from fakeStore itself so every other test in this file keeps the "always active" default
// (fakeStore alone does not implement activeBotChecker, per Runner.slotIsActive's fail-open rule).
type activeBotStore struct {
	*fakeStore
	active map[int32]bool
}

func (s activeBotStore) ActiveDeliveryBotIDs(context.Context) (map[int32]bool, error) {
	return s.active, nil
}

// A quarantined or retired bot (docs/02 §7) must not be handed new work: its pending rows stay
// claimable by whichever slot next reports itself active, exactly like a stale claim would be.
func TestPassSkipsAQuarantinedOrRetiredSlot(t *testing.T) {
	t.Parallel()

	inner := newStore(newJob(111, 1, "plm_1"))
	inner.cloneLoc[1] = cloneLoc{channelID: -100111, messageID: 11}
	store := activeBotStore{fakeStore: inner, active: map[int32]bool{}} // bot 7 not listed: inactive
	bot := newBot()

	r, _ := newTestRunner(store, []Slot{{BotID: 7, Username: "dl1_bot", Bot: bot}})
	if err := r.Run(t.Context(), true); err != nil {
		t.Fatalf("Run: %v", err)
	}

	if len(bot.attempts()) != 0 {
		t.Error("an inactive slot copied a file; it should have been skipped entirely")
	}
	if store.doneJobs()[jobKey{111, 1}] {
		t.Error("job was marked done despite its only slot being inactive")
	}
}

// A fresh 401/429 must reach the configured health.Reporter so bot rotation (docs/02 §7) actually
// fires, not just get logged and stepped over.
func TestPassReportsHealthOnDeliveryFailure(t *testing.T) {
	t.Parallel()

	store := newStore(newJob(111, 1, "plm_1"))
	store.cloneLoc[1] = cloneLoc{channelID: -100111, messageID: 11}
	bot := newBot()
	bot.fail[11] = &tg.Error{Code: http.StatusUnauthorized, Description: "Unauthorized"}

	reported := &fakeHealthReporter{}
	r := New(store, []Slot{{BotID: 7, Username: "dl1_bot", Bot: bot}}, nil, quietLog(), reported)

	// The copy genuinely fails (the bot is banned), so Run correctly reports it as an undelivered
	// job in "once" mode -- what this test cares about is that health.Report still saw it.
	if err := r.Run(t.Context(), true); err == nil {
		t.Fatal("Run: want an error for the undelivered job, got nil")
	}

	calls := reported.calls()
	if len(calls) != 1 {
		t.Fatalf("health.Report called %d times, want 1", len(calls))
	}
	if calls[0].botID != 7 || calls[0].role != "delivery" || calls[0].username != "dl1_bot" {
		t.Errorf("Report called with %+v, want botID=7 role=delivery username=dl1_bot", calls[0])
	}
}

type healthReport struct {
	botID          int32
	role, username string
}

type fakeHealthReporter struct {
	mu   sync.Mutex
	seen []healthReport
}

func (f *fakeHealthReporter) Report(_ context.Context, botID int32, role, username string, _ error) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.seen = append(f.seen, healthReport{botID: botID, role: role, username: username})
	return true
}

func (f *fakeHealthReporter) calls() []healthReport {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]healthReport, len(f.seen))
	copy(out, f.seen)
	return out
}

// A delivery bot has never contacted this reader before -- the pool assignment is effectively
// random, so nothing could have warmed it in advance, unlike a sticky API bot (docs/06 §3,
// confirmed live 2026-08-26). The fallback must resolve it by username and still deliver.
func TestCopyFallsBackToUsernameOnFirstContact(t *testing.T) {
	t.Parallel()

	store := newStore(newJob(111, 1, "plm_1"))
	store.cloneLoc[1] = cloneLoc{channelID: -100111, messageID: 11}
	store.usernames[111] = "the_readers_bot"
	bot := newBot()
	bot.fail[11] = chatNotFound()
	r, _ := newTestRunner(store, []Slot{{BotID: 7, Username: "dl1_bot", Bot: bot}})

	if err := r.Run(t.Context(), true); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !store.doneJobs()[jobKey{111, 1}] {
		t.Fatal("job was not delivered via the username fallback")
	}
	attempts := bot.usernameAttempts()
	if len(attempts) != 1 {
		t.Fatalf("got %d username attempts, want 1", len(attempts))
	}
	if attempts[0].username != "the_readers_bot" || attempts[0].caption != "plm_1" {
		t.Errorf("username attempt = %+v, want the reader's handle and the locator as caption", attempts[0])
	}
}

// A session reader has no username on file at all -- there is nothing to fall back to, and
// "chat not found" from a real account is a genuine anomaly, not a peer gap. It must surface
// unchanged rather than being swallowed by a fallback that was never applicable.
func TestCopyDoesNotFallBackWhenNoUsernameIsOnFile(t *testing.T) {
	t.Parallel()

	store := newStore(newJob(111, 1, "plm_1")) // no store.usernames entry for 111
	store.cloneLoc[1] = cloneLoc{channelID: -100111, messageID: 11}
	bot := newBot()
	bot.fail[11] = chatNotFound()
	r, _ := newTestRunner(store, []Slot{{BotID: 7, Username: "dl1_bot", Bot: bot}})

	err := r.Run(t.Context(), true)
	if err == nil {
		t.Fatal("Run reported success for a reader with no fallback available")
	}
	if len(bot.usernameAttempts()) != 0 {
		t.Error("attempted a username fallback with no username on file")
	}
	if store.doneJobs()[jobKey{111, 1}] {
		t.Error("job recorded done despite the copy failing")
	}
}

// The fallback itself can fail too (a stale or wrong username, say); that failure must be
// reported, not silently treated as "nothing to fall back to".
func TestUsernameFallbackFailureIsReported(t *testing.T) {
	t.Parallel()

	store := newStore(newJob(111, 1, "plm_1"))
	store.cloneLoc[1] = cloneLoc{channelID: -100111, messageID: 11}
	store.usernames[111] = "stale_handle"
	bot := newBot()
	bot.fail[11] = chatNotFound()
	bot.usernameFail["stale_handle"] = errors.New("telegram: 400 Bad Request: chat not found")
	r, _ := newTestRunner(store, []Slot{{BotID: 7, Username: "dl1_bot", Bot: bot}})

	err := r.Run(t.Context(), true)
	if err == nil {
		t.Fatal("Run reported success when the username fallback itself failed")
	}
	// Failure detail lives in the log, same as every other stepped-over job in this package (once
	// mode's return value is a count, not a cause) -- what must hold here is that the fallback was
	// genuinely attempted, and the job was not wrongly marked done.
	if len(bot.usernameAttempts()) != 1 {
		t.Fatalf("got %d username attempts, want exactly 1", len(bot.usernameAttempts()))
	}
	if store.doneJobs()[jobKey{111, 1}] {
		t.Error("job recorded done despite the fallback failing")
	}
}

// Looking up the username is itself a database read and can fail like any other.
func TestReaderUsernameLookupFailureIsReported(t *testing.T) {
	t.Parallel()

	store := newStore(newJob(111, 1, "plm_1"))
	store.cloneLoc[1] = cloneLoc{channelID: -100111, messageID: 11}
	store.usernameErr = errors.New("connection reset")
	bot := newBot()
	bot.fail[11] = chatNotFound()
	r, _ := newTestRunner(store, []Slot{{BotID: 7, Username: "dl1_bot", Bot: bot}})

	err := r.Run(t.Context(), true)
	if err == nil {
		t.Fatal("Run reported success when the username lookup itself failed")
	}
	if len(bot.usernameAttempts()) != 0 {
		t.Error("attempted a copy despite the username lookup failing")
	}
}

// fakeSender stands in for an API bot: the Ready push goes out through it, never
// through the delivery bot that copied the file.
type fakeSender struct {
	mu   sync.Mutex
	sent []struct {
		chatID int64
		text   string
	}
	err error
}

func (f *fakeSender) SendMessage(_ context.Context, chatID int64, text string) (tg.Message, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return tg.Message{}, f.err
	}
	f.sent = append(f.sent, struct {
		chatID int64
		text   string
	}{chatID, text})
	return tg.Message{}, nil
}

func (f *fakeSender) texts() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]string, len(f.sent))
	for i, s := range f.sent {
		out[i] = s.text
	}
	return out
}

// A completed delivery pushes Ready (docs/02 §8) through an API bot -- not the delivery bot
// dl1_bot that just did the copy (ADR 0012: pool round-robin, not a bound primary).
func TestDeliverySendsAReadyPushThroughAnAPIBot(t *testing.T) {
	t.Parallel()

	store := newStore(newJob(111, 4211, "plm_4211"))
	store.cloneLoc[4211] = cloneLoc{channelID: -100111, messageID: 11}
	bot := newBot()
	sender := &fakeSender{}
	r := New(store, []Slot{{BotID: 7, Username: "dl1_bot", Bot: bot}},
		map[string]Sender{"api1_bot": sender}, quietLog(), nil)

	if err := r.Run(t.Context(), true); err != nil {
		t.Fatalf("Run: %v", err)
	}

	texts := sender.texts()
	if len(texts) != 1 {
		t.Fatalf("sent %d pushes, want 1", len(texts))
	}
	env, err := wire.DecodeText(texts[0])
	if err != nil {
		t.Fatalf("decode push: %v", err)
	}
	if !env.IsPush() {
		t.Errorf("corr = %d, want 0 (a push, docs/02 §8)", env.Corr)
	}
	if env.Type != uint64(sushiv1.MsgType_MSG_TYPE_PUSH_READY) {
		t.Errorf("type = %d, want PUSH_READY", env.Type)
	}
	var ready sushiv1.Ready
	if err := proto.Unmarshal(env.Payload, &ready); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if ready.GetFileId() != 4211 || ready.GetBotId() != 7 {
		t.Errorf("ready = %+v, want file 4211 bot 7", &ready)
	}
}

func TestDeliveryRoundRobinsReadyPushesAcrossAPIBots(t *testing.T) {
	t.Parallel()

	store := newStore(newJob(111, 1, "a"), newJob(111, 2, "b"))
	store.cloneLoc[1] = cloneLoc{channelID: -100111, messageID: 11}
	store.cloneLoc[2] = cloneLoc{channelID: -100111, messageID: 12}
	first, second := &fakeSender{}, &fakeSender{}
	r := New(store, []Slot{{BotID: 7, Username: "dl1_bot", Bot: newBot()}},
		map[string]Sender{"api_z": second, "api_a": first}, quietLog(), nil)

	if err := r.Run(t.Context(), true); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(first.texts()) != 1 || len(second.texts()) != 1 {
		t.Errorf("pushes api_a=%d api_z=%d, want 1 each (sorted round-robin)",
			len(first.texts()), len(second.texts()))
	}
}

// The push is a best-effort safety net (docs/02 §8): a failure sending it must never undo an
// otherwise-successful delivery.
func TestDeliverySucceedsEvenWhenTheReadyPushFails(t *testing.T) {
	t.Parallel()

	store := newStore(newJob(111, 1, "plm_1"))
	store.cloneLoc[1] = cloneLoc{channelID: -100111, messageID: 11}
	bot := newBot()
	sender := &fakeSender{err: errors.New("telegram: 500 internal error")}
	r := New(store, []Slot{{BotID: 7, Username: "dl1_bot", Bot: bot}},
		map[string]Sender{"api1_bot": sender}, quietLog(), nil)

	if err := r.Run(t.Context(), true); err != nil {
		t.Fatalf("Run: %v, want the delivery to succeed despite the push failing", err)
	}
	if !store.doneJobs()[jobKey{111, 1}] {
		t.Error("job was not marked done despite the copy succeeding")
	}
}

// deliveryd starting with no API_BOTS configured (pushBots nil) is a valid, if degraded, config
// (docs/02 §8: every push has a polling fallback) -- deliveries must still complete.
func TestDeliverySucceedsWithNoPushBotsConfigured(t *testing.T) {
	t.Parallel()

	store := newStore(newJob(111, 1, "plm_1"))
	store.cloneLoc[1] = cloneLoc{channelID: -100111, messageID: 11}
	bot := newBot()
	r, _ := newTestRunner(store, []Slot{{BotID: 7, Username: "dl1_bot", Bot: bot}})

	if err := r.Run(t.Context(), true); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !store.doneJobs()[jobKey{111, 1}] {
		t.Error("job was not marked done")
	}
}

// A personal video (ADR 0037) is copied out of the personal channels, never out of the catalog
// clone -- even when a catalog file with the same number exists -- and its Ready push names it as a
// personal id, so the client cannot mistake it for that catalog file.
func TestPersonalJobCopiesFromThePersonalChannelAndPushesAPersonalID(t *testing.T) {
	t.Parallel()

	j := newJob(111, 42, "plp_42")
	j.kind = catalog.MediaPersonal
	store := newStore(j)
	store.cloneLoc[42] = cloneLoc{channelID: -100111, messageID: 11} // catalog file 42: must not be used
	store.personalLoc[42] = cloneLoc{channelID: -100777, messageID: 77}
	bot := newBot()
	sender := &fakeSender{}
	r := New(store, []Slot{{BotID: 7, Username: "dl1_bot", Bot: bot}},
		map[string]Sender{"api1_bot": sender}, quietLog(), nil)

	if err := r.Run(t.Context(), true); err != nil {
		t.Fatalf("Run: %v", err)
	}
	attempts := bot.attempts()
	if len(attempts) != 1 || attempts[0].source != -100777 || attempts[0].messageID != 77 {
		t.Fatalf("attempts = %+v, want one copy from the personal location -100777/77", attempts)
	}
	if !store.doneJobs()[jobKey{111, 42}] {
		t.Error("personal job was not completed")
	}

	texts := sender.texts()
	if len(texts) != 1 {
		t.Fatalf("sent %d pushes, want 1", len(texts))
	}
	env, err := wire.DecodeText(texts[0])
	if err != nil {
		t.Fatalf("decode push: %v", err)
	}
	var ready sushiv1.Ready
	if err := proto.Unmarshal(env.Payload, &ready); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if ready.GetPersonalId() != 42 || ready.GetFileId() != 0 {
		t.Errorf("ready = %+v, want personal_id 42 and no file_id", &ready)
	}
}
