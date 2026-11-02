package api

import "fmt"

// packIndex is one extra zip entry worth archiving under its own episode.
type packIndex struct {
	File            PackedSubtitle
	Season, Episode int
}

// packCutType prefers the entry name, then the pack's release name. Inner files of a season
// zip are often "Show.S03E01.srt" while the type lives on the release ("...BluRay...").
func packCutType(name, releaseName string) string {
	if cut := subtitleCutType(name); cut != "" {
		return cut
	}
	return subtitleCutType(releaseName)
}

// pickPackedSubtitle chooses the zip entry for this request. One file is that episode.
// Several files with no S/E match are an error — files[0] was the Friends S3E12 miss.
func pickPackedSubtitle(files []PackedSubtitle, season, episode int) (PackedSubtitle, int, error) {
	if len(files) == 0 {
		return PackedSubtitle{}, -1, fmt.Errorf("zip has no .srt/.ass files")
	}
	if season > 0 && episode > 0 {
		return pickEpisodeSubtitle(files, season, episode)
	}
	file, idx := pickFullSubtitle(files)
	return file, idx, nil
}

func pickEpisodeSubtitle(files []PackedSubtitle, season, episode int) (PackedSubtitle, int, error) {
	if len(files) == 1 {
		return files[0], 0, nil
	}
	epOnly := -1
	for i, f := range files {
		if !f.HasEpisode || f.Episode != episode {
			continue
		}
		if f.HasSeason && f.Season == season {
			return f, i, nil
		}
		if !f.HasSeason && epOnly < 0 {
			epOnly = i
		}
	}
	if epOnly >= 0 {
		return files[epOnly], epOnly, nil
	}
	return PackedSubtitle{}, -1, fmt.Errorf("zip has no matching s%02de%02d file", season, episode)
}

func pickFullSubtitle(files []PackedSubtitle) (PackedSubtitle, int) {
	full, noSDH := -1, -1
	for i, f := range files {
		if !f.SDH && !f.Forced && full < 0 {
			full = i
		}
		if !f.SDH && noSDH < 0 {
			noSDH = i
		}
	}
	switch {
	case full >= 0:
		return files[full], full
	case noSDH >= 0:
		return files[noSDH], noSDH
	default:
		return files[0], 0
	}
}

// siblingPackIndexes lists the other dialogue files in this zip that can become their own cache
// row. Same season only, and only when the name parses to SxxEyy. SDH, forced, other seasons,
// unparsed names, and files with no cut type are left out. A typeless file is not stored: a
// later lookup will not return it. The served entry is not repeated. A second file with the
// same episode, type, and minute as the served one is not repeated either (utf-8 vs ansi).
func siblingPackIndexes(files []PackedSubtitle, servedIdx int, releaseName string, reqSeason, reqEpisode int) []packIndex {
	if servedIdx < 0 || servedIdx >= len(files) {
		return nil
	}
	seen := servedPackKeys(files[servedIdx], releaseName, reqSeason, reqEpisode)
	var out []packIndex
	for i, f := range files {
		sib, ok := nextSibling(f, i == servedIdx, releaseName, reqSeason, reqEpisode, seen)
		if !ok {
			continue
		}
		out = append(out, sib)
	}
	return out
}

func servedPackKeys(served PackedSubtitle, releaseName string, season, episode int) map[string]struct{} {
	seen := map[string]struct{}{}
	cut := packCutType(served.Name, releaseName)
	mins := subtitleDurationMin(served.Text)
	if cut != "" && mins > 0 {
		seen[packKey(season, episode, cut, mins)] = struct{}{}
	}
	return seen
}

func nextSibling(f PackedSubtitle, isServed bool, releaseName string, reqSeason, reqEpisode int, seen map[string]struct{}) (packIndex, bool) {
	if isServed || f.SDH || f.Forced {
		return packIndex{}, false
	}
	season, episode, ok := siblingEpisode(f, reqSeason, reqEpisode)
	if !ok {
		return packIndex{}, false
	}
	cut := packCutType(f.Name, releaseName)
	mins := subtitleDurationMin(f.Text)
	if cut == "" || mins <= 0 {
		return packIndex{}, false
	}
	key := packKey(season, episode, cut, mins)
	if _, dup := seen[key]; dup {
		return packIndex{}, false
	}
	seen[key] = struct{}{}
	return packIndex{File: f, Season: season, Episode: episode}, true
}

func siblingEpisode(f PackedSubtitle, reqSeason, reqEpisode int) (season, episode int, ok bool) {
	if reqSeason > 0 && reqEpisode > 0 {
		if !f.HasSeason || !f.HasEpisode || f.Season != reqSeason {
			return 0, 0, false
		}
		return f.Season, f.Episode, true
	}
	// A movie zip. An entry that names its own episode belongs to some series, not this film.
	if f.HasSeason && f.HasEpisode {
		return 0, 0, false
	}
	return 0, 0, true
}

func packKey(season, episode int, cut string, mins int) string {
	return fmt.Sprintf("%d/%d/%s/%d", season, episode, cut, mins)
}
