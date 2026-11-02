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

type deliveryKey struct {
	readerChatID int64
	kind         catalog.MediaKind
	fileID       int64
}

type deliveryRow struct {
	botID       int32
	messageID   int64
	locator     string
	deliveredAt time.Time
	enqueuedAt  time.Time
	claimed     bool
}

// fakeDeliveries is the in-process store Play and Ack run against, mirroring the real table's
// state machine closely enough to exercise it: message_id == 0 is pending, exactly like a NULL
// column read back as the zero value (be/internal/adapter/pg/deliveries.go's deref).
type fakeDeliveries struct {
	mu   sync.Mutex
	rows map[deliveryKey]deliveryRow
	err  error

	acked []ackCall
}

type ackCall struct {
	readerChatID int64
	kind         catalog.MediaKind
	fileID       int64
	messageID    int64
}

func (f *fakeDeliveries) Delivery(_ context.Context, readerChatID int64, kind catalog.MediaKind, fileID int64) (api.Delivery, bool, error) {
	if f.err != nil {
		return api.Delivery{}, false, f.err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	row, ok := f.rows[deliveryKey{readerChatID, kind, fileID}]
	if !ok {
		return api.Delivery{}, false, nil
	}
	return api.Delivery{BotID: row.botID, MessageID: row.messageID, Locator: row.locator}, true, nil
}

func (f *fakeDeliveries) PendingCount(_ context.Context, readerChatID int64) (int, error) {
	if f.err != nil {
		return 0, f.err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for k, row := range f.rows {
		if k.readerChatID == readerChatID && row.messageID == 0 {
			n++
		}
	}
	return n, nil
}

func (f *fakeDeliveries) UnclaimedPendingOlderThan(_ context.Context, readerChatID int64, age time.Duration) (int, error) {
	if f.err != nil {
		return 0, f.err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	cutoff := time.Now().Add(-age)
	n := 0
	for k, row := range f.rows {
		if k.readerChatID == readerChatID && row.messageID == 0 && !row.claimed && !row.enqueuedAt.After(cutoff) {
			n++
		}
	}
	return n, nil
}

func (f *fakeDeliveries) DeliveredCountSince(_ context.Context, readerChatID int64, since time.Time) (int, error) {
	if f.err != nil {
		return 0, f.err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for k, row := range f.rows {
		if k.readerChatID == readerChatID && row.messageID != 0 && row.deliveredAt.After(since) {
			n++
		}
	}
	return n, nil
}

func (f *fakeDeliveries) Enqueue(_ context.Context, readerChatID int64, kind catalog.MediaKind, fileID int64, locator string) error {
	if f.err != nil {
		return f.err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	key := deliveryKey{readerChatID, kind, fileID}
	if _, exists := f.rows[key]; exists {
		return nil // ON CONFLICT DO NOTHING
	}
	if f.rows == nil {
		f.rows = map[deliveryKey]deliveryRow{}
	}
	f.rows[key] = deliveryRow{locator: locator, enqueuedAt: time.Now()}
	return nil
}

func (f *fakeDeliveries) ForceRedeliver(_ context.Context, readerChatID int64, kind catalog.MediaKind, fileID int64) error {
	if f.err != nil {
		return f.err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.rows, deliveryKey{readerChatID, kind, fileID})
	return nil
}

func (f *fakeDeliveries) Ack(_ context.Context, readerChatID int64, kind catalog.MediaKind, fileID, messageID int64) error {
	if f.err != nil {
		return f.err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.acked = append(f.acked, ackCall{readerChatID, kind, fileID, messageID})
	return nil
}

// deliver marks a row as already completed for reader 111 by bot 7, as if deliveryd had already run.
func (f *fakeDeliveries) deliver(fileID, messageID int64, locator string) {
	if f.rows == nil {
		f.rows = map[deliveryKey]deliveryRow{}
	}
	f.rows[deliveryKey{readerChatID: 111, kind: catalog.MediaCatalog, fileID: fileID}] = deliveryRow{
		botID: 7, messageID: messageID, locator: locator, deliveredAt: time.Now(),
	}
}

func (f *fakeDeliveries) enqueueStale(fileID int64, age time.Duration, locator string) {
	if f.rows == nil {
		f.rows = map[deliveryKey]deliveryRow{}
	}
	f.rows[deliveryKey{readerChatID: 111, kind: catalog.MediaCatalog, fileID: fileID}] = deliveryRow{
		locator: locator, enqueuedAt: time.Now().Add(-age),
	}
}

func playAPI(d *fakeDeliveries, limits api.RateLimits) *api.API {
	return api.New(&fakeRails{}, d, &fakeCatalog{}, &fakeSearch{}, limits)
}

func TestPlayEnqueuesWhenNothingExists(t *testing.T) {
	t.Parallel()

	d := &fakeDeliveries{}
	res, err := playAPI(d, api.RateLimits{PerHour: 30, MaxConcurrent: 2}).
		Play(context.Background(), api.Principal{ReaderChatID: 111}, &sushiv1.PlayReq{FileId: 4211})
	if err != nil {
		t.Fatalf("Play: %v", err)
	}
	pending := res.GetPending()
	if pending == nil {
		t.Fatalf("outcome = %T, want Pending", res.GetOutcome())
	}
	if pending.GetLocator() != "plm_4211" {
		t.Errorf("locator = %q, want plm_4211 (docs/05 §3's own example)", pending.GetLocator())
	}
	n, err := d.PendingCount(context.Background(), 111)
	if err != nil {
		t.Fatalf("PendingCount: %v", err)
	}
	if n != 1 {
		t.Errorf("pending count = %d, want 1 (Play must actually enqueue)", n)
	}
}

func TestPlayReturnsAnExistingDeliveredRowWithoutTouchingTheStore(t *testing.T) {
	t.Parallel()

	d := &fakeDeliveries{}
	d.deliver(4211, 99000, "plm_4211")

	res, err := playAPI(d, api.RateLimits{PerHour: 30, MaxConcurrent: 2}).
		Play(context.Background(), api.Principal{ReaderChatID: 111}, &sushiv1.PlayReq{FileId: 4211})
	if err != nil {
		t.Fatalf("Play: %v", err)
	}
	delivered := res.GetDelivered()
	if delivered == nil {
		t.Fatalf("outcome = %T, want Delivered", res.GetOutcome())
	}
	if delivered.GetBotId() != 7 || delivered.GetMessageId() != 99000 {
		t.Errorf("delivered = %+v, want bot 7 message 99000", delivered)
	}
}

func TestPlayReturnsTheSameLocatorForAnAlreadyPendingRow(t *testing.T) {
	t.Parallel()

	d := &fakeDeliveries{}
	ctx := context.Background()
	a := playAPI(d, api.RateLimits{PerHour: 30, MaxConcurrent: 2})

	first, err := a.Play(ctx, api.Principal{ReaderChatID: 111}, &sushiv1.PlayReq{FileId: 4211})
	if err != nil {
		t.Fatalf("first Play: %v", err)
	}
	second, err := a.Play(ctx, api.Principal{ReaderChatID: 111}, &sushiv1.PlayReq{FileId: 4211})
	if err != nil {
		t.Fatalf("second Play: %v", err)
	}
	if second.GetPending() == nil {
		t.Fatalf("second outcome = %T, want still Pending", second.GetOutcome())
	}
	if second.GetPending().GetLocator() != first.GetPending().GetLocator() {
		t.Error("a second /play for the same pending file minted a different locator")
	}
	n, err := d.PendingCount(ctx, 111)
	if err != nil {
		t.Fatalf("PendingCount: %v", err)
	}
	if n != 1 {
		t.Errorf("pending count = %d, want exactly 1 (must not double-enqueue)", n)
	}
}

func TestPlayForceDeletesAndRedeliversEvenADeliveredRow(t *testing.T) {
	t.Parallel()

	d := &fakeDeliveries{}
	d.deliver(4211, 99000, "plm_4211")

	res, err := playAPI(d, api.RateLimits{PerHour: 30, MaxConcurrent: 2}).
		Play(context.Background(), api.Principal{ReaderChatID: 111}, &sushiv1.PlayReq{FileId: 4211, Force: true})
	if err != nil {
		t.Fatalf("Play: %v", err)
	}
	if res.GetPending() == nil {
		t.Fatalf("outcome = %T, want Pending after force (docs/05 §4 broken row)", res.GetOutcome())
	}
	n, err := d.PendingCount(context.Background(), 111)
	if err != nil {
		t.Fatalf("PendingCount: %v", err)
	}
	if n != 1 {
		t.Errorf("pending count = %d, want 1 after force re-delivery", n)
	}
}

func TestPlayRejectsAtTheConcurrentJobCap(t *testing.T) {
	t.Parallel()

	d := &fakeDeliveries{}
	ctx := context.Background()
	a := playAPI(d, api.RateLimits{PerHour: 30, MaxConcurrent: 2})

	if _, err := a.Play(ctx, api.Principal{ReaderChatID: 111}, &sushiv1.PlayReq{FileId: 1}); err != nil {
		t.Fatalf("Play 1: %v", err)
	}
	if _, err := a.Play(ctx, api.Principal{ReaderChatID: 111}, &sushiv1.PlayReq{FileId: 2}); err != nil {
		t.Fatalf("Play 2: %v", err)
	}

	_, err := a.Play(ctx, api.Principal{ReaderChatID: 111}, &sushiv1.PlayReq{FileId: 3})
	var rl *api.RateLimitError
	if !errors.As(err, &rl) {
		t.Fatalf("err = %v, want a *api.RateLimitError (R-SEC-11: 2 concurrent copy jobs)", err)
	}
	if rl.RetryAfter <= 0 {
		t.Error("RetryAfter is zero; docs/02 §6 treats zero as \"do not retry\", never send it for ERR_RATE")
	}
}

func TestPlayRejectsWhenDeliverydIsNotClaiming(t *testing.T) {
	t.Parallel()

	d := &fakeDeliveries{}
	d.enqueueStale(1, time.Minute, "plm_1")

	_, err := playAPI(d, api.RateLimits{PerHour: 30, MaxConcurrent: 2}).
		Play(context.Background(), api.Principal{ReaderChatID: 111}, &sushiv1.PlayReq{FileId: 2})
	if !errors.Is(err, api.ErrDeliveryStalled) {
		t.Fatalf("err = %v, want ErrDeliveryStalled (dead copier, not a rate limit)", err)
	}
}

func TestPlayStillReturnsPendingForTheStalledFileItself(t *testing.T) {
	t.Parallel()

	d := &fakeDeliveries{}
	d.enqueueStale(4211, time.Minute, "plm_4211")

	res, err := playAPI(d, api.RateLimits{PerHour: 30, MaxConcurrent: 2}).
		Play(context.Background(), api.Principal{ReaderChatID: 111}, &sushiv1.PlayReq{FileId: 4211})
	if err != nil {
		t.Fatalf("Play: %v", err)
	}
	if res.GetPending() == nil {
		t.Fatalf("outcome = %T, want Pending for the file already queued", res.GetOutcome())
	}
}

func TestPlayRejectsAtTheHourlyDeliveryCap(t *testing.T) {
	t.Parallel()

	d := &fakeDeliveries{}
	d.deliver(1, 100, "plm_1")
	d.deliver(2, 200, "plm_2")

	_, err := playAPI(d, api.RateLimits{PerHour: 2, MaxConcurrent: 10}).
		Play(context.Background(), api.Principal{ReaderChatID: 111}, &sushiv1.PlayReq{FileId: 3})
	var rl *api.RateLimitError
	if !errors.As(err, &rl) {
		t.Fatalf("err = %v, want a *api.RateLimitError (R-SEC-11: 30 deliveries/hour default, 2 here)", err)
	}
}

func TestPlayLimitsAreScopedPerReaderChat(t *testing.T) {
	t.Parallel()

	// One user with a session and a personal bot has two readers; one being at its cap must not
	// block the other (docs/06 §3).
	d := &fakeDeliveries{}
	ctx := context.Background()
	a := playAPI(d, api.RateLimits{PerHour: 30, MaxConcurrent: 1})

	if _, err := a.Play(ctx, api.Principal{ReaderChatID: 111}, &sushiv1.PlayReq{FileId: 1}); err != nil {
		t.Fatalf("reader 111: %v", err)
	}
	if _, err := a.Play(ctx, api.Principal{ReaderChatID: 222}, &sushiv1.PlayReq{FileId: 1}); err != nil {
		t.Errorf("reader 222 was blocked by reader 111's cap: %v", err)
	}
}

func TestPlaySurfacesStoreFailures(t *testing.T) {
	t.Parallel()

	want := errors.New("database is down")
	_, err := playAPI(&fakeDeliveries{err: want}, api.RateLimits{PerHour: 30, MaxConcurrent: 2}).
		Play(context.Background(), api.Principal{ReaderChatID: 111}, &sushiv1.PlayReq{FileId: 1})
	if !errors.Is(err, want) {
		t.Errorf("err = %v, want it to wrap %v", err, want)
	}
}

func TestAckRecordsTheMessageIdAgainstTheReaderChat(t *testing.T) {
	t.Parallel()

	d := &fakeDeliveries{}
	_, err := playAPI(d, api.RateLimits{PerHour: 30, MaxConcurrent: 2}).
		Ack(context.Background(), api.Principal{ReaderChatID: 111}, &sushiv1.AckReq{FileId: 4211, MessageId: 99000})
	if err != nil {
		t.Fatalf("Ack: %v", err)
	}
	if len(d.acked) != 1 {
		t.Fatalf("got %d ack calls, want 1", len(d.acked))
	}
	got := d.acked[0]
	if got.readerChatID != 111 || got.fileID != 4211 || got.messageID != 99000 {
		t.Errorf("acked %+v, want reader 111 file 4211 message 99000", got)
	}
}
