package main

import (
	"context"
	"fmt"
	"html"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/Aryan-mor/sushi-study/be/internal/adapter/tg"
	"github.com/Aryan-mor/sushi-study/be/internal/app/identity"
	"github.com/Aryan-mor/sushi-study/be/internal/app/onlineview"
	"github.com/Aryan-mor/sushi-study/be/internal/core/catalog"
)

const (
	cbMenuOnline       = "m:on"
	cbOnlinePagePrefix = "on:p:"
	cbOnlineUserPrefix = "on:u:"

	// Progress is throttled to about 30s (docs/11). Five minutes still means they are playing now,
	// whatever list window the admin picked.
	onlineWindowMins int32 = 5
	onlinePageSize         = 8
	onlineWatchPage  int32 = 8
	// Telegram text cap is 4096. Leave room for the parse and the keyboard edit.
	onlineTextLimit = 3900

	onlineCbList = 1
	onlineCbUser = 2
	onlineCbPick = 3
)

var (
	onlineBtn       = message{en: "🟢 Online", fa: "🟢 آنلاین"}
	onlineBtnBack   = message{en: "⬅️ Online", fa: "⬅️ آنلاین"}
	onlinePickBack  = message{en: "⬅️ Time", fa: "⬅️ زمان"}
	onlinePickTitle = message{en: "🟢 <b>Online</b>\n\nPick a window.", fa: "🟢 <b>آنلاین</b>\n\nیک بازه را انتخاب کن."}
	onlinePrev      = message{en: "◀️ Prev", fa: "◀️ قبلی"}
	onlineNext      = message{en: "▶️ Next", fa: "▶️ بعدی"}
	onlineTitle     = message{en: "🟢 <b>Online</b>", fa: "🟢 <b>آنلاین</b>"}
	onlineHint      = message{en: "Playback or a search in this window. Tap someone.", fa: "پخش یا جستجو تو این بازه. یکی رو بزن."}
	onlineEmpty     = message{en: "Nobody in this window.", fa: "تو این بازه کسی نیست."}
	onlineFailed    = message{en: "😅 Couldn't load who's online. Try again.", fa: "😅 لیست آنلاین لود نشد. دوباره امتحان کن."}
	onlineGone      = message{en: "That user is gone.", fa: "این کاربر دیگه نیست."}
	onlineIdle      = message{en: "⏸ <b>Now:</b> not playing anything.", fa: "⏸ <b>الان:</b> چیزی پخش نمی‌کنه."}
	onlineNow       = message{en: "▶️ <b>Now</b>", fa: "▶️ <b>الان</b>"}
	onlineHistory   = message{en: "<b>Watched</b> · newest → oldest", fa: "<b>دیده‌ها</b> · جدید → قدیم"}
	onlineNoWatch   = message{en: "Nothing watched yet.", fa: "هنوز چیزی ندیده."}
	onlineWatchFail = message{en: "😅 Couldn't load this watch list. Try again.", fa: "😅 این لیست لود نشد. دوباره امتحان کن."}
)

// onlineSpan is one glass-button window on the online list. code rides in callback_data (64 bytes).
// "0" is the live window (onlineWindowMins). "1m" is 30 days.
type onlineSpan struct {
	code string
	mins int32
	en   string
	fa   string
}

func (s onlineSpan) pick(locale string) string {
	if locale == "fa" {
		return s.fa
	}
	return s.en
}

var onlineSpans = []onlineSpan{
	{code: "0", mins: onlineWindowMins, en: "Online", fa: "آنلاین"},
	{code: "30", mins: 30, en: "30 min", fa: "30 دقیقه"},
	{code: "3h", mins: 3 * 60, en: "3 hours", fa: "3 ساعت"},
	{code: "6h", mins: 6 * 60, en: "6 hours", fa: "6 ساعت"},
	{code: "12h", mins: 12 * 60, en: "12 hours", fa: "12 ساعت"},
	{code: "1d", mins: 24 * 60, en: "1 day", fa: "1 روز"},
	{code: "2d", mins: 2 * 24 * 60, en: "2 days", fa: "2 روز"},
	{code: "3d", mins: 3 * 24 * 60, en: "3 days", fa: "3 روز"},
	{code: "5d", mins: 5 * 24 * 60, en: "5 days", fa: "5 روز"},
	{code: "1w", mins: 7 * 24 * 60, en: "1 week", fa: "1 هفته"},
	{code: "2w", mins: 14 * 24 * 60, en: "2 weeks", fa: "2 هفته"},
	{code: "1m", mins: 30 * 24 * 60, en: "1 month", fa: "1 ماه"},
}

func onlineSpanByCode(code string) (onlineSpan, bool) {
	for _, s := range onlineSpans {
		if s.code == code {
			return s, true
		}
	}
	return onlineSpan{}, false
}

func (b *bot) routeOnlineCallback(
	ctx context.Context, q *tg.CallbackQuery, s screen, user identity.User, locale string, log *slog.Logger,
) bool {
	kind, page, logPage, userID, span, ok := parseOnlineCallback(q.Data)
	if !ok {
		return false
	}
	if !isAdmin(user.Role) {
		b.toast(ctx, q, reqToastDenied.pick(locale))
		return true
	}
	b.toast(ctx, q, "")
	switch kind {
	case onlineCbUser:
		b.showOnlineUser(ctx, s, userID, page, logPage, span, locale, log)
	case onlineCbPick:
		b.showOnlinePick(ctx, s, locale, log)
	default:
		b.showOnlineList(ctx, s, page, span, locale, log)
	}
	return true
}

func parseOnlineCallback(data string) (kind, page, logPage int, userID int64, span onlineSpan, ok bool) {
	switch {
	case data == cbMenuOnline:
		return onlineCbPick, 0, -1, 0, onlineSpan{}, true
	case strings.HasPrefix(data, cbOnlinePagePrefix):
		p, span, ok := parseOnlinePage(strings.TrimPrefix(data, cbOnlinePagePrefix))
		if !ok {
			return 0, 0, -1, 0, onlineSpan{}, false
		}
		return onlineCbList, p, -1, 0, span, true
	case strings.HasPrefix(data, cbOnlineUserPrefix):
		return parseOnlineUser(strings.TrimPrefix(data, cbOnlineUserPrefix))
	default:
		return 0, 0, -1, 0, onlineSpan{}, false
	}
}

func parseOnlinePage(rest string) (page int, span onlineSpan, ok bool) {
	pagePart, code, found := strings.Cut(rest, ":")
	if !found {
		code = onlineSpans[0].code
	}
	p, err := strconv.Atoi(pagePart)
	span, known := onlineSpanByCode(code)
	if err != nil || p < 0 || !known {
		return 0, onlineSpan{}, false
	}
	return p, span, true
}

func parseOnlineUser(rest string) (kind, page, logPage int, userID int64, span onlineSpan, ok bool) {
	parts := strings.Split(rest, ":")
	if len(parts) < 2 || len(parts) > 4 {
		return 0, 0, -1, 0, onlineSpan{}, false
	}
	code := onlineSpans[0].code
	logPage = -1
	if len(parts) >= 3 {
		code = parts[2]
	}
	if len(parts) == 4 {
		n, err := strconv.Atoi(parts[3])
		if err != nil || n < 0 {
			return 0, 0, -1, 0, onlineSpan{}, false
		}
		logPage = n
	}
	id, err1 := strconv.ParseInt(parts[0], 10, 64)
	p, err2 := strconv.Atoi(parts[1])
	span, known := onlineSpanByCode(code)
	if err1 != nil || err2 != nil || id <= 0 || p < 0 || !known {
		return 0, 0, -1, 0, onlineSpan{}, false
	}
	return onlineCbUser, p, logPage, id, span, true
}

func (b *bot) showOnlinePick(ctx context.Context, s screen, locale string, log *slog.Logger) {
	b.show(ctx, s, onlinePickTitle.pick(locale), onlinePickKeyboard(locale), log)
}

func (b *bot) showOnlineList(ctx context.Context, s screen, page int, span onlineSpan, locale string, log *slog.Logger) {
	if span.code == "" {
		span = onlineSpans[0]
	}
	shell := onlineListNav(locale)
	if b.online == nil {
		b.show(ctx, s, onlineFailed.pick(locale), shell, log)
		return
	}
	if page < 0 {
		page = 0
	}
	total, err := b.online.CountOnline(ctx, span.mins)
	if err != nil {
		log.Warn("online count", "err", err)
		b.show(ctx, s, onlineFailed.pick(locale), shell, log)
		return
	}
	if total == 0 {
		b.show(ctx, s, formatOnlineEmpty(locale, span), shell, log)
		return
	}
	page = clampOnlinePage(page, total)
	rows, err := b.online.ListOnline(ctx, span.mins, onlinePageSize, int32(page*onlinePageSize))
	if err != nil {
		log.Warn("online page", "err", err)
		b.show(ctx, s, onlineFailed.pick(locale), shell, log)
		return
	}
	if len(rows) == 0 {
		b.show(ctx, s, formatOnlineEmpty(locale, span), shell, log)
		return
	}
	text := formatOnlineList(locale, span, page, len(rows), total)
	b.show(ctx, s, text, onlineListKeyboard(rows, page, total, span, locale), log)
}

func clampOnlinePage(page int, total int64) int {
	maxPage := int((total - 1) / int64(onlinePageSize))
	if page > maxPage {
		return maxPage
	}
	return page
}

func formatOnlineEmpty(locale string, span onlineSpan) string {
	return fmt.Sprintf("%s · %s\n\n%s", onlineTitle.pick(locale), span.pick(locale), onlineEmpty.pick(locale))
}

func formatOnlineList(locale string, span onlineSpan, page, count int, total int64) string {
	first := page*onlinePageSize + 1
	last := first + count - 1
	return fmt.Sprintf("%s · %s  (%d–%d / %d)\n\n%s",
		onlineTitle.pick(locale), span.pick(locale), first, last, total, onlineHint.pick(locale))
}

func onlinePickKeyboard(locale string) tg.InlineKeyboard {
	kb := tg.InlineKeyboard{Rows: onlineSpanRows(locale)}
	kb.Rows = append(kb.Rows, actionsNavKeyboard(locale).Rows...)
	return kb
}

func onlineListNav(locale string) tg.InlineKeyboard {
	return tg.InlineKeyboard{Rows: [][]tg.InlineButton{onlineListFooter(locale)}}
}

func onlineListKeyboard(viewers []onlineview.Viewer, page int, total int64, span onlineSpan, locale string) tg.InlineKeyboard {
	kb := tg.InlineKeyboard{}
	for _, v := range viewers {
		kb.Rows = append(kb.Rows, []tg.InlineButton{{
			Text: viewerButtonText(v),
			Data: onlineUserData(v.ID, page, -1, span),
		}})
	}
	if nav := onlinePager(page, int64(page*onlinePageSize+len(viewers)), total, span, locale); len(nav) > 0 {
		kb.Rows = append(kb.Rows, nav)
	}
	kb.Rows = append(kb.Rows, onlineListFooter(locale))
	return kb
}

func onlineListFooter(locale string) []tg.InlineButton {
	return []tg.InlineButton{
		{Text: onlinePickBack.pick(locale), Data: cbMenuOnline},
		{Text: reqBtnHome.pick(locale), Data: cbMenuHome},
	}
}

func onlineSpanRows(locale string) [][]tg.InlineButton {
	rows := make([][]tg.InlineButton, 0, (len(onlineSpans)+1)/2)
	var row []tg.InlineButton
	for _, s := range onlineSpans {
		row = append(row, tg.InlineButton{Text: s.pick(locale), Data: onlinePageData(0, s)})
		if len(row) == 2 {
			rows = append(rows, row)
			row = nil
		}
	}
	if len(row) > 0 {
		rows = append(rows, row)
	}
	return rows
}

func onlinePageData(page int, span onlineSpan) string {
	return fmt.Sprintf("%s%d:%s", cbOnlinePagePrefix, page, span.code)
}

func onlineUserData(id int64, page, logPage int, span onlineSpan) string {
	base := fmt.Sprintf("%s%d:%d:%s", cbOnlineUserPrefix, id, page, span.code)
	if logPage < 0 {
		return base
	}
	return fmt.Sprintf("%s:%d", base, logPage)
}

func onlinePager(page int, last, total int64, span onlineSpan, locale string) []tg.InlineButton {
	var nav []tg.InlineButton
	if page > 0 {
		nav = append(nav, tg.InlineButton{
			Text: onlinePrev.pick(locale), Data: onlinePageData(page-1, span),
		})
	}
	if last < total {
		nav = append(nav, tg.InlineButton{
			Text: onlineNext.pick(locale), Data: onlinePageData(page+1, span),
		})
	}
	return nav
}

func viewerButtonText(v onlineview.Viewer) string {
	label := strings.TrimSpace(v.Name)
	if label == "" && v.Username != "" {
		label = "@" + v.Username
	}
	if label == "" {
		label = strconv.FormatInt(v.ID, 10)
	}
	if dev := deviceSummary(v.Platforms, "en"); dev != "" {
		label += " · " + dev
	}
	return clipRunes(label, 40)
}

func (b *bot) showOnlineUser(ctx context.Context, s screen, userID int64, page, logPage int, span onlineSpan, locale string, log *slog.Logger) {
	if span.code == "" {
		span = onlineSpans[0]
	}
	if b.online == nil {
		b.show(ctx, s, onlineWatchFail.pick(locale), onlineUserKeyboard(userID, page, 0, 0, span, locale), log)
		return
	}
	person, entries, total, latest, devices, logPage, err := b.loadOnlineUser(ctx, userID, logPage, span, log)
	if err != nil {
		b.show(ctx, s, onlineWatchFail.pick(locale), onlineUserKeyboard(userID, page, logPage, total, span, locale), log)
		return
	}
	if !person.Found {
		b.show(ctx, s, onlineGone.pick(locale), onlineUserKeyboard(userID, page, 0, 0, span, locale), log)
		return
	}
	view := onlineWatchView{Person: person, Entries: entries, Total: total, Page: logPage, Devices: devices, Latest: latest}
	text := formatOnlineUser(locale, view, time.Now())
	b.show(ctx, s, text, onlineUserKeyboard(userID, page, logPage, total, span, locale), log)
}

func (b *bot) loadOnlineUser(
	ctx context.Context, userID int64, logPage int, span onlineSpan, log *slog.Logger,
) (onlineview.Person, []onlineview.Entry, int64, *onlineview.Entry, []onlineview.Device, int, error) {
	total, err := b.online.WatchCount(ctx, userID)
	if err != nil {
		log.Warn("online watch count", "err", err, "user", userID)
		return onlineview.Person{}, nil, 0, nil, nil, 0, err
	}
	logPage = clampWatchPage(logPage, total)
	person, entries, err := b.online.WatchLog(ctx, userID, onlineWatchPage, int32(logPage)*onlineWatchPage)
	if err != nil {
		log.Warn("online watch log", "err", err, "user", userID)
		return onlineview.Person{}, nil, total, nil, nil, logPage, err
	}
	var latest *onlineview.Entry
	if row, ok, lerr := b.online.LatestWatch(ctx, userID); lerr != nil {
		log.Warn("online latest watch", "err", lerr, "user", userID)
	} else if ok {
		latest = &row
	}
	devices, derr := b.online.Devices(ctx, userID, span.mins)
	if derr != nil {
		log.Warn("online devices", "err", derr, "user", userID)
		devices = nil
	}
	return person, entries, total, latest, devices, logPage, nil
}

func clampWatchPage(page int, total int64) int {
	pages := watchPageCount(total)
	if page < 0 {
		return 0
	}
	if page >= pages {
		return pages - 1
	}
	return page
}

func watchPageCount(total int64) int {
	if total <= 0 {
		return 1
	}
	n := int((total + int64(onlineWatchPage) - 1) / int64(onlineWatchPage))
	if n < 1 {
		return 1
	}
	return n
}

func onlineUserKeyboard(userID int64, listPage, logPage int, total int64, span onlineSpan, locale string) tg.InlineKeyboard {
	kb := tg.InlineKeyboard{}
	if nav := onlineWatchPager(userID, listPage, logPage, total, span, locale); len(nav) > 0 {
		kb.Rows = append(kb.Rows, nav)
	}
	kb.Rows = append(kb.Rows, []tg.InlineButton{
		{Text: onlineBtnBack.pick(locale), Data: onlinePageData(listPage, span)},
		{Text: reqBtnHome.pick(locale), Data: cbMenuHome},
	})
	return kb
}

func onlineWatchPager(userID int64, listPage, logPage int, total int64, span onlineSpan, locale string) []tg.InlineButton {
	var nav []tg.InlineButton
	if logPage > 0 {
		nav = append(nav, tg.InlineButton{
			Text: onlinePrev.pick(locale), Data: onlineUserData(userID, listPage, logPage-1, span),
		})
	}
	if int64(logPage+1)*int64(onlineWatchPage) < total {
		nav = append(nav, tg.InlineButton{
			Text: onlineNext.pick(locale), Data: onlineUserData(userID, listPage, logPage+1, span),
		})
	}
	return nav
}

type onlineWatchView struct {
	Person  onlineview.Person
	Entries []onlineview.Entry
	Total   int64
	Page    int
	Devices []onlineview.Device
	Latest  *onlineview.Entry
}

func formatOnlineUser(locale string, view onlineWatchView, now time.Time) string {
	current := playingNow(view.Latest, now)
	var b strings.Builder
	b.WriteString(onlineUserHeader(locale, view.Person, current, view.Devices))
	b.WriteString("\n\n")
	b.WriteString(onlineHistory.pick(locale))
	if len(view.Entries) == 0 {
		b.WriteString("\n")
		b.WriteString(onlineNoWatch.pick(locale))
		return b.String()
	}
	first := view.Page*int(onlineWatchPage) + 1
	last := first + len(view.Entries) - 1
	fmt.Fprintf(&b, "  %d–%d / %d", first, last, view.Total)
	for i, e := range view.Entries {
		writeWatchLine(&b, first+i, e, current, locale)
	}
	return b.String()
}

func onlineUserHeader(locale string, person onlineview.Person, current *onlineview.Entry, devices []onlineview.Device) string {
	var b strings.Builder
	fmt.Fprintf(&b, "👤 <b>%s</b>", html.EscapeString(personHeading(person)))
	if name := strings.TrimSpace(person.Name); name != "" {
		if u := strings.TrimSpace(person.Username); u != "" {
			fmt.Fprintf(&b, " (@%s)", html.EscapeString(u))
		}
	}
	b.WriteString("\n\n")
	if current == nil {
		b.WriteString(onlineIdle.pick(locale))
	} else {
		fmt.Fprintf(&b, "%s\n%s", onlineNow.pick(locale), tmdbAnchor(*current))
		if sub := watchSubline(*current, locale); sub != "" {
			fmt.Fprintf(&b, "\n%s", html.EscapeString(sub))
		}
	}
	if line := deviceSummary(deviceCodes(devices), locale); line != "" {
		fmt.Fprintf(&b, "\n\n📱 %s", html.EscapeString(line))
	}
	return b.String()
}

// watchZone is Iran, year-round +03:30 (no DST since 2022). Fixed so Windows tests
// don't depend on the zoneinfo database.
var watchZone = time.FixedZone("IRST", 3*3600+30*60)

func watchStamp(at time.Time) string {
	if at.IsZero() {
		return ""
	}
	return at.In(watchZone).Format("2006-01-02 15:04")
}

func writeWatchLine(b *strings.Builder, n int, e onlineview.Entry, current *onlineview.Entry, locale string) {
	mark := ""
	if current != nil && sameEntry(e, *current) {
		mark = " ▶️"
	}
	if stamp := watchStamp(e.At); stamp != "" {
		fmt.Fprintf(b, "\n\n%d. %s · %s%s", n, stamp, tmdbAnchor(e), mark)
	} else {
		fmt.Fprintf(b, "\n\n%d. %s%s", n, tmdbAnchor(e), mark)
	}
	if sub := watchSubline(e, locale); sub != "" {
		fmt.Fprintf(b, "\n%s", html.EscapeString(sub))
	}
}

func deviceCodes(devices []onlineview.Device) []string {
	out := make([]string, 0, len(devices))
	for _, d := range devices {
		out = append(out, d.Platform)
	}
	return out
}

func deviceSummary(codes []string, locale string) string {
	var parts []string
	seen := map[string]struct{}{}
	for _, code := range codes {
		label := deviceLabel(code, locale)
		if label == "" {
			continue
		}
		if _, ok := seen[label]; ok {
			continue
		}
		seen[label] = struct{}{}
		parts = append(parts, label)
	}
	return strings.Join(parts, " · ")
}

func deviceLabel(code, locale string) string {
	switch code {
	case "android_new", "android_old":
		return "Android"
	case "android_tv":
		if locale == "fa" {
			return "تلویزیون"
		}
		return "Android TV"
	case "windows":
		return "Windows"
	case "linux":
		return "Linux"
	case "macos":
		return "macOS"
	case "ios":
		return "iOS"
	default:
		return ""
	}
}

func tmdbAnchor(e onlineview.Entry) string {
	title := e.ItemTitle
	if title == "" {
		title = "?"
	}
	title = clipRunes(title, 80)
	safe := html.EscapeString(title)
	if e.TMDBID <= 0 {
		return safe
	}
	path := "movie"
	if e.Kind == int16(catalog.KindSeries) {
		path = "tv"
	}
	return fmt.Sprintf("<a href=\"https://www.themoviedb.org/%s/%d\">%s</a>", path, e.TMDBID, safe)
}

func watchSubline(e onlineview.Entry, locale string) string {
	progress := watchProgress(e, locale)
	if e.Kind != int16(catalog.KindSeries) || (e.SeasonNo == 0 && e.EpisodeNo == 0) {
		return progress
	}
	line := fmt.Sprintf("S%02dE%02d", e.SeasonNo, e.EpisodeNo)
	if ep := strings.TrimSpace(e.EpisodeTitle); ep != "" {
		line += " · " + clipRunes(ep, 40)
	}
	if progress != "" {
		line += " · " + progress
	}
	return line
}

func personHeading(p onlineview.Person) string {
	if name := strings.TrimSpace(p.Name); name != "" {
		return name
	}
	if u := strings.TrimSpace(p.Username); u != "" {
		return "@" + u
	}
	return strconv.FormatInt(p.ID, 10)
}

func playingNow(latest *onlineview.Entry, now time.Time) *onlineview.Entry {
	if latest == nil {
		return nil
	}
	cutoff := now.Add(-time.Duration(onlineWindowMins) * time.Minute)
	if latest.At.Before(cutoff) {
		return nil
	}
	return latest
}

func sameEntry(a, b onlineview.Entry) bool {
	return a.At.Equal(b.At) && a.ItemTitle == b.ItemTitle && a.SeasonNo == b.SeasonNo && a.EpisodeNo == b.EpisodeNo
}

func watchProgress(e onlineview.Entry, locale string) string {
	if e.Done {
		if locale == "fa" {
			return "تمام"
		}
		return "done"
	}
	return formatClock(e.PositionS)
}

func formatClock(sec int32) string {
	if sec < 0 {
		sec = 0
	}
	h := sec / 3600
	m := (sec % 3600) / 60
	s := sec % 60
	if h > 0 {
		return fmt.Sprintf("%d:%02d:%02d", h, m, s)
	}
	return fmt.Sprintf("%d:%02d", m, s)
}

func clipRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	if n < 1 {
		return ""
	}
	return string(r[:n-1]) + "…"
}
