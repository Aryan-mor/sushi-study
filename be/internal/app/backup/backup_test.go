package backup

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Aryan-mor/sushi-study/be/internal/adapter/pglib"
	"github.com/Aryan-mor/sushi-study/be/internal/adapter/tg"
	"github.com/Aryan-mor/sushi-study/be/internal/platform/config"
)

// fakeStore is the library, in memory. Its CloneLocationsMissingRole answers the same question the
// real query does -- clone rows this role has no copy of yet -- so a row that fails to copy really
// does come back on the next page, which is the whole trap this package has to survive.
type fakeStore struct {
	mu    sync.Mutex
	clone []pglib.CloneLocationsMissingRoleRow
	saved map[string]pglib.SetFileLocationParams
	// saveErr fails SetFileLocation for one file id, leaving an orphan copy in the channel.
	saveErr map[int64]error
	listErr error
	lists   int
}

func newStore(rows ...pglib.CloneLocationsMissingRoleRow) *fakeStore {
	return &fakeStore{
		clone:   rows,
		saved:   map[string]pglib.SetFileLocationParams{},
		saveErr: map[int64]error{},
	}
}

func key(role string, fileID int64) string { return role + ":" + strconv.FormatInt(fileID, 10) }

func (s *fakeStore) CloneLocationsMissingRole(
	_ context.Context, arg pglib.CloneLocationsMissingRoleParams,
) ([]pglib.CloneLocationsMissingRoleRow, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lists++
	if s.listErr != nil {
		return nil, s.listErr
	}
	out := []pglib.CloneLocationsMissingRoleRow{}
	for _, row := range s.clone {
		if row.FileID <= arg.AfterFileID {
			continue
		}
		if _, done := s.saved[key(arg.Role, row.FileID)]; done {
			continue
		}
		out = append(out, row)
		if len(out) == int(arg.PageSize) {
			break
		}
	}
	return out, nil
}

func (s *fakeStore) SetFileLocation(_ context.Context, arg pglib.SetFileLocationParams) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.saveErr[arg.FileID]; err != nil {
		return err
	}
	s.saved[key(arg.Role, arg.FileID)] = arg
	return nil
}

func (s *fakeStore) savedFor(role string, fileID int64) (pglib.SetFileLocationParams, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.saved[key(role, fileID)]
	return v, ok
}

type call struct {
	dest, source, messageID int64
}

// fakeBot answers CopyMessage from a script keyed by clone message id, and records every attempt.
type fakeBot struct {
	mu       sync.Mutex
	calls    []call
	fail     map[int64]error // permanent failure, by clone message id
	rateOnce map[int64]*tg.Error
	next     int64
}

func newBot() *fakeBot {
	return &fakeBot{fail: map[int64]error{}, rateOnce: map[int64]*tg.Error{}, next: 9000}
}

func (b *fakeBot) CopyMessage(_ context.Context, destChat, sourceChat, messageID int64, _ string) (int64, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.calls = append(b.calls, call{dest: destChat, source: sourceChat, messageID: messageID})
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

func (b *fakeBot) attempts() []call {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make([]call, len(b.calls))
	copy(out, b.calls)
	return out
}

func quietLog() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func row(fileID, channelID, messageID int64) pglib.CloneLocationsMissingRoleRow {
	return pglib.CloneLocationsMissingRoleRow{FileID: fileID, ChannelID: channelID, MessageID: messageID}
}

func slot(role string, channelID int64, bot Copier) Slot {
	return Slot{
		BackupSlot: config.BackupSlot{Role: role, Username: role + "_bot", ChannelID: channelID},
		Bot:        bot,
	}
}

// newTestRunner builds a runner whose pauses are recorded instead of taken.
func newTestRunner(store Store, slots []Slot) (*Runner, *[]time.Duration) {
	var mu sync.Mutex
	waits := []time.Duration{}
	r := New(store, slots, quietLog())
	r.sleep = func(_ context.Context, d time.Duration) error {
		mu.Lock()
		defer mu.Unlock()
		waits = append(waits, d)
		return nil
	}
	return r, &waits
}

func TestRunCopiesTheBacklogAndRecordsEveryLocation(t *testing.T) {
	t.Parallel()

	store := newStore(row(1, -100111, 11), row(2, -100111, 12), row(3, -100111, 13))
	bot := newBot()
	r, _ := newTestRunner(store, []Slot{slot("backup1", -100222, bot)})

	if err := r.Run(t.Context(), true); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got := len(bot.attempts()); got != 3 {
		t.Fatalf("copied %d messages, want 3", got)
	}
	for _, fileID := range []int64{1, 2, 3} {
		saved, ok := store.savedFor("backup1", fileID)
		if !ok {
			t.Fatalf("file %d has no backup1 location", fileID)
		}
		if saved.ChannelID != -100222 {
			t.Errorf("file %d recorded in channel %d, want the backup channel -100222", fileID, saved.ChannelID)
		}
	}
}

// A clone message that can never be copied used to sit at the head of every page and stop the
// role for ever. It must cost one attempt and then be stepped over.
func TestRunStepsOverAPermanentlyFailingRow(t *testing.T) {
	t.Parallel()

	store := newStore(row(1, -100111, 11), row(2, -100111, 12), row(3, -100111, 13))
	bot := newBot()
	bot.fail[12] = &tg.Error{Code: http.StatusBadRequest, Description: "MESSAGE_ID_INVALID"}
	r, _ := newTestRunner(store, []Slot{slot("backup1", -100222, bot)})

	err := r.Run(t.Context(), true)
	if err == nil {
		t.Fatal("Run reported success while a row could not be copied")
	}
	if !strings.Contains(err.Error(), "backup1") {
		t.Errorf("error does not name the role: %v", err)
	}

	for _, fileID := range []int64{1, 3} {
		if _, ok := store.savedFor("backup1", fileID); !ok {
			t.Errorf("file %d was not backed up; the broken row blocked the queue behind it", fileID)
		}
	}
	if _, ok := store.savedFor("backup1", 2); ok {
		t.Error("file 2 recorded a location despite the copy failing")
	}
	// One attempt for the broken row, not a retry storm.
	broken := 0
	for _, c := range bot.attempts() {
		if c.messageID == 12 {
			broken++
		}
	}
	if broken != 1 {
		t.Errorf("broken row attempted %d times in one pass, want 1", broken)
	}
}

// One dead bot must not take the other backup channels down with it. This is why Run does not use
// an errgroup: errgroup would cancel the shared context on the first failure.
func TestOneBrokenSlotDoesNotStopTheOthers(t *testing.T) {
	t.Parallel()

	store := newStore(row(1, -100111, 11), row(2, -100111, 12))
	dead := newBot()
	dead.fail[11] = &tg.Error{Code: http.StatusForbidden, Description: "bot was kicked"}
	dead.fail[12] = &tg.Error{Code: http.StatusForbidden, Description: "bot was kicked"}
	healthy := newBot()

	r, _ := newTestRunner(store, []Slot{
		slot("backup1", -100222, dead),
		slot("backup2", -100333, healthy),
	})

	err := r.Run(t.Context(), true)
	if err == nil {
		t.Fatal("Run reported success while backup1 copied nothing")
	}
	if !strings.Contains(err.Error(), "backup1") {
		t.Errorf("error does not name the broken role: %v", err)
	}
	for _, fileID := range []int64{1, 2} {
		if _, ok := store.savedFor("backup2", fileID); !ok {
			t.Errorf("backup2 lost file %d because backup1 failed", fileID)
		}
	}
}

// A 429 is allowed to arrive with no retry_after. Waiting zero seconds turns a rate limit into a
// hot loop against the API.
func TestRateLimitWithoutRetryAfterWaitsTheFloor(t *testing.T) {
	t.Parallel()

	store := newStore(row(1, -100111, 11))
	bot := newBot()
	bot.rateOnce[11] = &tg.Error{Code: http.StatusTooManyRequests, Description: "Too Many Requests"}
	r, waits := newTestRunner(store, []Slot{slot("backup1", -100222, bot)})

	if err := r.Run(t.Context(), true); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(*waits) != 1 {
		t.Fatalf("waited %d times, want 1", len(*waits))
	}
	if (*waits)[0] < minRetryAfter {
		t.Errorf("waited %v after a 429 with no retry_after, want at least %v", (*waits)[0], minRetryAfter)
	}
	if _, ok := store.savedFor("backup1", 1); !ok {
		t.Error("the retry did not land")
	}
}

func TestRateLimitHonoursTelegramsRetryAfter(t *testing.T) {
	t.Parallel()

	store := newStore(row(1, -100111, 11))
	bot := newBot()
	bot.rateOnce[11] = &tg.Error{
		Code:       http.StatusTooManyRequests,
		RetryAfter: 42 * time.Second,
	}
	r, waits := newTestRunner(store, []Slot{slot("backup1", -100222, bot)})

	if err := r.Run(t.Context(), true); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(*waits) != 1 || (*waits)[0] != 42*time.Second {
		t.Errorf("waits = %v, want exactly [42s]", *waits)
	}
}

// The source channel is whatever the clone row records, not whatever CHANNELS names today.
func TestCopiesFromTheChannelTheCloneRowRecords(t *testing.T) {
	t.Parallel()

	const retiredClone = -100999
	store := newStore(row(1, retiredClone, 11))
	bot := newBot()
	r, _ := newTestRunner(store, []Slot{slot("backup1", -100222, bot)})

	if err := r.Run(t.Context(), true); err != nil {
		t.Fatalf("Run: %v", err)
	}
	attempts := bot.attempts()
	if len(attempts) != 1 {
		t.Fatalf("got %d copies, want 1", len(attempts))
	}
	if attempts[0].source != retiredClone {
		t.Errorf("copied from channel %d, want the one the row records (%d)", attempts[0].source, retiredClone)
	}
}

// A copy that lands while its row does not leaves a message nobody points at. It has to be named.
func TestOrphanedCopyIsReported(t *testing.T) {
	t.Parallel()

	store := newStore(row(1, -100111, 11))
	store.saveErr[1] = errors.New("connection reset")
	bot := newBot()
	r, _ := newTestRunner(store, []Slot{slot("backup1", -100222, bot)})

	err := r.Run(t.Context(), true)
	if err == nil {
		t.Fatal("Run reported success after the location write failed")
	}
	if !strings.Contains(err.Error(), "could not be copied") {
		t.Errorf("error does not report the failure: %v", err)
	}
}

// A database that is briefly away ends the pass; it does not end the process.
func TestUnreadableBacklogIsReportedNotFatal(t *testing.T) {
	t.Parallel()

	store := newStore(row(1, -100111, 11))
	store.listErr = errors.New("server closed the connection")
	bot := newBot()
	r, _ := newTestRunner(store, []Slot{slot("backup1", -100222, bot)})

	err := r.Run(t.Context(), true)
	if err == nil {
		t.Fatal("a failed backlog read was reported as success")
	}
	if !strings.Contains(err.Error(), "list missing backup1") {
		t.Errorf("error does not say what failed: %v", err)
	}
}

// Continuous mode must stop when the process is asked to.
func TestRunStopsOnContextCancel(t *testing.T) {
	t.Parallel()

	store := newStore(row(1, -100111, 11))
	bot := newBot()
	ctx, cancel := context.WithCancel(t.Context())

	r := New(store, []Slot{slot("backup1", -100222, bot)}, quietLog())
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
