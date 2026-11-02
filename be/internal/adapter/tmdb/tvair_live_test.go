//go:build livetmdb

package tmdb

import (
	"os"
	"strings"
	"testing"
)

func TestLiveFallonElizabethWarrenDates(t *testing.T) {
	key := os.Getenv("TMDB_API_KEY")
	if key == "" {
		t.Skip("set TMDB_API_KEY")
	}
	c := New(key)
	ctx := t.Context()
	years := map[int]struct{}{2019: {}}
	seasons, err := c.tvSeasonNums(ctx, 59941, years)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("season nums for 2019±1: %v", seasons)
	want := map[string]struct{}{"2019-12-04": {}, "2019-04-12": {}}
	for _, n := range seasons {
		eps, err := c.tvSeasonEpisodes(ctx, 59941, n)
		if err != nil {
			t.Fatal(err)
		}
		for _, ep := range eps {
			name := strings.ToLower(ep.Name + " " + ep.Overview)
			_, dateHit := want[ep.AirDate]
			guest := strings.Contains(name, "warren") || strings.Contains(name, "keira") || strings.Contains(name, "knightley")
			if dateHit || guest {
				t.Logf("S%dE%d air=%s name=%q", ep.Season, ep.Episode, ep.AirDate, ep.Name)
			}
		}
	}
	s, e, ok, err := c.TVFindByAirDate(ctx, 59941, []string{"2019-12-04", "2019-04-12"}, "Elizabeth Warren")
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("TVFindByAirDate ok=%v S%dE%d", ok, s, e)
}
