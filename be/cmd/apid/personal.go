package main

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/Aryan-mor/sushi-study/be/internal/adapter/pg"
	"github.com/Aryan-mor/sushi-study/be/internal/adapter/pglib"
	"github.com/Aryan-mor/sushi-study/be/internal/app/api"
	"github.com/Aryan-mor/sushi-study/be/internal/platform/config"
)

// personalActions composes the two databases a personal action touches (ADR 0038): the person's
// Telegram id and the share queue live in the environment; the grant a delete removes lives in the
// library.
type personalActions struct {
	env *pg.Store
	lib *pglib.Personal
}

func (a personalActions) DeletePersonalGrant(ctx context.Context, userID, videoID int64) error {
	tg, err := a.env.TelegramIDForUser(ctx, userID)
	if err != nil {
		return err
	}
	return a.lib.DeleteGrant(ctx, videoID, tg)
}

func (a personalActions) EnqueuePersonalShare(ctx context.Context, userID, videoID int64) error {
	return a.env.EnqueuePersonalShare(ctx, userID, videoID)
}

// wirePersonalActions opens the library connection share/delete need and attaches them. Without
// LIBRARY_DATABASE_URL personal actions answer not found and everything else serves as before. The
// returned close is always safe to call.
func wirePersonalActions(
	ctx context.Context, cfg config.APID, store *pg.Store, service *api.API, log *slog.Logger,
) (func(), error) {
	if cfg.LibraryURL == "" {
		log.Warn("LIBRARY_DATABASE_URL unset: personal video share/delete are off (ADR 0038)")
		return func() {}, nil
	}
	lib, err := pg.OpenRequestPath(ctx, cfg.LibraryURL, 2)
	if err != nil {
		return func() {}, fmt.Errorf("library connect: %w", err)
	}
	service.WithPersonalActions(personalActions{env: store, lib: pglib.NewPersonal(lib)})
	return lib.Close, nil
}
