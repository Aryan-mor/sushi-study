package catalog

import "regexp"

// A trailer is never a pick-list version: it is not the film, and a 15 MB "Fragman" sitting next
// to six real encodings is exactly how a picker ends up offering the wrong thing. The name alone
// is not enough ("The Trailer" is a real title), so a trailer is a trailer token *and* short.
const (
	trailerMaxDurationS = 10 * 60
	trailerMaxBytes     = 300 << 20
)

// Fragman is Turkish for trailer; تریلر / تیزر / آنونس are what Persian channels caption them with.
var reTrailer = regexp.MustCompile(`(?i)((?:^|[^a-z])(?:trailer|teaser|fragman)(?:$|[^a-z])|تریلر|تیزر|آنونس)`)

// IsTrailer reports whether a source file is a trailer/teaser rather than the title itself.
// A zero duration or size is unknown, never "short".
func IsTrailer(fileName, caption string, durationS int32, sizeBytes int64) bool {
	if !reTrailer.MatchString(fileName) && !reTrailer.MatchString(caption) {
		return false
	}
	shortRun := durationS > 0 && durationS < trailerMaxDurationS
	smallFile := sizeBytes > 0 && sizeBytes < trailerMaxBytes
	return shortRun || smallFile
}
