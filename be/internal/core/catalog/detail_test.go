package catalog

import "testing"

func TestParseOxplayerMetadata(t *testing.T) {
	t.Parallel()

	raw := []byte(
		`{
		"logoUrl":"https://image.tmdb.org/t/p/w500/wordmark.png",
		"backdropUrl":"https://image.tmdb.org/t/p/w1280/back.jpg",
		"genres":["Action","Science Fiction"],
		"runtimeSeconds":136,
		"people":[{"name":"Keanu Reeves","role":"Neo","profilePath":"/keanu.jpg"}],
		"tmdbCollectionId":2344,
		"tmdbCollectionName":"The Matrix Collection",
		"originCountry":["US"],
		"tmdbSimilar":{"results":[{"tmdbId":604,"title":"Matrix Reloaded"},{"tmdbId":604},{"id":605},{"tmdbId":0}]}
	}`,
	)
	d := ParseOxplayerMetadata(raw)
	if d.Logo != "wordmark" || d.Backdrop != "back" {
		t.Fatalf("images %+v", d)
	}
	if d.Genres != "Action, Science Fiction" || d.RuntimeS != 136 {
		t.Fatalf("genres/runtime %+v", d)
	}
	if d.Country != "US" {
		t.Fatalf("country %q", d.Country)
	}
	if len(d.People) != 1 || d.People[0].Name != "Keanu Reeves" || d.People[0].Role != "Neo" || d.People[0].Profile != "keanu" {
		t.Fatalf("people %+v", d.People)
	}
	if d.CollectionID != 2344 || d.CollectionName != "The Matrix Collection" {
		t.Fatalf("collection %+v", d)
	}
	if len(d.SimilarTMDB) != 2 || d.SimilarTMDB[0] != 604 || d.SimilarTMDB[1] != 605 {
		t.Fatalf("similar %+v", d.SimilarTMDB)
	}

	jelly := ParseOxplayerMetadata([]byte(`{"People":[{"Name":"Carrie-Anne Moss","Role":"Trinity"}]}`))
	if len(jelly.People) != 1 || jelly.People[0].Name != "Carrie-Anne Moss" {
		t.Fatalf("People key %+v", jelly.People)
	}

	round := UnmarshalDetail(MarshalDetail(d))
	if round.Logo != d.Logo || round.CollectionID != d.CollectionID || len(round.SimilarTMDB) != 2 || round.Country != "US" {
		t.Fatalf("roundtrip %+v", round)
	}
}

func TestComposeRelatedPrefersCatalogRow(t *testing.T) {
	t.Parallel()
	d := Detail{
		SimilarTMDB: []uint64{1, 2},
		RelatedPreview: []Row{
			{TMDBID: 1, Title: "preview-one", Poster: "prev1"},
			{TMDBID: 2, Title: "preview-two", Poster: "prev2"},
		},
	}
	got := ComposeRelated(d, []Row{{TMDBID: 1, Title: "catalog-one"}})
	if len(got) != 2 || got[0].Title != "catalog-one" || got[1].Title != "preview-two" {
		t.Fatalf("%+v", got)
	}
	if got[0].Poster != "prev1" {
		t.Fatalf("catalog without poster must keep preview poster, got %q", got[0].Poster)
	}
}

func TestParseStoredBackdrops(t *testing.T) {
	t.Parallel()
	if stems, known := ParseStoredBackdrops(""); known || stems != nil {
		t.Fatalf("missing key: %v %v", stems, known)
	}
	stems, known := ParseStoredBackdrops("[]")
	if !known || len(stems) != 0 {
		t.Fatalf("empty list: %v %v", stems, known)
	}
	stems, known = ParseStoredBackdrops(`["one","one"," ","two"]`)
	if !known || len(stems) != 2 || stems[0] != "one" || stems[1] != "two" {
		t.Fatalf("%v", stems)
	}
	round := UnmarshalDetail(MarshalDetail(Detail{Backdrops: []string{"one"}}))
	if len(round.Backdrops) != 1 || round.Backdrops[0] != "one" {
		t.Fatalf("roundtrip %+v", round.Backdrops)
	}
}

func TestDetailNeedsHydrate(t *testing.T) {
	t.Parallel()
	if !DetailNeedsHydrate(Detail{}) {
		t.Fatal("empty")
	}
	if !DetailNeedsHydrate(Detail{
		Logo:           "word",
		People:         []Person{{Name: "A"}},
		RelatedPreview: []Row{{TMDBID: 1}},
	}) {
		t.Fatal("people without tmdb id need hydrate")
	}
	if !DetailNeedsHydrate(Detail{
		People:         []Person{{Name: "A", TMDBID: 6384}},
		RelatedPreview: []Row{{TMDBID: 1}},
	}) {
		t.Fatal("missing logo needs hydrate")
	}
	if DetailNeedsHydrate(Detail{
		Logo:           "word",
		People:         []Person{{Name: "A", TMDBID: 6384}},
		RelatedPreview: []Row{{TMDBID: 1}},
		Country:        "US",
	}) {
		t.Fatal("complete")
	}
	if DetailNeedsHydrate(Detail{
		Logo:           LogoNone,
		People:         []Person{{Name: "A", TMDBID: 6384}},
		RelatedPreview: []Row{{TMDBID: 1}},
		Country:        CountryNone,
	}) {
		t.Fatal("LogoNone means checked")
	}
	if !DetailNeedsHydrate(Detail{
		Logo:           "word",
		People:         []Person{{Name: "A", TMDBID: 6384}},
		RelatedPreview: []Row{{TMDBID: 1}},
	}) {
		t.Fatal("missing country needs hydrate")
	}
	if WireLogo(LogoNone) != "" || WireLogo("") != "" || WireLogo("abc") != "abc" {
		t.Fatal("WireLogo")
	}
}

func TestParsePeopleProviderIds(t *testing.T) {
	t.Parallel()
	d := ParseOxplayerMetadata([]byte(`{
		"people":[{"Name":"Keanu","Role":"Neo","Id":"aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee","ProviderIds":{"Tmdb":"6384"}}]
	}`))
	if len(d.People) != 1 || d.People[0].TMDBID != 6384 {
		t.Fatalf("provider ids %+v", d.People)
	}
	jelly := ParseOxplayerMetadata([]byte(`{"People":[{"Name":"Carrie","Id":"not-a-number"}]}`))
	if len(jelly.People) != 1 || jelly.People[0].TMDBID != 0 {
		t.Fatalf("guid must stay zero %+v", jelly.People)
	}
}

func TestMergeDetailReplacesPeopleWithoutIDs(t *testing.T) {
	t.Parallel()
	base := Detail{People: []Person{{Name: "Keanu"}}}
	extra := Detail{People: []Person{{Name: "Keanu Reeves", TMDBID: 6384, Role: "Neo"}}}
	got := MergeDetail(base, extra)
	if len(got.People) != 1 || got.People[0].TMDBID != 6384 {
		t.Fatalf("%+v", got.People)
	}
}

func TestAbsentSentinelsMatch(t *testing.T) {
	t.Parallel()
	if LogoNone != CountryNone || CountryNone != TrailerNone || TrailerNone != NetworkNone {
		t.Fatal("preferPresent compares every absent sentinel to LogoNone")
	}
}

func TestMergeDetailFillsCountry(t *testing.T) {
	t.Parallel()
	got := MergeDetail(Detail{Country: CountryNone}, Detail{Country: "JP"})
	if got.Country != "JP" {
		t.Fatalf("CountryNone should yield to a real code, got %q", got.Country)
	}
	got = MergeDetail(Detail{Country: "US"}, Detail{Country: "JP"})
	if got.Country != "US" {
		t.Fatalf("kept %q", got.Country)
	}
}

func TestMergeDetailFillsTrailer(t *testing.T) {
	t.Parallel()
	got := MergeDetail(Detail{TrailerKey: TrailerNone}, Detail{TrailerKey: "abc123"})
	if got.TrailerKey != "abc123" {
		t.Fatalf("TrailerNone should yield to a real key, got %q", got.TrailerKey)
	}
	got = MergeDetail(Detail{TrailerKey: "kept123"}, Detail{TrailerKey: "abc123"})
	if got.TrailerKey != "kept123" {
		t.Fatalf("kept %q", got.TrailerKey)
	}
	if WireTrailer(TrailerNone) != "" || WireTrailer("") != "" || WireTrailer("abc123") != "abc123" {
		t.Fatal("WireTrailer")
	}
	if got := MarkTrailerAbsent(Detail{}); got.TrailerKey != TrailerNone {
		t.Fatalf("MarkTrailerAbsent on empty = %q, want TrailerNone", got.TrailerKey)
	}
	if got := MarkTrailerAbsent(Detail{TrailerKey: "abc123"}); got.TrailerKey != "abc123" {
		t.Fatalf("MarkTrailerAbsent must not overwrite a real key, got %q", got.TrailerKey)
	}
}

func TestTrailerKeyRoundtrip(t *testing.T) {
	t.Parallel()
	d := Detail{TrailerKey: "abc123"}
	got := UnmarshalDetail(MarshalDetail(d))
	if got.TrailerKey != "abc123" {
		t.Fatalf("trailerKey = %q, want abc123", got.TrailerKey)
	}
}

func TestRelatedPreviewRoundtrip(t *testing.T) {
	t.Parallel()
	d := Detail{RelatedPreview: []Row{{
		TMDBID: 604, Kind: KindMovie, Title: "Reloaded", Year: 2003, Rating: 70, Poster: "rel",
	}}}
	got := UnmarshalDetail(MarshalDetail(d))
	if len(got.RelatedPreview) != 1 || got.RelatedPreview[0].Title != "Reloaded" {
		t.Fatalf("%+v", got.RelatedPreview)
	}
}

func TestParseOxplayerMetadataEmpty(t *testing.T) {
	t.Parallel()
	if ParseOxplayerMetadata(nil).Logo != "" {
		t.Fatal("nil")
	}
	if UnmarshalDetail(nil).RuntimeS != 0 {
		t.Fatal("empty detail")
	}
}
