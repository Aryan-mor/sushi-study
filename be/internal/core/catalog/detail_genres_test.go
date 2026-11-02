package catalog

import (
	"encoding/json"
	"testing"
)

func TestSplitGenresDropsBlanksAndTrims(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		in   string
		want []string
	}{
		{"", nil},
		{"   ", nil},
		{"Action", []string{"Action"}},
		{"Action, Science Fiction", []string{"Action", "Science Fiction"}},
		{"Action,Science Fiction", []string{"Action", "Science Fiction"}},
		{"Action, , Drama", []string{"Action", "Drama"}},
		{"Sci-Fi & Fantasy", []string{"Sci-Fi & Fantasy"}},
	} {
		got := SplitGenres(tc.in)
		if len(got) != len(tc.want) {
			t.Fatalf("SplitGenres(%q) = %q, want %q", tc.in, got, tc.want)
		}
		for i := range got {
			if got[i] != tc.want[i] {
				t.Fatalf("SplitGenres(%q) = %q, want %q", tc.in, got, tc.want)
			}
		}
	}
}

// The whole point of genreList: browse asks the database "does this title have exactly this genre",
// and gets an answer that does not also match every genre this one is a substring of.
func TestMarshalDetailWritesGenreListBesideTheDisplayString(t *testing.T) {
	t.Parallel()

	raw := MarshalDetail(Detail{Genres: "Sci-Fi & Fantasy, Drama"})

	var stored struct {
		Genres    string   `json:"genres"`
		GenreList []string `json:"genreList"`
	}
	if err := json.Unmarshal(raw, &stored); err != nil {
		t.Fatalf("unmarshal %s: %v", raw, err)
	}
	if stored.Genres != "Sci-Fi & Fantasy, Drama" {
		t.Errorf("display string = %q, want it untouched", stored.Genres)
	}
	if len(stored.GenreList) != 2 || stored.GenreList[0] != "Sci-Fi & Fantasy" || stored.GenreList[1] != "Drama" {
		t.Errorf("genreList = %q, want the two names split apart", stored.GenreList)
	}
	// "Fantasy" is a substring of the first name and must not be one of the names.
	for _, g := range stored.GenreList {
		if g == "Fantasy" {
			t.Error("genreList contains Fantasy; containment would match a genre the title lacks")
		}
	}
}

func TestDetailRoundTripKeepsGenresAsTheDisplayValue(t *testing.T) {
	t.Parallel()

	got := UnmarshalDetail(MarshalDetail(Detail{Genres: "Action, Drama", RuntimeS: 90}))
	if got.Genres != "Action, Drama" {
		t.Errorf("Genres = %q, want the display string back unchanged", got.Genres)
	}
	if got.RuntimeS != 90 {
		t.Errorf("RuntimeS = %d, want 90", got.RuntimeS)
	}
}

func TestMarshalDetailStaysEmptyWhenThereIsNothingToStore(t *testing.T) {
	t.Parallel()

	if got := string(MarshalDetail(Detail{})); got != "{}" {
		t.Errorf("MarshalDetail(empty) = %s, want {}", got)
	}
}

func TestMarshalDetailWritesLastAirDate(t *testing.T) {
	t.Parallel()

	raw := MarshalDetail(Detail{LastAirDate: "2019-05-19"})
	got := UnmarshalDetail(raw)
	if got.LastAirDate != "2019-05-19" {
		t.Errorf("LastAirDate = %q, want 2019-05-19", got.LastAirDate)
	}
}
