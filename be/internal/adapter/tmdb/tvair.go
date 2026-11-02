package tmdb

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"unicode"
)

// TVFindByAirDate returns the season/episode whose air_date matches one of dates (ISO YYYY-MM-DD).
// When two calendar interpretations both hit, hint tokens (guest names from the filename) pick
// the episode whose name/overview overlaps more. No hit is ok=false, not an error.
func (c *Client) TVFindByAirDate(ctx context.Context, tmdbID uint64, dates []string, hint string) (season, episode int, ok bool, err error) {
	if c == nil || c.key == "" || tmdbID == 0 {
		return 0, 0, false, nil
	}
	want, years := airDateWant(dates)
	if len(want) == 0 {
		return 0, 0, false, nil
	}
	seasons, err := c.tvSeasonNums(ctx, tmdbID, years)
	if err != nil {
		return 0, 0, false, err
	}
	hits, err := c.airHits(ctx, tmdbID, seasons, want)
	if err != nil {
		return 0, 0, false, err
	}
	if hit, ok := pickAirHit(hits, hint); ok {
		return hit.Season, hit.Episode, true, nil
	}
	return 0, 0, false, nil
}

func airDateWant(dates []string) (map[string]struct{}, map[int]struct{}) {
	want := map[string]struct{}{}
	years := map[int]struct{}{}
	for _, d := range dates {
		d = strings.TrimSpace(d)
		if len(d) < 10 {
			continue
		}
		d = d[:10]
		want[d] = struct{}{}
		if y, e := strconv.Atoi(d[:4]); e == nil {
			years[y] = struct{}{}
		}
	}
	return want, years
}

func (c *Client) airHits(ctx context.Context, tmdbID uint64, seasons []int, want map[string]struct{}) ([]airHit, error) {
	var hits []airHit
	for _, n := range seasons {
		eps, err := c.tvSeasonEpisodes(ctx, tmdbID, n)
		if err != nil {
			return nil, err
		}
		for _, ep := range eps {
			if _, ok := want[ep.AirDate]; ok {
				hits = append(hits, ep)
			}
		}
	}
	return hits, nil
}

type airHit struct {
	Season, Episode int
	AirDate         string
	Name            string
	Overview        string
}

type tvSeasonBrief struct {
	SeasonNumber int    `json:"season_number"`
	AirDate      string `json:"air_date"`
}

func eatJSON(raw []byte, dest any) bool {
	return json.Unmarshal(raw, dest) == nil
}

func (c *Client) tvSeasonNums(ctx context.Context, tmdbID uint64, years map[int]struct{}) ([]int, error) {
	raw, err := c.get(ctx, "tv/"+strconv.FormatUint(tmdbID, 10), nil)
	if err != nil || len(raw) == 0 {
		return nil, err
	}
	var wrap struct {
		Seasons []tvSeasonBrief `json:"seasons"`
	}
	if !eatJSON(raw, &wrap) {
		return nil, nil
	}
	return pickSeasonNums(wrap.Seasons, years), nil
}

func pickSeasonNums(seasons []tvSeasonBrief, years map[int]struct{}) []int {
	numbered := uniqueNumberedSeasons(seasons)
	var out []int
	for _, s := range numbered {
		if seasonYearClose(s.AirDate, years) {
			out = append(out, s.SeasonNumber)
		}
	}
	if len(out) > 0 {
		return out
	}
	for _, s := range numbered {
		out = append(out, s.SeasonNumber)
	}
	return out
}

func uniqueNumberedSeasons(seasons []tvSeasonBrief) []tvSeasonBrief {
	var out []tvSeasonBrief
	seen := map[int]struct{}{}
	for _, s := range seasons {
		if s.SeasonNumber < 1 {
			continue
		}
		if _, dup := seen[s.SeasonNumber]; dup {
			continue
		}
		seen[s.SeasonNumber] = struct{}{}
		out = append(out, s)
	}
	return out
}

func seasonYearClose(air string, years map[int]struct{}) bool {
	if len(years) == 0 {
		return true
	}
	if len(air) < 4 {
		return true
	}
	y, err := strconv.Atoi(air[:4])
	if err != nil {
		return true
	}
	for want := range years {
		if y == want || y == want-1 || y == want+1 {
			return true
		}
	}
	return false
}

func (c *Client) tvSeasonEpisodes(ctx context.Context, tmdbID uint64, season int) ([]airHit, error) {
	raw, err := c.get(ctx, "tv/"+strconv.FormatUint(tmdbID, 10)+"/season/"+strconv.Itoa(season), nil)
	if err != nil || len(raw) == 0 {
		return nil, err
	}
	var wrap struct {
		SeasonNumber int `json:"season_number"`
		Episodes     []struct {
			EpisodeNumber int    `json:"episode_number"`
			AirDate       string `json:"air_date"`
			Name          string `json:"name"`
			Overview      string `json:"overview"`
		} `json:"episodes"`
	}
	if !eatJSON(raw, &wrap) {
		return nil, nil
	}
	out := make([]airHit, 0, len(wrap.Episodes))
	for _, ep := range wrap.Episodes {
		d := strings.TrimSpace(ep.AirDate)
		if len(d) >= 10 {
			d = d[:10]
		}
		out = append(out, airHit{
			Season:   wrap.SeasonNumber,
			Episode:  ep.EpisodeNumber,
			AirDate:  d,
			Name:     ep.Name,
			Overview: ep.Overview,
		})
	}
	return out, nil
}

func pickAirHit(hits []airHit, hint string) (airHit, bool) {
	if len(hits) == 0 {
		return airHit{}, false
	}
	if len(hits) == 1 {
		return hits[0], true
	}
	best, bestN := 0, -1
	for i, h := range hits {
		n := hintOverlap(hint, h.Name+" "+h.Overview)
		if n > bestN {
			best, bestN = i, n
		}
	}
	if bestN <= 0 {
		return airHit{}, false
	}
	return hits[best], true
}

func hintOverlap(hint, blob string) int {
	ht := airTokens(hint)
	bt := airTokens(blob)
	if len(ht) == 0 || len(bt) == 0 {
		return 0
	}
	n := 0
	for t := range ht {
		if _, ok := bt[t]; ok {
			n++
		}
	}
	return n
}

func airTokens(s string) map[string]struct{} {
	out := map[string]struct{}{}
	var b strings.Builder
	flush := func() {
		t := strings.ToLower(b.String())
		b.Reset()
		if len(t) < 3 {
			return
		}
		switch t {
		case "the", "and", "show", "live", "starring", "tonight", "with", "from", "feat":
			return
		}
		out[t] = struct{}{}
	}
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
			continue
		}
		flush()
	}
	flush()
	return out
}
