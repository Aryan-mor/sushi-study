package tmdb

import (
	"context"
	"encoding/json"
	"net/url"
	"strconv"
	"strings"

	"github.com/Aryan-mor/sushi-study/be/internal/core/catalog"
)

const (
	searchPerSourceCap = 20
	// 20 movie + 20 tv + 20 multi. A shared 20-row merge cap lets search/movie fill every slot
	// and then drop search/tv (Secret Service S01E05 never saw series 287450).
	searchMergeCap = 60
)

// SearchTitles runs TMDB GET /search/multi and returns movie/tv compact rows only (ADR 0010).
// Empty key or empty query yields nil. Person hits are dropped.
//
// When the query ends in a 19xx/20xx year, typed searches use year= / first_air_date_year=.
// Without a year, typed searches still run: search/multi ranks by popularity and often omits a
// new TV show that shares a title with an older film (Secret Service 2026 vs movie 733976).
func (c *Client) SearchTitles(ctx context.Context, query string) ([]catalog.Row, error) {
	if c == nil || c.key == "" {
		return nil, nil
	}
	q := strings.TrimSpace(query)
	if q == "" {
		return nil, nil
	}
	raw, err := c.get(ctx, "search/multi", url.Values{"query": {q}})
	if err != nil {
		return nil, err
	}
	title, year := splitTitleYear(q)
	if title == "" {
		title = q
	}
	// Typed hits first: search/multi can fill the 20-row cap with popular films and then
	// mergeSearchRows would drop the new TV show that search/tv actually returned.
	return mergeSearchRows(c.typedSearch(ctx, title, year), parseSearchMulti(raw)), nil
}

func (c *Client) typedSearch(ctx context.Context, title string, year uint32) []catalog.Row {
	movieQ := url.Values{"query": {title}}
	tvQ := url.Values{"query": {title}}
	if year > 0 {
		ys := strconv.FormatUint(uint64(year), 10)
		movieQ.Set("year", ys)
		tvQ.Set("first_air_date_year", ys)
	}
	var out []catalog.Row
	if tvRaw, e := c.get(ctx, "search/tv", tvQ); e == nil {
		out = mergeSearchRows(out, parseSearchTyped(tvRaw, catalog.KindSeries))
	}
	if movieRaw, e := c.get(ctx, "search/movie", movieQ); e == nil {
		out = mergeSearchRows(out, parseSearchTyped(movieRaw, catalog.KindMovie))
	}
	return out
}

type searchHit struct {
	ID            uint64  `json:"id"`
	MediaType     string  `json:"media_type"`
	Title         string  `json:"title"`
	Name          string  `json:"name"`
	OriginalTitle string  `json:"original_title"`
	OriginalName  string  `json:"original_name"`
	PosterPath    string  `json:"poster_path"`
	ReleaseDate   string  `json:"release_date"`
	FirstAir      string  `json:"first_air_date"`
	Vote          float64 `json:"vote_average"`
}

func parseSearchMulti(raw []byte) []catalog.Row {
	var wrap struct {
		Results []searchHit `json:"results"`
	}
	if json.Unmarshal(raw, &wrap) != nil {
		return nil
	}
	out := make([]catalog.Row, 0, searchPerSourceCap)
	seen := map[string]struct{}{}
	for _, r := range wrap.Results {
		var kind catalog.Kind
		switch r.MediaType {
		case "movie":
			kind = catalog.KindMovie
		case "tv":
			kind = catalog.KindSeries
		default:
			continue
		}
		out = appendSearchRow(out, seen, rowFromHit(r, kind), searchPerSourceCap)
	}
	return out
}

func parseSearchTyped(raw []byte, kind catalog.Kind) []catalog.Row {
	var wrap struct {
		Results []searchHit `json:"results"`
	}
	if json.Unmarshal(raw, &wrap) != nil {
		return nil
	}
	out := make([]catalog.Row, 0, searchPerSourceCap)
	seen := map[string]struct{}{}
	for _, r := range wrap.Results {
		out = appendSearchRow(out, seen, rowFromHit(r, kind), searchPerSourceCap)
	}
	return out
}

func rowFromHit(r searchHit, kind catalog.Kind) catalog.Row {
	title := strings.TrimSpace(r.Title)
	if title == "" {
		title = strings.TrimSpace(r.Name)
	}
	date := r.ReleaseDate
	if date == "" {
		date = r.FirstAir
	}
	return catalog.Row{
		TMDBID:    r.ID,
		Kind:      kind,
		Title:     title,
		OrigTitle: distinctOrig(title, r.OriginalTitle, r.OriginalName),
		Year:      catalog.Year(nil, date),
		Rating:    catalog.Rating(strconv.FormatFloat(r.Vote, 'f', 1, 64)),
		Poster:    catalog.PosterPath(r.PosterPath),
	}
}

func appendSearchRow(out []catalog.Row, seen map[string]struct{}, row catalog.Row, maxN int) []catalog.Row {
	if row.TMDBID == 0 || len(out) >= maxN {
		return out
	}
	key := strconv.FormatUint(row.TMDBID, 10) + ":" + strconv.Itoa(int(row.Kind))
	if _, ok := seen[key]; ok {
		return out
	}
	seen[key] = struct{}{}
	return append(out, row)
}

func mergeSearchRows(base, extra []catalog.Row) []catalog.Row {
	seen := map[string]struct{}{}
	out := make([]catalog.Row, 0, searchMergeCap)
	for _, r := range base {
		out = appendSearchRow(out, seen, r, searchMergeCap)
	}
	for _, r := range extra {
		out = appendSearchRow(out, seen, r, searchMergeCap)
	}
	return out
}

// splitTitleYear peels a trailing 19xx/20xx token off a search query so the year-filtered
// movie/tv endpoints get a bare title.
func splitTitleYear(q string) (title string, year uint32) {
	fs := strings.Fields(strings.TrimSpace(q))
	if len(fs) < 2 {
		return q, 0
	}
	last := strings.Trim(fs[len(fs)-1], "()[]{}")
	if len(last) != 4 || (last[:2] != "19" && last[:2] != "20") {
		return q, 0
	}
	y, err := strconv.Atoi(last)
	if err != nil {
		return q, 0
	}
	return strings.Join(fs[:len(fs)-1], " "), uint32(y)
}

// distinctOrig keeps TMDB original_title/original_name only when it differs from the
// localised card title, so classify can score a native-script query against it.
func distinctOrig(title, origTitle, origName string) string {
	orig := strings.TrimSpace(origTitle)
	if orig == "" {
		orig = strings.TrimSpace(origName)
	}
	if orig == title {
		return ""
	}
	return orig
}
