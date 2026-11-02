package newsroom

import (
	"strings"
	"testing"

	"github.com/Aryan-mor/sushi-study/be/internal/core/catalog"
)

func TestFormatLaterAddedCarriesTitleAndAppFooter(t *testing.T) {
	t.Parallel()

	item := catalog.Item{
		Row: catalog.Row{
			TMDBID: 550,
			Kind:   catalog.KindMovie,
			Title:  "Fight Club",
			Year:   1999,
			Rating: 84,
			Poster: "p.jpg",
		},
		Overview: "An insomniac office worker.",
		Detail:   catalog.Detail{Genres: "Drama", RuntimeS: 8340},
	}
	fa := FormatLaterAdded("fa", item)
	if !strings.Contains(fa, "رفت تو لیست بعداً تماشا") {
		t.Errorf("fa header missing: %s", fa)
	}
	if !strings.Contains(fa, "سوشی") {
		t.Errorf("fa footer should point at the app: %s", fa)
	}
	if !strings.Contains(fa, "Fight Club") {
		t.Errorf("fa title missing: %s", fa)
	}
	en := FormatLaterAdded("en", item)
	if !strings.Contains(en, "Watch Later") {
		t.Errorf("en header missing: %s", en)
	}
	if !strings.Contains(en, "Sushi") {
		t.Errorf("en footer should point at the app: %s", en)
	}
}
