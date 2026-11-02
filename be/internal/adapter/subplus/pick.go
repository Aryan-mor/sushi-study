package subplus

import (
	"regexp"
	"strings"
)

var (
	sePattern      = regexp.MustCompile(`s(\d{1,2})[ ._-]?e(\d{1,3})`)
	crossPattern   = regexp.MustCompile(`\b(\d{1,2})x(\d{1,3})\b`)
	seasonPattern  = regexp.MustCompile(`(?:\bs|season)[ ._-]?(\d{1,2})\b`)
	episodePattern = regexp.MustCompile(`(?:\be|episode|ep)[ ._-]?(\d{1,3})\b`)
)

// SeasonEpisode is a best-effort parse of a release or ZIP entry name.
type SeasonEpisode struct {
	Season  int // 0 = unknown
	Episode int // 0 = unknown
	HasS    bool
	HasE    bool
}

// ParseSeasonEpisode handles S03E01, s3e1, 3x01, Season 3, and a bare E01.
func ParseSeasonEpisode(raw string) SeasonEpisode {
	s := strings.ToLower(raw)
	if m := sePattern.FindStringSubmatch(s); m != nil {
		return SeasonEpisode{Season: atoi(m[1]), Episode: atoi(m[2]), HasS: true, HasE: true}
	}
	if m := crossPattern.FindStringSubmatch(s); m != nil {
		return SeasonEpisode{Season: atoi(m[1]), Episode: atoi(m[2]), HasS: true, HasE: true}
	}
	out := SeasonEpisode{}
	if m := seasonPattern.FindStringSubmatch(s); m != nil {
		out.Season = atoi(m[1])
		out.HasS = true
	}
	if m := episodePattern.FindStringSubmatch(s); m != nil {
		out.Episode = atoi(m[1])
		out.HasE = true
	}
	return out
}

func atoi(s string) int {
	n := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			break
		}
		n = n*10 + int(c-'0')
	}
	return n
}

// IsSDH reports a hearing-impaired track. Those are not the dialogue file a cache row should serve.
func IsSDH(name string) bool { return fileLooksSDH(name) }

// IsForced reports a forced-only track (foreign lines, not full dialogue).
func IsForced(name string) bool { return fileLooksForced(name) }

func fileLooksSDH(name string) bool {
	n := strings.ToLower(name)
	return strings.Contains(n, "[sdh]") ||
		strings.Contains(n, ".sdh.") ||
		strings.Contains(n, "_sdh") ||
		strings.Contains(n, "-sdh") ||
		strings.Contains(n, " sdh")
}

func fileLooksForced(name string) bool {
	n := strings.ToLower(name)
	return strings.Contains(n, "[forced]") ||
		strings.Contains(n, ".forced.") ||
		strings.Contains(n, "_forced") ||
		strings.Contains(n, "-forced") ||
		strings.Contains(n, " forced")
}

// PickMovieSubFile prefers a full dialogue track over SDH or forced-only copies.
func PickMovieSubFile(files []SubFile) *SubFile {
	if len(files) == 0 {
		return nil
	}
	var full, noSDH *SubFile
	for i := range files {
		f := &files[i]
		sdh, forced := fileLooksSDH(f.Name), fileLooksForced(f.Name)
		if !sdh && !forced && full == nil {
			full = f
		}
		if !sdh && noSDH == nil {
			noSDH = f
		}
	}
	if full != nil {
		return full
	}
	if noSDH != nil {
		return noSDH
	}
	return &files[0]
}

// PickEpisodeFile returns the ZIP entry for season/episode. One file is returned as-is.
// Multiple files with no S/E match return nil rather than files[0] (that was Friends S3E12:
// a season pack whose first entry was another episode).
func PickEpisodeFile(files []SubFile, season, episode int) *SubFile {
	if len(files) == 0 {
		return nil
	}
	if len(files) == 1 {
		return &files[0]
	}
	var epOnly *SubFile
	for i := range files {
		f := &files[i]
		se := ParseSeasonEpisode(f.Name)
		if !se.HasE || se.Episode != episode {
			continue
		}
		if se.HasS && se.Season == season {
			return f
		}
		if !se.HasS && epOnly == nil {
			epOnly = f
		}
	}
	return epOnly
}

// PickSubFile chooses a series episode file when season/episode are set, otherwise a movie track.
func PickSubFile(files []SubFile, season, episode int) *SubFile {
	if season > 0 && episode > 0 {
		return PickEpisodeFile(files, season, episode)
	}
	return PickMovieSubFile(files)
}
