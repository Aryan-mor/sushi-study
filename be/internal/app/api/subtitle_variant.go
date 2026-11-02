package api

import (
	"math"
	"regexp"
	"strings"
)

// cutToken matches a source word that is not glued inside a longer word ("web" in
// "AMZN.WEB", not "web" in "webcam").
var (
	webToken  = regexp.MustCompile(`(?i)(^|[^a-z])web([^a-z]|$)`)
	hdtsToken = regexp.MustCompile(`(?i)(^|[^a-z])hdts([^a-z]|$)`)
	camToken  = regexp.MustCompile(`(?i)(^|[^a-z])cam([^a-z]|$)`)
)

// subtitleCutType is the release family a label names. Source families (dvd, web, bluray,
// hdtv, telesync, cam) win over a delivery word (softsub, hardsub) when both appear. Empty
// means the label does not say which cut it is ("1080p" alone).
func subtitleCutType(label string) string {
	s := strings.ToLower(label)
	switch {
	case strings.Contains(s, "dvd"):
		return "dvd"
	case strings.Contains(s, "webrip") || strings.Contains(s, "web-dl") || strings.Contains(s, "webdl") || strings.Contains(s, "web.dl") || webToken.MatchString(s):
		return "web"
	case strings.Contains(s, "bluray") || strings.Contains(s, "blu-ray") || strings.Contains(s, "bdrip") || strings.Contains(s, "brrip"):
		return "bluray"
	case strings.Contains(s, "hdtv"):
		return "hdtv"
	case strings.Contains(s, "telesync") || hdtsToken.MatchString(s):
		return "telesync"
	case strings.Contains(s, "camrip") || strings.Contains(s, "cam-rip") || strings.Contains(s, "hdcam") || strings.Contains(s, "hdtc") || camToken.MatchString(s):
		return "cam"
	case strings.Contains(s, "softsub") || strings.Contains(s, "soft sub") || strings.Contains(s, "سافت"):
		return "softsub"
	case strings.Contains(s, "hardsub") || strings.Contains(s, "hard sub") || strings.Contains(s, "هارد"):
		return "hardsub"
	default:
		return ""
	}
}

var cueEnd = regexp.MustCompile(`(\d{1,2}):(\d{2}):(\d{2})[,.](\d{1,3})`)

// subtitleDurationMin is the subtitle timeline length in whole minutes (rounded). 0 when the
// text has no cue timestamps.
func subtitleDurationMin(text string) int {
	sec := subtitleTimelineSeconds(text)
	if sec <= 0 {
		return 0
	}
	return int(math.Round(sec / 60))
}

func subtitleTimelineSeconds(text string) float64 {
	var maxEnd float64
	for _, line := range strings.Split(text, "\n") {
		if !strings.Contains(line, "-->") && !strings.Contains(strings.ToLower(line), "dialogue:") {
			continue
		}
		ms := cueEnd.FindAllStringSubmatch(line, -1)
		if len(ms) == 0 {
			continue
		}
		// SRT has start and end on one line; the end is the last stamp. ASS Dialogue too.
		last := ms[len(ms)-1]
		end := cueSeconds(last)
		if end > maxEnd {
			maxEnd = end
		}
	}
	return maxEnd
}

func cueSeconds(m []string) float64 {
	if len(m) != 5 {
		return 0
	}
	h := atof(m[1])
	mins := atof(m[2])
	sec := atof(m[3])
	frac := m[4]
	for len(frac) < 3 {
		frac += "0"
	}
	ms := atof(frac[:3])
	return h*3600 + mins*60 + sec + ms/1000
}

func atof(s string) float64 {
	n := 0
	for _, r := range s {
		if r < '0' || r > '9' {
			break
		}
		n = n*10 + int(r-'0')
	}
	return float64(n)
}

// matchCachedSubtitle picks the archived cut that is the same type and the same minute count
// as the file being watched. A row with no type is not a match, even when the minutes agree:
// Friends BluRay and SoftSub are both 23 minutes. A playback label with no type is not a match
// either. Legacy rows (duration_min 0, empty type) stay unused.
func matchCachedSubtitle(rows []CachedSubtitle, wantType string, wantMin int) (CachedSubtitle, bool) {
	if wantType == "" || wantMin <= 0 {
		return CachedSubtitle{}, false
	}
	var best CachedSubtitle
	found := false
	for _, row := range rows {
		if row.SubType == "" || row.DurationMin <= 0 {
			continue
		}
		if int(row.DurationMin) != wantMin || row.SubType != wantType {
			continue
		}
		if !found || row.DurationMin > best.DurationMin {
			best = row
			found = true
		}
	}
	return best, found
}
