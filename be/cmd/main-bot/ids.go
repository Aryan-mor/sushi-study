package main

import (
	"context"
	"fmt"
	"html"
	"log/slog"
	"regexp"
	"strconv"
	"strings"

	"github.com/Aryan-mor/sushi-study/be/internal/adapter/tg"
	"github.com/Aryan-mor/sushi-study/be/internal/app/identity"
	"github.com/Aryan-mor/sushi-study/be/internal/core/catalog"
)

// idCatalog is the TMDB slice the admin converter needs (R-GO-5).
type idCatalog interface {
	TitlePage(ctx context.Context, tmdbID uint64, kind catalog.Kind) (catalog.Item, error)
	FindByIMDB(ctx context.Context, imdbID string) (catalog.Item, error)
}

type idQuery struct {
	imdb   string
	tmdb   uint64
	forced catalog.Kind
}

var (
	idsBtn   = message{en: "🔄 TMDB ↔ IMDb", fa: "🔄 TMDB ↔ IMDb"}
	idsTitle = message{
		en: "🔄 <b>TMDB ↔ IMDb</b>\n\nReply to the next message with a TMDB or IMDb id — or either site's URL. I'll convert it the other way.",
		fa: "🔄 <b>TMDB ↔ IMDb</b>\n\nآیدی یا لینک TMDB یا IMDb رو به پیام بعدی ریپلای کن. برعکسش رو می‌دم.",
	}
	idsPrompt = message{
		en: "Paste a TMDB number, an IMDb <code>tt…</code> id, or a URL.",
		fa: "عدد TMDB، شناسهٔ IMDb شبیه <code>tt…</code>، یا لینک رو بفرست.",
	}
	idsLooking = message{en: "🔄 Looking that up…", fa: "🔄 دارم پیدا می‌کنم…"}
	idsBad     = message{
		en: "That doesn't look like a TMDB or IMDb id. Use a number, <code>tt…</code>, or a URL, and <b>reply</b> here.",
		fa: "این شبیه آیدی TMDB یا IMDb نیست. عدد، <code>tt…</code>، یا لینک بفرست و اینجا <b>ریپلای</b> کن.",
	}
	idsNone = message{
		en: "Nothing matched that id. Reply with another, or cancel.",
		fa: "چیزی با این آیدی پیدا نشد. یکی دیگه ریپلای کن، یا لغو کن.",
	}
	idsFailed = message{
		en: "😅 TMDB didn't answer. Try again in a moment.",
		fa: "😅 TMDB جواب نداد. یه لحظه بعد دوباره امتحان کن.",
	}
	idsNoIMDB = message{
		en: "TMDB has no IMDb id for this title.",
		fa: "TMDB برای این عنوان شناسهٔ IMDb نداره.",
	}
	idsAgainBtn   = message{en: "🔄 Convert another", fa: "🔄 یکی دیگه"}
	idsCancelBtn  = message{en: "❌ Cancel", fa: "❌ لغو"}
	idsKindMovie  = message{en: "movie", fa: "فیلم"}
	idsKindSeries = message{en: "series", fa: "سریال"}
)

const idsPlaceholder = "tt0137523 or 550"

var (
	reIDsIMDBURL  = regexp.MustCompile(`(?i)imdb\.com/title/(tt\d+)`)
	reIDsIMDBBare = regexp.MustCompile(`(?i)\b(tt\d{5,})\b`)
	reIDsTMDBURL  = regexp.MustCompile(`(?i)themoviedb\.org/(movie|tv)/(\d{1,9})`)
	reIDsTMDBTag  = regexp.MustCompile(`(?i)(?:tmdb|themoviedb)[^0-9]{0,12}?(\d{2,9})`)
)

func (b *bot) rememberIDWait(chatID int64) {
	if b.waitingIDs == nil {
		b.waitingIDs = map[int64]bool{}
	}
	b.waitingIDs[chatID] = true
}

func (b *bot) clearIDWait(chatID int64) {
	if b.waitingIDs != nil {
		delete(b.waitingIDs, chatID)
	}
}

func (b *bot) awaitingIDs(chatID int64) bool {
	return b.waitingIDs != nil && b.waitingIDs[chatID]
}

func idsCancelKeyboard(locale string) tg.InlineKeyboard {
	return tg.InlineKeyboard{Rows: [][]tg.InlineButton{
		{{Text: idsCancelBtn.pick(locale), Data: cbIDsCancel}},
	}}
}

func idsResultKeyboard(locale string) tg.InlineKeyboard {
	return tg.InlineKeyboard{Rows: [][]tg.InlineButton{
		{{Text: idsAgainBtn.pick(locale), Data: cbMenuIDs}},
		{
			{Text: adminBtnBack.pick(locale), Data: cbMenuAdmin},
			{Text: reqBtnHome.pick(locale), Data: cbMenuHome},
		},
	}}
}

func (b *bot) routeIDsCallback(
	ctx context.Context, q *tg.CallbackQuery, s screen, user identity.User, locale string, log *slog.Logger,
) bool {
	switch q.Data {
	case cbMenuIDs:
		if !isAdmin(user.Role) {
			b.toast(ctx, q, reqToastDenied.pick(locale))
			return true
		}
		b.toast(ctx, q, "")
		b.startIDConvert(ctx, s, locale, log)
		return true
	case cbIDsCancel:
		if !isAdmin(user.Role) {
			b.toast(ctx, q, reqToastDenied.pick(locale))
			return true
		}
		b.toast(ctx, q, "")
		b.cancelIDConvert(ctx, s, user.Role, locale, log)
		return true
	default:
		return false
	}
}

func (b *bot) startIDConvert(ctx context.Context, s screen, locale string, log *slog.Logger) {
	b.clearGeminiWait(s.chatID)
	b.clearNewsWait(s.chatID)
	b.show(ctx, s, idsTitle.pick(locale), idsCancelKeyboard(locale), log)
	b.sendIDForceReply(ctx, s.chatID, locale, log)
}

func (b *bot) cancelIDConvert(ctx context.Context, s screen, role, locale string, log *slog.Logger) {
	b.clearIDWait(s.chatID)
	b.dropMessages(ctx, s.chatID)
	b.showAdminMenu(ctx, s, locale, log)
}

func (b *bot) sendIDForceReply(ctx context.Context, chatID int64, locale string, log *slog.Logger) {
	msg, err := b.client.SendForceReplyHTML(ctx, chatID, idsPrompt.pick(locale), idsPlaceholder)
	if err != nil {
		log.Error("id convert prompt", "err", err)
		b.show(ctx, screen{chatID: chatID}, failed.pick(locale), staffNavKeyboard(locale), log)
		return
	}
	b.rememberPrompt(chatID, msg.ID)
	b.rememberIDWait(chatID)
}

func (b *bot) handlePastedIDs(ctx context.Context, s screen, user identity.User, in inbound, locale string, log *slog.Logger) {
	b.clearIDWait(s.chatID)
	b.dropMessages(ctx, s.chatID, in.msgID, in.replyToID)
	if !isAdmin(user.Role) {
		b.show(ctx, s, unknown.pick(locale), homeKeyboardFor(locale, user.Role), log)
		return
	}
	q, ok := parseIDQuery(in.rest)
	if !ok {
		b.show(ctx, screen{chatID: s.chatID}, idsBad.pick(locale), idsCancelKeyboard(locale), log)
		b.sendIDForceReply(ctx, s.chatID, locale, log)
		return
	}
	pending, err := b.client.SendKeyboard(ctx, s.chatID, idsLooking.pick(locale), idsCancelKeyboard(locale))
	if err != nil {
		log.Error("id convert pending", "err", err)
		b.show(ctx, screen{chatID: s.chatID}, idsFailed.pick(locale), idsResultKeyboard(locale), log)
		return
	}
	out := screen{chatID: s.chatID, messageID: pending.ID}
	items, err := lookupIDs(ctx, b.ids, q)
	if err != nil {
		log.Error("id convert lookup", "err", err)
		b.show(ctx, out, idsFailed.pick(locale), idsResultKeyboard(locale), log)
		return
	}
	if len(items) == 0 {
		b.show(ctx, out, idsNone.pick(locale), idsCancelKeyboard(locale), log)
		b.sendIDForceReply(ctx, s.chatID, locale, log)
		return
	}
	b.show(ctx, out, formatIDResult(locale, items), idsResultKeyboard(locale), log)
}

func parseIDQuery(raw string) (idQuery, bool) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return idQuery{}, false
	}
	if m := reIDsIMDBURL.FindStringSubmatch(s); len(m) == 2 {
		return idQuery{imdb: strings.ToLower(m[1])}, true
	}
	if m := reIDsIMDBBare.FindStringSubmatch(s); len(m) == 2 {
		return idQuery{imdb: strings.ToLower(m[1])}, true
	}
	if m := reIDsTMDBURL.FindStringSubmatch(s); len(m) == 3 {
		id, err := strconv.ParseUint(m[2], 10, 64)
		if err != nil || id == 0 {
			return idQuery{}, false
		}
		forced := catalog.KindMovie
		if strings.EqualFold(m[1], "tv") {
			forced = catalog.KindSeries
		}
		return idQuery{tmdb: id, forced: forced}, true
	}
	if m := reIDsTMDBTag.FindStringSubmatch(s); len(m) == 2 {
		id, err := strconv.ParseUint(m[1], 10, 64)
		if err != nil || id == 0 {
			return idQuery{}, false
		}
		return idQuery{tmdb: id}, true
	}
	if id, err := strconv.ParseUint(s, 10, 64); err == nil && id > 0 && id <= 999_999_999 {
		return idQuery{tmdb: id}, true
	}
	return idQuery{}, false
}

func lookupIDs(ctx context.Context, api idCatalog, q idQuery) ([]catalog.Item, error) {
	if api == nil {
		return nil, fmt.Errorf("tmdb client missing")
	}
	if q.imdb != "" {
		item, err := api.FindByIMDB(ctx, q.imdb)
		if err != nil {
			return nil, err
		}
		if item.TMDBID == 0 {
			return nil, nil
		}
		if item.IMDBID == "" {
			item.IMDBID = q.imdb
		}
		return []catalog.Item{item}, nil
	}
	kinds := []catalog.Kind{catalog.KindMovie, catalog.KindSeries}
	switch q.forced {
	case catalog.KindMovie:
		kinds = []catalog.Kind{catalog.KindMovie}
	case catalog.KindSeries:
		kinds = []catalog.Kind{catalog.KindSeries}
	case catalog.KindUnknown:
	}
	var out []catalog.Item
	for _, k := range kinds {
		item, err := api.TitlePage(ctx, q.tmdb, k)
		if err != nil {
			return nil, err
		}
		if item.TMDBID == 0 || item.Title == "" {
			continue
		}
		item.Kind = k
		out = append(out, item)
	}
	return out, nil
}

func formatIDResult(locale string, items []catalog.Item) string {
	parts := make([]string, 0, len(items))
	for _, item := range items {
		parts = append(parts, formatIDItem(locale, item))
	}
	return strings.Join(parts, "\n\n")
}

func formatIDItem(locale string, item catalog.Item) string {
	title := html.EscapeString(strings.TrimSpace(item.Title))
	if title == "" {
		title = "—"
	}
	head := kindIcon(item.Kind) + " <b>" + title + "</b>"
	if item.Year > 0 {
		head += fmt.Sprintf(" (%d)", item.Year)
	}
	head += " · " + kindWord(item.Kind, locale)

	tmdb := fmt.Sprintf(
		"<a href=\"https://www.themoviedb.org/%s/%d\">TMDB</a>: <code>%d</code>",
		tmdbPath(item.Kind), item.TMDBID, item.TMDBID,
	)
	imdb := strings.TrimSpace(item.IMDBID)
	var body string
	if imdb != "" {
		e := html.EscapeString(imdb)
		body = tmdb + fmt.Sprintf("\n<a href=\"https://www.imdb.com/title/%s/\">IMDB</a>: <code>%s</code>", e, e)
	} else {
		body = tmdb + "\n" + idsNoIMDB.pick(locale)
	}
	return head + "\n\n" + body
}

func kindWord(k catalog.Kind, locale string) string {
	if k == catalog.KindSeries {
		return idsKindSeries.pick(locale)
	}
	return idsKindMovie.pick(locale)
}
