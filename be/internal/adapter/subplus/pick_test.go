package subplus

import "testing"

func file(name string) SubFile {
	return SubFile{Name: name, Ext: ".srt", Text: "x"}
}

func TestParseSeasonEpisode(t *testing.T) {
	t.Parallel()
	cases := map[string]SeasonEpisode{
		"Euphoria.US.S03E08.1080p.WEB.h264-ETHEL": {Season: 3, Episode: 8, HasS: true, HasE: true},
		"friends s03 e12 UNCUT.DVDRip.XviD":       {Season: 3, Episode: 12, HasS: true, HasE: true},
		"The Wire 3x11.srt":                       {Season: 3, Episode: 11, HasS: true, HasE: true},
		"Episode 02.srt":                          {Episode: 2, HasE: true},
		"Some.Movie.2021.1080p.BluRay":            {},
	}
	for in, want := range cases {
		got := ParseSeasonEpisode(in)
		if got != want {
			t.Errorf("%q: got %+v want %+v", in, got, want)
		}
	}
}

func TestPickEpisodeFileSeasonPack(t *testing.T) {
	t.Parallel()
	files := []SubFile{
		file("Friends.S03E01.srt"),
		file("Friends.S03E12.srt"),
		file("Friends.S03E02.srt"),
	}
	got := PickEpisodeFile(files, 3, 12)
	if got == nil || got.Name != "Friends.S03E12.srt" {
		t.Fatalf("got %+v, want S03E12", got)
	}
}

func TestPickEpisodeFileRefusesFirstEntryOnMiss(t *testing.T) {
	t.Parallel()
	files := []SubFile{file("Friends.S03E01.srt"), file("Friends.S03E02.srt")}
	if got := PickEpisodeFile(files, 3, 12); got != nil {
		t.Fatalf("got %s, want nil (not files[0])", got.Name)
	}
}

func TestPickEpisodeFileSingle(t *testing.T) {
	t.Parallel()
	got := PickEpisodeFile([]SubFile{file("whatever.srt")}, 9, 9)
	if got == nil || got.Name != "whatever.srt" {
		t.Fatalf("got %+v", got)
	}
}

func TestPickMovieSubFileSkipsSDH(t *testing.T) {
	t.Parallel()
	files := []SubFile{file("movie.en[sdh].srt"), file("movie.en.srt")}
	got := PickMovieSubFile(files)
	if got == nil || got.Name != "movie.en.srt" {
		t.Fatalf("got %+v", got)
	}
}
