// Command deliveryd drains the pending-delivery backlog API.Play writes: it copies a file from
// Sushi's clone channel into the reader's chat and records where it landed. Independent of apid —
// a stalled copyMessage must never delay a /home the same way a stalled backup must never delay
// the next clone copy (ADR 0006).
//
// Composition only (R-GO-3).
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/Aryan-mor/sushi-study/be/internal/adapter/pg"
	"github.com/Aryan-mor/sushi-study/be/internal/adapter/pglib"
	"github.com/Aryan-mor/sushi-study/be/internal/adapter/tg"
	"github.com/Aryan-mor/sushi-study/be/internal/app/delivery"
	"github.com/Aryan-mor/sushi-study/be/internal/app/health"
	"github.com/Aryan-mor/sushi-study/be/internal/app/opsnotify"
	"github.com/Aryan-mor/sushi-study/be/internal/core/catalog"
	"github.com/Aryan-mor/sushi-study/be/internal/platform/botpool"
	"github.com/Aryan-mor/sushi-study/be/internal/platform/config"
)

func main() {
	once := flag.Bool("once", false, "drain the backlog and exit")
	flag.Parse()

	log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	if err := run(log, *once); err != nil && !errors.Is(err, context.Canceled) {
		log.Error("deliveryd stopped", "err", err)
		os.Exit(1)
	}
}

func run(log *slog.Logger, once bool) error {
	cfg, err := config.LoadDelivery(os.Getenv("SUSHI_ENV_FILE"))
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	env, err := pg.OpenRequestPath(ctx, cfg.DatabaseURL, 0)
	if err != nil {
		return fmt.Errorf("environment: %w", err)
	}
	defer env.Close()

	lib, err := pg.OpenRequestPath(ctx, cfg.LibraryURL, 0)
	if err != nil {
		return fmt.Errorf("library: %w", err)
	}
	defer lib.Close()

	store := storeAdapter{env: pg.NewStore(env), lib: pglib.New(lib)}

	slots := make([]delivery.Slot, 0, cfg.Bots.Len())
	for _, b := range cfg.Bots.Bots() {
		bot := tg.New(b.Token)
		me, meErr := bot.Me(ctx)
		if meErr != nil {
			return fmt.Errorf("getMe %s: %w", b.Username, meErr)
		}
		botID, regErr := store.env.RegisterDeliveryBot(ctx, me.Username)
		if regErr != nil {
			return fmt.Errorf("register %s: %w", me.Username, regErr)
		}
		log.Info("slot", "configured", b.Username, "telegram", me.Username, "bot_id", botID)
		slots = append(slots, delivery.Slot{BotID: botID, Username: me.Username, Bot: bot})
	}

	pushBots, err := readySenders(ctx, cfg.APIBots)
	if err != nil {
		return err
	}
	if len(pushBots) == 0 {
		log.Warn("no API_BOTS configured; deliveries will not send the Ready push (docs/02 §8) -- " +
			"every push has a polling fallback, so this is degraded, not broken")
	}

	notify := cfg.OpsNotify
	var reporter *health.Reporter
	if notify.BotToken != "" && len(notify.AdminIDs) > 0 {
		notifier := opsnotify.New(tg.New(notify.BotToken), notify.AdminIDs, log)
		reporter = health.New(store.env, notifier, log)
	} else {
		reporter = health.New(store.env, nil, log)
	}

	log.Info("starting", "env", cfg.Env, "slots", len(slots), "push_bots", len(pushBots),
		"superadmins", len(notify.AdminIDs), "once", once)
	return delivery.New(store, slots, pushBots, log, reporter).Run(ctx, once)
}

// readySenders builds one client per API bot, purely to send the Ready push through -- never to
// poll, so this never competes with apid's own poller for the same bot (R-OPS-3 is about
// GetUpdates, not this). pool may be nil: API_BOTS is optional for deliveryd (config.Delivery).
func readySenders(ctx context.Context, pool *botpool.Pool) (map[string]delivery.Sender, error) {
	if pool == nil {
		return nil, nil
	}
	out := make(map[string]delivery.Sender, pool.Len())
	for _, b := range pool.Bots() {
		client := tg.New(b.Token)
		me, err := client.Me(ctx)
		if err != nil {
			return nil, fmt.Errorf("getMe %s: %w", b.Username, err)
		}
		out[me.Username] = client
	}
	return out, nil
}

// storeAdapter composes the environment and library databases into the one small interface
// deliveryd needs (internal/app/delivery.Store): a job's claim and completion live in the
// environment; where its bytes actually are lives in the library.
type storeAdapter struct {
	env *pg.Store
	lib *pglib.Queries
}

func (s storeAdapter) ClaimPendingDeliveries(
	ctx context.Context, botID int32, batchSize int, staleAfter time.Duration,
) ([]delivery.PendingJob, error) {
	return s.env.ClaimPendingDeliveries(ctx, botID, batchSize, staleAfter)
}

func (s storeAdapter) CloneLocation(ctx context.Context, fileID int64) (channelID, messageID int64, err error) {
	row, err := s.lib.CloneLocationForFile(ctx, fileID)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, 0, fmt.Errorf("file %d has no clone location yet", fileID)
	}
	if err != nil {
		return 0, 0, fmt.Errorf("clone location for file %d: %w", fileID, err)
	}
	return row.ChannelID, row.MessageID, nil
}

func (s storeAdapter) PersonalLocation(ctx context.Context, videoID int64) (channelID, messageID int64, err error) {
	row, err := s.lib.PersonalLocationForVideo(ctx, videoID)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, 0, fmt.Errorf("personal video %d has no location yet", videoID)
	}
	if err != nil {
		return 0, 0, fmt.Errorf("personal location for video %d: %w", videoID, err)
	}
	return row.ChannelID, row.MessageID, nil
}

func (s storeAdapter) CompleteDelivery(
	ctx context.Context, readerChatID int64, kind catalog.MediaKind, fileID int64, botID int32, messageID int64,
) error {
	return s.env.CompleteDelivery(ctx, readerChatID, kind, fileID, botID, messageID)
}

func (s storeAdapter) ReaderUsername(ctx context.Context, readerChatID int64) (string, bool, error) {
	return s.env.ReaderUsername(ctx, readerChatID)
}

// ActiveDeliveryBotIDs satisfies delivery's optional activeBotChecker seam (docs/02 §7). Lifts any
// expired quarantine first, right before the read it protects, rather than via a background
// sweeper.
func (s storeAdapter) ActiveDeliveryBotIDs(ctx context.Context) (map[int32]bool, error) {
	if err := s.env.ReactivateExpiredQuarantines(ctx); err != nil {
		return nil, err
	}
	return s.env.ActiveDeliveryBotIDs(ctx)
}
