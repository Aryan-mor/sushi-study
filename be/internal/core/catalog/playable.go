package catalog

import "strings"

// SubtitleLangIsPersian matches the app: empty means the user never picked a language and
// still gets hardsub and dubbed files. Anything else is a real non-Persian choice.
func SubtitleLangIsPersian(lang string) bool {
	switch strings.ToLower(strings.TrimSpace(lang)) {
	case "", "fa", "fas", "per", "farsi", "persian":
		return true
	default:
		return false
	}
}

// FileHiddenForNonPersianSubtitle is the server copy of sushiFilterFilesForSubtitlePref.
// Dubbed and hardsub files are not playable for a non-Persian subtitle language, so a
// request for that user must not come back ALREADY_AVAILABLE.
func FileHiddenForNonPersianSubtitle(label, subLangs string) bool {
	blob := strings.ToLower(label)
	if dubLabel.MatchString(blob) || strings.Contains(blob, "دوبله") || strings.Contains(label, "🎙") {
		return true
	}
	// A real soft-sub track beats a hardsub-looking label (چسبیده on a file that muxed subs).
	if hasCatalogSubs(subLangs) ||
		softSubLabel.MatchString(blob) ||
		strings.Contains(blob, "softsub") ||
		strings.Contains(blob, "سافت") ||
		strings.Contains(blob, "soft sub") {
		return false
	}
	if hardSubLabel.MatchString(blob) ||
		strings.Contains(blob, "hardsub") ||
		strings.Contains(blob, "هاردساب") ||
		strings.Contains(blob, "هارد ساب") ||
		strings.Contains(blob, "burned") ||
		hdtsLabel.MatchString(blob) ||
		strings.Contains(blob, "hdcam") ||
		strings.Contains(blob, "hard sub") ||
		strings.Contains(blob, "چسبیده") {
		return true
	}
	return false
}

func hasCatalogSubs(subLangs string) bool {
	for _, part := range strings.Split(subLangs, ",") {
		if strings.TrimSpace(part) != "" {
			return true
		}
	}
	return false
}
