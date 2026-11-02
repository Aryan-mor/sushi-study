// Package catalog turns what the prototype's database holds into what the protocol sends.
//
// This is where the compact row of docs/12-metadata-and-tmdb.md §1 is actually built, and where
// the catalog's gaps are absorbed: about a quarter of titles have no poster, rating or year
// (doc 12 §7.1). Filling those is the prototype's job, not ours - so every field here degrades to a
// zero value rather than dropping the row, because a card with no poster is still a card the user
// can play (R-META-2).
package catalog

import (
	"strconv"
	"strings"
)

// PosterPathPrefix is what the prototype stores in front of every poster file name.
const posterMarker = "/t/p/"

// PosterPath extracts the TMDB file path from a stored poster URL, stripped of everything constant.
//
// the prototype stores the full URL:
//
//	https://image.tmdb.org/t/p/w500/mk8UH7JRmK8adcqJJpB1ygP7B1C.jpg
//
// The host, the size segment and the extension are all fixed, so 27 characters carry the whole
// thing - and they are the single largest field of a row. The size in the stored URL is discarded:
// the client asks for the size its surface needs (R-META-8).
//
// Anything that does not look like a TMDB image URL yields "", which renders as a placeholder.
func PosterPath(rawURL string) string {
	if rawURL == "" {
		return ""
	}

	i := strings.Index(rawURL, posterMarker)
	if i < 0 {
		// the prototype people.profilePath is "/file.jpg", not a full image.tmdb.org URL.
		return tmdbFileStem(rawURL)
	}
	rest := rawURL[i+len(posterMarker):]

	// Skip the size segment ("w500/", "original/", …).
	slash := strings.IndexByte(rest, '/')
	if slash < 0 {
		return ""
	}
	return tmdbFileStem(rest[slash+1:])
}

// PosterURL is PosterPath's inverse: it puts the host, the size segment and the extension back so
// a stored stem can actually be fetched.
//
// Appending ".jpg" is not optional. What the database holds is the stem alone — no leading slash,
// no extension — so a URL built by prepending the size segment and nothing else is a 404. TMDB
// answers it with an error page rather than an image, Telegram reports only that sendPhoto failed,
// and the caller downgrades to a text message: the poster does not break loudly, it just quietly
// stops appearing. A raw TMDB path ("/abc.jpg") is tolerated too, because the prototype's
// people.profilePath uses that form.
//
// image.tmdb.org is the one third-party host the client is allowed to reach (R-SEC-12). The same
// URL in a Telegram message is Telegram fetching it on our behalf, which reveals nothing of ours.
func PosterURL(path, size string) string {
	p := strings.TrimPrefix(strings.TrimSpace(path), "/")
	if p == "" {
		return ""
	}
	if size == "" {
		size = "w500"
	}
	if !strings.Contains(p, ".") {
		p += ".jpg"
	}
	return "https://image.tmdb.org" + posterMarker + size + "/" + p
}

func tmdbFileStem(name string) string {
	name = strings.TrimPrefix(strings.TrimSpace(name), "/")
	if name == "" || strings.ContainsAny(name, "/?#") {
		return ""
	}
	dot := strings.LastIndexByte(name, '.')
	if dot <= 0 {
		return ""
	}
	ext, stem := name[dot+1:], name[:dot]
	if stem == "" || !isAlnum(stem) || !isAlnum(ext) {
		return ""
	}
	return stem
}

func isAlnum(s string) bool {
	for i := range len(s) {
		c := s[i]
		switch {
		case c >= '0' && c <= '9', c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z':
		default:
			return false
		}
	}
	return true
}

// Rating converts TMDB's 0-10 float, stored as text, to the 0-100 byte the wire carries.
//
// Returns 0 for anything unparseable, which the client renders as "unrated" - the alternative,
// failing the whole row, would hide a playable title over a cosmetic field.
func Rating(raw string) uint32 {
	if raw == "" {
		return 0
	}
	f, err := strconv.ParseFloat(strings.TrimSpace(raw), 64)
	if err != nil || f <= 0 {
		return 0
	}
	if f > 10 {
		f = 10
	}
	return uint32(f*10 + 0.5)
}

// Year picks a release year from the two places the prototype keeps one.
//
// production_year is authoritative when set; premiereDate is an ISO date whose first four
// characters are the year. Neither is guaranteed: a quarter of the catalog has no date at all.
func Year(productionYear *int32, premiereDate string) uint32 {
	if productionYear != nil && *productionYear > 0 {
		return uint32(*productionYear)
	}
	if len(premiereDate) >= 4 {
		if y, err := strconv.Atoi(premiereDate[:4]); err == nil && y > 1800 && y < 2200 {
			return uint32(y)
		}
	}
	return 0
}

// TMDBID parses the id the prototype stores as text.
//
// A row whose tmdb_id will not parse has no address in this protocol (docs/06 §5), so the caller
// must drop it - which is why this reports failure instead of returning zero.
func TMDBID(raw *string) (uint64, bool) {
	if raw == nil {
		return 0, false
	}
	id, err := strconv.ParseUint(strings.TrimSpace(*raw), 10, 64)
	if err != nil || id == 0 {
		return 0, false
	}
	return id, true
}
