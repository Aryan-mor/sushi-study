package tg

import (
	"strconv"
	"strings"

	"github.com/Aryan-mor/sushi-study/be/internal/core/catalog"
)

// WatchLaterPayload is the /start rest for a news-channel watch-later tap (ADR 0022).
func WatchLaterPayload(kind catalog.Kind, tmdbID uint64) string {
	if tmdbID == 0 {
		return ""
	}
	return "wl_" + mediaLetter(kind) + "_" + strconv.FormatUint(tmdbID, 10)
}

// ParseWatchLaterPayload reads wl_m_550 / wl_t_1399.
func ParseWatchLaterPayload(rest string) (catalog.Kind, uint64, bool) {
	kind, tmdbID, ok := parseLetterID(strings.TrimSpace(rest), "wl_")
	return kind, tmdbID, ok
}

// AppWatchPayload is the news "watch in the app" /start rest: add to Watch Later, then the
// download picker (ADR 0022).
func AppWatchPayload(kind catalog.Kind, tmdbID uint64) string {
	if tmdbID == 0 {
		return ""
	}
	return "app_" + mediaLetter(kind) + "_" + strconv.FormatUint(tmdbID, 10)
}

// ParseAppWatchPayload reads app_m_550 / app_t_1399.
func ParseAppWatchPayload(rest string) (catalog.Kind, uint64, bool) {
	return parseLetterID(strings.TrimSpace(rest), "app_")
}

// CatalogPayload is the /start rest that opens catalog-bot on a title (ADR 0022).
func CatalogPayload(kind catalog.Kind, tmdbID uint64) string {
	if tmdbID == 0 {
		return ""
	}
	return mediaLetter(kind) + "_" + strconv.FormatUint(tmdbID, 10)
}

// ParseCatalogPayload reads m_550 / t_1399.
func ParseCatalogPayload(rest string) (catalog.Kind, uint64, bool) {
	return parseLetterID(strings.TrimSpace(rest), "")
}

func mediaLetter(kind catalog.Kind) string {
	if kind == catalog.KindSeries {
		return "t"
	}
	return "m"
}

func parseLetterID(rest, prefix string) (catalog.Kind, uint64, bool) {
	rest = strings.TrimSpace(rest)
	if prefix != "" {
		if !strings.HasPrefix(strings.ToLower(rest), prefix) {
			return 0, 0, false
		}
		rest = rest[len(prefix):]
	}
	letter, idPart, found := strings.Cut(rest, "_")
	if !found {
		return 0, 0, false
	}
	var kind catalog.Kind
	switch strings.ToLower(strings.TrimSpace(letter)) {
	case "m":
		kind = catalog.KindMovie
	case "t":
		kind = catalog.KindSeries
	default:
		return 0, 0, false
	}
	tmdbID, err := strconv.ParseUint(strings.TrimSpace(idPart), 10, 64)
	if err != nil || tmdbID == 0 {
		return 0, 0, false
	}
	return kind, tmdbID, true
}
