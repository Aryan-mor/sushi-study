package catalog_test

import (
	"testing"

	"github.com/Aryan-mor/sushi-study/be/internal/core/catalog"
)

func TestEpisodeWatched(t *testing.T) {
	t.Parallel()
	cases := []struct {
		pos, dur int32
		want     bool
	}{
		{0, 100, false},
		{89, 100, false},
		{90, 100, true},
		{100, 100, true},
		{50, 0, false},
		{0, 0, false},
	}
	for _, c := range cases {
		if got := catalog.EpisodeWatched(c.pos, c.dur); got != c.want {
			t.Errorf("EpisodeWatched(%d, %d) = %v, want %v", c.pos, c.dur, got, c.want)
		}
	}
}

func TestEpisodeStarted(t *testing.T) {
	t.Parallel()
	if catalog.EpisodeStarted(4, 100) {
		t.Error("4% of 100s must not count as started")
	}
	if !catalog.EpisodeStarted(5, 100) {
		t.Error("5% of 100s must count as started")
	}
	if catalog.EpisodeStarted(29, 0) {
		t.Error("29s with unknown duration must not count as started")
	}
	if !catalog.EpisodeStarted(30, 0) {
		t.Error("30s with unknown duration must count as started")
	}
}
