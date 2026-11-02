// Command main-bot is the only bot a person starts. It logs them in — language onboarding
// questions, then a binding — and then it gets out of the way (docs/02 §1, ADR 0004).
//
// Composition only (R-GO-3).
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
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/Aryan-mor/sushi-study/be/internal/adapter/pg"
	"github.com/Aryan-mor/sushi-study/be/internal/adapter/pglib"
	"github.com/Aryan-mor/sushi-study/be/internal/adapter/tg"
	"github.com/Aryan-mor/sushi-study/be/internal/adapter/tmdb"
	"github.com/Aryan-mor/sushi-study/be/internal/app/adminreq"
	"github.com/Aryan-mor/sushi-study/be/internal/app/contentnotify"
	"github.com/Aryan-mor/sushi-study/be/internal/app/dailystats"
	"github.com/Aryan-mor/sushi-study/be/internal/app/enroll"
	"github.com/Aryan-mor/sushi-study/be/internal/app/identity"
	"github.com/Aryan-mor/sushi-study/be/internal/app/newsroom"
	"github.com/Aryan-mor/sushi-study/be/internal/app/onboarding"
	"github.com/Aryan-mor/sushi-study/be/internal/app/onlineview"
	"github.com/Aryan-mor/sushi-study/be/internal/app/opsnotify"
	"github.com/Aryan-mor/sushi-study/be/internal/app/personal"
	"github.com/Aryan-mor/sushi-study/be/internal/platform/config"
	"github.com/Aryan-mor/sushi-study/be/internal/platform/secretbox"
)

func main() {
	log := newLogger()

	if err := run(log); err != nil && !errors.Is(err, context.Canceled) {
		log.Error("main-bot stopped", "err", err)
		os.Exit(1)
	}
	log.Info("main-bot stopped")
}

// newLogger writes to stdout and, when it can, mirrors to be/tmp/main-bot.log. main-bot runs
// under watch-bots.mjs with inherited stdio, so a file is the only way to read what it logged
// after the fact.
func newLogger() *slog.Logger {
	var w io.Writer = os.Stdout
	if f, err := os.OpenFile(filepath.Join("tmp", "main-bot.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644); err == nil {
		w = io.MultiWriter(os.Stdout, f)
	}
	return slog.New(slog.NewTextHandler(w, &slog.HandlerOptions{Level: slog.LevelDebug}))
}

func run(log *slog.Logger) error {
	cfg, err := config.LoadMainBot(os.Getenv("SUSHI_ENV_FILE"))
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

	lib, err := pg.OpenRequestPath(ctx, cfg.LibraryURL, 0)
	if err != nil {
		return err
	}
	defer lib.Close()

	// main-bot is the one process that stores a personal-bot token, so it is the one process given
	// the key that seals it (env/017). A dev checkout with no SUSHI_SECRET_KEY falls back to the
	// published dev key -- which protects nothing, and says so rather than looking encrypted.
	secrets, err := secretbox.New(cfg.SecretKey)
	if err != nil {
		return fmt.Errorf("secret key: %w", err)
	}
	if cfg.SecretKey == secretbox.DevKey {
		log.Warn("SUSHI_SECRET_KEY is unset: stored personal-bot tokens and Gemini API keys are sealed with the " +
			"published dev key and are not protected")
	}

	store := pg.NewStore(pool).WithSecrets(secrets)
	if err = store.RequireIdentitySchema(ctx); err != nil {
		return err
	}
	client := tg.New(cfg.Token)

	me, err := client.Me(ctx)
	if err != nil {
		return fmt.Errorf("getMe: %w", err)
	}
	if err := store.RegisterMainBot(ctx, me.Username); err != nil {
		return err
	}
	if err := client.DeleteWebhook(ctx); err != nil {
		return fmt.Errorf("deleteWebhook: %w", err)
	}
	if err := registerMenus(ctx, client); err != nil {
		return err
	}

	log.Info("starting", "env", cfg.Env, "bot", me.Username, "init_bot_username", cfg.InitBotUsername)
	if cfg.InitBotUsername == "" {
		log.Warn("INIT_BOTS has no username; the bot-login Bot-to-Bot probe cannot run and enrolment will fail")
	}

	return serveMainBot(ctx, cfg, store, pglib.NewOps(lib), pglib.NewPersonal(lib), client, log)
}

func serveMainBot(
	ctx context.Context, cfg config.MainBot, store *pg.Store, ops *pglib.Ops, personalLib *pglib.Personal,
	client *tg.Client, log *slog.Logger,
) error {
	identitySvc := mainIdentity(cfg, store, log)
	syncRolesAtBoot(ctx, identitySvc, log)
	tmdbAPI := tmdb.New(cfg.TMDBKey)
	b := &bot{
		client:        client,
		identity:      identitySvc,
		onboarding:    onboarding.New(store),
		enroll:        enroll.New(tokenVerifier{}, identitySvc),
		tokens:        store,
		gemini:        store,
		requests:      store,
		news:          store,
		countries:     tmdbAPI,
		networks:      tmdbAPI,
		backdrops:     tmdbAPI,
		gallery:       ops,
		ids:           tmdbAPI,
		initUsername:  cfg.InitBotUsername,
		supportUser:   cfg.SupportBotUsername,
		newsChannelID: cfg.NewsChannelID,
		newsTapQuota:  cfg.NewsTapQuotaPerHour,
		catalogUser:   cfg.CatalogBotUsername,
		appChannelID:  cfg.AppChannelID,
		releases:      store,
		invites:       store,
		accessClosed:  cfg.Access.Closed(),
		stats:         store,
		online:        store,
		log:           log,
	}
	b.personal = newPersonalIntake(cfg, personalLib, client, log)
	b.sharing, b.shareQueue = personal.NewSharing(personalLib), store
	b.translate = newsTranslator(ctx, cfg, log)
	b.overviews = ops
	topics, err := b.bindRequestForum(ctx, client, store, cfg, log)
	if err != nil {
		return err
	}
	logUnsetOptionals(cfg, log)
	b.notify = opsnotify.New(client, cfg.SuperadminIDs, log)
	announceDeploy(ctx, cfg.Env, cfg.ReleaseTag, store, b.notify, log)
	return runServices(
		ctx, b,
		contentnotify.New(store, dmSender{client}, log, cfg.FollowNotifyDebounce, cfg.RequestChannelID).
			WithForum(topics),
		newsroom.New(store, newsPreview{client}, log, cfg.AnnounceDelay, client.Username()).
			WithCountries(b.countries).
			WithNetworks(b.networks).
			WithBackdrops(b.backdrops, b.gallery).
			WithTranslator(b.translate, b.overviews),
	)
}

// mainIdentity is the identity service with main-bot's two extras: it alone writes users.role
// (ADR 0014 §D1), and it is where a stranger first meets the access gate (ADR 0039).
func mainIdentity(cfg config.MainBot, store *pg.Store, log *slog.Logger) *identity.Service {
	svc := identity.New(store).WithRoleSync(cfg.SuperadminIDs, cfg.AdminIDs)
	if cfg.Access.Closed() {
		svc = svc.WithClosedRegistration(store, cfg.Access.StaffIDs)
	}
	log.Info("access mode", "mode", cfg.Access.Mode)
	return svc
}

// logUnsetOptionals says, once at boot, which optional channels and bots are not configured, so a
// quiet feature reads as configuration and not as a bug.
func logUnsetOptionals(cfg config.MainBot, log *slog.Logger) {
	if cfg.NewsChannelID == 0 {
		log.Info("CHANNELS names no news channel; nothing will be announced (ADR 0017)")
	}
	if cfg.CatalogBotUsername == "" {
		log.Info("CATALOG_BOT unset; news posts have no download button (ADR 0022)")
	}
	if cfg.AppChannelID == 0 {
		log.Info("RELEASE_CHANNEL unset; Download copies nothing until it is set")
	}
}

// runServices runs the update poll beside the two reconcilers — content requests (ADR 0014 §D4) and
// the announce channel (ADR 0017) — plus backup-watch, ingest-rush pings, and the daily stats
// digest. Any returning ends main-bot (R-GO-11).
//
// The newsroom reconciler runs even with no channel configured: it still queues debuts and shows
// staff the previews, and only the final post has nowhere to go. That way provisioning a channel
// later starts working without a backfill.
func runServices(
	ctx context.Context, b *bot,
	requests *contentnotify.Reconciler, news *newsroom.Reconciler,
) error {
	g, gctx := errgroup.WithContext(ctx)
	g.Go(func() error { return b.poll(gctx) })
	g.Go(func() error { return requests.Run(gctx) })
	g.Go(func() error { return news.Run(gctx) })
	g.Go(func() error { return b.watchDailyStats(gctx) })
	g.Go(func() error { return b.watchPersonalShares(gctx) })
	return g.Wait()
}

// syncRolesAtBoot is best-effort: a stale role must not stop main-bot from serving logins, and the
// next boot retries.
func syncRolesAtBoot(ctx context.Context, svc *identity.Service, log *slog.Logger) {
	n, err := svc.SyncRoles(ctx)
	switch {
	case err != nil:
		log.Warn("role sync failed", "err", err)
	case n > 0:
		log.Info("synced user roles", "changed", n)
	}
}

type bot struct {
	client     *tg.Client
	identity   *identity.Service
	onboarding *onboarding.Service
	enroll     *enroll.Service
	tokens     tokenShelf
	gemini     geminiKeyShelf
	requests   adminreq.Store
	personal   *personal.Service // nil when intake is off (ADR 0037)
	sharing    *personal.Sharing // share links (ADR 0038); works whether or not intake is on
	shareQueue shareQueue
	news       newsStore
	countries  newsroom.CountryLookup
	networks   newsroom.NetworkLookup
	backdrops  newsroom.BackdropLookup
	gallery    newsroom.BackdropStore
	translate  newsroom.Translator
	overviews  newsroom.OverviewStore
	ids        idCatalog
	invites    inviteShelf
	// accessClosed is SUSHI_ACCESS_MODE=testing (ADR 0039). The panel only reads it to say so.
	accessClosed bool
	initUsername string
	supportUser  string
	// newsChannelID is zero when CHANNELS names no announce channel (ADR 0017 §2).
	newsChannelID int64
	// requestChannelID is the staff inbox (ADR 0023). requestTopics moves cards between
	// open / reject / done when that inbox is a forum supergroup (ADR 0036).
	requestChannelID int64
	requestTopics    contentnotify.ForumTopics
	newsTapQuota     int
	catalogUser      string
	// appChannelID is the private dump CI posts binaries into. Zero means Download has nowhere to copy from.
	appChannelID     int64
	releases         appReleaseStore
	stats            dailystats.Store
	online           onlineview.Store
	notify           adminNotifier
	log              *slog.Logger
	offset           int64
	promptID         map[int64]int64         // chat → ForceReply / checking message to delete before success
	replaceMsg       map[int64]int64         // chat → replace-cancel keyboard message
	waitingGemini    map[int64]bool          // chat → ForceReply waiting for a Gemini key paste
	geminiReplaceMsg map[int64]int64         // chat → "updating your Gemini key" message to delete once a new key is accepted or the replace is cancelled
	waitingIDs       map[int64]bool          // chat → ForceReply waiting for a TMDB/IMDb id
	waitingAccess    map[int64]bool          // chat → ForceReply waiting for a Telegram id to authorize
	waitingNews      map[int64]bool          // chat → ForceReply waiting for a TMDB id to draft as news
	waitingTitle     map[int64]int64         // chat → personal video id waiting for this person's title
	newsTaps         map[int64]newsTapWindow // user → channel-post tap quota window
	// loginLanguage is set only while /start login (or the login button) is waiting for a
	// language tap. That tap finishes onboarding and binds. A bare /start tap only stores the
	// bot-chat language and opens Home.
	loginLanguage map[int64]bool
}

func (b *bot) poll(ctx context.Context) error {
	b.log.Info("polling", "bot", b.client.Username())

	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		updates, err := b.client.GetUpdates(ctx, b.offset, 30*time.Second)
		if err != nil {
			if stop := b.backOff(ctx, err); stop != nil {
				return stop
			}
			continue
		}

		b.dispatch(ctx, updates)
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

func (b *bot) dispatch(ctx context.Context, updates []tg.Update) {
	for _, u := range updates {
		if u.ID >= b.offset {
			b.offset = u.ID + 1
		}
		switch {
		case u.ChannelPost != nil:
			b.handleAppReleasePost(ctx, u.ChannelPost)
		case u.Message != nil:
			b.handle(ctx, u.Message)
		case u.Callback != nil:
			b.handleCallback(ctx, u.Callback)
		}
	}
}

func (b *bot) handle(ctx context.Context, msg *tg.Message) {
	if msg.From == nil || !directMessage(msg.Chat.ID) {
		return
	}

	in := parseInbound(msg.Text)
	in.msgID = msg.ID
	if msg.ReplyTo != nil {
		in.replyToID = msg.ReplyTo.ID
	}
	log := b.log.With("chat", msg.Chat.ID, "cmd", in.cmd)
	s := screen{chatID: msg.Chat.ID}

	user, err := b.identity.EnsureUser(ctx, msg.From.ID, msg.From.Name, msg.From.Username)
	if b.refuseIfClosed(ctx, s, msg.From, err, log) {
		return
	}
	if err != nil {
		log.Error("ensure user", "err", err)
		hint := localeHint(msg.From)
		b.show(ctx, s, failed.pick(hint), homeKeyboard(hint), log)
		return
	}

	locale := b.localeFor(ctx, user.ID, msg.From)
	b.tuneChat(ctx, msg.Chat.ID, locale, user.Role, log)
	if user.Deleted {
		b.showDeleted(ctx, s, locale, log)
		return
	}
	b.noteNewUser(ctx, user)
	if b.routeVideo(ctx, s, msg, locale, log) {
		return
	}
	b.route(ctx, s, user, in, locale)
}

// tuneChat keeps this private chat's menu button on the commands drawer and, for staff, adds
// /admin (staff hub) that the global default list does not carry.
func (b *bot) tuneChat(ctx context.Context, chatID int64, locale, role string, log *slog.Logger) {
	if err := b.client.SetChatMenuButton(ctx, chatID); err != nil {
		log.Debug("setChatMenuButton", "err", err)
	}
	if err := b.client.SetMyCommandsForChat(ctx, chatID, menuCommandsFor(locale, role)); err != nil {
		log.Debug("setMyCommands chat", "err", err)
	}
}

func (b *bot) localeFor(ctx context.Context, userID int64, from *tg.User) string {
	locale := localeHint(from)
	if userID == 0 {
		return locale
	}
	if prefs, err := b.onboarding.Prefs(ctx, userID); err == nil && prefs.Locale != "" {
		return prefs.Locale
	}
	return locale
}

func (b *bot) route(ctx context.Context, s screen, user identity.User, in inbound, locale string) {
	log := b.log.With("chat", s.chatID, "cmd", in.cmd)
	if b.routePaste(ctx, s, user, in, locale, log) {
		return
	}

	if loc, known := b.savedLocale(ctx, user.ID); known && loc == "" && !skipsLanguageGate(in) {
		b.clearLoginLanguage(s.chatID)
		b.show(ctx, s, askLanguage.pick(locale), languageKeyboard(), log)
		return
	}

	switch in.cmd {
	case "start":
		b.routeStart(ctx, s, user, in.rest, locale, log)
	case "download":
		b.showDownload(ctx, s, locale, log)
	case "login":
		b.startLogin(ctx, s, user, locale, log)
	case "help":
		b.showHelp(ctx, s, user.Role, locale, log)
	case "settings":
		b.showSettings(ctx, s, user, locale, log)
	case "language":
		b.show(ctx, s, askLanguage.pick(locale), languageKeyboard(), log)
	case "admin":
		b.openAdmin(ctx, s, user, locale, log)
	default:
		b.show(ctx, s, unknown.pick(locale), homeKeyboardFor(locale, user.Role), log)
	}
}

// routePaste handles a reply that is a token, key, or other awaited paste. It returns true when
// the message was consumed so route does not also run a command.
func (b *bot) routePaste(ctx context.Context, s screen, user identity.User, in inbound, locale string, log *slog.Logger) bool {
	if in.token {
		b.clearGeminiWait(s.chatID)
		b.handlePastedToken(ctx, s, user, in, locale)
		return true
	}
	if in.gemini {
		b.handlePastedGeminiKey(ctx, s, user, in, locale)
		return true
	}
	if b.awaitingGemini(s.chatID) && in.cmd == "" {
		b.handleBadGeminiPaste(ctx, s, locale, log)
		return true
	}
	b.clearGeminiWait(s.chatID)
	if b.awaitingIDs(s.chatID) && in.cmd == "" {
		b.handlePastedIDs(ctx, s, user, in, locale, log)
		return true
	}
	b.clearIDWait(s.chatID)
	if b.awaitingAccess(s.chatID) && in.cmd == "" {
		b.handlePastedInvite(ctx, s, user, in, locale, log)
		return true
	}
	b.clearAccessWait(s.chatID)
	if b.awaitingNewsDraft(s.chatID) && in.cmd == "" {
		b.handlePastedNewsDraft(ctx, s, user, in, locale, log)
		return true
	}
	b.clearNewsWait(s.chatID)
	if videoID, ok := b.awaitingTitle(s.chatID, in); ok {
		return b.handlePastedTitle(ctx, s, user, videoID, in, locale, log)
	}
	return false
}
