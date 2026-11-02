package newsroom

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/Aryan-mor/sushi-study/be/internal/core/catalog"
)

const laterCaptionMax = 1024

// FormatLaterAdded is the DM under the poster after a news-channel watch-later tap (ADR 0022).
// Bilingual: a DM goes to one person whose locale we know (R-I18N-10). The channel post itself
// stays Persian.
func FormatLaterAdded(locale string, item catalog.Item) string {
	fa := locale == "fa"
	header := "✅ <b>Added to Watch Later</b>"
	footer := "Find it in Sushi, under Watch Later."
	if fa {
		header = "✅ <b>رفت تو لیست بعداً تماشا</b>"
		footer = "تو اپلیکیشن سوشی، بخش «بعداً تماشا می‌کنم» پیداش می‌کنی."
	}
	head := laterHead(fa, item)
	for budget := 280; budget >= 40; budget -= 40 {
		caption := joinBlocks(header, head, escapeTruncate(item.Overview, budget), footer)
		if utf8.RuneCountInString(caption) <= laterCaptionMax {
			return caption
		}
	}
	return joinBlocks(header, head, footer)
}

func laterHead(fa bool, item catalog.Item) string {
	var b strings.Builder
	title := strings.TrimSpace(item.Title)
	if title == "" {
		title = fmt.Sprintf("TMDB %d", item.TMDBID)
	}
	fmt.Fprintf(&b, "<b>%s</b>", tmdbLink(item.Kind, item.TMDBID, title))
	if item.Year > 0 {
		if fa {
			fmt.Fprintf(&b, "\n📅 %s", persianDigits(int64(item.Year)))
		} else {
			fmt.Fprintf(&b, "\n📅 %d", item.Year)
		}
	}
	if g := genreLine(item.Detail.Genres); g != "" {
		fmt.Fprintf(&b, "\n🎭 %s", g)
	}
	if m := laterMeta(fa, item); m != "" {
		b.WriteString("\n")
		b.WriteString(m)
	}
	return b.String()
}

func laterMeta(fa bool, item catalog.Item) string {
	var parts []string
	if item.Rating > 0 {
		if fa {
			parts = append(parts, fmt.Sprintf("⭐ %s/%s",
				persianDecimal(int64(item.Rating)), persianDigits(10)))
		} else {
			parts = append(parts, fmt.Sprintf("⭐ %.1f/10", float64(item.Rating)/10))
		}
	}
	if mins := item.Detail.RuntimeS / 60; mins > 0 {
		if fa {
			parts = append(parts, fmt.Sprintf("⏱ %s دقیقه", persianDigits(int64(mins))))
		} else {
			parts = append(parts, fmt.Sprintf("⏱ %d min", mins))
		}
	}
	return strings.Join(parts, " · ")
}
