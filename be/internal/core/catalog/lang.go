package catalog

import (
	"sort"
	"strings"
)

// Sushi's own audience, in the order a person here would want to see them (docs/07 §3).
var preferredLangs = map[string]int{"fa": 1, "en": 2}

// iso6391 maps the three-letter codes ffprobe reports onto ISO 639-1.
//
// This table exists because the source catalog carries three different standards at once. Persian
// alone arrives as "per" (639-2/B), "fas" (639-2/T) and "fa" (639-1) - 8590, 1496 and 94 tracks
// respectively, measured against production. Storing those verbatim means "has Persian subtitles"
// silently misses one in seven files, which is the kind of bug nobody reports: the film just is
// not in the results.
//
// Only languages actually present in the source are listed. An unmapped code is dropped rather
// than stored as-is (see NormalizeLangs), so this field is uniformly two-letter and a client never
// has to know that 639-2 exists.
var iso6391 = map[string]string{
	"alb": "sq", "ara": "ar", "arm": "hy", "aze": "az", "baq": "eu", "bul": "bg",
	"bur": "my", "cat": "ca", "ces": "cs", "chi": "zh", "cmn": "zh", "cze": "cs",
	"dan": "da", "deu": "de", "dut": "nl", "ell": "el", "eng": "en", "est": "et",
	"eus": "eu", "fas": "fa", "fil": "tl", "fin": "fi", "fra": "fr", "fre": "fr",
	"geo": "ka", "ger": "de", "glg": "gl", "gre": "el", "heb": "he", "hin": "hi",
	"hrv": "hr", "hun": "hu", "hye": "hy", "ice": "is", "ind": "id", "isl": "is",
	"ita": "it", "jpn": "ja", "kan": "kn", "kat": "ka", "kaz": "kk", "khm": "km",
	"kir": "ky", "kor": "ko", "lav": "lv", "lit": "lt", "mac": "mk", "mal": "ml",
	"may": "ms", "mkd": "mk", "mon": "mn", "msa": "ms", "mya": "my", "nld": "nl",
	"nno": "nn", "nob": "nb", "nor": "no", "per": "fa", "pol": "pl", "por": "pt",
	"ron": "ro", "rum": "ro", "rus": "ru", "slk": "sk", "slo": "sk", "slv": "sl",
	"spa": "es", "sqi": "sq", "srp": "sr", "swe": "sv", "tam": "ta", "tat": "tt",
	"tel": "te", "tha": "th", "ton": "to", "tur": "tr", "ukr": "uk", "uzb": "uz",
	"vie": "vi", "zho": "zh",
}

// NormalizeLangs turns the language codes of one file's tracks into the stored form: a
// comma-separated list of ISO 639-1 codes, deduplicated, Persian and English first.
//
// Two kinds of input are thrown away, both deliberately:
//
//   - placeholders. "und" (undetermined) and "mul" (multiple) name no language, and 759 tracks
//     carry no language at all. A track nobody can label is not a language the interface can
//     offer, so it does not become an empty entry in the list;
//   - languages with no 639-1 code, such as Saraiki. Five tracks in the whole catalog. Keeping
//     them would make this field a mix of two- and three-letter codes for the sake of five rows,
//     and every consumer would then have to handle both.
//
// Ordering is by usefulness rather than alphabet: this is read straight onto a screen, and for
// this audience Persian belongs first (R-META-2 - a field renders or it is empty, never wrong).
func NormalizeLangs(raw []string) string {
	if len(raw) == 0 {
		return ""
	}

	seen := make(map[string]struct{}, len(raw))
	out := make([]string, 0, len(raw))

	for _, reported := range raw {
		code, ok := toISO6391(reported)
		if !ok {
			continue
		}
		if _, dup := seen[code]; dup {
			continue
		}
		seen[code] = struct{}{}
		out = append(out, code)
	}

	sort.Slice(out, func(i, j int) bool { return lessLang(out[i], out[j]) })

	return strings.Join(out, ",")
}

// toISO6391 reduces one reported code to a two-letter one, reporting whether anything usable came
// back. Both kinds of rejection - a placeholder, a language with no 639-1 code - land here.
func toISO6391(code string) (string, bool) {
	code = strings.ToLower(strings.TrimSpace(code))

	if len(code) == 3 {
		mapped, ok := iso6391[code]
		if !ok {
			return "", false
		}
		code = mapped
	}

	if len(code) != 2 || code[0] < 'a' || code[0] > 'z' || code[1] < 'a' || code[1] > 'z' {
		return "", false
	}
	return code, true
}

// lessLang orders the preferred languages first, in their listed order, and everything else
// alphabetically behind them.
func lessLang(a, b string) bool {
	pa, pb := preferredLangs[a], preferredLangs[b]
	switch {
	case pa == pb:
		return a < b
	case pa == 0:
		return false
	case pb == 0:
		return true
	default:
		return pa < pb
	}
}

// SplitLangs reads back what NormalizeLangs stored.
func SplitLangs(stored string) []string {
	if stored == "" {
		return nil
	}
	return strings.Split(stored, ",")
}

// HasLang reports whether a stored list offers a language, without allocating.
func HasLang(stored, code string) bool {
	for rest := stored; rest != ""; {
		entry, tail, _ := strings.Cut(rest, ",")
		if entry == code {
			return true
		}
		rest = tail
	}
	return false
}
