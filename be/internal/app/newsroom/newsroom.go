// Package newsroom is main-bot's announce-channel pipeline (ADR 0017): notice that something became
// playable, queue it, and show it to staff before anyone else sees it.
//
// It is deliberately two passes on one ticker, not one. Discovery is cheap and wants to run often so
// a new release is queued while it is still new; publishing waits for a human, and a human is slow.
// Splitting them means a backlog of unapproved posts never stops new debuts being noticed.
//
// It lives in main-bot because a channel post is prose to people, and main-bot is the only bot
// allowed to write that (ADR 0004 R-VOICE-1). Nothing here imports Telegram: the reconciler renders
// a caption and hands it to a Messenger that cmd/main-bot supplies (R-GO-2).
package newsroom

import (
	"context"
	"log/slog"
	"strings"
	"time"

	"github.com/Aryan-mor/sushi-study/be/internal/core/catalog"
)

const (
	defaultInterval = 45 * time.Second
	defaultBatch    = 40
	// defaultDebutWindow is how far back discovery looks. Wide enough that a reconciler restart or a
	// stalled deploy does not silently lose a day of releases, narrow enough that the first tick
	// against an existing catalog does not queue its entire history.
	defaultDebutWindow = 48 * time.Hour
	// defaultAnnounceDelay is the quiet period between noticing a debut and showing it to staff, so
	// a series landing eight episodes at once is reviewed as eight settled rows rather than raced.
	defaultAnnounceDelay = 10 * time.Minute
	// defaultPremiereWindow is the first half of the freshness rule: a title that premiered this
	// recently is news whenever its file arrives.
	defaultPremiereWindow = 90 * 24 * time.Hour
)

// Debut is a newly playable thing worth announcing. Season and Episode are zero for a movie, which
// is also how the catalog stores one (docs/06 §2).
type Debut struct {
	TMDBID           uint64
	Kind             catalog.Kind
	Title            string
	Subtitle         string // episode name, empty for a movie or an untitled episode
	Season           int32
	Episode          int32
	Poster           string
	Backdrop         string // items.detail->>'backdrop'; empty is fine, the slideshow just has the poster
	Genres           string // display string, as items.detail->>'genres'
	Overview         string
	RuntimeS         int32
	Year             int32
	Rating           int32 // TMDB rating 0-100, as items.rating
	PremiereOn       time.Time
	FirstAvailableAt time.Time
	// Manual is a staff draft from the admin hub. Freshness does not apply: the person
	// asked for this title, including one the reconciler would never queue.
	Manual bool
}

// Announcement is a queued row, on its way to a staff preview.
type Announcement struct {
	ID       int64
	TMDBID   uint64
	Kind     catalog.Kind
	Season   int32
	Episode  int32
	Title    string
	Subtitle string
	Poster   string
	Backdrop string
	// Backdrops are extra TMDB stems from items.detail.backdrops. BackdropsKnown is false when
	// that key was never stored, so FillBackdrops may ask TMDB once and save the list.
	Backdrops      []string
	BackdropsKnown bool
	// Logo is items.detail->>'logo'. Empty or catalog.LogoNone means the card draws the title as text.
	Logo     string
	Genres   string
	Overview string
	// OverviewFa is the stored Persian translation of Overview (items/episodes.overview_fa), empty
	// until main-bot has made one. FillOverview swaps it into Overview before rendering.
	OverviewFa string
	RuntimeS   int32
	Year       int32
	Rating     int32
	PremiereOn time.Time
	// Manual skips the 90-day premiere window on preview and on approve.
	Manual bool
	// Country is ISO 3166-1 alpha-2 from items.detail. Empty or catalog.CountryNone omits the row.
	Country string
	// TrailerKey is items.detail.trailerKey. Empty or catalog.TrailerNone omits the caption link.
	TrailerKey string
	// Network is items.detail.network (Netflix, Disney+). Empty or catalog.NetworkNone omits the row.
	Network string
	// Handle is the public main-bot username stamped at render time, not stored.
	// Empty omits the lion line rather than inventing a brand hashtag.
	Handle string
}

// Admin is one recipient of the preview, with the locale their copy is written in. Staff read a
// preview in their own language even though the post itself is Persian (ADR 0017 §8).
type Admin struct {
	TelegramID int64
	Locale     string
}

// Store is the queue and the catalog reads behind it. Declared here by the consumer (R-GO-5);
// *pg.Store satisfies it.
type Store interface {
	// MovieDebuts and EpisodeDebuts return things that became playable since `since` and that no
	// announcement row covers yet.
	MovieDebuts(ctx context.Context, since, premieredAfter time.Time, limit int32) ([]Debut, error)
	EpisodeDebuts(ctx context.Context, since, premieredAfter time.Time, limit int32) ([]Debut, error)
	// QueueAnnouncement inserts one debut. queued is false when a live row already covers it —
	// two overlapping ticks, not an error.
	QueueAnnouncement(ctx context.Context, d Debut, announceAfter time.Time) (queued bool, err error)
	// QueueStaleSeriesEpisode inserts one series episode, then keeps only the latest queued
	// episode of that series. A later episode replaces the row and keeps announceAfter (another
	// full hold). An older episode than the one already waiting is dropped and does not move
	// announceAfter. queued is false when nothing new is waiting. A posted episode stays; the
	// next one that airs later is its own news item.
	QueueStaleSeriesEpisode(ctx context.Context, d Debut, announceAfter time.Time) (queued bool, err error)
	DueAnnouncements(ctx context.Context, limit int32) ([]Announcement, error)
	// MarkAwaitingApproval records the preview message an approve press will edit.
	MarkAwaitingApproval(ctx context.Context, id, adminChatID, adminMessageID int64) error
	// MarkSkipped parks a row nobody can review, so the due scan does not stall on it forever.
	MarkSkipped(ctx context.Context, id int64) error
	NewsAdminRecipients(ctx context.Context) ([]Admin, error)
}

// StaffPreview is one admin DM: rich HTML first, photo caption as the fallback when Telegram
// refuses sendRichMessage (ordinary parse_mode=HTML does not accept <p> / <tg-slideshow>).
type StaffPreview struct {
	ID           int64
	PhotoURL     string
	Rich         string
	Caption      string
	Locale       string
	HeroJPEG     []byte
	Announcement Announcement
}

// Messenger sends the staff preview. The announcement id and locale ride along so cmd/main-bot can
// build the approve / reject keyboard — this package writes prose and knows nothing about buttons.
type Messenger interface {
	SendPreview(ctx context.Context, chatID int64, p StaffPreview) (messageID int64, err error)
}

// CountryLookup is TMDB origin_country at preview/approve time. items.detail often lacks it
// until hydrate fills extras, and a movie premiere this week skips the 10m delay.
// Nil skips the row rather than blocking the post.
type CountryLookup interface {
	OriginCountry(ctx context.Context, tmdbID uint64, kind catalog.Kind) (string, error)
}

// FillCountry sets Announcement.Country from TMDB when the catalog join left it empty.
func FillCountry(ctx context.Context, lookup CountryLookup, a Announcement) Announcement {
	if catalog.CountryISO(a.Country) != "" || lookup == nil || a.TMDBID == 0 {
		return a
	}
	c, err := lookup.OriginCountry(ctx, a.TMDBID, a.Kind)
	if err != nil || catalog.CountryISO(c) == "" {
		return a
	}
	a.Country = catalog.CountryISO(c)
	return a
}

// NetworkLookup is the TMDB network (or streaming studio) at preview/approve time.
// items.detail often lacks it until a title page hydrate. Nil omits the row.
type NetworkLookup interface {
	Network(ctx context.Context, tmdbID uint64, kind catalog.Kind) (string, error)
}

// FillNetwork sets Announcement.Network from TMDB when the catalog join left it empty.
func FillNetwork(ctx context.Context, lookup NetworkLookup, a Announcement) Announcement {
	if strings.TrimSpace(a.Network) != "" || lookup == nil || a.TMDBID == 0 {
		return a
	}
	n, err := lookup.Network(ctx, a.TMDBID, a.Kind)
	if err != nil {
		return a
	}
	n = strings.TrimSpace(n)
	if n == "" || n == catalog.NetworkNone {
		return a
	}
	a.Network = n
	return a
}

// Reconciler is the ticker and its two passes.
type Reconciler struct {
	store     Store
	dm        Messenger
	log       *slog.Logger
	interval  time.Duration
	batch     int32
	window    time.Duration
	delay     time.Duration
	premiere  time.Duration
	handle    string
	countries CountryLookup
	networks  NetworkLookup
	backdrops BackdropLookup
	gallery   BackdropStore
	translate Translator
	overviews OverviewStore
	// now is time.Now except in tests.
	now func() time.Time
}

// New builds a Reconciler on main-bot's 45s cadence. A zero delay falls back to 10 minutes.
// handle is getMe's username (no @); staff previews stamp it on the lion line.
func New(store Store, dm Messenger, log *slog.Logger, delay time.Duration, handle string) *Reconciler {
	if delay <= 0 {
		delay = defaultAnnounceDelay
	}
	return &Reconciler{
		store: store, dm: dm, log: log,
		interval: defaultInterval,
		batch:    defaultBatch,
		window:   defaultDebutWindow,
		delay:    delay,
		premiere: defaultPremiereWindow,
		handle:   strings.TrimPrefix(strings.TrimSpace(handle), "@"),
		now:      time.Now,
	}
}

// WithCountries fills کشور تولید from TMDB when items.detail has no country yet.
func (r *Reconciler) WithCountries(lookup CountryLookup) *Reconciler {
	r.countries = lookup
	return r
}

// WithNetworks fills the شبکه row from TMDB when items.detail has no network yet.
func (r *Reconciler) WithNetworks(lookup NetworkLookup) *Reconciler {
	r.networks = lookup
	return r
}

// WithBackdrops fills the slideshow from TMDB when items.detail has no backdrops list yet,
// and stores the stems on the library row.
func (r *Reconciler) WithBackdrops(lookup BackdropLookup, store BackdropStore) *Reconciler {
	r.backdrops, r.gallery = lookup, store
	return r
}

// WithTranslator puts the synopsis into Persian (ADR 0032) and keeps the translation in the
// library. A nil Translator leaves the TMDB text as it is.
func (r *Reconciler) WithTranslator(tr Translator, store OverviewStore) *Reconciler {
	r.translate, r.overviews = tr, store
	return r
}

// Run ticks until ctx is cancelled. It is one of main-bot's errgroup goroutines (R-GO-11).
func (r *Reconciler) Run(ctx context.Context) error {
	r.log.Info("newsroom reconciler started", "interval", r.interval, "announce_delay", r.delay)
	t := time.NewTicker(r.interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-t.C:
			r.Tick(ctx)
		}
	}
}

// Tick runs both passes once. Exported so a test can drive it without a clock.
func (r *Reconciler) Tick(ctx context.Context) {
	r.discover(ctx)
	r.preview(ctx)
}

// discover queues every debut it has not seen. Movies and episodes share the prototype's freshness
// rule (ADR 0017 §4): a file that appeared today is not news if the title itself is old.
func (r *Reconciler) discover(ctx context.Context) {
	now := r.now()
	since := now.Add(-r.window)
	premieredAfter := now.Add(-r.premiere)

	movies, err := r.store.MovieDebuts(ctx, since, premieredAfter, r.batch)
	if err != nil {
		r.log.Warn("newsroom discover: movies", "err", err)
	}
	episodes, err := r.store.EpisodeDebuts(ctx, since, premieredAfter, r.batch)
	if err != nil {
		r.log.Warn("newsroom discover: episodes", "err", err)
	}

	for _, d := range append(movies, episodes...) {
		if ctx.Err() != nil {
			return
		}
		r.queueOne(ctx, d, now)
	}
}

func (r *Reconciler) queueOne(ctx context.Context, d Debut, now time.Time) {
	if !freshDebut(d, now) {
		return
	}
	after := now.Add(r.queueDelay(d, now))
	var queued bool
	var err error
	if d.Kind == catalog.KindSeries {
		queued, err = r.store.QueueStaleSeriesEpisode(ctx, d, after)
	} else {
		queued, err = r.store.QueueAnnouncement(ctx, d, after)
	}
	switch {
	case err != nil:
		r.log.Warn("newsroom discover: queue", "err", err, "tmdb", d.TMDBID, "kind", d.Kind)
	case queued:
		r.log.Info("announcement queued", "tmdb", d.TMDBID, "kind", d.Kind,
			"season", d.Season, "episode", d.Episode)
	}
}

func (r *Reconciler) queueDelay(d Debut, now time.Time) time.Duration {
	if d.Kind == catalog.KindMovie && skipsAnnounceDelay(d.PremiereOn, now) {
		return 0
	}
	return r.delay
}

// preview shows staff every row whose delay has elapsed. Each admin gets their own message, because
// each reads their own language; the first one that lands is the message approve/reject edits, and
// the rest go stale harmlessly (the status guard makes a second press a no-op).
func (r *Reconciler) preview(ctx context.Context) {
	due, err := r.store.DueAnnouncements(ctx, r.batch)
	if err != nil {
		r.log.Warn("newsroom preview: list", "err", err)
		return
	}
	if len(due) == 0 {
		return
	}
	admins, err := r.store.NewsAdminRecipients(ctx)
	if err != nil {
		r.log.Warn("newsroom preview: recipients", "err", err)
		return
	}

	for _, a := range due {
		if ctx.Err() != nil {
			return
		}
		r.previewOne(ctx, a, admins)
	}
}

func (r *Reconciler) previewOne(ctx context.Context, a Announcement, admins []Admin) {
	if !a.Manual && !FreshAnnouncement(a, r.now()) {
		r.skip(ctx, a.ID, "stale")
		return
	}
	if len(admins) == 0 {
		// Nobody can approve it, so it would re-list every 45 seconds forever. Park it; a row that
		// was skipped for want of staff can be found by hand later.
		r.skip(ctx, a.ID, "no admins configured")
		return
	}

	a = FillCountry(ctx, r.countries, a)
	a = FillNetwork(ctx, r.networks, a)
	a = FillBackdrops(ctx, r.backdrops, r.gallery, r.log, a)
	a = FillOverview(ctx, r.translate, r.overviews, r.log, a)
	stamped := r.stamp(a)

	var chatID, messageID int64
	for _, ad := range admins {
		id, err := r.dm.SendPreview(ctx, ad.TelegramID, StaffPreview{
			ID:           a.ID,
			PhotoURL:     PosterURL(a.Poster),
			Rich:         FormatPreview(stamped, ad.Locale),
			Caption:      FormatPreviewCaption(stamped, ad.Locale),
			Locale:       ad.Locale,
			Announcement: stamped,
		})
		if err != nil {
			r.log.Warn("newsroom preview: send", "err", err, "admin", ad.TelegramID, "announcement", a.ID)
			continue
		}
		if chatID == 0 {
			chatID, messageID = ad.TelegramID, id
		}
	}
	if chatID == 0 {
		// Every send failed. Leave it queued: the next tick retries, which is what we want for a
		// Telegram outage.
		return
	}
	if err := r.store.MarkAwaitingApproval(ctx, a.ID, chatID, messageID); err != nil {
		r.log.Warn("newsroom preview: mark", "err", err, "announcement", a.ID)
	}
}

func (r *Reconciler) stamp(a Announcement) Announcement {
	a.Handle = r.handle
	return a
}

func (r *Reconciler) skip(ctx context.Context, id int64, reason string) {
	if err := r.store.MarkSkipped(ctx, id); err != nil {
		r.log.Warn("newsroom preview: skip", "err", err, "announcement", id, "reason", reason)
		return
	}
	r.log.Info("announcement skipped", "announcement", id, "reason", reason)
}
