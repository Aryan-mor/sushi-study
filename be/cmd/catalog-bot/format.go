package main

import (
	"fmt"
	"html"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/Aryan-mor/sushi-study/be/internal/adapter/pg"
	"github.com/Aryan-mor/sushi-study/be/internal/core/catalog"
)

const (
	buttonTextMax   = 64
	captionMaxRunes = 1020
)

func fileButtonText(f catalog.File) string {
	return fileButtonTextAt(f, false)
}

func fileButtonTextAt(f catalog.File, persian bool) string {
	label := filePickKey(f, persian)
	size := formatFileSize(f.SizeBytes)
	if size == "" {
		return truncateRunes(label, buttonTextMax)
	}
	suffix := " · " + size
	maxLabel := buttonTextMax - utf8.RuneCountInString(suffix)
	if maxLabel < 6 {
		return truncateRunes(size, buttonTextMax)
	}
	return truncateRunes(label, maxLabel) + suffix
}

func fileQualityLabel(f catalog.File) string {
	return fileQualityLabelAt(f, false)
}

// filePickKey is the button text without the size, so identical encodes collapse to one row.
func filePickKey(f catalog.File, persian bool) string {
	label := fileQualityLabelAt(f, persian)
	if name := strings.TrimSpace(f.DisplayName); name != "" {
		label = name
	}
	return fileButtonMarksAt(f, persian) + label
}

func fileQualityLabelAt(f catalog.File, persian bool) string {
	var parts []string
	if tech := fileTechLabel(f); tech != "" {
		parts = append(parts, tech)
	}
	if kind := fileDeliveryKindAt(f, persian); kind != "" {
		parts = append(parts, kind)
	}
	if len(parts) == 0 {
		return "دانلود"
	}
	return strings.Join(parts, " · ")
}

func fileTechLabel(f catalog.File) string {
	spec := catalog.ParseFileSpec(f.QualityLabel)
	if f.Height > spec.Height {
		spec.Height = f.Height
	}
	res := pickerQuality(spec.Height)
	spec.Height = 0
	rest := spec.Tech()
	if res == "" {
		return rest
	}
	if rest == "" {
		return res
	}
	return res + " " + rest
}

// pickerQuality snaps a probed or labelled height onto the four tiers the download list shows.
// A raw frame height such as 1706 is not a quality name; 900–1799 is 1080p, 1800+ is 2160p.
func pickerQuality(height int32) string {
	switch {
	case height >= 1800:
		return "2160p"
	case height >= 900:
		return "1080p"
	case height >= 600:
		return "720p"
	case height > 0:
		return "480p"
	default:
		return ""
	}
}

// fileButtonMarks prefixes the download button: 🎯 when the file has subtitles, 🎤 when it is dubbed.
func fileButtonMarks(f catalog.File) string {
	return fileButtonMarksAt(f, false)
}

func fileButtonMarksAt(f catalog.File, persian bool) string {
	spec := catalog.ParseFileSpec(f.QualityLabel)
	var marks string
	if fileHasSubsAt(f, spec, persian) {
		marks += "🎯 "
	}
	if fileIsDubbedAt(f, spec, persian) {
		marks += "🎤 "
	}
	return marks
}

func fileHasSubs(f catalog.File, spec catalog.FileSpec) bool {
	return fileHasSubsAt(f, spec, false)
}

func fileHasSubsAt(f catalog.File, spec catalog.FileSpec, persian bool) bool {
	if strings.TrimSpace(f.SubLangs) != "" {
		return true
	}
	// A Persian-original title has no burned-in foreign sub. "HardSub" / "SoftSub" on those
	// labels is leftover intake text (no subtitle track to back it).
	if persian {
		return false
	}
	return spec.Delivery == catalog.DeliverySoftSub || spec.Delivery == catalog.DeliveryHardSub
}

func fileIsDubbed(f catalog.File, spec catalog.FileSpec) bool {
	return fileIsDubbedAt(f, spec, false)
}

func fileIsDubbedAt(f catalog.File, spec catalog.FileSpec, persian bool) bool {
	if spec.Delivery == catalog.DeliveryDub {
		return true
	}
	// HardSub plus Persian audio is a burned-in subtitle, not a dub.
	if spec.Delivery == catalog.DeliveryHardSub {
		return false
	}
	// Persian audio on an Iranian title is the original track.
	if persian {
		return false
	}
	return catalog.HasLang(f.AudioLangs, "fa")
}

func fileDeliveryKind(f catalog.File) string {
	return fileDeliveryKindAt(f, false)
}

func fileDeliveryKindAt(f catalog.File, persian bool) string {
	audio := f.AudioLangs
	if persian {
		audio = ""
	}
	switch catalog.ResolveDelivery(catalog.ParseFileSpec(f.QualityLabel), audio, f.SubLangs) {
	case catalog.DeliveryDub:
		return "dubbed"
	case catalog.DeliveryHardSub:
		if persian && strings.TrimSpace(f.SubLangs) == "" {
			break
		}
		return "Hard sub"
	case catalog.DeliverySoftSub:
		if persian && strings.TrimSpace(f.SubLangs) == "" {
			break
		}
		return "softsub"
	}
	if persian {
		return spokenLangLine(dropOriginalAudio(f.AudioLangs))
	}
	return spokenLangLine(f.AudioLangs)
}

// dropOriginalAudio hides fa/en on a Persian-original title. Encoders tag the Persian track as
// eng, and "نسخه اورجینال" is not an English dub.
func dropOriginalAudio(stored string) string {
	var keep []string
	for _, code := range catalog.SplitLangs(stored) {
		switch strings.ToLower(code) {
		case "fa", "fas", "per", "en", "eng":
			continue
		default:
			keep = append(keep, code)
		}
	}
	return strings.Join(keep, ",")
}

func spokenLangLine(stored string) string {
	var names []string
	for _, code := range catalog.SplitLangs(stored) {
		if n := langName(code); n != "" {
			names = append(names, n)
		}
	}
	return strings.Join(names, "، ")
}

func langName(code string) string {
	switch strings.ToLower(strings.TrimSpace(code)) {
	case "fa", "fas", "per":
		return "فارسی"
	case "en", "eng":
		return "انگلیسی"
	case "ar", "ara":
		return "عربی"
	case "ja", "jpn":
		return "ژاپنی"
	case "ko", "kor":
		return "کره‌ای"
	case "fr", "fre", "fra":
		return "فرانسوی"
	case "de", "ger", "deu":
		return "آلمانی"
	case "es", "spa":
		return "اسپانیایی"
	case "tr", "tur":
		return "ترکی"
	case "ru", "rus":
		return "روسی"
	case "it", "ita":
		return "ایتالیایی"
	case "hi", "hin":
		return "هندی"
	case "zh", "chi", "zho":
		return "چینی"
	default:
		return ""
	}
}

func formatFileSize(bytes int64) string {
	if bytes <= 0 {
		return ""
	}
	const kb, mb, gb = 1024, 1024 * 1024, 1024 * 1024 * 1024
	switch {
	case bytes >= gb:
		v := float64(bytes) / float64(gb)
		if v >= 10 {
			return fmt.Sprintf("%.0f GB", v)
		}
		return fmt.Sprintf("%.1f GB", v)
	case bytes >= mb:
		return fmt.Sprintf("%.0f MB", float64(bytes)/float64(mb))
	case bytes >= kb:
		return fmt.Sprintf("%.0f KB", float64(bytes)/float64(kb))
	default:
		return fmt.Sprintf("%d B", bytes)
	}
}

func truncateRunes(s string, limit int) string {
	s = strings.TrimSpace(s)
	if limit <= 0 {
		return ""
	}
	rs := []rune(s)
	if len(rs) <= limit {
		return s
	}
	if limit == 1 {
		return "…"
	}
	return string(rs[:limit-1]) + "…"
}

func displayOverview(item catalog.Item) string {
	if fa := strings.TrimSpace(item.OverviewFa); fa != "" {
		return fa
	}
	return strings.TrimSpace(item.Overview)
}

func catalogCaption(item catalog.Item, footer, handle string) string {
	return catalogCaptionAt(item, footer, handle, 0, 0, "")
}

func catalogCaptionAt(item catalog.Item, footer, handle string, season, ep int32, epTitle string) string {
	head := catalogHead(item, season, ep, epTitle)
	brand := brandFooter(handle, item.Title)
	overviewMax := 220
	if footer != "" || brand != "" {
		overviewMax = 150
	}
	for budget := overviewMax; budget >= 40; budget -= 30 {
		caption := joinCaption(head, html.EscapeString(truncateRunes(displayOverview(item), budget)), footer, brand)
		if utf8.RuneCountInString(caption) <= captionMaxRunes {
			return caption
		}
	}
	return joinCaption(head, footer, brand)
}

func catalogHead(item catalog.Item, season, ep int32, epTitle string) string {
	kindLabel := kindMovie
	icon := "🎬"
	if item.Kind == catalog.KindSeries {
		kindLabel = kindSeries
		icon = "📺"
	}
	title := strings.TrimSpace(item.Title)
	if title == "" && item.TMDBID > 0 {
		title = fmt.Sprintf("TMDB %d", item.TMDBID)
	}
	if title == "" {
		title = "عنوان"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%s <b>%s</b>\n\n", icon, kindLabel)
	if href := titlePageURL(item.Kind, item.TMDBID, item.IMDBID); href != "" {
		fmt.Fprintf(&b, "<b>%s</b>", htmlLink(href, title))
	} else {
		fmt.Fprintf(&b, "<b>%s</b>", html.EscapeString(title))
	}
	if ids := catalogIDLinks(item.Kind, item.TMDBID, item.IMDBID, false); ids != "" {
		fmt.Fprintf(&b, "\n%s", ids)
	}
	if season > 0 {
		fmt.Fprintf(&b, "\nفصل <b>%d</b>", season)
	}
	if ep > 0 {
		line := fmt.Sprintf("قسمت <b>%d</b>", ep)
		if sub := strings.TrimSpace(epTitle); sub != "" && !strings.EqualFold(sub, title) {
			line += " — " + html.EscapeString(sub)
		}
		fmt.Fprintf(&b, "\n%s", line)
	}
	if item.Year > 0 {
		fmt.Fprintf(&b, "\n📅 %d", item.Year)
	}
	if g := genreLine(item.Detail.Genres); g != "" {
		fmt.Fprintf(&b, "\n🎭 %s", g)
	}
	if meta := titleMetaLine(item); meta != "" {
		fmt.Fprintf(&b, "\n%s", meta)
	}
	return b.String()
}

func titleMetaLine(item catalog.Item) string {
	var parts []string
	if item.Rating > 0 {
		parts = append(parts, fmt.Sprintf("⭐ %.1f/10", float64(item.Rating)/10))
	}
	if item.Kind == catalog.KindMovie && item.Detail.RuntimeS > 0 {
		if mins := item.Detail.RuntimeS / 60; mins > 0 {
			parts = append(parts, fmt.Sprintf("⏱ %d دقیقه", mins))
		}
	}
	if item.Kind == catalog.KindSeries {
		if n := len(seasonNums(item)); n > 0 {
			parts = append(parts, fmt.Sprintf("📚 %d فصل آماده", n))
		}
	}
	return strings.Join(parts, " · ")
}

func genreLine(genres string) string {
	names := catalog.SplitGenres(genres)
	if len(names) == 0 {
		return ""
	}
	if len(names) > 4 {
		names = names[:4]
	}
	escaped := make([]string, 0, len(names))
	for _, n := range names {
		escaped = append(escaped, html.EscapeString(n))
	}
	return strings.Join(escaped, "، ")
}

func deliveryCaption(item catalog.Item, origin pg.FileOrigin) string {
	title, kind, tmdbID, year := deliveryIdentity(item, origin)
	var b strings.Builder
	writeDeliveryTitle(&b, title, kind, tmdbID, year, origin.SeasonNo, item.IMDBID)
	writeDeliveryEpisode(&b, origin, title)
	writeDeliveryTracks(&b, origin.File, catalog.PersianOrigin(item.Detail.Country))
	return capCaption(b.String())
}

func deliveryIdentity(item catalog.Item, origin pg.FileOrigin) (title string, kind catalog.Kind, tmdbID uint64, year uint32) {
	title = strings.TrimSpace(item.Title)
	if title == "" {
		title = strings.TrimSpace(origin.Title)
	}
	kind, tmdbID, year = item.Kind, item.TMDBID, item.Year
	if tmdbID == 0 {
		tmdbID, kind, year = origin.TMDBID, origin.Kind, origin.Year
	}
	if title == "" && tmdbID > 0 {
		title = fmt.Sprintf("TMDB %d", tmdbID)
	}
	return title, kind, tmdbID, year
}

func writeDeliveryTitle(b *strings.Builder, title string, kind catalog.Kind, tmdbID uint64, year uint32, season int32, imdb string) {
	if title == "" {
		return
	}
	b.WriteString("<b>عنوان:</b> ")
	label := title
	if year > 0 && season <= 0 {
		label = fmt.Sprintf("%s (%d)", title, year)
	}
	if href := titlePageURL(kind, tmdbID, imdb); href != "" {
		b.WriteString(htmlLink(href, label))
	} else {
		b.WriteString(html.EscapeString(label))
	}
	if ids := catalogIDLinks(kind, tmdbID, imdb, true); ids != "" {
		b.WriteByte('\n')
		b.WriteString(ids)
	}
}

func writeDeliveryEpisode(b *strings.Builder, origin pg.FileOrigin, seriesTitle string) {
	if origin.SeasonNo > 0 {
		writeCaptionLine(b, "فصل", fmt.Sprintf("%d", origin.SeasonNo))
	}
	if origin.EpisodeNo <= 0 {
		return
	}
	epLabel := fmt.Sprintf("%d", origin.EpisodeNo)
	if name := strings.TrimSpace(origin.EpisodeTitle); name != "" && !strings.EqualFold(name, seriesTitle) {
		epLabel = fmt.Sprintf("%d (%s)", origin.EpisodeNo, name)
	}
	writeCaptionLine(b, "قسمت", html.EscapeString(epLabel))
}

func writeDeliveryTracks(b *strings.Builder, f catalog.File, persian bool) {
	if q := fileTechLabel(f); q != "" {
		writeCaptionLine(b, "کیفیت", html.EscapeString(q))
	}
	if size := formatFileSize(f.SizeBytes); size != "" {
		writeCaptionLine(b, "حجم", html.EscapeString(size))
	}
	if lang := fileDeliveryKindAt(f, persian); lang != "" {
		writeCaptionLine(b, "زبان", html.EscapeString(lang))
	}
	audioLangs := f.AudioLangs
	if persian {
		audioLangs = dropOriginalAudio(audioLangs)
	}
	if audio := spokenLangLine(audioLangs); audio != "" {
		writeCaptionLine(b, "صدا", html.EscapeString(audio))
	}
	if subs := spokenLangLine(f.SubLangs); subs != "" {
		writeCaptionLine(b, "زیرنویس", html.EscapeString(subs))
	}
}

func writeCaptionLine(b *strings.Builder, label, value string) {
	if b.Len() > 0 {
		b.WriteByte('\n')
	}
	fmt.Fprintf(b, "<b>%s:</b> %s", label, value)
}

func capCaption(out string) string {
	if utf8.RuneCountInString(out) <= captionMaxRunes {
		return out
	}
	return truncateRunes(out, captionMaxRunes)
}

func deliveryNotice(item catalog.Item, origin pg.FileOrigin, bulk bool) string {
	note := ephemeralNote
	if bulk {
		note = ephemeralBulk
	}
	var b strings.Builder
	b.WriteString(note)
	summary := deliveryTitleSummary(item, origin)
	if summary != "" {
		fmt.Fprintf(&b, "\n\n<b>عنوان:</b> %s", html.EscapeString(summary))
	}
	if q := fileTechLabel(origin.File); q != "" {
		fmt.Fprintf(&b, "\n<b>کیفیت:</b> %s", html.EscapeString(q))
	}
	if size := formatFileSize(origin.File.SizeBytes); size != "" {
		fmt.Fprintf(&b, "\n<b>حجم:</b> %s", html.EscapeString(size))
	}
	return b.String()
}

func deliveryTitleSummary(item catalog.Item, origin pg.FileOrigin) string {
	title := strings.TrimSpace(item.Title)
	if title == "" {
		title = strings.TrimSpace(origin.Title)
	}
	if title == "" {
		return ""
	}
	if origin.SeasonNo > 0 && origin.EpisodeNo > 0 {
		return fmt.Sprintf("%s — فصل %d قسمت %d", title, origin.SeasonNo, origin.EpisodeNo)
	}
	if item.Year > 0 {
		return fmt.Sprintf("%s (%d)", title, item.Year)
	}
	if origin.Year > 0 {
		return fmt.Sprintf("%s (%d)", title, origin.Year)
	}
	return title
}

func brandFooter(handle, title string) string {
	var lines []string
	if h := strings.TrimPrefix(strings.TrimSpace(handle), "@"); h != "" {
		lines = append(lines, "🦁 @"+h)
	}
	if tag := titleHashtag(title); tag != "" {
		lines = append(lines, "🌞 "+tag)
	}
	return strings.Join(lines, "\n")
}

func titleHashtag(title string) string {
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

func tmdbPageURL(kind catalog.Kind, tmdbID uint64) string {
	if tmdbID == 0 {
		return ""
	}
	path := "movie"
	if kind == catalog.KindSeries {
		path = "tv"
	}
	return fmt.Sprintf("https://www.themoviedb.org/%s/%d", path, tmdbID)
}

var reIMDBID = regexp.MustCompile(`^tt\d+$`)

func canonicalIMDB(id string) string {
	id = strings.TrimSpace(id)
	if !reIMDBID.MatchString(id) {
		return ""
	}
	return id
}

func imdbURL(id string) string {
	id = canonicalIMDB(id)
	if id == "" {
		return ""
	}
	return "https://www.imdb.com/title/" + id + "/"
}

func titlePageURL(kind catalog.Kind, tmdbID uint64, imdb string) string {
	if u := imdbURL(imdb); u != "" {
		return u
	}
	return tmdbPageURL(kind, tmdbID)
}

func htmlLink(url, label string) string {
	return fmt.Sprintf(`<a href="%s">%s</a>`, html.EscapeString(url), html.EscapeString(label))
}

func catalogIDLinks(kind catalog.Kind, tmdbID uint64, imdb string, withCodes bool) string {
	var parts []string
	if u := imdbURL(imdb); u != "" {
		part := htmlLink(u, "IMDB")
		if withCodes {
			part += fmt.Sprintf(": <code>%s</code>", html.EscapeString(canonicalIMDB(imdb)))
		}
		parts = append(parts, part)
	}
	if u := tmdbPageURL(kind, tmdbID); u != "" {
		part := htmlLink(u, "TMDB")
		if withCodes {
			part += fmt.Sprintf(": <code>%d</code>", tmdbID)
		}
		parts = append(parts, part)
	}
	if len(parts) == 0 {
		return ""
	}
	return "🔗 " + strings.Join(parts, " · ")
}

func joinCaption(parts ...string) string {
	var out []string
	for _, p := range parts {
		if strings.TrimSpace(p) != "" {
			out = append(out, p)
		}
	}
	return strings.Join(out, "\n\n")
}

func shareQuery(kind catalog.Kind, tmdbID uint64) string {
	letter := "m"
	if kind == catalog.KindSeries {
		letter = "t"
	}
	return fmt.Sprintf("%s %s:%d", inlineShareMark, letter, tmdbID)
}

func parseShareQuery(raw string) (catalog.Kind, uint64, bool) {
	raw = strings.TrimSpace(raw)
	if !strings.HasPrefix(strings.ToLower(raw), inlineShareMark) {
		return 0, 0, false
	}
	rest := strings.TrimSpace(raw[len(inlineShareMark):])
	return decodeResultID(strings.ReplaceAll(rest, ":", "_"))
}

func episodePickedFooter(season, ep int32) string {
	return fmt.Sprintf("فصل <b>%d</b> قسمت <b>%d</b>\n\n%s", season, ep, pickQuality)
}

func episodeListFooter(season int32, page, pages int) string {
	if pages > 1 {
		return fmt.Sprintf("فصل <b>%d</b> — صفحه <b>%d</b> از <b>%d</b>\n%s", season, page+1, pages, pickEpisode)
	}
	return fmt.Sprintf("فصل <b>%d</b> — %s", season, pickEpisode)
}

func bulkPickFooter(season int32) string {
	return fmt.Sprintf("فصل <b>%d</b>\n\n%s", season, pickBulk)
}
