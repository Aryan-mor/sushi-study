package tmdb

import (
	"context"
	"encoding/json"
	"net/url"
	"strconv"

	"github.com/Aryan-mor/sushi-study/be/internal/core/catalog"
)

// TrendingWeek loads TMDB's own /trending/{movie,tv}/week list, one page, in TMDB's rank order.
// The rails builder intersects it with our catalog (docs/12 §2.3) -- this call never reaches the
// request path (R-RAIL-3), only the hourly rebuild.
func (c *Client) TrendingWeek(ctx context.Context, tab catalog.Kind) ([]uint64, error) {
	rows, err := c.TrendingTitles(ctx, tab, 1)
	if err != nil {
		return nil, err
	}
	ids := make([]uint64, 0, len(rows))
	for _, r := range rows {
		ids = append(ids, r.TMDBID)
	}
	return ids, nil
}

// TrendingTitles returns compact cards (title + poster) from TMDB trending for the taste wizard.
// page is 1-based; TMDB returns up to 20 results per page.
func (c *Client) TrendingTitles(ctx context.Context, tab catalog.Kind, page int) ([]catalog.Row, error) {
	if c == nil || c.key == "" {
		return nil, nil
	}
	if page < 1 {
		page = 1
	}
	path := "trending/movie/week"
	kind := catalog.KindMovie
	if tab == catalog.KindSeries {
		path = "trending/tv/week"
		kind = catalog.KindSeries
	}
	raw, err := c.get(ctx, path, url.Values{"page": {strconv.Itoa(page)}})
	if err != nil {
		return nil, err
	}
	return parseTrendingRows(raw, kind), nil
}

func parseTrending(raw []byte) []uint64 {
	rows := parseTrendingRows(raw, catalog.KindMovie)
	ids := make([]uint64, 0, len(rows))
	seen := map[uint64]struct{}{}
	for _, r := range rows {
		if r.TMDBID == 0 {
			continue
		}
		if _, ok := seen[r.TMDBID]; ok {
			continue
		}
		seen[r.TMDBID] = struct{}{}
		ids = append(ids, r.TMDBID)
	}
	return ids
}

func parseTrendingRows(raw []byte, kind catalog.Kind) []catalog.Row {
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
