// Command init-bot is the machine half of login (ADR 0004): "who am I, which API bot is mine?"
// Bot-login enrolment lives on main-bot (ADR 0013). A token here is deleted and pointed away.
// init-bot also posts sealed login blobs to the public preview channel.
//
// Composition only (R-GO-3).
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/Aryan-mor/sushi-study/be/internal/adapter/pg"
	"github.com/Aryan-mor/sushi-study/be/internal/adapter/tg"
	transport "github.com/Aryan-mor/sushi-study/be/internal/adapter/transport/tg"
	"github.com/Aryan-mor/sushi-study/be/internal/app/botlogin"
	"github.com/Aryan-mor/sushi-study/be/internal/app/enroll"
	"github.com/Aryan-mor/sushi-study/be/internal/app/identity"
	"github.com/Aryan-mor/sushi-study/be/internal/app/loginpost"
	"github.com/Aryan-mor/sushi-study/be/internal/app/onboarding"
	"github.com/Aryan-mor/sushi-study/be/internal/platform/config"
)

func main() {
	log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))

	if err := run(log); err != nil && !errors.Is(err, context.Canceled) {
		log.Error("init-bot stopped", "err", err)
		os.Exit(1)
	}
	log.Info("init-bot stopped")
}

func run(log *slog.Logger) error {
	cfg, err := config.LoadInitBot(os.Getenv("SUSHI_ENV_FILE"))
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
	if err = store.RequireIdentitySchema(ctx); err != nil {
		return err
	}
	client := tg.New(cfg.Token)

	me, err := client.Me(ctx)
	if err != nil {
		return fmt.Errorf("getMe: %w", err)
	}
	if err := store.RegisterInitBot(ctx, me.Username); err != nil {
		return err
	}
	if err := client.DeleteWebhook(ctx); err != nil {
		return fmt.Errorf("deleteWebhook: %w", err)
	}

	log.Info("starting", "env", cfg.Env, "bot", me.Username, "access", cfg.Access.Mode)

	identitySvc := identity.New(store)
	if cfg.Access.Closed() {
		identitySvc = identitySvc.WithClosedRegistration(store, cfg.Access.StaffIDs)
	}
	dispatcher := transport.NewInitDispatcher(identitySvc, cfg.DeliveryBots)
	resolver := transport.BoundResolver{Identity: identitySvc}
	handler := transport.NewHandler(dispatcher, client, resolver, cfg.MainBotUsername, log).WithAccess(identitySvc)

	b := &bot{
		client:     client,
		handler:    handler,
		onboarding: onboarding.New(store),
		identity:   identitySvc,
		log:        log,
	}

	g, ctx := errgroup.WithContext(ctx)
	g.Go(func() error { return b.poll(ctx) })
	g.Go(func() error {
		return loginpost.Run(ctx, store, tgChannel{c: client}, cfg.LoginChannelID, log)
	})
	return g.Wait()
}

type bot struct {
	client     *tg.Client
	handler    *transport.Handler
	onboarding *onboarding.Service
	identity   *identity.Service
	log        *slog.Logger
	offset     int64
}

func (b *bot) poll(ctx context.Context) error {
	b.log.Info("polling", "bot", b.client.Username())

	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		updates, err := b.client.GetUpdates(ctx, b.offset, transport.PollTimeout)
		if err != nil {
			if stop := b.backOff(ctx, err); stop != nil {
				return stop
			}
			continue
		}

		b.dispatch(ctx, updates)
	}
}

func (b *bot) dispatch(ctx context.Context, updates []tg.Update) {
	for _, u := range updates {
		if u.ID >= b.offset {
			b.offset = u.ID + 1
		}
		if u.Message != nil {
			b.handle(ctx, u.Message)
		}
	}
}

func (b *bot) backOff(ctx context.Context, cause error) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	b.log.Warn("getUpdates failed", "err", cause)

	select {
	case <-time.After(3 * time.Second):
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// handle routes one message: a pasted bot token is bot-login enrolment (docs/10 Q3, the one
// prose exception this bot makes), everything else is the ordinary protocol path (/start,
// /initbot), which Handler already knows how to serve.
func (b *bot) handle(ctx context.Context, msg *tg.Message) {
	if msg.From == nil {
		return
	}
	if botlogin.IsPing(msg.Text) {
		if err := b.client.DeleteMessage(ctx, msg.Chat.ID, msg.ID); err != nil {
			b.log.Debug("delete b2b ping", "err", err)
		}
		return
	}
	if enroll.LooksLikeToken(msg.Text) {
		log := b.log.With("chat", msg.Chat.ID)
		if err := b.client.DeleteMessage(ctx, msg.Chat.ID, msg.ID); err != nil {
			log.Warn("could not delete a pasted token", "err", err)
		}
		b.send(ctx, msg.Chat.ID, tokenWrongChat.pick(b.localeFor(ctx, msg)), log)
		return
	}
	b.handler.Handle(ctx, msg)
}

// localeFor reads the person's onboarding answer if there is one, falling back to what Telegram
// says about them -- the same fallback main-bot uses before it has ever asked anything.
func (b *bot) localeFor(ctx context.Context, msg *tg.Message) string {
	hint := localeHint(msg.From)

	user, err := b.identity.EnsureUser(ctx, msg.From.ID, msg.From.Name, msg.From.Username)
	if err != nil || user.Deleted || user.ID == 0 {
		return hint
	}
	prefs, err := b.onboarding.Prefs(ctx, user.ID)
	if err != nil || prefs.Locale == "" {
		return hint
	}
	return prefs.Locale
}

func localeHint(from *tg.User) string {
	if from != nil && strings.HasPrefix(from.Language, "fa") {
		return "fa"
	}
	return "en"
}

func (b *bot) send(ctx context.Context, chatID int64, text string, log *slog.Logger) {
	if _, err := b.client.SendMessage(ctx, chatID, text); err != nil {
		log.Error("send failed", "err", err)
	}
}

type tgChannel struct{ c *tg.Client }

func (c tgChannel) Post(ctx context.Context, chatID int64, text string) (int64, error) {
	msg, err := c.c.SendMessage(ctx, chatID, text)
	if err != nil {
		return 0, err
	}
	return msg.ID, nil
}

func (c tgChannel) Delete(ctx context.Context, chatID, messageID int64) error {
	return c.c.DeleteMessage(ctx, chatID, messageID)
}
