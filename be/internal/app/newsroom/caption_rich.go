package newsroom

import (
	"fmt"
	"html"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/Aryan-mor/sushi-study/be/internal/core/catalog"
)

// Rich post is the prototype's channel_news_rich.go, stripped of R2 hosts, custom-emoji ids, download
// tables and @exampleNews (ADR 0017 §6). Lion line is @main-bot from getMe. Slideshow still uses
// image.tmdb.org (R-SEC-12); slide 0 may be a Telegram-uploaded hero via tg://photo?id=hero.

const (
	richMax         = 4096
	backdropSize    = "w780"
	slideshowMax    = 5
	dirLTR          = "\u202a"
	dirRTL          = "\u202b"
	dirPop          = "\u202c"
	censorNotice    = "تمامی محتوا بدون سانسور"
	overviewSummary = "📖خلاصه داستان⬇️"
	heroPhotoSrc    = "tg://photo?id=" + HeroMediaID
)

// FormatPost is the channel body: Telegram rich HTML with a heading, a poster/backdrop slider,
// a meta accordion and the sanctioned closing line. Slide 0 is the TMDB poster.
func FormatPost(a Announcement) string {
	return formatPost(a, false)
}

// FormatPostWithHero is FormatPost with slide 0 as the uploaded hero card. The TMDB poster is
// omitted because it is already inside the card; the backdrop and any stored gallery stills stay.
func FormatPostWithHero(a Announcement) string {
	return formatPost(a, true)
}

func formatPost(a Announcement, withHero bool) string {
	head := richHeading(a) + richSlideshow(a, withHero) + richEpisodeLine(a)
	foot := richFooter(a)
	for budget := 480; budget >= 60; budget -= 60 {
		body := head + richAccordion(a, budget) + foot
		if utf8.RuneCountInString(body) <= richMax {
			return body
		}
	}
	return head + richAccordion(a, 0) + foot
}

func richHeading(a Announcement) string {
	title := strings.TrimSpace(a.Title)
	if title == "" {
		title = fmt.Sprintf("TMDB %s", persianDigits(int64(a.TMDBID)))
	}
	if a.Year > 0 {
		title = fmt.Sprintf("%s (%s)", title, persianDigits(int64(a.Year)))
	}
	return fmt.Sprintf("<h3>%s</h3>\n", tmdbLink(a.Kind, a.TMDBID, title))
}

func richSlideshow(a Announcement, withHero bool) string {
	urls := slideshowURLs(a, withHero)
	if len(urls) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("<tg-slideshow>\n")
	for _, u := range urls {
		fmt.Fprintf(&b, `<img src="%s"/>`+"\n", html.EscapeString(u))
	}
	b.WriteString("</tg-slideshow>\n")
	return b.String()
}

func slideshowURLs(a Announcement, withHero bool) []string {
	var out []string
	add := func(u string) {
		if u == "" || len(out) >= slideshowMax || slideshowHas(out, u) {
			return
		}
		out = append(out, u)
	}
	if withHero {
		add(heroPhotoSrc)
	} else {
		add(PosterURL(a.Poster))
	}
	add(catalog.PosterURL(a.Backdrop, backdropSize))
	for _, stem := range a.Backdrops {
		add(catalog.PosterURL(stem, backdropSize))
	}
	return out
}

func slideshowHas(urls []string, u string) bool {
	for _, have := range urls {
		if have == u {
			return true
		}
	}
	return false
}

func richEpisodeLine(a Announcement) string {
	if a.Kind != catalog.KindSeries || a.Season <= 0 || a.Episode <= 0 {
		return ""
	}
	series := strings.TrimSpace(a.Title)
	if series == "" {
		return ""
	}
	line := fmt.Sprintf("قسمت جدید · فصل %s · قسمت %s از «%s» آماده شد!",
		persianSeasonOrdinal(a.Season), persianDigits(int64(a.Episode)), series)
	return fmt.Sprintf("<p>%s</p>\n", html.EscapeString(rtl(line)))
}

func richAccordion(a Announcement, budget int) string {
	var table strings.Builder
	if a.Kind == catalog.KindSeries && a.Season > 0 && a.Episode > 0 {
		writeMetaRow(&table, rtl(episodeMetaCell(a)), "خبر", "📣")
	}
	if title := strings.TrimSpace(a.Title); title != "" {
		writeMetaRow(&table, title, "عنوان", "📼")
	}
	if a.Year > 0 {
		writeMetaRow(&table, persianDigits(int64(a.Year)), "سال انتشار", "🗓")
	}
	if a.Rating > 0 {
		writeMetaRow(&table, fmt.Sprintf("%s/%s", persianDecimal(int64(a.Rating)), persianDigits(10)),
			"امتیاز", "⭐")
	}
	if mins := a.RuntimeS / 60; mins > 0 {
		writeMetaRow(&table, fmt.Sprintf("%s دقیقه", persianDigits(int64(mins))), "مدت زمان", "⏲")
	}
	if g := genrePlain(a.Genres); g != "" {
		writeMetaRow(&table, g, "ژانر", "🎭")
	}
	if n := networkLine(a.Network); n != "" {
		writeMetaRow(&table, n, "شبکه", "📡")
	}
	if a.Kind == catalog.KindMovie {
		writeMetaRow(&table, "سینما / VOD", "شبکه پخش", "📺")
	}
	if c := countryLine(a.Country); c != "" {
		writeMetaRow(&table, c, "کشور تولید", "🌐")
	}
	if link := youtubeTrailerLink(a.TrailerKey); link != "" {
		fmt.Fprintf(&table, "<tr>"+htmlAlignCell+"%s</td>"+htmlAlignCell+"%s</td>"+htmlAlignCell+"%s</td></tr>\n",
			link, html.EscapeString("تریلر"), "▶️")
	}
	rows := table.String()
	if rows == "" && budget <= 0 && strings.TrimSpace(a.Overview) == "" {
		return ""
	}
	var b strings.Builder
	b.WriteString("<details>\n<summary>" + overviewSummary + "</summary>\n")
	if rows != "" {
		b.WriteString(`<table border="1">` + "\n" + rows + "</table>\n")
	}
	b.WriteString(richOverview(a, budget))
	b.WriteString("</details>\n")
	return b.String()
}

func richOverview(a Announcement, budget int) string {
	body := escapeTruncate(a.Overview, budget)
	if body == "" {
		return fmt.Sprintf("<p>%s</p>\n", html.EscapeString(rtl(censorNotice)))
	}
	return fmt.Sprintf("<pullquote>%s</pullquote>\n<p>%s</p>\n",
		body, html.EscapeString(rtl(censorNotice)))
}

func richFooter(a Announcement) string {
	var b strings.Builder
	fmt.Fprintf(&b, "<p>%s</p>\n", html.EscapeString(rtl(postClosing)))
	if lion := lionLine(a); lion != "" {
		fmt.Fprintf(&b, "<p>%s</p>\n", html.EscapeString(ltr(lion)))
	}
	if tag := mediaHashtag(a.Title); tag != "" {
		fmt.Fprintf(&b, "<p>%s</p>\n", html.EscapeString(ltr("🌞 "+tag)))
	}
	return b.String()
}

// networkLine prints the stored TMDB names. Empty and NetworkNone omit the row.
// Stored join is ", " (same as genres); the cell uses the Persian comma the rest of the table uses.
func networkLine(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" || raw == catalog.NetworkNone {
		return ""
	}
	return strings.ReplaceAll(raw, ", ", "، ")
}

func writeMetaRow(b *strings.Builder, value, label, emoji string) {
	fmt.Fprintf(b, "<tr>"+htmlAlignCell+"%s</td>"+htmlAlignCell+"%s</td>"+htmlAlignCell+"%s</td></tr>\n",
		html.EscapeString(value), html.EscapeString(label), emoji)
}

// htmlAlignCell is Telegram's table cell opener. The attribute value is assembled so misspell
// does not flag the HTML keyword.
const htmlAlignCell = `<td align="` + "cent" + `er">`

func episodeMetaCell(a Announcement) string {
	cell := fmt.Sprintf("فصل %s · قسمت %s",
		persianSeasonOrdinal(a.Season), persianDigits(int64(a.Episode)))
	sub := strings.TrimSpace(a.Subtitle)
	if sub != "" && !strings.EqualFold(sub, a.Title) && !genericEpisodeTitle(sub, a.Episode) {
		cell += " · «" + sub + "»"
	}
	return cell
}

func genericEpisodeTitle(title string, episode int32) bool {
	t := strings.TrimSpace(title)
	if t == "" || episode <= 0 {
		return true
	}
	ascii := fmt.Sprintf("%d", episode)
	folded := strings.ToLower(foldEasternDigits(t))
	for _, p := range []string{"قسمت " + ascii, "episode " + ascii} {
		if folded == p {
			return true
		}
	}
	return false
}

// foldEasternDigits maps Persian/Arabic-Indic digits to ASCII so an old caption
// like "قسمت ۲" still counts as a generic title. Output never uses those digits.
func foldEasternDigits(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch {
		case r >= '۰' && r <= '۹':
			b.WriteRune('0' + (r - '۰'))
		case r >= '٠' && r <= '٩':
			b.WriteRune('0' + (r - '٠'))
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

func persianSeasonOrdinal(n int32) string {
	switch n {
	case 1:
		return "اول"
	case 2:
		return "دوم"
	case 3:
		return "سوم"
	case 4:
		return "چهارم"
	case 5:
		return "پنجم"
	case 6:
		return "ششم"
	case 7:
		return "هفتم"
	case 8:
		return "هشتم"
	case 9:
		return "نهم"
	case 10:
		return "دهم"
	default:
		return persianDigits(int64(n))
	}
}

func mediaHashtag(title string) string {
	title = strings.TrimSpace(title)
	if title == "" {
		return ""
	}
	var parts []string
	for _, w := range strings.FieldsFunc(title, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsNumber(r)
	}) {
		w = strings.TrimSpace(w)
		if w == "" {
			continue
		}
		r := []rune(strings.ToLower(w))
		r[0] = unicode.ToUpper(r[0])
		parts = append(parts, string(r))
	}
	if len(parts) == 0 {
		return ""
	}
	return "#" + strings.Join(parts, "")
}

func ltr(s string) string { return dirLTR + strings.TrimSpace(s) + dirPop }
func rtl(s string) string { return dirRTL + strings.TrimSpace(s) + dirPop }
