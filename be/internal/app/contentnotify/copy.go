package contentnotify

import (
	"fmt"
	"html"
	"strings"

	"github.com/Aryan-mor/sushi-study/be/internal/core/catalog"
)

// message is one string in both locales, picked per recipient (R-I18N-10). Same shape as
// main-bot's copy.go and declared again here rather than shared: a cmd package cannot be
// imported, and a six-line type costs less than an i18n package with two callers.
//
// These DMs used to carry Persian and English in the same body, because the reconciler did not
// know who it was writing to. It does now — every notify query selects the recipient's locale —
// so each person reads one language instead of skimming past the other.
type message struct{ en, fa string }

func (m message) pick(locale string) string {
	if locale == "fa" {
		return m.fa
	}
	return m.en
}

// The three notifications this reconciler sends, each as header / footer around a shared title
// card. Warm, second person, and never "type this" (doc 13 §5). The footer says where to go
// next, which for us is always the app — no bot button can open it (R-SEC-14 rules out a link to
// a domain of ours, and Telegram rejects a custom scheme in a url button).
var (
	readyHeader = message{
		en: "✅ <b>Ready to watch</b>",
		fa: "✅ <b>آماده‌ی تماشاست</b>",
	}
	readyFooter = message{
		en: "It's in the catalog now — open Sushi and press play.",
		fa: "حالا تو کاتالوگه — سوشی رو باز کن و پخش رو بزن.",
	}
	episodeHeader = message{
		en: "🆕 <b>New episode</b>",
		fa: "🆕 <b>قسمت جدید</b>",
	}
	episodeFooter = message{
		en: "Open Sushi to pick up where you left off.",
		fa: "سوشی رو باز کن و از همون‌جا ادامه بده.",
	}
	requestHeader = message{
		en: "📥 <b>New request</b>",
		fa: "📥 <b>درخواست جدید</b>",
	}
	// Channel-post close lines (ADR 0023). The inbox is Persian; DMs keep pick(locale).
	closedAdded    = "✅ اضافه شد"
	closedRejected = "❌ رد شد"
	// moreEpisodes is the tail of a coalesced follow DM: the debounce window turned a 20-episode
	// import into one message, and this is how it admits to the other 19.
	moreEpisodes = message{
		en: "+%d more",
		fa: "+%d تای دیگه",
	}
)

// maxTitleRunes bounds the one unbounded field a card carries.
//
// Bounding the input is what keeps a rendered card inside Telegram's 1024-character caption
// limit. Clamping the finished HTML instead would risk cutting through a tag or a character
// entity, and Telegram rejects the whole send when it cannot parse the markup — so a long title
// would not truncate, it would silently lose the notification.
const maxTitleRunes = 120

// maxNameRunes does the same for a requester's display name, which is theirs to choose.
const maxNameRunes = 64

func kindIcon(k catalog.Kind) string {
	if k == catalog.KindSeries {
		return "📺"
	}
	return "🎬"
}

// titleOr renders the title in bold-ready escaped HTML, falling back to the TMDB id so a card for
// a title we never hydrated still says which title it is.
func titleOr(title string, tmdbID uint64) string {
	if t := strings.TrimSpace(title); t != "" {
		return html.EscapeString(clampRunes(t, maxTitleRunes))
	}
	return fmt.Sprintf("TMDB %d", tmdbID)
}

// titleCard is the block every notification shares: kind emoji and bold title, then the meta line
// under it when there is anything to put there.
func titleCard(kind catalog.Kind, title string, tmdbID uint64, year, rating int32) string {
	card := kindIcon(kind) + " <b>" + titleOr(title, tmdbID) + "</b>"
	if meta := metaLine(year, rating); meta != "" {
		card += "\n" + meta
	}
	return card
}

// metaLine is what is worth one glance: the year, then the TMDB score out of ten.
//
// Both are missing for about a quarter of the catalog (doc 12 §7.1), so each is dropped rather
// than printed as a zero, and a card with neither loses the line altogether instead of showing an
// empty one. rating is stored scaled to 0-100 (doc 12 §1), hence the tenth.
func metaLine(year, rating int32) string {
	var parts []string
	if year > 0 {
		parts = append(parts, fmt.Sprintf("📅 %d", year))
	}
	if rating > 0 {
		parts = append(parts, fmt.Sprintf("⭐ %.1f", float64(rating)/10))
	}
	return strings.Join(parts, " · ")
}

// formatRequesterNotice is the DM a requester gets once the title they asked for can be played.
func formatRequesterNotice(n RequesterNotice) string {
	return joinBlocks(
		readyHeader.pick(n.Locale),
		titleCard(n.Kind, n.Title, n.TMDBID, n.Year, n.Rating),
		readyFooter.pick(n.Locale),
	)
}

// formatFollowNotice names the latest episode and, when the debounce coalesced several, how many
// more came with it.
func formatFollowNotice(n FollowNotice) string {
	facts := []string{fmt.Sprintf("<code>S%02dE%02d</code>", n.MaxSeason, n.MaxEp)}
	if n.NewCount > 1 {
		facts = append(facts, fmt.Sprintf(moreEpisodes.pick(n.Locale), n.NewCount-1))
	}
	// The episode is the news here, so it takes the meta line's place; the year trails it.
	if year := metaLine(n.Year, 0); year != "" {
		facts = append(facts, year)
	}
	body := kindIcon(catalog.KindSeries) + " <b>" + titleOr(n.Title, n.TMDBID) + "</b>\n" +
		strings.Join(facts, " · ")

	return joinBlocks(episodeHeader.pick(n.Locale), body, episodeFooter.pick(n.Locale))
}

// formatAdminAlert is the caption under the poster (or the whole DM when there is no poster).
// Written in the reading admin's locale, which is why it takes one.
func formatAdminAlert(a AdminAlert, locale string) string {
	return joinBlocks(
		requestHeader.pick(locale),
		titleCard(a.Kind, a.Title, a.TMDBID, a.Year, a.Rating),
		formatSubtitlePref(a.SubtitleLang, locale),
		formatRequester(a),
		idLinks(a),
	)
}

// formatSubtitlePref is the requester's subtitle language. Never asked is a dash, not a blank
// line — staff still need to see that nobody chose one.
func formatSubtitlePref(lang, locale string) string {
	label := "Subtitles"
	if locale == "fa" {
		label = "زیرنویس"
	}
	code := strings.TrimSpace(lang)
	if code == "" {
		code = "-"
	}
	return label + ": <code>" + html.EscapeString(code) + "</code>"
}

// formatRequester is the "who asked" line: display name in bold, @username as a t.me link when
// there is one, Telegram id always, monospace. No locale — a name is a name.
func formatRequester(a AdminAlert) string {
	var b strings.Builder
	b.WriteString("👤 ")
	if n := strings.TrimSpace(a.RequesterName); n != "" {
		b.WriteString("<b>" + html.EscapeString(clampRunes(n, maxNameRunes)) + "</b> ")
	}
	if u := strings.TrimPrefix(strings.TrimSpace(a.RequesterUsername), "@"); u != "" {
		e := html.EscapeString(u)
		fmt.Fprintf(&b, "<a href=\"https://t.me/%s\">@%s</a> ", e, e)
	}
	fmt.Fprintf(&b, "· <code>%d</code>", a.RequesterTG)
	return b.String()
}

// idLinks is the admin's way out to the sources: the labels are links, the ids are monospace so a
// tap copies them. IMDB is dropped when the request carries no id.
func idLinks(a AdminAlert) string {
	tmdbPath := "movie"
	if a.Kind == catalog.KindSeries {
		tmdbPath = "tv"
	}
	ids := fmt.Sprintf(
		"<a href=\"https://www.themoviedb.org/%s/%d\">TMDB</a>: <code>%d</code>",
		tmdbPath, a.TMDBID, a.TMDBID,
	)
	if imdb := strings.TrimSpace(a.IMDBID); imdb != "" {
		e := html.EscapeString(imdb)
		ids += fmt.Sprintf(
			"\n<a href=\"https://www.imdb.com/title/%s/\">IMDB</a>: <code>%s</code>",
			e, e,
		)
	}
	return ids
}

// joinBlocks stacks the parts of a card with a blank line between them, dropping the ones that
// came out empty so a missing block never leaves a double gap.
func joinBlocks(blocks ...string) string {
	kept := make([]string, 0, len(blocks))
	for _, b := range blocks {
		if strings.TrimSpace(b) != "" {
			kept = append(kept, b)
		}
	}
	return strings.Join(kept, "\n\n")
}

// clampRunes cuts to at most limit runes, marking the cut. Runes, not bytes: Telegram measures a
// caption in characters, and a Persian title spends two bytes on each one.
func clampRunes(s string, limit int) string {
	r := []rune(s)
	if len(r) <= limit {
		return s
	}
	return strings.TrimSpace(string(r[:limit-1])) + "…"
}
