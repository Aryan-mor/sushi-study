package tmdb

import (
	"context"
	"strconv"

	"github.com/Aryan-mor/sushi-study/be/internal/core/catalog"
)

// SeriesLastAirDate is the cheap /tv GET (no credits/images). Browse YEAR sort and the library
// backfill use it so a 1989 show ranks by TMDB's last episode, not our ingest clock.
func (c *Client) SeriesLastAirDate(ctx context.Context, tmdbID uint64) (string, error) {
	if c == nil || c.key == "" || tmdbID == 0 {
		return "", nil
	}
	raw, err := c.get(ctx, "tv/"+strconv.FormatUint(tmdbID, 10), nil)
	if err != nil {
		return "", err
	}
	if len(raw) == 0 {
		return "", nil
	}
	return parseTitlePage(raw, tmdbID, catalog.KindSeries).Detail.LastAirDate, nil
}
