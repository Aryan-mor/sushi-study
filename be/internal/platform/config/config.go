// Package config loads and validates every setting once, at boot.
//
// A missing or malformed value is a fatal startup error, never a silent default (R-GO-9): a
// service that starts with half its configuration is a service that fails later, somewhere else,
// for reasons nobody connects back to here.
package config

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/Aryan-mor/sushi-study/be/internal/platform/botpool"
	"github.com/Aryan-mor/sushi-study/be/internal/platform/channels"
	"github.com/Aryan-mor/sushi-study/be/internal/platform/pairs"
	"github.com/Aryan-mor/sushi-study/be/internal/platform/secretbox"
)

// Env is which environment this process belongs to. It exists mostly to make one mistake
// impossible: a dev build talking to the production database (docs/10 Q19).
type Env string

const (
	EnvDev     Env = "dev"
	EnvStaging Env = "staging"
	EnvProd    Env = "prod"
)

// APID is everything the API bot service needs.
type APID struct {
	Env Env
	// Access says whether strangers may register (ADR 0039).
	Access      Access
	DatabaseURL string
	// LibraryURL lets apid delete a person's own personal-video grant (ADR 0038). Optional: without
	// it a personal delete answers an error and everything else serves as before.
	LibraryURL string
	APIBots    *botpool.Pool

	// MainBotUsername is the one handle an API bot ever mentions: where to go when a chat has no
	// binding (docs/02 §9). It is also the only name baked into the client (R-SEC-6).
	MainBotUsername string

	// AllowUnbound accepts any chat and treats its id as the user id.
	//
	// This is a development shortcut that skips /initbot and the bindings table, so the protocol
	// can be exercised before main-bot exists. It is refused outside dev, loudly, because in
	// production it would let anyone talk to the API as anyone.
	AllowUnbound bool

	// DeliveriesPerHour, MaxConcurrentDeliveries and SearchPerHour are the R-SEC-11 abuse-control
	// caps, per identity. Config-driven because "tuning them must never need a deploy" (docs/10
	// Q18) — a deploy is exactly what changing a Go constant would need.
	DeliveriesPerHour       int
	MaxConcurrentDeliveries int
	SearchPerHour           int
	// RequestsPerDay is the content-request cap for a non-staff user (ADR 0014 §D2), a rolling 24h
	// window. Same "tune without a deploy" reason as the caps above.
	RequestsPerDay int

	// OpsNotify is who hears about a banned/rate-limited API bot (docs/02 §7). Optional.
	OpsNotify OpsNotify

	// TMDBKey hydrates /item and /person misses (ADR 0009). Empty disables on-demand TMDB.
	TMDBKey string

	// SubDLKeys round-robins the subtitle command across one or more subdl.com API keys (doc 15
	// §7). Empty disables the provider entirely -- Subtitle then falls through to whatever else is
	// configured, same "absent = feature off" shape as TMDBKey.
	SubDLKeys []string

	// SubtitleChannelID is the archive channel a fetched subtitle is uploaded to (WorkerBot) and
	// copied from into a reader's chat (DeliveryBots) -- doc 05-style delivery, reused for
	// subtitles because the wire protocol's per-message text limit cannot carry a whole file
	// (doc 15 §7). Zero disables server-side caching/delivery; Subtitle then has nowhere to put a
	// fetched file and returns an error.
	SubtitleChannelID int64
	// WorkerToken/WorkerUsername upload a newly-fetched subtitle to SubtitleChannelID. Reuses
	// WORKER_BOTS (indexerd's sendDocument-only bot, opsd's TELEGRAM_DB_BACKUP_CHANNEL_ID poster) —
	// same "send documents, never getUpdates" role, a new destination.
	WorkerToken    string
	WorkerUsername string
	// DeliveryBots copies a cached subtitle from SubtitleChannelID into the requesting reader's own
	// chat (protect_content, doc 05 §3) — the same pool deliveryd uses for video, round-robinned
	// the same way (picked per copy, not pre-warmed by apid).
	DeliveryBots *botpool.Pool

	// SecretKey opens the user's Gemini API key at rest (env/028, platform/secretbox) so /prefs
	// can return it to the bound client. Same SUSHI_SECRET_KEY as main-bot. Required outside
	// dev; in dev an empty value falls back to secretbox.DevKey.
	SecretKey string
}

// LoadAPID reads the environment, falling back to a .env file when one is present.
//
// Real deployments run under `infisical run` and never see a .env at all (docs/08 §1); the file is
// a local-dev convenience and nothing else reads it.
func LoadAPID(envFile string) (APID, error) {
	if envFile != "" {
		if err := loadDotEnv(envFile); err != nil {
			return APID{}, err
		}
	}

	cfg := APID{
		Env:          Env(getenvDefault("SUSHI_ENV", string(EnvDev))),
		DatabaseURL:  os.Getenv("DATABASE_URL"),
		LibraryURL:   os.Getenv("LIBRARY_DATABASE_URL"),
		AllowUnbound: os.Getenv("SUSHI_ALLOW_UNBOUND") == "true",
	}
	if b, packProblems := firstPackedBot("MAIN_BOTS", os.Getenv("MAIN_BOTS")); len(packProblems) == 0 {
		cfg.MainBotUsername = b.Username
	}

	var problems []string
	if cfg.DatabaseURL == "" {
		problems = append(problems, "DATABASE_URL is empty")
	}

	raw := os.Getenv("API_BOTS")
	if raw == "" {
		problems = append(problems, "API_BOTS is empty")
	} else {
		pool, err := botpool.Parse("API_BOTS", raw)
		if err != nil {
			problems = append(problems, err.Error())
		} else {
			cfg.APIBots = pool
		}
	}

	problems = append(problems, envProblems(cfg.Env)...)
	cfg.Access, problems = appendAccess(problems)

	if cfg.AllowUnbound && cfg.Env != EnvDev {
		problems = append(problems, "SUSHI_ALLOW_UNBOUND is only allowed in dev: outside it, anyone could talk to the API as anyone")
	}

	caps, capProblems := loadAbuseCaps()
	problems = append(problems, capProblems...)
	cfg.DeliveriesPerHour = caps.deliveriesPerHour
	cfg.MaxConcurrentDeliveries = caps.maxConcurrent
	cfg.SearchPerHour = caps.searchPerHour
	cfg.RequestsPerDay = caps.requestsPerDay

	cfg.OpsNotify = LoadOpsNotify()
	cfg.TMDBKey = strings.TrimSpace(os.Getenv("TMDB_API_KEY"))
	cfg.SubDLKeys = stringList(os.Getenv("SUBDL_API_KEYS"))
	problems = loadSubtitleArchive(&cfg, problems)

	secret, secretProblems := loadSecretKey(cfg.Env)
	problems = append(problems, secretProblems...)
	cfg.SecretKey = secret

	if len(problems) > 0 {
		return APID{}, fmt.Errorf("config: %s", strings.Join(problems, "; "))
	}
	return cfg, nil
}

// loadSubtitleArchive fills SubtitleChannelID / WorkerBot / DeliveryBots. All three optional
// together (a deployment with no subtitle channel yet just serves no subtitles), but a malformed
// one present is still a real error — same shape as API_BOTS vs. AllowUnbound above.
func loadSubtitleArchive(cfg *APID, problems []string) []string {
	cfg.SubtitleChannelID, problems = channelID("SUBTITLE_CHANNEL_ID", problems)
	if raw := os.Getenv("WORKER_BOTS"); raw != "" {
		if b, packProblems := firstPackedBot("WORKER_BOTS", raw); len(packProblems) > 0 {
			problems = append(problems, packProblems...)
		} else {
			cfg.WorkerToken, cfg.WorkerUsername = b.Token, b.Username
		}
	}
	if raw := os.Getenv("DELIVERY_BOTS"); raw != "" {
		pool, parseErr := botpool.Parse("DELIVERY_BOTS", raw)
		if parseErr != nil {
			problems = append(problems, parseErr.Error())
		} else {
			cfg.DeliveryBots = pool
		}
	}
	return problems
}

// abuseCaps are the R-SEC-11 per-identity limits (docs/10 Q18, ADR 0014 §D2), tuned by config so
// changing one never needs a deploy.
type abuseCaps struct {
	deliveriesPerHour, maxConcurrent, searchPerHour, requestsPerDay int
}

// loadAbuseCaps reads the four caps in one place, collecting any "not a positive integer"
// complaints so LoadAPID can fold them into its own list.
func loadAbuseCaps() (abuseCaps, []string) {
	var problems []string
	read := func(key, fallback string) int {
		v, probs := positiveInt(key, fallback)
		problems = append(problems, probs...)
		return v
	}
	return abuseCaps{
		deliveriesPerHour: read("DELIVERY_RATE_PER_HOUR", "30"),
		maxConcurrent:     read("DELIVERY_MAX_CONCURRENT", "2"),
		searchPerHour:     read("SEARCH_RATE_PER_HOUR", "60"),
		requestsPerDay:    read("REQUEST_DAILY_QUOTA", "10"),
	}, problems
}

// OpsNotify is what any daemon needs to alert a human when a bot goes bad (docs/02 §7).
type OpsNotify struct {
	// BotToken sends through main-bot -- the one bot people already expect prose from, never the
	// bot that actually failed, which may be unreachable.
	BotToken string
	// AdminIDs are Telegram user ids that receive the alert. Stored in Infisical as a single
	// packed list alongside the bot pools (docs/08 §2.1), not per-environment secrets each.
	AdminIDs []int64
}

// LoadOpsNotify reads MAIN_BOTS (first entry's token) and SUPERADMIN_IDS. Call it after the
// daemon's own Load* (whose loadDotEnv already populated the process environment) -- unlike every
// other Load* in this file, this never returns an error: a missing MAIN_BOTS token here means
// health alerts are silently disabled, not that the calling daemon should fail to start. The
// Bot rotation (docs/02 §7) is best-effort infrastructure layered on a working service.
func LoadOpsNotify() OpsNotify {
	var out OpsNotify
	if b, problems := firstPackedBot("MAIN_BOTS", os.Getenv("MAIN_BOTS")); len(problems) == 0 {
		out.BotToken = b.Token
	}

	out.AdminIDs = intList(os.Getenv("SUPERADMIN_IDS"))
	return out
}

// positiveInt reads an integer env var, defaulting when unset, and rejects zero or negative —
// a rate limit of zero would silently forbid every delivery, which R-GO-9 wants loud rather than
// discovered by a user reporting playback is broken.
func positiveInt(key, fallback string) (int, []string) {
	raw := getenvDefault(key, fallback)
	n, err := strconv.Atoi(raw)
	if err != nil {
		return 0, []string{fmt.Sprintf("%s %q is not an integer", key, raw)}
	}
	if n <= 0 {
		return 0, []string{fmt.Sprintf("%s must be positive, got %d", key, n)}
	}
	return n, nil
}

// MainBot is everything the login bot needs.
type MainBot struct {
	Env Env
	// Access says whether strangers may register (ADR 0039).
	Access      Access
	DatabaseURL string
	// LibraryURL is sushi_library: the indexer ops panel (ADR 0018) reads and writes
	// indexer_settings / source_channels, which are not replicated into the environment.
	LibraryURL string
	Token      string
	Username   string
	// SecretKey seals the personal-bot token and the user's Gemini API key at rest
	// (env/017, env/028, platform/secretbox). Required outside dev; in dev an empty value falls
	// back to the published secretbox.DevKey and main-bot says so, loudly, at boot.
	SecretKey string
	// InitBotUsername is the handle used to probe Bot-to-Bot from the person's token (ADR 0013).
	// Token is not needed here; @username is what Telegram resolves on first contact.
	InitBotUsername string
	// SuperadminIDs and AdminIDs are the env-authoritative role lists (ADR 0014 §D1). main-bot is
	// the single writer of users.role and reconciles it to these at boot and per message.
	// SUPERADMIN_IDS is also OpsNotify's alert list (docs/02 §7); ADMIN_IDS is new. Both optional —
	// an empty list just means nobody holds that role. A malformed id is dropped, not fatal: a
	// typo in one entry should not stop logins from working.
	SuperadminIDs []int64
	AdminIDs      []int64
	// FollowNotifyDebounce is the quiet period a followed series must go without a new episode
	// before its followers are DMed (ADR 0014 §D3). Config so a bulk-import window can be widened
	// without a deploy.
	FollowNotifyDebounce time.Duration
	// SupportBotUsername is the public handle people message for help (no @). Not a secret;
	// Infisical so the handle can move without a deploy. Default exampleSupportBot.
	SupportBotUsername string
	// NewsChannelID is the announce channel from CHANNELS (ADR 0017). Zero means CHANNELS named no
	// `news` role, and main-bot announces nothing — unlike the clone, its absence is not an error,
	// so a dev checkout and an env that has not provisioned a channel both just stay quiet.
	NewsChannelID int64
	// RequestChannelID is the staff request inbox from CHANNELS (ADR 0023). Zero means CHANNELS
	// named no `requests` role, and new-request alerts still DM each admin.
	RequestChannelID int64
	// RequestForum overrides the topic thread ids main-bot otherwise creates and stores
	// (ADR 0036). Zero means REQUEST_TOPICS is unset.
	RequestForum RequestForum
	// AnnounceDelay is the quiet period between noticing a debut and showing it to staff, so a
	// series landing eight episodes at once is reviewed settled. Config, like FollowNotifyDebounce,
	// so an import window can be widened without a deploy.
	AnnounceDelay time.Duration
	// NewsTapQuotaPerHour caps watch-later presses on a channel post, per person, per hour
	// (R-SEC-11: the values live in config so tuning never needs a deploy). Matches the search
	// quota, because a tap costs about what a search costs.
	NewsTapQuotaPerHour int
	// AppChannelID is where sushi-app CI posts release binaries (Infisical RELEASE_CHANNEL).
	// Zero means Download has nothing to copy yet. A malformed value is fatal (R-GO-9).
	AppChannelID int64
	// PersonalCloneID is where main-bot copies a user's personal video (ADR 0037). Zero means
	// personal intake is off: CHANNELS names no personal_clone, or PERSONAL_INTAKE=off (how dev's
	// testing window closes).
	PersonalCloneID int64
	// PersonalUploadsPerDay caps new files one person can put into storage per rolling 24h
	// (R-SEC-11 shape: config, never a deploy). Grants on files someone else stored are free.
	PersonalUploadsPerDay int
	// ReleaseTag is the image tag this container was deployed at: SUSHI_IMAGE_TAG, the git sha
	// the deploy script writes into /run/sushi.env beside the image it loaded (ADR 0016).
	// Empty in a dev checkout, where nothing was deployed and there is nothing to announce.
	ReleaseTag string
	// CatalogBotUsername is CATALOG_BOT's public handle (ADR 0022). Empty means the news post
	// has no download button — the catalog-bot process is what requires the token.
	CatalogBotUsername string
	// TMDBKey fills news-post origin country when items.detail has not hydrated it yet.
	// Empty omits the row; main-bot still runs.
	TMDBKey string
	// LLM is the ADR 0025 route, reused to put news-post synopses into Persian (ADR 0032).
	// Optional: an unconfigured LLM posts TMDB's text as it is.
	LLM LLM
	// TranslateModel is LLM_TRANSLATE_MODEL; default gemini-2.5-flash on google (flash-lite, the
	// classify default, writes stiff Persian), else the classify model.
	TranslateModel string
}

// LoadMainBot reads the environment for main-bot.
func LoadMainBot(envFile string) (MainBot, error) {
	if envFile != "" {
		if err := loadDotEnv(envFile); err != nil {
			return MainBot{}, err
		}
	}

	cfg := MainBot{
		Env:         Env(getenvDefault("SUSHI_ENV", string(EnvDev))),
		DatabaseURL: os.Getenv("DATABASE_URL"),
		LibraryURL:  os.Getenv("LIBRARY_DATABASE_URL"),
	}

	var problems []string
	if cfg.DatabaseURL == "" {
		problems = append(problems, "DATABASE_URL is empty")
	}
	if cfg.LibraryURL == "" {
		problems = append(problems, "LIBRARY_DATABASE_URL is empty")
	}
	bot, botProblems := firstPackedBot("MAIN_BOTS", os.Getenv("MAIN_BOTS"))
	problems = append(problems, botProblems...)
	cfg.Token = bot.Token
	cfg.Username = bot.Username
	problems = append(problems, envProblems(cfg.Env)...)
	cfg.Access, problems = appendAccess(problems)

	secret, secretProblems := loadSecretKey(cfg.Env)
	problems = append(problems, secretProblems...)
	cfg.SecretKey = secret

	initBot, initProblems := firstPackedBot("INIT_BOTS", os.Getenv("INIT_BOTS"))
	problems = append(problems, initProblems...)
	cfg.InitBotUsername = initBot.Username

	cfg.SuperadminIDs = intList(os.Getenv("SUPERADMIN_IDS"))
	cfg.AdminIDs = intList(os.Getenv("ADMIN_IDS"))
	problems = append(problems, loadMainBotExtras(&cfg)...)

	if len(problems) > 0 {
		return MainBot{}, fmt.Errorf("config: %s", strings.Join(problems, "; "))
	}
	return cfg, nil
}

func loadMainBotExtras(cfg *MainBot) []string {
	var problems []string
	problems = append(problems, loadFollow(cfg)...)
	problems = append(problems, loadSupportAndAnnounce(cfg)...)
	problems = append(problems, loadStaffAndPersonal(cfg)...)
	problems = append(problems, loadCatalogAndLLM(cfg)...)
	return problems
}

func loadFollow(cfg *MainBot) []string {
	var problems []string
	debounce, debounceProblems := positiveDuration("FOLLOW_NOTIFY_DEBOUNCE", "15m")
	problems = append(problems, debounceProblems...)
	cfg.FollowNotifyDebounce = debounce
	return problems
}

func loadSupportAndAnnounce(cfg *MainBot) []string {
	var problems []string
	cfg.SupportBotUsername = telegramUsername(getenvDefault("SUPPORT_BOT_USERNAME", "exampleSupportBot"))
	if cfg.SupportBotUsername == "" {
		problems = append(problems, "SUPPORT_BOT_USERNAME is empty")
	}
	delay, delayProblems := positiveDuration("ANNOUNCE_DELAY", "10m")
	problems = append(problems, delayProblems...)
	cfg.AnnounceDelay = delay
	return problems
}

func loadStaffAndPersonal(cfg *MainBot) []string {
	var problems []string
	newsID, requestID, newsProblems := staffChannelIDs(os.Getenv("CHANNELS"))
	problems = append(problems, newsProblems...)
	cfg.NewsChannelID = newsID
	cfg.RequestChannelID = requestID
	forum, forumProblems := requestForumTopics(os.Getenv("REQUEST_TOPICS"))
	problems = append(problems, forumProblems...)
	cfg.RequestForum = forum

	taps, tapProblems := positiveInt("NEWS_TAP_QUOTA_PER_HOUR", "60")
	problems = append(problems, tapProblems...)
	cfg.NewsTapQuotaPerHour = taps

	personalID, personalProblems := personalCloneID(os.Getenv("CHANNELS"), os.Getenv("PERSONAL_INTAKE"))
	problems = append(problems, personalProblems...)
	cfg.PersonalCloneID = personalID
	uploads, uploadProblems := positiveInt("PERSONAL_UPLOADS_PER_DAY", "20")
	problems = append(problems, uploadProblems...)
	cfg.PersonalUploadsPerDay = uploads
	return problems
}

func loadCatalogAndLLM(cfg *MainBot) []string {
	var problems []string
	appID, appProblems := appChannelID(os.Getenv("RELEASE_CHANNEL"))
	problems = append(problems, appProblems...)
	cfg.AppChannelID = appID
	cfg.ReleaseTag = strings.TrimSpace(os.Getenv("SUSHI_IMAGE_TAG"))

	catalogUser, catalogProblems := optionalPackedUsername("CATALOG_BOT", os.Getenv("CATALOG_BOT"))
	problems = append(problems, catalogProblems...)
	cfg.CatalogBotUsername = catalogUser
	cfg.TMDBKey = strings.TrimSpace(os.Getenv("TMDB_API_KEY"))
	cfg.LLM = loadIndexerLLM()
	cfg.TranslateModel = translateModel(cfg.LLM)
	return problems
}

func translateModel(l LLM) string {
	if m := strings.TrimSpace(os.Getenv("LLM_TRANSLATE_MODEL")); m != "" {
		return m
	}
	if strings.EqualFold(strings.TrimSpace(l.Provider), "google") {
		return "gemini-2.5-flash"
	}
	return l.ClassifyModel
}

// staffChannelIDs reads the optional news and requests roles out of CHANNELS (ADR 0017, ADR 0023).
//
// main-bot is the one process that needs CHANNELS without needing a clone, so an unset CHANNELS is
// not a problem here — it is the normal state of a checkout that only serves logins. A CHANNELS that
// is set but malformed still is: silently announcing nothing because of a typo is exactly the
// half-working config R-GO-9 rules out.
func staffChannelIDs(raw string) (newsID, requestID int64, problems []string) {
	if strings.TrimSpace(raw) == "" {
		return 0, 0, nil
	}
	set, err := channels.Parse("CHANNELS", raw)
	if err != nil {
		return 0, 0, []string{err.Error()}
	}
	if ch, ok := set.News(); ok {
		newsID = ch.ID
	}
	if ch, ok := set.Requests(); ok {
		requestID = ch.ID
	}
	return newsID, requestID, nil
}

// personalCloneID is CHANNELS personal_clone, unless PERSONAL_INTAKE turns intake off
// (ADR 0037 §1). on/off/empty only: a typo must not silently open or close intake.
func personalCloneID(rawChannels, rawIntake string) (int64, []string) {
	switch strings.ToLower(strings.TrimSpace(rawIntake)) {
	case "off":
		return 0, nil
	case "", "on":
	default:
		return 0, []string{fmt.Sprintf("PERSONAL_INTAKE = %q; want on or off", rawIntake)}
	}
	if strings.TrimSpace(rawChannels) == "" {
		return 0, nil
	}
	set, err := channels.Parse("CHANNELS", rawChannels)
	if err != nil {
		return 0, []string{err.Error()}
	}
	ch, ok := set.PersonalClone()
	if !ok {
		return 0, nil
	}
	return ch.ID, nil
}

// RequestForum is the open / reject / done topic thread ids (ADR 0036).
type RequestForum struct {
	Open   int64
	Reject int64
	Done   int64
}

// Enabled reports whether REQUEST_TOPICS named all three topics.
func (f RequestForum) Enabled() bool {
	return f.Open > 0 && f.Reject > 0 && f.Done > 0
}

// requestForumTopics parses REQUEST_TOPICS=open|11,reject|22,done|33. Empty is unset, not an error:
// main-bot creates the topics and stores the thread ids. A partial or unknown set fails boot.
func requestForumTopics(raw string) (RequestForum, []string) {
	if strings.TrimSpace(raw) == "" {
		return RequestForum{}, nil
	}
	entries, err := pairs.Parser{FieldNames: []string{"topic", "thread id"}}.Parse("REQUEST_TOPICS", raw)
	if err != nil {
		return RequestForum{}, []string{err.Error()}
	}
	var out RequestForum
	seen := map[string]bool{}
	for i, e := range entries {
		name := strings.ToLower(e.Key())
		if seen[name] {
			return RequestForum{}, []string{fmt.Sprintf("REQUEST_TOPICS: duplicate topic %s", name)}
		}
		seen[name] = true
		id, parseErr := strconv.ParseInt(e[1], 10, 64)
		if parseErr != nil || id <= 0 {
			return RequestForum{}, []string{fmt.Sprintf("REQUEST_TOPICS: entry %d (%s) is not a topic thread id", i+1, name)}
		}
		switch name {
		case "open":
			out.Open = id
		case "reject":
			out.Reject = id
		case "done":
			out.Done = id
		default:
			return RequestForum{}, []string{fmt.Sprintf("REQUEST_TOPICS: unknown topic %q; expected open, reject, done", name)}
		}
	}
	if !out.Enabled() {
		return RequestForum{}, []string{"REQUEST_TOPICS: need open, reject, and done"}
	}
	return out, nil
}

// telegramUsername strips a leading @ and surrounding space so Infisical can store either form.
func telegramUsername(raw string) string {
	return strings.TrimPrefix(strings.TrimSpace(raw), "@")
}

// loadSecretKey reads SUSHI_SECRET_KEY. Empty is allowed only in dev, where it falls back to the
// published secretbox.DevKey (not a secret — main-bot and apid warn at boot).
func loadSecretKey(env Env) (string, []string) {
	key := strings.TrimSpace(os.Getenv("SUSHI_SECRET_KEY"))
	if key != "" {
		return key, nil
	}
	if env == EnvDev {
		return secretbox.DevKey, nil
	}
	return "", []string{
		"SUSHI_SECRET_KEY is empty; it seals stored personal-bot tokens and Gemini API keys and is required outside dev",
	}
}

// positiveDuration reads a Go duration env var (e.g. "15m"), defaulting when unset, and rejects
// zero or negative — the same "loud on misconfiguration" contract as positiveInt (R-GO-9).
func positiveDuration(key, fallback string) (time.Duration, []string) {
	raw := getenvDefault(key, fallback)
	d, err := time.ParseDuration(raw)
	if err != nil {
		return 0, []string{fmt.Sprintf("%s %q is not a duration", key, raw)}
	}
	if d <= 0 {
		return 0, []string{fmt.Sprintf("%s must be positive, got %s", key, d)}
	}
	return d, nil
}

// nonNegativeDuration is positiveDuration that also accepts 0 (or "off") as "disabled".
func nonNegativeDuration(key, fallback string) (time.Duration, []string) {
	raw := getenvDefault(key, fallback)
	if raw == "0" || strings.EqualFold(raw, "off") {
		return 0, nil
	}
	d, err := time.ParseDuration(raw)
	if err != nil {
		return 0, []string{fmt.Sprintf("%s %q is not a duration", key, raw)}
	}
	if d < 0 {
		return 0, []string{fmt.Sprintf("%s must not be negative, got %s", key, d)}
	}
	return d, nil
}

// intList parses a comma-separated list of integers, skipping blanks and anything unparseable — a
// typo in one Telegram id should drop that entry, not fail the daemon. Same leniency as
// LoadOpsNotify, which predates this helper.
func intList(raw string) []int64 {
	var out []int64
	for _, p := range strings.Split(raw, ",") {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		id, err := strconv.ParseInt(p, 10, 64)
		if err != nil {
			continue
		}
		out = append(out, id)
	}
	return out
}

// stringList parses a comma-separated list of strings, skipping blanks -- same leniency as
// intList, for env values that are not integers (e.g. SUBDL_API_KEYS).
func stringList(raw string) []string {
	var out []string
	for _, p := range strings.Split(raw, ",") {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		out = append(out, p)
	}
	return out
}

// InitBot is everything the machine-handshake bot needs (ADR 0004).
type InitBot struct {
	Env Env
	// Access says whether strangers may register (ADR 0039).
	Access      Access
	DatabaseURL string
	Token       string
	Username    string
	// MainBotUsername is where a person who wandered in is sent (docs/02 §9's canned reply, the
	// same courtesy apid extends to an unbound chat).
	MainBotUsername string
	// DeliveryBots rides along in the Assignment (docs/05 §6, docs/10 Q3) so the client can
	// pre-start every delivery bot the same way it warms its own API bot — otherwise a reader's
	// first play always fails `400 chat not found` against whichever bot deliveryd's round-robin
	// happens to pick, since nothing else ever gives that bot a reason to have seen this chat.
	// Best-effort: an init-bot that cannot parse DELIVERY_BOTS still assigns API bots fine, so a
	// parse failure here does not fail LoadInitBot the way a bad INIT_BOTS does.
	DeliveryBots []string
	// LoginChannelUsername / LoginChannelID are the public preview channel (ADR 0013). Same
	// value in every environment. init-bot must be an admin of this chat.
	LoginChannelUsername string
	LoginChannelID       int64
}

// LoadInitBot reads the environment for init-bot.
func LoadInitBot(envFile string) (InitBot, error) {
	if envFile != "" {
		if err := loadDotEnv(envFile); err != nil {
			return InitBot{}, err
		}
	}

	cfg := InitBot{
		Env:         Env(getenvDefault("SUSHI_ENV", string(EnvDev))),
		DatabaseURL: os.Getenv("DATABASE_URL"),
	}
	if b, packProblems := firstPackedBot("MAIN_BOTS", os.Getenv("MAIN_BOTS")); len(packProblems) == 0 {
		cfg.MainBotUsername = b.Username
	}

	var problems []string
	if cfg.DatabaseURL == "" {
		problems = append(problems, "DATABASE_URL is empty")
	}
	bot, botProblems := firstPackedBot("INIT_BOTS", os.Getenv("INIT_BOTS"))
	problems = append(problems, botProblems...)
	cfg.Token = bot.Token
	cfg.Username = bot.Username

	if pool, _ := deliveryPool(os.Getenv("DELIVERY_BOTS")); pool != nil {
		for _, b := range pool.Bots() {
			cfg.DeliveryBots = append(cfg.DeliveryBots, b.Username)
		}
	}

	chUser, chID, chProblems := loginPostChannel(os.Getenv("LOGIN_POST_CHANNEL"))
	problems = append(problems, chProblems...)
	cfg.LoginChannelUsername = chUser
	cfg.LoginChannelID = chID

	problems = append(problems, envProblems(cfg.Env)...)
	cfg.Access, problems = appendAccess(problems)

	if len(problems) > 0 {
		return InitBot{}, fmt.Errorf("config: %s", strings.Join(problems, "; "))
	}
	return cfg, nil
}

// LLM is the provider-agnostic route for classify + referee + verifier (ADR 0025). Provider is
// "google" or "openrouter". With neither secret the classifier still runs filename-first.
type LLM struct {
	Provider           string
	ClassifyModel      string
	RefereeModel       string
	VerifierModel      string
	FallbackModel      string
	VertexProject      string
	VertexLocation     string
	VertexSAJSON       string
	OpenRouterAPIKey   string
	OpenRouterProvider string
}

// Configured reports whether the caption LLM pass can run.
func (l LLM) Configured() bool {
	switch strings.ToLower(strings.TrimSpace(l.Provider)) {
	case "google":
		return strings.TrimSpace(l.VertexSAJSON) != "" &&
			strings.TrimSpace(l.VertexProject) != "" &&
			strings.TrimSpace(l.ClassifyModel) != ""
	case "openrouter":
		return strings.TrimSpace(l.OpenRouterAPIKey) != "" && strings.TrimSpace(l.ClassifyModel) != ""
	default:
		return false
	}
}

// JuryConfigured is true when both referee seats have a model. Indexerd skips the jury otherwise.
func (l LLM) JuryConfigured() bool {
	return l.Configured() &&
		strings.TrimSpace(l.RefereeModel) != "" &&
		strings.TrimSpace(l.VerifierModel) != ""
}

// Indexer is everything indexerd needs: the library it writes, TMDB for resolution, the LLM route
// for captions, and the worker bot + review channel for the moderation feeds. A second writer
// on the library (partitioned by the `tgforward` source namespace, R-LIB-1). It
// never touches an environment database — the copy into Telegram is the Python reader's job
// (ADR 0015 D2/D6). The worker bot is optional: without it the classifier still fills the catalog,
// it just posts nothing to review.
type Indexer struct {
	Env             Env
	LibraryURL      string
	TMDBKey         string
	LLM             LLM
	OpsNotify       OpsNotify
	WorkerToken     string
	WorkerUsername  string
	ReviewChannelID int64
	// AuditChannelID carries the audit feed on its own channel (ADR 0021). 0 = fold audit back
	// into the review channel, as before.
	AuditChannelID int64
	// Batch is how many queue rows one classify pass claims. Small: each row may be an LLM call.
	Batch int
}

// ReviewConfigured reports whether the moderation feeds can run.
func (i Indexer) ReviewConfigured() bool { return i.WorkerToken != "" && i.ReviewChannelID != 0 }

// channelID reads an optional "-100…" channel id from the environment, appending a problem for a
// non-integer or non-negative value. An empty var yields 0 and no problem.
func channelID(name string, problems []string) (int64, []string) {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return 0, problems
	}
	id, err := strconv.ParseInt(raw, 10, 64)
	switch {
	case err != nil:
		return 0, append(problems, name+" is not an integer")
	case id >= 0:
		return 0, append(problems, name+" must be negative (a channel id)")
	default:
		return id, problems
	}
}

// LoadIndexer reads the environment for indexerd.
func LoadIndexer(envFile string) (Indexer, error) {
	if envFile != "" {
		if err := loadDotEnv(envFile); err != nil {
			return Indexer{}, err
		}
	}

	cfg := Indexer{
		Env:        Env(getenvDefault("SUSHI_ENV", string(EnvDev))),
		LibraryURL: os.Getenv("LIBRARY_DATABASE_URL"),
		TMDBKey:    strings.TrimSpace(os.Getenv("TMDB_API_KEY")),
		LLM:        loadIndexerLLM(),
	}
	cfg.OpsNotify = LoadOpsNotify()
	batch, batchProblems := positiveInt("INDEXER_BATCH", "5")
	cfg.Batch = batch

	problems := batchProblems
	if cfg.LibraryURL == "" {
		problems = append(problems, "LIBRARY_DATABASE_URL is empty")
	}
	if cfg.TMDBKey == "" {
		problems = append(problems, "TMDB_API_KEY is empty (the classifier cannot resolve a title without it)")
	}

	// The worker bot + review channel are optional; a malformed WORKER_BOTS is still a real
	// misconfiguration, but an absent one just disables the moderation feeds.
	if raw := os.Getenv("WORKER_BOTS"); raw != "" {
		if b, packProblems := firstPackedBot("WORKER_BOTS", raw); len(packProblems) > 0 {
			problems = append(problems, packProblems...)
		} else {
			cfg.WorkerToken, cfg.WorkerUsername = b.Token, b.Username
		}
	}
	cfg.ReviewChannelID, problems = channelID("REVIEW_CHANNEL_ID", problems)
	cfg.AuditChannelID, problems = channelID("AUDIT_CHANNEL_ID", problems)
	problems = append(problems, llmProblems(cfg.LLM)...)

	problems = append(problems, envProblems(cfg.Env)...)

	if len(problems) > 0 {
		return Indexer{}, fmt.Errorf("config: %s", strings.Join(problems, "; "))
	}
	return cfg, nil
}

// envProblems rejects an environment name that is not one of the three we deploy. Every loader
// runs it, because SUSHI_ENV is what stops a dev build opening the production database (docs/10 Q19).
func envProblems(env Env) []string {
	switch env {
	case EnvDev, EnvStaging, EnvProd:
		return nil
	default:
		return []string{fmt.Sprintf("SUSHI_ENV %q is not dev, staging or prod", env)}
	}
}

// firstPackedBot takes the first entry of a username|token list (doc 08 §2.1). init-bot
// runs exactly one bot; extra entries in the secret are somebody else's budget.
func firstPackedBot(secretName, raw string) (botpool.Bot, []string) {
	if raw == "" {
		return botpool.Bot{}, []string{secretName + " is empty"}
	}
	pool, err := botpool.Parse(secretName, raw)
	switch {
	case err != nil:
		return botpool.Bot{}, []string{err.Error()}
	case pool.Len() < 1:
		return botpool.Bot{}, []string{secretName + " has no bots"}
	default:
		return pool.Bots()[0], nil
	}
}

// channelSet parses CHANNELS. A nil set means the problems it returned are fatal, so callers must
// not read a clone id out of it.
func channelSet(raw string) (*channels.Set, []string) {
	if raw == "" {
		return nil, []string{"CHANNELS is empty"}
	}
	set, err := channels.Parse("CHANNELS", raw)
	if err != nil {
		return nil, []string{err.Error()}
	}
	return set, nil
}

// Backup is everything backupd needs. It never talks to the prototype.
type Backup struct {
	Env        Env
	LibraryURL string
	CloneID    int64
	Slots      []BackupSlot
	// PersonalBackup copies personal_clone into personal_backup (ADR 0037): the backup1 bot, bound
	// to the personal_backup channel. Nil when CHANNELS names no personal_backup.
	PersonalBackup *BackupSlot
}

// BackupSlot is one backup bot bound to one destination channel.
type BackupSlot struct {
	Role      string
	Username  string
	Token     string
	ChannelID int64
}

// LoadBackup reads BACKUP_BOTS and CHANNELS. Role names come from CHANNELS, matched by channel id.
func LoadBackup(envFile string) (Backup, error) {
	if envFile != "" {
		if err := loadDotEnv(envFile); err != nil {
			return Backup{}, err
		}
	}

	cfg := Backup{
		Env:        Env(getenvDefault("SUSHI_ENV", string(EnvDev))),
		LibraryURL: os.Getenv("LIBRARY_DATABASE_URL"),
	}

	var problems []string
	if cfg.LibraryURL == "" {
		problems = append(problems, "LIBRARY_DATABASE_URL is empty")
	}

	set, channelProblems := channelSet(os.Getenv("CHANNELS"))
	problems = append(problems, channelProblems...)
	if set != nil {
		cfg.CloneID = set.Clone().ID
	}

	rawBots := os.Getenv("BACKUP_BOTS")
	switch {
	case rawBots == "":
		problems = append(problems, "BACKUP_BOTS is empty")
	case set != nil:
		slots, slotProblems := backupSlots(rawBots, set)
		problems = append(problems, slotProblems...)
		cfg.Slots = slots
		personal, err := personalBackupSlot(slots, set)
		if err != nil {
			problems = append(problems, err.Error())
		}
		cfg.PersonalBackup = personal
	}

	problems = append(problems, envProblems(cfg.Env)...)

	if len(problems) > 0 {
		return Backup{}, fmt.Errorf("config: %s", strings.Join(problems, "; "))
	}
	return cfg, nil
}

// personalBackupSlot is the backup1 bot pointed at personal_backup (ADR 0037 §2): personal_backup
// mirrors backup1, so the same bot fills both. Absent personal_backup is not an error.
func personalBackupSlot(slots []BackupSlot, set *channels.Set) (*BackupSlot, error) {
	ch, ok := set.PersonalBackup()
	if !ok {
		return nil, nil
	}
	for _, s := range slots {
		if s.Role == "backup1" {
			return &BackupSlot{
				Role:      string(channels.RolePersonalBackup),
				Username:  s.Username,
				Token:     s.Token,
				ChannelID: ch.ID,
			}, nil
		}
	}
	return nil, fmt.Errorf("CHANNELS: personal_backup needs the backup1 bot in BACKUP_BOTS to fill it")
}

// backupSlots parses BACKUP_BOTS and binds each bot to the channel role CHANNELS gives its id.
func backupSlots(raw string, set *channels.Set) ([]BackupSlot, []string) {
	pool, err := botpool.ParseBound("BACKUP_BOTS", raw)
	if err != nil {
		return nil, []string{err.Error()}
	}
	slots, err := bindBackupSlots(pool.Bots(), set)
	if err != nil {
		return nil, []string{err.Error()}
	}
	return slots, nil
}

func bindBackupSlots(bots []botpool.BoundBot, set *channels.Set) ([]BackupSlot, error) {
	byID := make(map[int64]channels.Channel, len(set.Backups()))
	for _, ch := range set.Backups() {
		byID[ch.ID] = ch
	}
	seen := make(map[int64]string, len(bots))
	out := make([]BackupSlot, 0, len(bots))
	for _, b := range bots {
		if b.ChannelID == set.Clone().ID {
			return nil, fmt.Errorf("BACKUP_BOTS: %s is bound to the clone channel; backup bots copy INTO a backup", b.Username)
		}
		ch, ok := byID[b.ChannelID]
		if !ok {
			return nil, fmt.Errorf("BACKUP_BOTS: %s channel %d is not in CHANNELS", b.Username, b.ChannelID)
		}
		if first, dup := seen[b.ChannelID]; dup {
			return nil, fmt.Errorf("BACKUP_BOTS: channel %d is bound to both %s and %s", b.ChannelID, first, b.Username)
		}
		seen[b.ChannelID] = b.Username
		out = append(out, BackupSlot{
			Role:      string(ch.Name),
			Username:  b.Username,
			Token:     b.Token,
			ChannelID: b.ChannelID,
		})
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("BACKUP_BOTS has no bots")
	}

	// The reverse check: every backup channel CHANNELS names needs exactly one bot filling it. A
	// channel with no bot would otherwise start backupd happily and never receive a single copy --
	// silently, because nothing ever looks at it again (R-GO-9: no config that starts half-working).
	for _, ch := range set.Backups() {
		if _, ok := seen[ch.ID]; !ok {
			return nil, fmt.Errorf("CHANNELS: backup channel %s (%d) has no bot in BACKUP_BOTS", ch.Name, ch.ID)
		}
	}
	return out, nil
}

// Delivery is everything deliveryd needs. It reads the library (to find where a file's clone copy
// landed) and writes the environment (the deliveries row).
type Delivery struct {
	Env         Env
	DatabaseURL string
	LibraryURL  string
	Bots        *botpool.Pool

	// APIBots is the same pool apid polls, borrowed here only to send (never to receive -- this
	// never competes with apid's own poller for a bot, R-OPS-3 is about GetUpdates, not this): the
	// Ready push (docs/02 §8) after a completed delivery has to go out through the reader's bound
	// API bot, which the client actually listens to, not the delivery bot that copied the file.
	// Optional: a push is a safety net with its own polling fallback (docs/02 §8), never something
	// correctness depends on, so its absence is a warning at boot, not a fatal misconfiguration.
	APIBots *botpool.Pool

	// OpsNotify is who hears about a banned/rate-limited delivery bot (docs/02 §7). Optional.
	OpsNotify OpsNotify
}

// LoadDelivery reads DELIVERY_BOTS. Unlike BACKUP_BOTS, delivery bots are not bound to one channel
// — they copy from the single shared clone into whichever reader chat asked, so a plain pool
// (botpool.Parse, the same grammar as API_BOTS) is all CHANNELS-free deliveryd needs.
func LoadDelivery(envFile string) (Delivery, error) {
	if envFile != "" {
		if err := loadDotEnv(envFile); err != nil {
			return Delivery{}, err
		}
	}

	cfg := Delivery{
		Env:         Env(getenvDefault("SUSHI_ENV", string(EnvDev))),
		DatabaseURL: os.Getenv("DATABASE_URL"),
		LibraryURL:  os.Getenv("LIBRARY_DATABASE_URL"),
	}

	var problems []string
	if cfg.DatabaseURL == "" {
		problems = append(problems, "DATABASE_URL is empty")
	}
	if cfg.LibraryURL == "" {
		problems = append(problems, "LIBRARY_DATABASE_URL is empty")
	}

	pool, poolProblems := deliveryPool(os.Getenv("DELIVERY_BOTS"))
	problems = append(problems, poolProblems...)
	cfg.Bots = pool

	// Unlike DELIVERY_BOTS, an empty API_BOTS is not a problem: the Ready push it enables is
	// optional (see the APIBots field doc). A malformed value is still rejected -- that is a real
	// misconfiguration, not an absent one.
	if raw := os.Getenv("API_BOTS"); raw != "" {
		apiPool, err := botpool.Parse("API_BOTS", raw)
		if err != nil {
			problems = append(problems, err.Error())
		} else {
			cfg.APIBots = apiPool
		}
	}

	cfg.OpsNotify = LoadOpsNotify()

	problems = append(problems, envProblems(cfg.Env)...)

	if len(problems) > 0 {
		return Delivery{}, fmt.Errorf("config: %s", strings.Join(problems, "; "))
	}
	return cfg, nil
}

// deliveryPool parses DELIVERY_BOTS: a plain, unbound pool (same grammar as API_BOTS) since a
// delivery bot reads the one shared clone but writes to whichever reader chat asked, so there is
// no per-entry destination to bind (docs/08 §2.1).
func deliveryPool(raw string) (*botpool.Pool, []string) {
	if raw == "" {
		return nil, []string{"DELIVERY_BOTS is empty"}
	}
	pool, err := botpool.Parse("DELIVERY_BOTS", raw)
	switch {
	case err != nil:
		return nil, []string{err.Error()}
	case pool.Len() < 1:
		return nil, []string{"DELIVERY_BOTS has no bots"}
	default:
		return pool, nil
	}
}

func getenvDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// loadDotEnv fills in variables the process does not already have. Anything already exported wins,
// so `SUSHI_ENV=prod ./apid` cannot be quietly overridden by a stale file on disk.
func loadDotEnv(path string) error {
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("config: open %s: %w", path, err)
	}
	defer func() { _ = f.Close() }()

	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		if _, set := os.LookupEnv(key); set {
			continue
		}
		value = strings.TrimSpace(value)
		if unquoted, err := strconv.Unquote(value); err == nil {
			value = unquoted
		}
		if err := os.Setenv(key, value); err != nil {
			return fmt.Errorf("config: set %s: %w", key, err)
		}
	}
	if err := sc.Err(); err != nil {
		return fmt.Errorf("config: read %s: %w", path, err)
	}
	return nil
}
