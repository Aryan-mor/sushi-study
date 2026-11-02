// Package dailystats is main-bot's superadmin digest: active users, registrations, copies
// delivered, and indexed variants. The panel UI lives in cmd/main-bot; this package owns the
// snapshot, the once-per-day claim, and the ticker that DMs it.
package dailystats

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"
)

const defaultInterval = time.Minute

// Snapshot is one read of the four numbers the digest shows.
type Snapshot struct {
	Online24h    int64
	Registered   int64
	Delivered24h int64
	Indexed      int64
}

// Store is what the digest and the Actions panel need. Declared here, by the consumer; *pg.Store
// satisfies it.
type Store interface {
	DailyStats(ctx context.Context) (Snapshot, error)
	ClaimDailyStatsSend(ctx context.Context, ymd string) (bool, error)
}

// Notifier DMs every superadmin. Same shape as opsnotify.Notifier — declared here so this package
// does not import Telegram (R-GO-5).
type Notifier interface {
	NotifyAdmins(ctx context.Context, text string)
}

// Digest sends one snapshot per local calendar day until ctx is cancelled.
type Digest struct {
	store    Store
	notify   Notifier
	log      *slog.Logger
	now      func() time.Time
	interval time.Duration
}

// New builds a Digest that ticks once a minute and claims today's YMD before sending.
func New(store Store, notify Notifier, log *slog.Logger) *Digest {
	if log == nil {
		log = slog.Default()
	}
	return &Digest{store: store, notify: notify, log: log, now: time.Now, interval: defaultInterval}
}

// Run ticks until ctx is cancelled. Best-effort: a failed read or claim is logged and retried
// next tick, never fatal (R-GO-10).
func (d *Digest) Run(ctx context.Context) error {
	if d.store == nil || d.notify == nil {
		<-ctx.Done()
		return ctx.Err()
	}
	d.log.Info("daily stats digest started", "interval", d.interval)
	d.tick(ctx)
	t := time.NewTicker(d.interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-t.C:
			d.tick(ctx)
		}
	}
}

func (d *Digest) tick(ctx context.Context) {
	ymd := YMDLocal(d.now())
	fresh, err := d.store.ClaimDailyStatsSend(ctx, ymd)
	if err != nil {
		d.log.Warn("daily stats claim", "err", err, "ymd", ymd)
		return
	}
	if !fresh {
		return
	}
	snap, err := d.store.DailyStats(ctx)
	if err != nil {
		d.log.Warn("daily stats snapshot", "err", err, "ymd", ymd)
		return
	}
	d.log.Info("daily stats sent", "ymd", ymd,
		"online_24h", snap.Online24h, "registered", snap.Registered,
		"delivered_24h", snap.Delivered24h, "indexed", snap.Indexed)
	d.notify.NotifyAdmins(ctx, Format(snap, "en", ymd))
}

// YMDLocal returns YYYY-MM-DD in the server's local timezone.
func YMDLocal(t time.Time) string {
	return fmt.Sprintf("%04d-%02d-%02d", t.Year(), t.Month(), t.Day())
}

// Format is the digest body. locale is "fa" or anything else (English). ymd is the header date.
func Format(s Snapshot, locale, ymd string) string {
	if locale == "fa" {
		return formatFA(s, ymd)
	}
	return formatEN(s, ymd)
}

func formatEN(s Snapshot, ymd string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "📊 Daily stats — %s\n\n", ymd)
	fmt.Fprintf(&b, "👤 Online (24h): %s\n", groupThousands(s.Online24h))
	fmt.Fprintf(&b, "🧾 Registered: %s\n", groupThousands(s.Registered))
	fmt.Fprintf(&b, "▶️ Videos delivered (24h): %s\n", groupThousands(s.Delivered24h))
	fmt.Fprintf(&b, "🎬 Indexed videos: %s", groupThousands(s.Indexed))
	return b.String()
}

func formatFA(s Snapshot, ymd string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "📊 آمار روزانه — %s\n\n", ymd)
	fmt.Fprintf(&b, "👤 آنلاین 24ساعت: %s\n", groupThousands(s.Online24h))
	fmt.Fprintf(&b, "🧾 ثبت‌نام‌شده: %s\n", groupThousands(s.Registered))
	fmt.Fprintf(&b, "▶️ پخش ویدئو (24ساعت): %s\n", groupThousands(s.Delivered24h))
	fmt.Fprintf(&b, "🎬 ویدئو ایندکس‌شده: %s", groupThousands(s.Indexed))
	return b.String()
}

// NewUserNotice is the superadmin DM for a first-contact insert. Plain text: opsnotify sends
// with no parse mode.
func NewUserNotice(tgUserID int64, name, username string) string {
	var b strings.Builder
	b.WriteString("👤 New user\n")
	fmt.Fprintf(&b, "id: %d", tgUserID)
	if name != "" {
		fmt.Fprintf(&b, "\nname: %s", name)
	}
	if username != "" {
		fmt.Fprintf(&b, "\n@%s", username)
	}
	return b.String()
}

func groupThousands(n int64) string {
	s := strconv.FormatInt(n, 10)
	neg := strings.HasPrefix(s, "-")
	if neg {
		s = s[1:]
	}
	var out []byte
	for i, c := range []byte(s) {
		if i > 0 && (len(s)-i)%3 == 0 {
			out = append(out, ' ')
		}
		out = append(out, c)
	}
	if neg {
		return "-" + string(out)
	}
	return string(out)
}
