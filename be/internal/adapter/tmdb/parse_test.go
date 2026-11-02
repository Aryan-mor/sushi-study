package tmdb

import (
	"testing"

	"github.com/Aryan-mor/sushi-study/be/internal/core/catalog"
)

func TestParseTitleMovie(t *testing.T) {
	t.Parallel()
	raw := []byte(
		`{
		"backdrop_path":"/back.jpg",
		"runtime":136,
		"genres":[{"name":"Action"},{"name":"Science Fiction"}],
		"belongs_to_collection":{"id":2344,"name":"The Matrix Collection"},
		"credits":{"cast":[{"id":6384,"name":"Keanu Reeves","character":"Neo","profile_path":"/keanu.jpg"}]},
		"title":"The Matrix","overview":"A hacker.","poster_path":"/p.jpg","release_date":"1999-03-31","vote_average":8.2,
		"origin_country":["US"],
		"production_countries":[{"iso_3166_1":"GB","name":"United Kingdom"}],
		"images":{"logos":[{"file_path":"/mark.svg","iso_639_1":"en"},{"file_path":"/word.png","iso_639_1":"en"}]},
		"similar":{"results":[{"id":604,"title":"Reloaded","poster_path":"/rel.jpg","release_date":"2003-05-15","vote_average":7.2}]}
	}`,
	)
	d := parseTitle(raw)
	if d.Logo != "word" || d.Backdrop != "back" {
		t.Fatalf("images %+v", d)
	}
	if d.Genres != "Action, Science Fiction" || d.RuntimeS != 136*60 {
		t.Fatalf("genres/runtime %+v", d)
	}
	if len(d.People) != 1 || d.People[0].Name != "Keanu Reeves" || d.People[0].Profile != "keanu" || d.People[0].TMDBID != 6384 {
		t.Fatalf("people %+v", d.People)
	}
	page := parseTitlePage(raw, 603, catalog.KindMovie)
	if page.Title != "The Matrix" || page.TMDBID != 603 || page.Year != 1999 {
		t.Fatalf("page row %+v", page.Row)
	}
	if d.CollectionID != 2344 || d.CollectionName != "The Matrix Collection" {
		t.Fatalf("collection %+v", d)
	}
	if d.Country != "US" {
		t.Fatalf("country %q, want US from origin_country", d.Country)
	}
	if len(d.RelatedPreview) != 1 || d.RelatedPreview[0].TMDBID != 604 || d.RelatedPreview[0].Poster != "rel" {
		t.Fatalf("related %+v", d.RelatedPreview)
	}
}

func TestParseTitlePicksOfficialTrailerOverOthers(t *testing.T) {
	t.Parallel()
	raw := []byte(`{"title":"The Matrix","videos":{"results":[
		{"key":"teaser1","site":"YouTube","type":"Teaser","official":true},
		{"key":"vimeo1","site":"Vimeo","type":"Trailer","official":true},
		{"key":"unofficial1","site":"YouTube","type":"Trailer","official":false},
		{"key":"official1","site":"YouTube","type":"Trailer","official":true}
	]}}`)
	d := parseTitle(raw)
	if d.TrailerKey != "official1" {
		t.Fatalf("trailer key = %q, want official1 (YouTube trailer over Vimeo, non-YouTube, and unofficial)", d.TrailerKey)
	}
}

func TestParseTitleFallsBackToTeaserWhenNoTrailer(t *testing.T) {
	t.Parallel()
	raw := []byte(`{"title":"The Matrix","videos":{"results":[
		{"key":"clip1","site":"YouTube","type":"Clip","official":true},
		{"key":"teaser1","site":"YouTube","type":"Teaser","official":false}
	]}}`)
	d := parseTitle(raw)
	if d.TrailerKey != "teaser1" {
		t.Fatalf("trailer key = %q, want teaser1", d.TrailerKey)
	}
}

func TestParseTitleNoTrailerIsEmpty(t *testing.T) {
	t.Parallel()
	raw := []byte(`{"title":"The Matrix","videos":{"results":[]}}`)
	d := parseTitle(raw)
	if d.TrailerKey != "" {
		t.Fatalf("trailer key = %q, want empty", d.TrailerKey)
	}
}

func TestParseTitleOriginCountryWhenProductionEmpty(t *testing.T) {
	t.Parallel()
	// Blackmail (2026) / 1752176: origin_country NZ, production_countries [].
	raw := []byte(`{"title":"Blackmail","origin_country":["NZ"],"production_countries":[]}`)
	d := parseTitle(raw)
	if d.Country != "NZ" {
		t.Fatalf("country %q, want NZ", d.Country)
	}
}

func TestParseTitlePageIMDBID(t *testing.T) {
	t.Parallel()

	// movie: imdb_id is top-level.
	movie := parseTitlePage([]byte(`{"title":"Dune","imdb_id":"tt1160419","poster_path":"/d.jpg"}`), 438631, catalog.KindMovie)
	if movie.IMDBID != "tt1160419" {
		t.Errorf("movie IMDBID = %q, want tt1160419", movie.IMDBID)
	}

	// tv: imdb_id lives under external_ids.
	tv := parseTitlePage([]byte(`{"name":"Lost","external_ids":{"imdb_id":"tt0411008"}}`), 4607, catalog.KindSeries)
	if tv.IMDBID != "tt0411008" {
		t.Errorf("tv IMDBID = %q, want tt0411008", tv.IMDBID)
	}

	// neither present -> empty, no panic.
	none := parseTitlePage([]byte(`{"title":"X"}`), 1, catalog.KindMovie)
	if none.IMDBID != "" {
		t.Errorf("IMDBID = %q, want empty", none.IMDBID)
	}
}

func TestParseTitlePageSeriesLastAirDate(t *testing.T) {
	t.Parallel()

	prefersEpisode := parseTitlePage([]byte(`{
		"name":"Game of Thrones",
		"first_air_date":"2011-04-17",
		"last_air_date":"2019-05-01",
		"last_episode_to_air":{"air_date":"2019-05-19","episode_number":6,"season_number":8}
	}`), 1399, catalog.KindSeries)
	if prefersEpisode.Detail.LastAirDate != "2019-05-19" {
		t.Errorf("LastAirDate = %q, want last_episode_to_air 2019-05-19", prefersEpisode.Detail.LastAirDate)
	}

	fallback := parseTitlePage([]byte(`{
		"name":"Lost",
		"first_air_date":"2004-09-22",
		"last_air_date":"2010-05-23"
	}`), 4607, catalog.KindSeries)
	if fallback.Detail.LastAirDate != "2010-05-23" {
		t.Errorf("LastAirDate = %q, want last_air_date 2010-05-23", fallback.Detail.LastAirDate)
	}

	movie := parseTitlePage([]byte(`{"title":"Dune","release_date":"2021-10-22"}`), 438631, catalog.KindMovie)
	if movie.Detail.LastAirDate != "" {
		t.Errorf("movie LastAirDate = %q, want empty", movie.Detail.LastAirDate)
	}
}

func TestParseSearchMulti(t *testing.T) {
	t.Parallel()
	raw := []byte(
		`{
		"results":[
			{"id":603,"media_type":"movie","title":"The Matrix","original_title":"The Matrix","poster_path":"/p.jpg","release_date":"1999-03-31","vote_average":8.2},
			{"id":1396,"media_type":"tv","name":"Breaking Bad","original_name":"Breaking Bad","poster_path":"/bb.jpg","first_air_date":"2008-01-20","vote_average":9.1},
			{"id":1,"media_type":"person","name":"Someone"},
			{"id":603,"media_type":"movie","title":"Dup"},
			{"id":99,"media_type":"tv","name":"Heartless","original_name":"بی عاطفه","first_air_date":"2019-01-01","vote_average":7.0}
		]
	}`,
	)
	rows := parseSearchMulti(raw)
	if len(rows) != 3 {
		t.Fatalf("got %d rows, want 3 (person+dup dropped)", len(rows))
	}
	if rows[0].TMDBID != 603 || rows[0].Kind != catalog.KindMovie || rows[0].Title != "The Matrix" || rows[0].Poster != "p" {
		t.Fatalf("movie %+v", rows[0])
	}
	if rows[1].TMDBID != 1396 || rows[1].Kind != catalog.KindSeries || rows[1].Title != "Breaking Bad" {
		t.Fatalf("series %+v", rows[1])
	}
	if rows[2].TMDBID != 99 || rows[2].OrigTitle != "بی عاطفه" || rows[2].Title != "Heartless" {
		t.Fatalf("original_name %+v", rows[2])
	}
}

func TestSplitTitleYear(t *testing.T) {
	t.Parallel()
	title, year := splitTitleYear("Lets Love 2026")
	if title != "Lets Love" || year != 2026 {
		t.Fatalf("got %q %d", title, year)
	}
	title, year = splitTitleYear("The Matrix")
	if title != "The Matrix" || year != 0 {
		t.Fatalf("bare title: %q %d", title, year)
	}
}

func TestPickBackdropsPrefersPlainAndSkipsPrimary(t *testing.T) {
	t.Parallel()
	raw := []byte(`{"backdrops":[
		{"file_path":"/primary.jpg","iso_639_1":null,"vote_average":1},
		{"file_path":"/texten.jpg","iso_639_1":"en","vote_average":99},
		{"file_path":"/plainlow.jpg","iso_639_1":null,"vote_average":2},
		{"file_path":"/plainhigh.jpg","iso_639_1":null,"vote_average":8},
		{"file_path":"/mark.svg","iso_639_1":null,"vote_average":100}
	]}`)
	got := pickBackdrops(raw, "primary")
	if len(got) != 3 || got[0] != "plainhigh" || got[1] != "plainlow" || got[2] != "texten" {
		t.Fatalf("%v", got)
	}
}

func TestParseSearchTypedMovie(t *testing.T) {
	t.Parallel()
	raw := []byte(`{"results":[{"id":1360708,"title":"Let's Love","release_date":"2026-06-09","vote_average":0}]}`)
	rows := parseSearchTyped(raw, catalog.KindMovie)
	if len(rows) != 1 || rows[0].TMDBID != 1360708 || rows[0].Year != 2026 || rows[0].Kind != catalog.KindMovie {
		t.Fatalf("%+v", rows)
	}
}
