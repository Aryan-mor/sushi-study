package catalog_test

import (
	"testing"

	"github.com/Aryan-mor/sushi-study/be/internal/core/catalog"
)

func TestPosterPath(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		in   string
		want string
	}{
		// The shape the prototype actually stores, verified against the dev database.
		"real url": {
			"https://image.tmdb.org/t/p/w500/mk8UH7JRmK8adcqJJpB1ygP7B1C.jpg",
			"mk8UH7JRmK8adcqJJpB1ygP7B1C",
		},
		"other size":    {"https://image.tmdb.org/t/p/original/abc123.jpg", "abc123"},
		"png":           {"https://image.tmdb.org/t/p/w342/abc123.png", "abc123"},
		"empty":         {"", ""},
		"not tmdb":      {"https://example.com/poster.jpg", ""},
		"no size":       {"https://image.tmdb.org/t/p/abc123.jpg", ""},
		"query string":  {"https://image.tmdb.org/t/p/w500/abc.jpg?v=2", ""},
		"relative path": {"/keanu.jpg", "keanu"},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			if got := catalog.PosterPath(tc.in); got != tc.want {
				t.Errorf("PosterPath(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

// PosterURL is PosterPath's inverse, and the extension is the part that is easy to forget: a URL
// missing ".jpg" is a 404, which Telegram reports only as a failed sendPhoto, so the poster stops
// appearing without anything logging an error.
func TestPosterURL(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		path, size, want string
	}{
		"stored stem": {
			"mk8UH7JRmK8adcqJJpB1ygP7B1C", "w500",
			"https://image.tmdb.org/t/p/w500/mk8UH7JRmK8adcqJJpB1ygP7B1C.jpg",
		},
		"default size": {"abc123", "", "https://image.tmdb.org/t/p/w500/abc123.jpg"},
		"review width": {"abc123", "w342", "https://image.tmdb.org/t/p/w342/abc123.jpg"},
		// the prototype's people.profilePath arrives in this form, extension already attached.
		"raw tmdb path": {"/keanu.jpg", "w342", "https://image.tmdb.org/t/p/w342/keanu.jpg"},
		"png kept":      {"wordmark.png", "w500", "https://image.tmdb.org/t/p/w500/wordmark.png"},
		"no poster":     {"", "w500", ""},
		"blank":         {"   ", "w500", ""},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			if got := catalog.PosterURL(tc.path, tc.size); got != tc.want {
				t.Errorf("PosterURL(%q, %q) = %q, want %q", tc.path, tc.size, got, tc.want)
			}
		})
	}
}

// A stem that went through PosterPath must come back out as the URL it came from, or one of the
// two is wrong about the shape TMDB serves.
func TestPosterURLRoundTripsPosterPath(t *testing.T) {
	t.Parallel()

	const url = "https://image.tmdb.org/t/p/w500/mk8UH7JRmK8adcqJJpB1ygP7B1C.jpg"
	if got := catalog.PosterURL(catalog.PosterPath(url), "w500"); got != url {
		t.Errorf("round trip gave %q, want %q", got, url)
	}
}

func TestPosterPathIsTheBudgetedSize(t *testing.T) {
	t.Parallel()

	// docs/12 §1 budgets 27 bytes for this field, which is what makes a home screen fit in one
	// message. If TMDB ever changed the shape, the size math would quietly stop holding.
	got := catalog.PosterPath("https://image.tmdb.org/t/p/w500/mk8UH7JRmK8adcqJJpB1ygP7B1C.jpg")
	if len(got) != 27 {
		t.Errorf("poster path is %d bytes, the wire budget assumes 27", len(got))
	}
}

func TestRating(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		in   string
		want uint32
	}{
		"typical":   {"7.609", 76},
		"rounds up": {"6.45", 65},
		"integer":   {"8", 80},
		"max":       {"10", 100},
		"empty":     {"", 0},
		"garbage":   {"n/a", 0},
		"negative":  {"-3", 0},
		"over ten":  {"11.5", 100},
		"padded":    {" 6.4 ", 64},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			if got := catalog.Rating(tc.in); got != tc.want {
				t.Errorf("Rating(%q) = %d, want %d", tc.in, got, tc.want)
			}
		})
	}
}

func TestRatingFitsOneByte(t *testing.T) {
	t.Parallel()

	// The wire carries this as a single byte; anything above 100 would mean the row is malformed.
	for _, in := range []string{"10", "10.0", "99", "1e9"} {
		if got := catalog.Rating(in); got > 100 {
			t.Errorf("Rating(%q) = %d, must never exceed 100", in, got)
		}
	}
}

func TestYear(t *testing.T) {
	t.Parallel()

	y2001 := int32(2001)
	zero := int32(0)

	cases := map[string]struct {
		production *int32
		premiere   string
		want       uint32
	}{
		"production year wins": {&y2001, "1999-05-01", 2001},
		"falls back to date":   {nil, "1993-11-11", 1993},
		"zero is not a year":   {&zero, "1993-11-11", 1993},
		"neither":              {nil, "", 0},
		"nonsense date":        {nil, "soon", 0},
		"implausible year":     {nil, "0007-01-01", 0},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			if got := catalog.Year(tc.production, tc.premiere); got != tc.want {
				t.Errorf("Year(%v, %q) = %d, want %d", tc.production, tc.premiere, got, tc.want)
			}
		})
	}
}

func TestTMDBID(t *testing.T) {
	t.Parallel()

	ok := "603"
	padded := " 27205 "
	zero := "0"
	junk := "tt0133093" // an IMDb id, which is not the address this protocol uses

	if id, valid := catalog.TMDBID(&ok); !valid || id != 603 {
		t.Errorf("TMDBID(603) = %d, %v", id, valid)
	}
	if id, valid := catalog.TMDBID(&padded); !valid || id != 27205 {
		t.Errorf("TMDBID(padded) = %d, %v", id, valid)
	}
	for _, bad := range []*string{nil, &zero, &junk} {
		if _, valid := catalog.TMDBID(bad); valid {
			t.Errorf("TMDBID(%v) reported valid; a row without an address must be dropped", bad)
		}
	}
}
