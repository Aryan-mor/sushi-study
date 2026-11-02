package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/Aryan-mor/sushi-study/be/internal/adapter/pg"
	"github.com/Aryan-mor/sushi-study/be/internal/adapter/pglib"
	"github.com/Aryan-mor/sushi-study/be/internal/adapter/tg"
	"github.com/Aryan-mor/sushi-study/be/internal/adapter/tmdb"
	"github.com/Aryan-mor/sushi-study/be/internal/app/identity"
	"github.com/Aryan-mor/sushi-study/be/internal/app/newsroom"
	"github.com/Aryan-mor/sushi-study/be/internal/platform/config"
)

func main() {
	log := newLogger()
	if err := run(log); err != nil && !errors.Is(err, context.Canceled) {
		log.Error("catalog-bot stopped", "err", err)
		os.Exit(1)
	}
	log.Info("catalog-bot stopped")
}

func newLogger() *slog.Logger {
	var w io.Writer = os.Stdout
	if f, err := os.OpenFile(filepath.Join("tmp", "catalog-bot.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644); err == nil {
		w = io.MultiWriter(os.Stdout, f)
	}
	return slog.New(slog.NewTextHandler(w, &slog.HandlerOptions{Level: slog.LevelInfo}))
}

func run(log *slog.Logger) error {
	cfg, err := config.LoadCatalogBot(os.Getenv("SUSHI_ENV_FILE"))
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	pool, err := pg.OpenRequestPath(ctx, cfg.DatabaseURL, 0)
	if err != nil {
		return err
	}
	defer pool.Close()

	store := pg.NewStore(pool)
	var overviews newsroom.OverviewStore
	if cfg.LibraryURL != "" {
		lib, libErr := pg.OpenRequestPath(ctx, cfg.LibraryURL, 0)
		if libErr != nil {
			log.Warn("library unavailable; Persian synopses will not be saved", "err", libErr)
		} else {
			defer lib.Close()
			overviews = pglib.NewOps(lib)
		}
	}
	client := tg.New(cfg.Token)
	me, err := client.Me(ctx)
	if err != nil {
		return fmt.Errorf("getMe: %w", err)
	}
	if err := client.DeleteWebhook(ctx); err != nil {
		return fmt.Errorf("deleteWebhook: %w", err)
	}
	if err := registerCatalogCommands(ctx, client); err != nil {
		return err
	}

	log.Info("starting", "env", cfg.Env, "bot", me.Username, "access", cfg.Access.Mode)
	identitySvc := identity.New(store)
	if cfg.Access.Closed() {
		identitySvc = identitySvc.WithClosedRegistration(store, cfg.Access.StaffIDs)
	}
	b := &bot{
		client:       client,
		store:        store,
		identity:     identitySvc,
		tmdb:         tmdb.New(cfg.TMDBAPIKey),
		mainUser:     cfg.MainBotUsername,
		requestQuota: cfg.RequestsPerDay,
		log:          log,
		translate:    catalogTranslator(ctx, cfg, log),
		overviews:    overviews,
	}
	return b.poll(ctx)
}
