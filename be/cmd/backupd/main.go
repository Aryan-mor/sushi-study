// Command backupd copies from Sushi's clone into each backup channel. Independent of fetcherd.
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

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Aryan-mor/sushi-study/be/internal/adapter/pglib"
	"github.com/Aryan-mor/sushi-study/be/internal/adapter/tg"
	"github.com/Aryan-mor/sushi-study/be/internal/app/backup"
	"github.com/Aryan-mor/sushi-study/be/internal/platform/config"
)

func main() {
	once := flag.Bool("once", false, "drain the backlog and exit")
	flag.Parse()

	log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	if err := run(log, *once); err != nil && !errors.Is(err, context.Canceled) {
		log.Error("backupd stopped", "err", err)
		os.Exit(1)
	}
}

func run(log *slog.Logger, once bool) error {
	cfg, err := config.LoadBackup(os.Getenv("SUSHI_ENV_FILE"))
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	lib, err := pgxpool.New(ctx, cfg.LibraryURL)
	if err != nil {
		return fmt.Errorf("library connect: %w", err)
	}
	defer lib.Close()

	ping, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := lib.Ping(ping); err != nil {
		return fmt.Errorf("library ping: %w", err)
	}

	slots := make([]backup.Slot, 0, len(cfg.Slots))
	for _, s := range cfg.Slots {
		bot := tg.New(s.Token)
		me, err := bot.Me(ctx)
		if err != nil {
			return fmt.Errorf("getMe %s: %w", s.Username, err)
		}
		log.Info("slot", "configured", s.Username, "telegram", me.Username, "role", s.Role, "channel", s.ChannelID)
		slots = append(slots, backup.Slot{BackupSlot: s, Bot: bot})
	}

	queries := pglib.New(lib)
	runner := backup.New(queries, slots, log)
	if p := cfg.PersonalBackup; p != nil {
		// The backup1 bot again, pointed at personal_backup (ADR 0037 §2). Same token, same client.
		log.Info("personal slot", "configured", p.Username, "channel", p.ChannelID)
		runner.WithPersonal(queries, backup.Slot{BackupSlot: *p, Bot: tg.New(p.Token)})
	}

	log.Info("starting", "env", cfg.Env, "clone", cfg.CloneID, "slots", len(slots),
		"personal", cfg.PersonalBackup != nil, "once", once)
	return runner.Run(ctx, once)
}
