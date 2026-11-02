package tmdb

import (
	"context"
	"strconv"
)

// TVHasEpisode reports whether TMDB lists this season (and episode, when episode > 0) for the
// series. GET /tv/{id}/season/{n}[/episode/{e}]: 404 is false, not an error.
func (c *Client) TVHasEpisode(ctx context.Context, tmdbID uint64, season, episode int) (bool, error) {
	if c == nil || c.key == "" || tmdbID == 0 {
		return false, nil
	}
	path := "tv/" + strconv.FormatUint(tmdbID, 10) + "/season/" + strconv.Itoa(season)
	if episode > 0 {
		path += "/episode/" + strconv.Itoa(episode)
	}
	raw, err := c.get(ctx, path, nil)
	if err != nil {
		return false, err
	}
	return len(raw) > 0, nil
}
