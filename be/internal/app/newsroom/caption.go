package newsroom

import (
	"fmt"
	"html"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Aryan-mor/sushi-study/be/internal/core/catalog"
)

// captionMax is Telegram's limit for a photo caption. The post is one photo with one caption, so
// this is the whole budget (ADR 0017 §6).
const captionMax = 1024

// posterSize is what the app itself asks for on a detail header (docs/12 §5.1) — no reason a channel
// post should differ, and it is already cached at that size for anyone who opened the title.
const posterSize = "w500"

// maxGenres caps the genre line. Four is where TMDB stops being informative and starts listing.
const maxGenres = 4

// PosterURL turns a stored TMDB path into a fetchable URL at the size a post wants. Empty path
// yields empty, which the caller reads as "send text, not a photo". image.tmdb.org is the one
// third-party host we may touch (R-SEC-12, docs/10 Q16).
//
// catalog.PosterURL does the actual work, and has to: the stored poster is a bare file stem, so the
// extension must be put back or the URL is a 404 that Telegram fails silently on (docs/13 §5.2).
func PosterURL(path string) string {
	return catalog.PosterURL(path, posterSize)
}

// FormatCaption is the photo-caption fallback when sendRichMessage is refused: Persian HTML that
// fits Telegram's 1024-rune photo caption. The channel itself is FormatPost (rich HTML).
func FormatCaption(a Announcement) string {
	head := postHead(a)
	tags := postHashtags(a)

	for budget := 320; budget >= 60; budget -= 40 {
		caption := joinBlocks(head, escapeTruncate(a.Overview, budget), postClosing, tags)
		if utf8.RuneCountInString(caption) <= captionMax {
			return caption
		}
	}
	return joinBlocks(head, "", postClosing, tags)
}

// postClosing is the sanctioned sentence from ADR 0004: it points at Sushi without naming a bot, a
// handle or a link of ours. The watch-in-app button is a t.me deep link (ADR 0022), not this line.
const postClosing = "سوشی رو باز کن و تماشا کن ✨"

// brandMention is the prototype's 🦁 @handle: main-bot's getMe username. Empty handle omits the lion
// line; we do not invent #Sushi. Explicit R-VOICE-1 exception for the news footer (ADR 0017 §6).
func brandMention(handle string) string {
	h := strings.TrimPrefix(strings.TrimSpace(handle), "@")
	if h == "" {
		return ""
	}
	return "@" + h
}

func lionLine(a Announcement) string {
	if m := brandMention(a.Handle); m != "" {
		return "🦁 " + m
	}
	return ""
}

func postHashtags(a Announcement) string {
	var lines []string
	if lion := lionLine(a); lion != "" {
		lines = append(lines, lion)
	}
	if tag := mediaHashtag(a.Title); tag != "" {
		lines = append(lines, "🌞 "+tag)
	}
	return strings.Join(lines, "\n")
}

func postHead(a Announcement) string {
	var b strings.Builder

	if a.Kind == catalog.KindSeries {
		b.WriteString("📺 <b>قسمت جدید</b>\n\n")
	} else {
		b.WriteString("🎬 <b>فیلم جدید اضافه شد</b>\n\n")
	}

	title := strings.TrimSpace(a.Title)
	if title == "" {
		title = fmt.Sprintf("TMDB %s", persianDigits(int64(a.TMDBID)))
	}
	fmt.Fprintf(&b, "<b>%s</b>", tmdbLink(a.Kind, a.TMDBID, title))

	if line := episodeLine(a); line != "" {
		b.WriteString("\n")
		b.WriteString(line)
	}
	if a.Year > 0 {
		fmt.Fprintf(&b, "\n📅 %s", persianDigits(int64(a.Year)))
	}
	if g := genreLine(a.Genres); g != "" {
		fmt.Fprintf(&b, "\n🎭 %s", g)
	}
	if m := metaLine(a); m != "" {
		b.WriteString("\n")
		b.WriteString(m)
	}
	return b.String()
}

// episodeLine names the season and episode, and the episode's own title when it has a real one.
func episodeLine(a Announcement) string {
	if a.Kind != catalog.KindSeries {
		return ""
	}
	line := fmt.Sprintf("فصل %s · قسمت %s",
		persianDigits(int64(a.Season)), persianDigits(int64(a.Episode)))
	if sub := strings.TrimSpace(a.Subtitle); sub != "" && !strings.EqualFold(sub, a.Title) {
		line += " — " + html.EscapeString(sub)
	}
	return line
}

// metaLine is the rating / runtime row, joined with the separator the rest of our Persian copy uses.
// Rating arrives as TMDB's 0-100 (catalog.proto Row.rating) and is shown back as x.y out of ten,
// because that is the number people recognise.
func metaLine(a Announcement) string {
	var parts []string
	if a.Rating > 0 {
		parts = append(parts, fmt.Sprintf("⭐ %s/%s",
			persianDecimal(int64(a.Rating)), persianDigits(10)))
	}
	if a.RuntimeS > 0 {
		if mins := a.RuntimeS / 60; mins > 0 {
			parts = append(parts, fmt.Sprintf("⏱ %s دقیقه", persianDigits(int64(mins))))
		}
	}
	return strings.Join(parts, " · ")
}

// genreLine reuses catalog.SplitGenres rather than splitting the display string again here: the
// separator the writers use has changed once already, and that helper is where that knowledge lives.
func genreLine(genres string) string {
	names := catalog.SplitGenres(genres)
	if len(names) == 0 {
		return ""
	}
	if len(names) > maxGenres {
		names = names[:maxGenres]
	}
	escaped := make([]string, 0, len(names))
	for _, n := range names {
		escaped = append(escaped, html.EscapeString(n))
	}
	return strings.Join(escaped, "، ")
}

func genrePlain(genres string) string {
	names := catalog.SplitGenres(genres)
	if len(names) == 0 {
		return ""
	}
	if len(names) > maxGenres {
		names = names[:maxGenres]
	}
	return strings.Join(names, "، ")
}

func tmdbLink(kind catalog.Kind, tmdbID uint64, label string) string {
	path := "movie"
	if kind == catalog.KindSeries {
		path = "tv"
	}
	// themoviedb.org is not a domain of ours, so R-SEC-14 does not reach it — the same link the
	// admin request panel already prints.
	return fmt.Sprintf("<a href=\"https://www.themoviedb.org/%s/%d\">%s</a>",
		path, tmdbID, html.EscapeString(label))
}

// youtubeTrailerLink is one clickable «تریلر» when items.detail has a YouTube id.
// Empty and catalog.TrailerNone omit the line. The key is restricted to the YouTube id alphabet
// so a bad stored value cannot break out of the href.
func youtubeTrailerLink(key string) string {
	key = strings.TrimSpace(key)
	if key == "" || key == catalog.TrailerNone || !youtubeKeyOK(key) {
		return ""
	}
	return fmt.Sprintf("<a href=\"https://www.youtube.com/watch?v=%s\">تریلر</a>", html.EscapeString(key))
}

func youtubeKeyOK(key string) bool {
	if len(key) < 6 || len(key) > 20 {
		return false
	}
	for _, r := range key {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '_', r == '-':
		default:
			return false
		}
	}
	return true
}

func joinBlocks(blocks ...string) string {
	out := make([]string, 0, len(blocks))
	for _, b := range blocks {
		if strings.TrimSpace(b) != "" {
			out = append(out, b)
		}
	}
	return strings.Join(out, "\n\n")
}

// escapeTruncate cuts to a rune budget and then escapes, in that order so an entity is never split
// in half. Truncation counts runes, not bytes: Persian is three bytes a character, and a byte-wise
// cut lands mid-rune and renders as a replacement glyph (which is what the prototype's
// truncatePlainText did — see ADR 0017, "what is changed on the way in").
func escapeTruncate(s string, budget int) string {
	s = strings.TrimSpace(s)
	if s == "" || budget <= 0 {
		return ""
	}
	if utf8.RuneCountInString(s) <= budget {
		return html.EscapeString(s)
	}
	if budget == 1 {
		return "…"
	}
	runes := []rune(s)
	return html.EscapeString(strings.TrimRight(string(runes[:budget-1]), " ")) + "…"
}

// persianDigits prints a count with English digits. Posts stay Persian; the numerals do not.
func persianDigits(n int64) string {
	return fmt.Sprintf("%d", n)
}

// persianDecimal renders n/10 to one decimal place — TMDB's 0-100 rating as 7.8.
func persianDecimal(n int64) string {
	return fmt.Sprintf("%.1f", float64(n)/10)
}

// FormatPreview wraps the exact post in a short staff header. Staff read the header in their own
// language and the post in the language it will be published in — showing them a translated preview
// would be showing them something we are not about to send (ADR 0017 §8).
func FormatPreview(a Announcement, locale string) string {
	return wrapPreview(a, locale, FormatPost(a))
}

// FormatPreviewWithHero is the staff DM when a hero JPEG will be attached, so they see slide 0.
func FormatPreviewWithHero(a Announcement, locale string) string {
	return wrapPreview(a, locale, FormatPostWithHero(a))
}

func wrapPreview(a Announcement, locale, post string) string {
	header := previewHeaderEN
	if strings.HasPrefix(locale, "fa") {
		header = previewHeaderFA
	}
	return "<p>" + header + "</p>\n" + post + "<p>" + previewFooter(a, locale) + "</p>\n"
}

// FormatPreviewCaption is the photo/text fallback: Telegram's ordinary HTML caption rejects <p>
// and <tg-slideshow>, which is the error that left three due rows retrying every 45s.
func FormatPreviewCaption(a Announcement, locale string) string {
	header := previewHeaderEN
	if strings.HasPrefix(locale, "fa") {
		header = previewHeaderFA
	}
	return header + "\n\n" + FormatCaption(a) + "\n\n" + previewFooter(a, locale)
}

const (
	previewHeaderEN = "📰 <b>Ready to announce</b> — approve to post it to the channel."
	previewHeaderFA = "📰 <b>آماده‌ی اعلام</b> — تأیید کن تا در کانال منتشر شود."
)

// previewFooter carries what the post itself deliberately hides: the ids and the moment it became
// playable. R-VOICE-1 governs what a *person* reads about the system; staff reviewing a queue are
// looking at the system on purpose.
func previewFooter(a Announcement, locale string) string {
	when := "—"
	if !a.PremiereOn.IsZero() {
		when = a.PremiereOn.Format(time.DateOnly)
	}
	if strings.HasPrefix(locale, "fa") {
		return fmt.Sprintf("<code>#%d</code> · TMDB <code>%d</code> · اکران: %s", a.ID, a.TMDBID, when)
	}
	return fmt.Sprintf("<code>#%d</code> · TMDB <code>%d</code> · premiere: %s", a.ID, a.TMDBID, when)
}
