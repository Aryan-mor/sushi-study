package api_test

import (
	"testing"

	"github.com/Aryan-mor/sushi-study/be/internal/app/api"
	"github.com/Aryan-mor/sushi-study/be/internal/core/catalog"
)

func TestMarshalLikedTitlesRoundTrip(t *testing.T) {
	t.Parallel()
	in := []api.TasteTitle{
		{TMDBID: 671, Kind: catalog.KindMovie},
		{TMDBID: 1668, Kind: catalog.KindSeries},
	}
	raw, err := api.MarshalLikedTitles(in)
	if err != nil {
		t.Fatal(err)
	}
	out, err := api.UnmarshalLikedTitles(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 2 || out[0].TMDBID != 671 || out[0].Kind != catalog.KindMovie {
		t.Fatalf("got %+v", out)
	}
	if out[1].TMDBID != 1668 || out[1].Kind != catalog.KindSeries {
		t.Fatalf("got %+v", out)
	}
}
