// Package delivery drains the pending-delivery backlog: claim a batch of rows API.Play enqueued,
// copy each file from Sushi's clone into the reader's chat, record where it landed.
//
// Shaped like internal/app/backup, for the same reason: it runs unattended against an API that
// fails in ways nobody controls, so a single bad job, a single bad bot, or a database blip must
// never stop the rest. Step over the failure, say so, and keep going.
package delivery

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"google.golang.org/protobuf/proto"

	"github.com/Aryan-mor/sushi-study/be/internal/adapter/tg"
	"github.com/Aryan-mor/sushi-study/be/internal/core/catalog"
	sushiv1 "github.com/Aryan-mor/sushi-study/be/internal/protocol/sushi/v1"
	"github.com/Aryan-mor/sushi-study/be/internal/wire"
)

const (
	// batchSize is how many pending rows one claim pulls per bot slot.
	batchSize = 8

	// staleClaimAfter is how long a claim is honoured before another pass may retake it. A worker
	// that crashed mid-copy leaves a stale claimed_at; this is what lets the row be tried again
	// without a heartbeat or a manual unstick.
	staleClaimAfter = 2 * time.Minute

	// idleDelay is the pause after a pass that claimed nothing, doubling up to maxIdleDelay -- the
	// same backoff *shape* as backupd, but not its ceiling. backupd's 5-minute cap is fine for a
	// bulk backlog nobody is watching; here every idle second is a second between somebody pressing
	// play and their copy starting. A live test caught this at 5m0s exactly (docs/09 R-TEST-6 --
	// this is that regression's test). The workload is inherently small (R-SEC-11 caps it at 2
	// concurrent jobs per user), so polling every few seconds forever costs nothing.
	idleDelay    = 2 * time.Second
	maxIdleDelay = 10 * time.Second

	// minRetryAfter floors a 429 that arrived with no retry_after (docs/02 §6): waiting zero would
	// turn "slow down" into a hot loop.
	minRetryAfter = time.Second

	// maxAttempts bounds retries on one job before the pass gives up on it and moves on. The job is
	// not lost -- it is still a pending row, and the next pass claims it again.
	maxAttempts = 5
)

// PendingJob is one claimed delivery row, ready to copy. It is this package's own type, not the
// generated pg row: an app package does not leak an adapter's shape upward.
type PendingJob struct {
	ReaderChatID int64
	// Kind says which id space FileID is from: a catalog file, or a personal video (ADR 0037).
	Kind    catalog.MediaKind
	FileID  int64
	Locator string
}

// Copier is the Telegram calls one delivery bot needs. Copies into the reader DM are
// protect_content (docs/05 §3); clone/backup use the unprotected Client.CopyMessage instead.
type Copier interface {
	CopyProtectedMessage(ctx context.Context, destChat, sourceChat, messageID int64, caption string) (int64, error)

	// CopyProtectedMessageToUsername is the fallback for a reader this bot has never contacted: a
	// bare chat id fails with "chat not found" on the very first message, even with Bot-to-Bot
	// mode on both sides -- @handle resolves on that first message, in either direction
	// (confirmed live 2026-08-26). The API pool is warmed in full (ADR 0011); a delivery bot is
	// picked from the pool per copy, so nothing can warm it in advance -- this is the one thing
	// that must fall back.
	CopyProtectedMessageToUsername(ctx context.Context, username string, sourceChat, messageID int64, caption string) (int64, error)
}

// Store is what deliveryd needs from storage. Declared here, by the consumer, and small (R-GO-5) —
// even though claiming and completing live in the environment database and the clone location
// lives in the library, this package only sees four methods. The concrete type composing both
// databases lives in cmd/deliveryd.
type Store interface {
	ClaimPendingDeliveries(ctx context.Context, botID int32, batchSize int, staleAfter time.Duration) ([]PendingJob, error)
	CloneLocation(ctx context.Context, fileID int64) (channelID, messageID int64, err error)
	// PersonalLocation is where a personal video's bytes are: personal_clone, else personal_backup
	// (ADR 0037).
	PersonalLocation(ctx context.Context, videoID int64) (channelID, messageID int64, err error)
	CompleteDelivery(ctx context.Context, readerChatID int64, kind catalog.MediaKind, fileID int64, botID int32, messageID int64) error

	// ReaderUsername is set only for a bot-login reader (docs/06 §3); ok is false for a session
	// reader, which is always resolvable cold and has nothing to fall back to.
	ReaderUsername(ctx context.Context, readerChatID int64) (username string, ok bool, err error)
}

// Sender is the one Telegram call a Ready push needs. deliveryd borrows a live client per API bot
// purely to send, never to poll, so this never competes with apid's own poller for that bot
// (R-OPS-3 is about GetUpdates, not this). Pushes round-robin the configured pool (ADR 0012).
type Sender interface {
	SendMessage(ctx context.Context, chatID int64, text string) (tg.Message, error)
}

// activeBotChecker is bot rotation's (docs/02 §7) hook into Store, kept separate from the Store
// interface above rather than folded into it: a Store that does not implement it (most existing
// tests) is treated as "every configured slot is active", so rotation layers on top of the core
// contract instead of forcing every caller to know about it.
type activeBotChecker interface {
	ActiveDeliveryBotIDs(ctx context.Context) (map[int32]bool, error)
}

// healthReporter is the one thing Runner needs from internal/app/health -- declared here, not
// imported as a concrete type, so this package does not need to know about bot rotation when
// nobody wires one in (R-GO-5), matching adapter/transport/tg.Poller's identical seam.
type healthReporter interface {
	Report(ctx context.Context, botID int32, role, username string, err error) bool
}

// Slot is one delivery bot plus the client that speaks for it. BotID is what a claim and a
// completion are recorded under.
type Slot struct {
	BotID    int32
	Username string
	Bot      Copier
}

// Runner drains the backlog across all slots.
type Runner struct {
	store      Store
	slots      []Slot
	pushBots   map[string]Sender
	pushOrder  []string
	pushCursor atomic.Uint64
	health     healthReporter
	log        *slog.Logger

	// sleep is a cancellable pause, replaced in tests so a backoff can be asserted without spending
	// it.
	sleep func(context.Context, time.Duration) error
}

// New builds a runner. pushBots is keyed by API bot username (docs/08 §2.1's usual key) and may be
// nil: the Ready push is a safety net with its own polling fallback (docs/02 §8), never something
// a delivery's correctness depends on, so running without it is a valid, if diminished, config.
// health may also be nil to skip bot-rotation reporting entirely.
func New(store Store, slots []Slot, pushBots map[string]Sender, log *slog.Logger, health healthReporter) *Runner {
	order := make([]string, 0, len(pushBots))
	for name := range pushBots {
		order = append(order, name)
	}
	sort.Strings(order)
	return &Runner{store: store, slots: slots, pushBots: pushBots, pushOrder: order, health: health, log: log, sleep: sleep}
}

// Run drains until ctx ends. If once is true it makes a single claim-and-copy pass per slot and
// returns, reporting jobs it could not deliver.
//
// Slots run independently, not under one errgroup: one dead bot must not stop the others from
// delivering, the same reasoning as backupd's Run.
func (r *Runner) Run(ctx context.Context, once bool) error {
	var wg sync.WaitGroup
	errs := make([]error, len(r.slots))
	for i, slot := range r.slots {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs[i] = r.runSlot(ctx, slot, once)
		}()
	}
	wg.Wait()
	return errors.Join(errs...)
}

func (r *Runner) runSlot(ctx context.Context, slot Slot, once bool) error {
	delay := idleDelay
	for {
		delivered, failed, err := r.pass(ctx, slot)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if once {
			return onceResult(slot, failed, err)
		}
		if err != nil {
			r.log.Error("backlog unreadable", "bot", slot.Username, "err", err)
		}
		if delivered > 0 && err == nil {
			delay = idleDelay
		} else {
			delay = min(delay*2, maxIdleDelay)
		}
		r.log.Info("pass done", "bot", slot.Username, "delivered", delivered, "failed", failed, "next_in", delay)
		if err := r.sleep(ctx, delay); err != nil {
			return err
		}
	}
}

func onceResult(slot Slot, failed int, err error) error {
	if err != nil {
		return fmt.Errorf("%s: %w", slot.Username, err)
	}
	if failed > 0 {
		return fmt.Errorf("%s: %d jobs could not be delivered", slot.Username, failed)
	}
	return nil
}

// pass claims one batch and works through it. A job that fails is logged and stepped over rather
// than retried in place -- it is still pending, so the next pass (this one's or another slot's)
// claims it again once its claim goes stale.
func (r *Runner) pass(ctx context.Context, slot Slot) (delivered, failed int, err error) {
	if !r.slotIsActive(ctx, slot) {
		// Quarantined or retired (docs/02 §7): this slot's pending rows stay claimable by whichever
		// slot picks them up next, exactly like a stale claim would. Reported as an ordinary idle
		// pass so the caller's backoff applies -- no point busy-polling a bot ops has to fix.
		return 0, 0, nil
	}

	jobs, err := r.store.ClaimPendingDeliveries(ctx, slot.BotID, batchSize, staleClaimAfter)
	if err != nil {
		return 0, 0, fmt.Errorf("claim: %w", err)
	}
	for _, job := range jobs {
		switch err := r.deliverOne(ctx, slot, job); {
		case err == nil:
			delivered++
		case ctx.Err() != nil:
			return delivered, failed, ctx.Err()
		default:
			failed++
			r.log.Error("delivery failed, stepping over",
				"reader", job.ReaderChatID, "file", job.FileID, "err", err)
			if r.health != nil {
				r.health.Report(ctx, slot.BotID, "delivery", slot.Username, err)
			}
		}
	}
	return delivered, failed, nil
}

// slotIsActive reports whether slot may be handed new work. A Store that does not implement
// activeBotChecker (most tests) is treated as "always active" -- bot rotation is a layer on top
// of the core Store contract, not part of it.
func (r *Runner) slotIsActive(ctx context.Context, slot Slot) bool {
	checker, ok := r.store.(activeBotChecker)
	if !ok {
		return true
	}
	active, err := checker.ActiveDeliveryBotIDs(ctx)
	if err != nil {
		// Unreadable health state must never stop delivery outright -- fail open, same as every
		// other "step over and keep going" decision in this package.
		return true
	}
	return active[slot.BotID]
}

func (r *Runner) deliverOne(ctx context.Context, slot Slot, job PendingJob) error {
	channelID, messageID, err := r.sourceOf(ctx, job)
	if err != nil {
		return err
	}

	destID, err := r.copyWithRetry(ctx, slot, job.ReaderChatID, channelID, messageID, job.Locator)
	if err != nil {
		return fmt.Errorf("copy file %d from %d/%d to reader %d: %w",
			job.FileID, channelID, messageID, job.ReaderChatID, err)
	}

	if err := r.store.CompleteDelivery(ctx, job.ReaderChatID, job.Kind, job.FileID, slot.BotID, destID); err != nil {
		// The copy landed and the row did not, so the reader now has a message nothing points at,
		// and the next claim will deliver the file again. Naming the orphan is what makes it
		// countable instead of invisible.
		return fmt.Errorf("record delivery of file %d to reader %d (orphan message %d): %w",
			job.FileID, job.ReaderChatID, destID, err)
	}

	r.pushReady(ctx, job, slot.BotID, destID)

	r.log.Info("delivered",
		"reader", job.ReaderChatID, "kind", job.Kind, "file", job.FileID, "bot", slot.Username, "message", destID)
	return nil
}

// sourceOf is where a job's bytes are copied from: the catalog clone for a file, the personal
// channels for a personal video. The two id spaces overlap, so the kind decides, never the id.
func (r *Runner) sourceOf(ctx context.Context, job PendingJob) (channelID, messageID int64, err error) {
	if job.Kind == catalog.MediaPersonal {
		channelID, messageID, err = r.store.PersonalLocation(ctx, job.FileID)
		if err != nil {
			return 0, 0, fmt.Errorf("personal location for video %d: %w", job.FileID, err)
		}
		return channelID, messageID, nil
	}
	channelID, messageID, err = r.store.CloneLocation(ctx, job.FileID)
	if err != nil {
		return 0, 0, fmt.Errorf("clone location for file %d: %w", job.FileID, err)
	}
	return channelID, messageID, nil
}

// pushReady sends the Ready push (docs/02 §8) through the next API bot in the pool (ADR 0012).
// Best-effort, deliberately: a push the client never sees is not a delivery failure -- every push
// has a polling fallback, so a failure here is logged and stepped over like any other job's
// non-fatal quirk, never returned to fail an otherwise-successful delivery.
func (r *Runner) pushReady(ctx context.Context, job PendingJob, botID int32, messageID int64) {
	username, sender, ok := r.nextPushBot()
	if !ok {
		return
	}

	ready := &sushiv1.Ready{BotId: botID, MessageId: messageID}
	if job.Kind == catalog.MediaPersonal {
		ready.PersonalId = job.FileID
	} else {
		ready.FileId = job.FileID
	}
	payload, err := proto.Marshal(ready)
	if err != nil {
		r.log.Warn("ready push: marshal failed", "err", err)
		return
	}
	text, err := wire.EncodeText(wire.Envelope{
		Corr: wire.PushCorr, Type: uint64(sushiv1.MsgType_MSG_TYPE_PUSH_READY), Payload: payload,
	})
	if err != nil {
		r.log.Warn("ready push: encode failed", "err", err)
		return
	}
	if _, err := sender.SendMessage(ctx, job.ReaderChatID, text); err != nil {
		r.log.Warn("ready push: send failed", "reader", job.ReaderChatID, "bot", username, "err", err)
		return
	}
	r.log.Info("ready push sent", "reader", job.ReaderChatID, "bot", username, "file", job.FileID)
}

func (r *Runner) nextPushBot() (string, Sender, bool) {
	n := len(r.pushOrder)
	if n == 0 {
		return "", nil, false
	}
	i := int(r.pushCursor.Add(1)-1) % n
	name := r.pushOrder[i]
	return name, r.pushBots[name], true
}

func (r *Runner) copyWithRetry(ctx context.Context, slot Slot, readerChatID, sourceChat, messageID int64, locator string) (int64, error) {
	for attempt := 1; ; attempt++ {
		// The locator IS the caption here, unlike backupd's empty one: the client verifies it
		// before trusting the copy (docs/05 §3), and it replaces whatever caption fetcherd stamped
		// on the clone copy.
		destID, err := slot.Bot.CopyProtectedMessage(ctx, readerChatID, sourceChat, messageID, locator)
		if err == nil {
			return destID, nil
		}

		var te *tg.Error
		if !errors.As(err, &te) {
			return 0, err
		}
		if te.IsChatNotFound() {
			if destID, attempted, ferr := r.tryByUsername(ctx, slot, readerChatID, sourceChat, messageID, locator); attempted {
				return destID, ferr
			}
			// No username on file: for a session reader this is a real error, not a peer gap.
			return 0, err
		}
		if !te.IsRateLimit() {
			return 0, err
		}
		if err := r.waitOutRateLimit(ctx, slot, te, attempt); err != nil {
			return 0, err
		}
	}
}

// waitOutRateLimit honours a 429's retry_after (floored, docs/02 §6), or gives up after
// maxAttempts -- the job is not lost, it is still pending and the next pass claims it again.
func (r *Runner) waitOutRateLimit(ctx context.Context, slot Slot, te *tg.Error, attempt int) error {
	if attempt >= maxAttempts {
		return fmt.Errorf("rate limited %d times in a row: %w", attempt, te)
	}
	wait := max(te.RetryAfter, minRetryAfter)
	r.log.Warn("rate limited", "bot", slot.Username, "wait", wait, "attempt", attempt)
	return r.sleep(ctx, wait)
}

// tryByUsername is the first-contact fallback (see Copier.CopyProtectedMessageToUsername). attempted is
// false only when there is no username on file at all -- a session reader, where the original
// "chat not found" is a genuine error and must not be swallowed.
func (r *Runner) tryByUsername(
	ctx context.Context, slot Slot, readerChatID, sourceChat, messageID int64, locator string,
) (destID int64, attempted bool, err error) {
	username, found, err := r.store.ReaderUsername(ctx, readerChatID)
	if err != nil {
		return 0, true, fmt.Errorf("reader username for %d: %w", readerChatID, err)
	}
	if !found {
		return 0, false, nil
	}

	destID, err = slot.Bot.CopyProtectedMessageToUsername(ctx, username, sourceChat, messageID, locator)
	if err != nil {
		return 0, true, fmt.Errorf("copy to @%s (reader %d): %w", username, readerChatID, err)
	}
	r.log.Info("resolved reader by username on first contact",
		"reader", readerChatID, "username", username, "bot", slot.Username)
	return destID, true, nil
}

// sleep waits, or gives up early when the process is shutting down.
func sleep(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}
