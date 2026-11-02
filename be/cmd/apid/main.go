// Command apid serves the bot protocol: it runs a shard of the API-bot pool and answers requests.
//
// Composition only — load config, build dependencies, run, shut down (R-GO-3).
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"sync/atomic"
	"syscall"

	"golang.org/x/sync/errgroup"

	"github.com/Aryan-mor/sushi-study/be/internal/adapter/pg"
	"github.com/Aryan-mor/sushi-study/be/internal/adapter/subdl"
	"github.com/Aryan-mor/sushi-study/be/internal/adapter/tg"
	"github.com/Aryan-mor/sushi-study/be/internal/adapter/tmdb"
	transport "github.com/Aryan-mor/sushi-study/be/internal/adapter/transport/tg"
	"github.com/Aryan-mor/sushi-study/be/internal/app/api"
	"github.com/Aryan-mor/sushi-study/be/internal/app/deliverywatch"
	"github.com/Aryan-mor/sushi-study/be/internal/app/health"
	"github.com/Aryan-mor/sushi-study/be/internal/app/identity"
	"github.com/Aryan-mor/sushi-study/be/internal/app/janitor"
	"github.com/Aryan-mor/sushi-study/be/internal/app/opsnotify"
	"github.com/Aryan-mor/sushi-study/be/internal/app/rails"
	"github.com/Aryan-mor/sushi-study/be/internal/platform/config"
	"github.com/Aryan-mor/sushi-study/be/internal/platform/secretbox"
)

func main() {
	log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))

	if err := run(log); err != nil && !errors.Is(err, context.Canceled) {
		log.Error("apid stopped", "err", err)
		os.Exit(1)
	}
	log.Info("apid stopped")
}

func run(log *slog.Logger) error {
	cfg, err := config.LoadAPID(os.Getenv("SUSHI_ENV_FILE"))
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// Request path: statement/lock/idle timeouts come with the connection (pg.OpenRequestPath), and
	// a small, bounded slice of the cluster's connections (docs/06 §7).
	pool, err := pg.OpenRequestPath(ctx, cfg.DatabaseURL, 8)
	if err != nil {
		return err
	}
	defer pool.Close()

	store := pg.NewStore(pool)
	if err = store.RequireIdentitySchema(ctx); err != nil {
		return err
	}
	secrets, err := secretbox.New(cfg.SecretKey)
	if err != nil {
		return fmt.Errorf("secret key: %w", err)
	}
	store.WithSecrets(secrets)
	if cfg.SecretKey == secretbox.DevKey {
		log.Warn("SUSHI_SECRET_KEY is unset: stored Gemini API keys are sealed with the " +
			"published dev key and are not protected")
	}
	resolver := principalResolver(cfg, store)
	service := newAPIService(cfg, store)
	closeLib, err := wirePersonalActions(ctx, cfg, store, service, log)
	if err != nil {
		return err
	}
	defer closeLib()
	trending := wireTMDB(cfg, service)
	if err = wireSubtitles(ctx, cfg, store, service); err != nil {
		return err
	}

	notify := cfg.OpsNotify
	reporter, notifier := newReporter(notify, store, log)

	log.Info("starting",
		"env", cfg.Env,
		"bots", cfg.APIBots.Usernames(),
		"unbound_chats_allowed", cfg.AllowUnbound,
		"superadmins", len(notify.AdminIDs),
		"tmdb", cfg.TMDBKey != "",
		"subdl", len(cfg.SubDLKeys))

	group, ctx := errgroup.WithContext(ctx)
	startBackground(ctx, group, store, trending, notifier, log)
	if err := startAPIBots(ctx, group, cfg, store, service, resolver, reporter, log); err != nil {
		return err
	}
	return group.Wait()
}

func newAPIService(cfg config.APID, store *pg.Store) *api.API {
	return api.New(store, store, store, store, api.RateLimits{
		PerHour:        cfg.DeliveriesPerHour,
		MaxConcurrent:  cfg.MaxConcurrentDeliveries,
		SearchPerHour:  cfg.SearchPerHour,
		RequestsPerDay: cfg.RequestsPerDay,
	}).WithBrowse(store).WithRequests(store).WithGeminiKeys(store).WithAppReleases(store).WithPlaybackPrefs(store).WithDevices(store).WithTaste(store).
		WithPersonal(store)
}

func wireTMDB(cfg config.APID, service *api.API) rails.TrendingSource {
	if cfg.TMDBKey == "" {
		return nil
	}
	tm := tmdb.New(cfg.TMDBKey)
	service.WithHydrate(tm, tm).WithTitleSearch(tm).WithPersonSearch(tmdbPeople{c: tm}).WithTasteSeeds(tm)
	return tm
}

func wireSubtitles(ctx context.Context, cfg config.APID, store *pg.Store, service *api.API) error {
	var subtitleProvider api.SubtitleProvider
	if len(cfg.SubDLKeys) > 0 {
		subtitleProvider = subdlProvider{subdl.New(cfg.SubDLKeys)}
	}
	if cfg.SubtitleChannelID == 0 || cfg.WorkerToken == "" || cfg.DeliveryBots == nil || cfg.DeliveryBots.Len() == 0 {
		return nil
	}
	deliverer, err := newSubtitleDeliverer(ctx, cfg)
	if err != nil {
		return fmt.Errorf("subtitle delivery bots: %w", err)
	}
	uploader := subtitleUploader{client: tg.New(cfg.WorkerToken), channelID: cfg.SubtitleChannelID}
	service.WithSubtitles(subtitleProvider, store, uploader, deliverer)
	return nil
}

// subdlProvider bridges *subdl.Client (its own Pack type) to api.SubtitleProvider (api's own
// SubtitlePack) -- the composition root's job, so neither package imports the other (R-GO-5), the
// same reason tmdb.Client satisfies TitleHydrator/TitleSearcher via the shared catalog.Row instead.
type subdlProvider struct{ c *subdl.Client }

func (p subdlProvider) Search(ctx context.Context, tmdbID uint64, isSeries bool, season, episode int, lang string) ([]api.SubtitlePack, error) {
	packs, err := p.c.Search(ctx, tmdbID, isSeries, season, episode, lang)
	if err != nil {
		return nil, err
	}
	out := make([]api.SubtitlePack, len(packs))
	for i, pk := range packs {
		out[i] = api.SubtitlePack{
			Tag:         pk.Tag,
			ReleaseName: pk.ReleaseName,
			Releases:    pk.Releases,
			Lang:        pk.Lang,
			Translator:  pk.Translator,
			HI:          pk.HI,
		}
	}
	return out, nil
}

func (p subdlProvider) FetchPack(ctx context.Context, tag string) ([]api.PackedSubtitle, error) {
	files, err := p.c.FetchPack(ctx, tag)
	if err != nil {
		return nil, err
	}
	out := make([]api.PackedSubtitle, len(files))
	for i, f := range files {
		out[i] = api.PackedSubtitle{
			Name: f.Name, Ext: f.Ext, Text: f.Text,
			Season: f.Season, Episode: f.Episode, HasSeason: f.HasSeason, HasEpisode: f.HasEpisode,
			SDH: f.SDH, Forced: f.Forced,
		}
	}
	return out, nil
}

// subtitleUploader places a newly-fetched subtitle in the archive channel via WorkerBot
// (sendDocument only, same role as opsd's backup poster) -- satisfies api.SubtitleUploader.
type subtitleUploader struct {
	client    *tg.Client
	channelID int64
}

func (u subtitleUploader) Upload(ctx context.Context, fileName, text, caption string) (channelID, messageID int64, err error) {
	tmp, err := os.CreateTemp("", "sushi-sub-*.srt")
	if err != nil {
		return 0, 0, fmt.Errorf("subtitle upload: temp file: %w", err)
	}
	defer func() { _ = os.Remove(tmp.Name()) }()
	if _, writeErr := tmp.WriteString(text); writeErr != nil {
		_ = tmp.Close()
		return 0, 0, fmt.Errorf("subtitle upload: write: %w", writeErr)
	}
	if closeErr := tmp.Close(); closeErr != nil {
		return 0, 0, fmt.Errorf("subtitle upload: close: %w", closeErr)
	}
	msgID, err := u.client.SendDocumentHTML(ctx, u.channelID, tmp.Name(), fileName, caption)
	if err != nil {
		return 0, 0, err
	}
	return u.channelID, msgID, nil
}

// subtitleDeliverer copies an archived subtitle into a reader's own chat, round-robinning across
// DELIVERY_BOTS -- satisfies api.SubtitleDeliverer. Picked per copy, same idiom as
// internal/app/delivery.Runner.nextPushBot, since apid (unlike the client's API-bot pool) has no
// notion of pre-warming one of these per reader.
type subtitleDeliverer struct {
	bots   []deliveryBotHandle
	cursor atomic.Uint64
}

type deliveryBotHandle struct {
	client *tg.Client
	botID  int64
}

// newSubtitleDeliverer resolves each configured delivery bot's own Telegram user id up front
// (client.Me, same as startAPIBots does for the API pool) so a later CopyToReader never needs an
// extra round trip just to answer "which bot did this come from".
func newSubtitleDeliverer(ctx context.Context, cfg config.APID) (*subtitleDeliverer, error) {
	bots := make([]deliveryBotHandle, 0, cfg.DeliveryBots.Len())
	for _, b := range cfg.DeliveryBots.Bots() {
		client := tg.New(b.Token)
		me, err := client.Me(ctx)
		if err != nil {
			return nil, fmt.Errorf("bot %s failed getMe: %w", b.Username, err)
		}
		bots = append(bots, deliveryBotHandle{client: client, botID: me.ID})
	}
	return &subtitleDeliverer{bots: bots}, nil
}

func (d *subtitleDeliverer) CopyToReader(ctx context.Context, readerChatID, channelID, messageID int64, locator string) (botID, newMessageID int64, err error) {
	if len(d.bots) == 0 {
		return 0, 0, fmt.Errorf("subtitle deliver: no delivery bots configured")
	}
	i := int(d.cursor.Add(1)-1) % len(d.bots)
	b := d.bots[i]
	newMessageID, err = b.client.CopyProtectedMessage(ctx, readerChatID, channelID, messageID, locator)
	if err != nil {
		return 0, 0, err
	}
	return b.botID, newMessageID, nil
}

// startBackground is everything apid runs that is not a protocol poller: retention, the
// deliveryd-is-dead watchdog, and home-rail rebuilds.
func startBackground(ctx context.Context, group *errgroup.Group, store *pg.Store, trending rails.TrendingSource, notifier deliverywatch.Notifier, log *slog.Logger) {
	// Retention. The append-only tables (play_events, search_events) and the TMDB caches
	// (browse_titles, browse_people) had none at all, and neither did the delivery rows of readers
	// nothing is bound to any more (docs/06 §7). It rides in apid because apid is the one process
	// that is always running against an environment database.
	group.Go(func() error { return janitor.New(store, janitor.DefaultPolicy(), log).Run(ctx) })

	// deliveryd is a separate process so a stalled copy cannot delay /home (docs/01). This loop
	// is what notices when that process is simply not running: unclaimed pending copies, not a
	// rate limit (R-OPS-2).
	group.Go(func() error { return deliverywatch.New(store, notifier, log).Run(ctx) })

	// Home rails are materialised, never computed per /home (R-RAIL-2). Without this loop the
	// table stays at whatever last manual build wrote, while fetcherd keeps adding titles.
	railBuilder := rails.New(func(ctx context.Context, fn func(rails.Store) error) error {
		return store.InTx(ctx, func(tx *pg.Store) error { return fn(tx) })
	}, trending, rails.DefaultPolicy())
	group.Go(func() error { return railBuilder.Run(ctx, log) })
}

// newReporter is health reporting, with ops alerts when there is somewhere to send them.
func newReporter(notify config.OpsNotify, store *pg.Store, log *slog.Logger) (*health.Reporter, deliverywatch.Notifier) {
	if notify.BotToken == "" || len(notify.AdminIDs) == 0 {
		return health.New(store, nil, log), nil
	}
	n := opsnotify.New(tg.New(notify.BotToken), notify.AdminIDs, log)
	return health.New(store, n, log), n
}

// startAPIBots puts one poller per configured API bot into group.
func startAPIBots(
	ctx context.Context,
	group *errgroup.Group,
	cfg config.APID,
	store *pg.Store,
	service *api.API,
	resolver transport.PrincipalResolver,
	reporter *health.Reporter,
	log *slog.Logger,
) error {
	for _, bot := range cfg.APIBots.Bots() {
		client := tg.New(bot.Token)

		// Verify the token before polling with it: a dead token should fail at boot, named, not
		// silently poll nothing forever (docs/08 §2.1 requirement 3).
		me, err := client.Me(ctx)
		if err != nil {
			return fmt.Errorf("bot %s failed getMe: %w", bot.Username, err)
		}
		if me.Username != bot.Username {
			log.Warn("configured handle differs from the real one; using Telegram's",
				"configured", bot.Username, "actual", me.Username)
		}

		botID, err := store.RegisterAPIBot(ctx, me.Username)
		if err != nil {
			return err
		}

		dispatcher := transport.NewAPIDispatcher(service).WithAppCopy(client, me.ID)
		handler := transport.NewHandler(dispatcher, client, resolver, cfg.MainBotUsername, log.With("bot", me.Username)).
			WithAccess(accessIdentity(cfg, store))
		poller := transport.NewPoller(client, handler, log, botID, reporter)
		group.Go(func() error { return poller.Run(ctx) })
	}
	return nil
}

// devResolver treats a chat id as a user id.
//
// It exists so the protocol can be exercised before main-bot and the bindings table do, and
// config refuses it outside dev — where it would let anyone talk to the API as anyone.
type devResolver struct{}

func (devResolver) Resolve(_ context.Context, chatID int64) (api.Principal, error) {
	return api.Principal{UserID: chatID, ReaderChatID: chatID}, nil
}

// accessIdentity only answers "is this person refused?" for the wording of /start and of an
// unbound chat's error (ADR 0039). apid never creates anyone, so it needs nothing else from it.
func accessIdentity(cfg config.APID, store *pg.Store) *identity.Service {
	svc := identity.New(store)
	if cfg.Access.Closed() {
		svc = svc.WithClosedRegistration(store, cfg.Access.StaffIDs)
	}
	return svc
}

func principalResolver(cfg config.APID, store *pg.Store) transport.PrincipalResolver {
	if cfg.AllowUnbound {
		return devResolver{}
	}
	return transport.BoundResolver{Identity: identity.New(store)}
}
