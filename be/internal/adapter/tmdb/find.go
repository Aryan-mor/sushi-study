package tmdb

import (
	"context"
	"encoding/json"
	"net/url"
	"regexp"
	"strings"

	"github.com/Aryan-mor/sushi-study/be/internal/core/catalog"
)

var reIMDBID = regexp.MustCompile(`(?i)^tt\d{5,}$`)

// FindByIMDB runs TMDB GET /find/{imdb_id}?external_source=imdb_id and returns the first movie
// or, if none, the first TV show. Person / episode / season hits are ignored. Empty key, empty
// id, or a 404-shaped empty body yields a zero Item, not an error.
func (c *Client) FindByIMDB(ctx context.Context, imdbID string) (catalog.Item, error) {
	id := canonicalIMDB(imdbID)
	if c == nil || c.key == "" || id == "" {
		return catalog.Item{}, nil
	}
	raw, err := c.get(ctx, "find/"+url.PathEscape(id), url.Values{"external_source": {"imdb_id"}})
	if err != nil {
		return catalog.Item{}, err
	}
	if len(raw) == 0 {
		return catalog.Item{}, nil
	}
	return parseFind(raw, id), nil
}

func canonicalIMDB(id string) string {
	id = strings.TrimSpace(id)
	if !reIMDBID.MatchString(id) {
		return ""
	}
	return strings.ToLower(id)
}

func parseFind(raw []byte, imdbID string) catalog.Item {
	var wrap struct {
		MovieResults []searchHit `json:"movie_results"`
		TVResults    []searchHit `json:"tv_results"`
	}
	if json.Unmarshal(raw, &wrap) != nil {
		return catalog.Item{}
	}
	if len(wrap.MovieResults) > 0 {
		return catalog.Item{Row: rowFromHit(wrap.MovieResults[0], catalog.KindMovie), IMDBID: imdbID}
	}
	if len(wrap.TVResults) > 0 {
		return catalog.Item{Row: rowFromHit(wrap.TVResults[0], catalog.KindSeries), IMDBID: imdbID}
	}
	return catalog.Item{}
}
