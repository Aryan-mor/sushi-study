// Package backup copies files from Sushi's clone channel into each backup channel.
//
// It never talks to the prototype or to fetcherd. A clone file_locations row is the whole trigger
// (ADR 0006).
//
// The shape of this package is dictated by one requirement: it runs unattended for a day, over
// ~32 000 files, against an API that fails in ways nobody controls. So nothing here treats a single
// bad row, a single bad bot, or a brief database outage as a reason to stop backing up. What it
// does instead is step over the failure, say so, and keep going.
package backup

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/Aryan-mor/sushi-study/be/internal/adapter/pglib"
	"github.com/Aryan-mor/sushi-study/be/internal/adapter/tg"
	"github.com/Aryan-mor/sushi-study/be/internal/platform/config"
)

const (
	// pageSize is how many clone rows one query pulls.
	pageSize = 16

	// idleDelay is the pause after a pass that copied nothing. Doubling from here up to
	// maxIdleDelay is what keeps a role whose backlog is entirely broken -- a bot kicked from its
	// channel, say -- from re-attempting the same rows every two seconds until somebody notices.
	idleDelay    = 2 * time.Second
	maxIdleDelay = 5 * time.Minute

	// minRetryAfter is the floor under Telegram's retry_after. docs/02 §6 says honour what Telegram
	// asks for rather than guessing a backoff, and this does -- but a 429 is allowed to arrive with
	// no retry_after at all, and waiting zero seconds turns "slow down" into a hot loop against the
	// API, which is the opposite of what was asked.
	minRetryAfter = time.Second

	// maxAttempts bounds how many times one row may be re-sent after a 429 before the pass gives up
	// on it and moves on. The row is not lost: it has no backup location yet, so the next pass
	// finds it again.
	maxAttempts = 5
)

// Copier is the Telegram call one backup bot needs.
type Copier interface {
	CopyMessage(ctx context.Context, destChat, sourceChat, messageID int64, caption string) (int64, error)
}

// Store is what a backup run needs from the library. Declared by the consumer and kept small
// (R-GO-5); it is also the seam the tests drive a failing copy through, without a database.
type Store interface {
	CloneLocationsMissingRole(ctx context.Context, arg pglib.CloneLocationsMissingRoleParams) ([]pglib.CloneLocationsMissingRoleRow, error)
	SetFileLocation(ctx context.Context, arg pglib.SetFileLocationParams) error
}

// PersonalStore is the personal-video half (ADR 0037): personal_clone copies with no
// personal_backup yet, and where to record one.
type PersonalStore interface {
	PersonalCloneMissingBackup(ctx context.Context, arg pglib.PersonalCloneMissingBackupParams) ([]pglib.PersonalCloneMissingBackupRow, error)
	SetPersonalLocation(ctx context.Context, arg pglib.SetPersonalLocationParams) error
}

// Slot is one backup bot plus the client that speaks for it.
type Slot struct {
	config.BackupSlot
	Bot Copier
}

// Runner fills backup channels from clone coordinates already in the library.
type Runner struct {
	lib   Store
	slots []Slot
	log   *slog.Logger

	// personal fills personal_backup from personal_clone (ADR 0037). Nil when not configured.
	personal     PersonalStore
	personalSlot Slot

	// sleep is a cancellable pause, replaced in tests so a backoff can be asserted without
	// spending it.
	sleep func(context.Context, time.Duration) error
}

// New builds a runner.
//
// There is deliberately no clone channel id here: the channel to copy *from* is the one each clone
// row records, not the one CHANNELS happens to name today.
func New(lib Store, slots []Slot, log *slog.Logger) *Runner {
	return &Runner{lib: lib, slots: slots, log: log, sleep: sleep}
}

// WithPersonal adds the personal_clone -> personal_backup copy (ADR 0037), run like one more slot:
// concurrent with the catalog slots and independent of them.
func (r *Runner) WithPersonal(store PersonalStore, slot Slot) *Runner {
	r.personal = store
	r.personalSlot = slot
	return r
}

// Run copies until ctx ends. If once is true it makes a single pass over each slot's backlog and
// returns, reporting rows it could not copy -- a drain that silently skipped half the catalog is
// not a drain.
//
// Slots run concurrently and independently. This is not an errgroup on purpose: errgroup cancels
// its context on the first error, so one revoked token would stop the other backup channels too,
// and each backup exists precisely so that the loss of one copy is survivable.
func (r *Runner) Run(ctx context.Context, once bool) error {
	var wg sync.WaitGroup
	errs := make([]error, len(r.slots)+1)
	for i, slot := range r.slots {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs[i] = r.runSlot(ctx, slot, once, r.pass)
		}()
	}
	if r.personal != nil {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs[len(r.slots)] = r.runSlot(ctx, r.personalSlot, once, r.personalPass)
		}()
	}
	wg.Wait()
	return errors.Join(errs...)
}

type passFunc func(ctx context.Context, slot Slot) (copied, failed int, err error)

func (r *Runner) runSlot(ctx context.Context, slot Slot, once bool, pass passFunc) error {
	delay := idleDelay
	for {
		copied, failed, err := pass(ctx, slot)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if once {
			return onceResult(slot, failed, err)
		}
		if err != nil {
			// A database that is briefly away is not a reason to stop backing up. Back off and
			// start the pass again from the top.
			r.log.Error("backlog unreadable", "role", slot.Role, "err", err)
		}
		if copied > 0 && err == nil {
			delay = idleDelay
		} else {
			delay = min(delay*2, maxIdleDelay)
		}
		r.log.Info("pass done", "role", slot.Role, "copied", copied, "failed", failed, "next_in", delay)
		if err := r.sleep(ctx, delay); err != nil {
			return err
		}
	}
}

func onceResult(slot Slot, failed int, err error) error {
	if err != nil {
		return fmt.Errorf("%s: %w", slot.Role, err)
	}
	if failed > 0 {
		return fmt.Errorf("%s: %d rows could not be copied", slot.Role, failed)
	}
	return nil
}

// pass walks this role's backlog once, from the lowest file id upward.
//
// A row that cannot be copied is logged and stepped over rather than retried in place. The query
// returns clone rows *missing this role*, so a row that fails permanently would otherwise come
// back at the head of every page and stop the role for good -- which is exactly what it used to
// do, and it took the other slots down with it.
func (r *Runner) pass(ctx context.Context, slot Slot) (copied, failed int, err error) {
	after := int64(0)
	for {
		page, err := r.lib.CloneLocationsMissingRole(ctx, pglib.CloneLocationsMissingRoleParams{
			Role:        slot.Role,
			AfterFileID: after,
			PageSize:    pageSize,
		})
		if err != nil {
			return copied, failed, fmt.Errorf("list missing %s after %d: %w", slot.Role, after, err)
		}
		if len(page) == 0 {
			return copied, failed, nil
		}
		for _, row := range page {
			after = row.FileID
			switch err := r.copyOne(ctx, slot, row); {
			case err == nil:
				copied++
			case ctx.Err() != nil:
				return copied, failed, ctx.Err()
			default:
				failed++
				r.log.Error("backup failed, stepping over", "file", row.FileID, "role", slot.Role, "err", err)
			}
		}
	}
}

func (r *Runner) copyOne(ctx context.Context, slot Slot, row pglib.CloneLocationsMissingRoleRow) error {
	// The source is the channel the clone row records, not the one CHANNELS names today. That row
	// is where the message actually is, and the two stop agreeing the moment a clone is replaced.
	destID, err := r.copyWithRetry(ctx, slot, row.ChannelID, row.MessageID)
	if err != nil {
		return fmt.Errorf("copy file %d from %d/%d to %s: %w", row.FileID, row.ChannelID, row.MessageID, slot.Role, err)
	}
	if err := r.lib.SetFileLocation(ctx, pglib.SetFileLocationParams{
		FileID:    row.FileID,
		Role:      slot.Role,
		ChannelID: slot.ChannelID,
		MessageID: destID,
	}); err != nil {
		// The copy landed and the row did not, so there is now a message in the backup channel
		// with nothing pointing at it, and the next pass will copy the file again. Naming the
		// orphan here is what makes it countable instead of invisible.
		return fmt.Errorf("record %s for file %d (orphan message %d in %d): %w", slot.Role, row.FileID, destID, slot.ChannelID, err)
	}
	r.log.Info(
		"backed up",
		"file", row.FileID,
		"role", slot.Role,
		"bot", slot.Username,
		"clone_msg", row.MessageID,
		"backup_msg", destID,
	)
	return nil
}

// personalPass is pass for personal videos: personal_clone rows with no personal_backup, from the
// lowest video id upward, stepping over what fails.
func (r *Runner) personalPass(ctx context.Context, slot Slot) (copied, failed int, err error) {
	after := int64(0)
	for {
		page, err := r.personal.PersonalCloneMissingBackup(ctx, pglib.PersonalCloneMissingBackupParams{
			AfterVideoID: after,
			PageSize:     pageSize,
		})
		if err != nil {
			return copied, failed, fmt.Errorf("list personal missing backup after %d: %w", after, err)
		}
		if len(page) == 0 {
			return copied, failed, nil
		}
		for _, row := range page {
			after = row.VideoID
			switch err := r.copyPersonal(ctx, slot, row); {
			case err == nil:
				copied++
			case ctx.Err() != nil:
				return copied, failed, ctx.Err()
			default:
				failed++
				r.log.Error("personal backup failed, stepping over", "video", row.VideoID, "err", err)
			}
		}
	}
}

func (r *Runner) copyPersonal(ctx context.Context, slot Slot, row pglib.PersonalCloneMissingBackupRow) error {
	destID, err := r.copyWithRetry(ctx, slot, row.ChannelID, row.MessageID)
	if err != nil {
		return fmt.Errorf("copy personal video %d from %d/%d: %w", row.VideoID, row.ChannelID, row.MessageID, err)
	}
	if err := r.personal.SetPersonalLocation(ctx, pglib.SetPersonalLocationParams{
		VideoID:   row.VideoID,
		Role:      slot.Role,
		ChannelID: slot.ChannelID,
		MessageID: destID,
	}); err != nil {
		return fmt.Errorf("record personal backup for video %d (orphan message %d in %d): %w",
			row.VideoID, destID, slot.ChannelID, err)
	}
	r.log.Info("personal backed up", "video", row.VideoID, "bot", slot.Username, "backup_msg", destID)
	return nil
}

func (r *Runner) copyWithRetry(ctx context.Context, slot Slot, sourceChat, messageID int64) (int64, error) {
	for attempt := 1; ; attempt++ {
		// The empty caption is deliberate: given none, Telegram keeps the original, so the storage
		// caption fetcherd stamped on the clone rides along into the backup (docs/05 §3).
		destID, err := slot.Bot.CopyMessage(ctx, slot.ChannelID, sourceChat, messageID, "")
		if err == nil {
			return destID, nil
		}
		var te *tg.Error
		if !errors.As(err, &te) || !te.IsRateLimit() {
			return 0, err
		}
		if attempt >= maxAttempts {
			return 0, fmt.Errorf("rate limited %d times in a row: %w", attempt, err)
		}
		wait := max(te.RetryAfter, minRetryAfter)
		r.log.Warn("rate limited", "role", slot.Role, "wait", wait, "attempt", attempt)
		if err := r.sleep(ctx, wait); err != nil {
			return 0, err
		}
	}
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

// Compile-time check that the generated queries match the port.
var (
	_ Store         = (*pglib.Queries)(nil)
	_ PersonalStore = (*pglib.Queries)(nil)
)
