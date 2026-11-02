package api

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/Aryan-mor/sushi-study/be/internal/core/catalog"
	sushiv1 "github.com/Aryan-mor/sushi-study/be/internal/protocol/sushi/v1"
)

// TasteProfile is what the wizard stores on user_prefs.
type TasteProfile struct {
	WatchKind        string
	Genres           []string
	LikedTitles      []TasteTitle
	LikedPeople      []uint64
	MaxHeight        int32
	Audio            string
	SubtitleLang     string
	Subs             string
	TasteCompletedAt *time.Time
}

// TasteTitle is one liked movie/series.
type TasteTitle struct {
	TMDBID uint64
	Kind   catalog.Kind
}

// TastePerson is a compact person card for seeds / search.
type TastePerson struct {
	TMDBID uint64
	Name   string
	Poster string
}

// TasteStore reads and writes the taste profile.
type TasteStore interface {
	Taste(ctx context.Context, userID int64) (TasteProfile, error)
	SetTaste(ctx context.Context, userID int64, p TasteProfile) error
}

// PersonSearcher is TMDB person search + popular seeds (taste wizard). Declared by the consumer.
type PersonSearcher interface {
	SearchPeople(ctx context.Context, query string) ([]TastePerson, error)
	PersonBrief(ctx context.Context, personID uint64) (TastePerson, error)
	PopularPeople(ctx context.Context, page int) ([]TastePerson, error)
}

// TasteTitleSeeder loads compact trending cards for the wizard picker (one TMDB call per page).
type TasteTitleSeeder interface {
	TrendingTitles(ctx context.Context, kind catalog.Kind, page int) ([]catalog.Row, error)
}

const tasteSeedPageSize = 20

// seedTitle is a fixed TMDB id used for the wizard picker (verified against TMDB).
type seedTitle struct {
	id   uint64
	kind catalog.Kind
}

var (
	seedMovies = []seedTitle{
		{671, catalog.KindMovie},    // Harry Potter and the Philosopher's Stone
		{1726, catalog.KindMovie},   // Iron Man
		{557, catalog.KindMovie},    // Spider-Man (2002)
		{843346, catalog.KindMovie}, // Fossil (Fasil)
		{448119, catalog.KindMovie}, // Good, Bad, Jelf
	}
	seedSeries = []seedTitle{
		{1668, catalog.KindSeries},  // Friends
		{1396, catalog.KindSeries},  // Breaking Bad
		{66732, catalog.KindSeries}, // Stranger Things
	}
	seedPeopleIDs = []uint64{
		31,      // Tom Hanks
		6193,    // Leonardo DiCaprio
		1245,    // Scarlett Johansson
		287,     // Brad Pitt
		500,     // Robert Downey Jr.
		3223,    // Meryl Streep
		1892,    // Matt Damon
		85,      // Johnny Depp
		1136406, // Golshifteh Farahani — may 404; skipped if empty
	}

	validWatchKinds   = map[string]bool{"": true, "movie": true, "series": true, "both": true}
	validTasteAudio   = map[string]bool{"": true, "original": true, "dub": true}
	validTasteSubs    = map[string]bool{"": true, "on": true, "off": true}
	validTasteHeights = map[int32]bool{0: true, 480: true, 720: true, 1080: true, 2160: true}
)

// WithTaste attaches taste profile storage.
func (a *API) WithTaste(s TasteStore) *API {
	a.taste = s
	return a
}

// WithPersonSearch attaches TMDB person search for the taste wizard.
func (a *API) WithPersonSearch(s PersonSearcher) *API {
	a.peopleSearch = s
	return a
}

// WithTasteSeeds attaches TMDB trending title cards for the wizard picker.
func (a *API) WithTasteSeeds(s TasteTitleSeeder) *API {
	a.tasteSeeds = s
	return a
}

// Taste handles read / write / person-search for the in-app taste wizard.
func (a *API) Taste(ctx context.Context, p Principal, req *sushiv1.TasteReq) (*sushiv1.TasteRes, error) {
	if a.taste == nil {
		return nil, fmt.Errorf("taste: not configured")
	}
	if q := strings.TrimSpace(req.GetPersonQuery()); q != "" {
		if req.GetWrite() {
			return nil, fmt.Errorf("taste: person_query cannot write")
		}
		return a.tastePersonSearch(ctx, q)
	}
	if req.GetWrite() {
		return a.tasteWrite(ctx, p.UserID, req)
	}
	return a.tasteRead(ctx, p.UserID, req)
}

func (a *API) tastePersonSearch(ctx context.Context, q string) (*sushiv1.TasteRes, error) {
	if a.peopleSearch == nil {
		return &sushiv1.TasteRes{}, nil
	}
	hits, err := a.peopleSearch.SearchPeople(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("taste: person search: %w", err)
	}
	out := &sushiv1.TasteRes{}
	for _, h := range hits {
		out.PersonHits = append(out.PersonHits, &sushiv1.TastePerson{
			TmdbId: h.TMDBID,
			Name:   h.Name,
			Poster: h.Poster,
		})
	}
	return out, nil
}

func (a *API) tasteRead(ctx context.Context, userID int64, req *sushiv1.TasteReq) (*sushiv1.TasteRes, error) {
	if personPage := req.GetPersonSeedPage(); personPage > 0 {
		people := a.loadSeedPeople(ctx, personPage)
		if personPage == 1 {
			if prof, err := a.taste.Taste(ctx, userID); err == nil {
				people = prependTastePeople(a.hydrateLikedPeople(ctx, prof.LikedPeople), people)
			}
		}
		return &sushiv1.TasteRes{SeedPeople: people}, nil
	}

	page := req.GetSeedPage()
	if page < 1 {
		page = 1
	}
	watchKindHint := strings.TrimSpace(req.GetWatchKind())

	// Later pages are seed-only — keeps the Telegram payload small for lazy load.
	if page > 1 {
		kindFilter := watchKindHint
		if kindFilter == "" {
			if prof, err := a.taste.Taste(ctx, userID); err == nil {
				kindFilter = prof.WatchKind
			}
		}
		return &sushiv1.TasteRes{SeedTitles: a.loadSeedTitles(ctx, kindFilter, page)}, nil
	}

	prof, err := a.taste.Taste(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("taste: %w", err)
	}
	out := profileToRes(prof)
	kindFilter := watchKindHint
	if kindFilter == "" {
		kindFilter = prof.WatchKind
	}
	// Previously picked cards first so reopen/profile review shows them at the top.
	out.SeedTitles = prependSeedRows(a.hydrateLikedTitles(ctx, prof.LikedTitles), a.loadSeedTitles(ctx, kindFilter, 1))
	out.SeedPeople = prependTastePeople(a.hydrateLikedPeople(ctx, prof.LikedPeople), a.loadSeedPeople(ctx, 1))
	return out, nil
}

func (a *API) hydrateLikedTitles(ctx context.Context, titles []TasteTitle) []*sushiv1.Row {
	out := make([]*sushiv1.Row, 0, len(titles))
	for _, t := range titles {
		if t.TMDBID == 0 {
			continue
		}
		row, ok := a.seedTitleRow(ctx, seedTitle{id: t.TMDBID, kind: t.Kind})
		if !ok {
			continue
		}
		out = append(out, row)
	}
	return out
}

func (a *API) hydrateLikedPeople(ctx context.Context, ids []uint64) []*sushiv1.TastePerson {
	if a.peopleSearch == nil || len(ids) == 0 {
		return nil
	}
	out := make([]*sushiv1.TastePerson, 0, len(ids))
	for _, id := range ids {
		if id == 0 {
			continue
		}
		hit, err := a.peopleSearch.PersonBrief(ctx, id)
		if err != nil || hit.TMDBID == 0 || hit.Name == "" {
			continue
		}
		out = append(out, &sushiv1.TastePerson{
			TmdbId: hit.TMDBID,
			Name:   hit.Name,
			Poster: hit.Poster,
		})
	}
	return out
}

func prependSeedRows(front, rest []*sushiv1.Row) []*sushiv1.Row {
	out := make([]*sushiv1.Row, 0, len(front)+len(rest))
	seen := map[string]struct{}{}
	add := func(r *sushiv1.Row) {
		if r == nil || r.GetTmdbId() == 0 {
			return
		}
		key := fmt.Sprintf("%d:%d", r.GetTmdbId(), r.GetKind())
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		out = append(out, r)
	}
	for _, r := range front {
		add(r)
	}
	for _, r := range rest {
		add(r)
	}
	return out
}

func prependTastePeople(front, rest []*sushiv1.TastePerson) []*sushiv1.TastePerson {
	out := make([]*sushiv1.TastePerson, 0, len(front)+len(rest))
	seen := map[uint64]struct{}{}
	add := func(p *sushiv1.TastePerson) {
		if p == nil || p.GetTmdbId() == 0 {
			return
		}
		if _, ok := seen[p.GetTmdbId()]; ok {
			return
		}
		seen[p.GetTmdbId()] = struct{}{}
		out = append(out, p)
	}
	for _, p := range front {
		add(p)
	}
	for _, p := range rest {
		add(p)
	}
	return out
}

func (a *API) tasteWrite(ctx context.Context, userID int64, req *sushiv1.TasteReq) (*sushiv1.TasteRes, error) {
	prof, err := validateTasteWrite(req)
	if err != nil {
		return nil, err
	}
	if err := a.taste.SetTaste(ctx, userID, prof); err != nil {
		return nil, fmt.Errorf("taste: save: %w", err)
	}
	now := time.Now().UTC()
	prof.TasteCompletedAt = &now
	return profileToRes(prof), nil
}

func validateTasteWrite(req *sushiv1.TasteReq) (TasteProfile, error) {
	wk := strings.TrimSpace(req.GetWatchKind())
	if !validWatchKinds[wk] {
		return TasteProfile{}, fmt.Errorf("taste: invalid watch_kind %q", wk)
	}
	audio := strings.TrimSpace(req.GetAudio())
	if !validTasteAudio[audio] {
		return TasteProfile{}, fmt.Errorf("taste: invalid audio %q", audio)
	}
	height := req.GetMaxHeight()
	if !validTasteHeights[height] {
		return TasteProfile{}, fmt.Errorf("taste: invalid max_height %d", height)
	}
	sub := strings.TrimSpace(req.GetSubtitleLang())
	if len(sub) > 16 {
		return TasteProfile{}, fmt.Errorf("taste: subtitle_lang too long")
	}
	subs := strings.TrimSpace(req.GetSubs())
	if !validTasteSubs[subs] {
		return TasteProfile{}, fmt.Errorf("taste: invalid subs %q", subs)
	}
	return TasteProfile{
		WatchKind:    wk,
		Genres:       sanitizeTasteGenres(req.GetGenres()),
		LikedTitles:  sanitizeTasteTitles(req.GetLikedTitles()),
		LikedPeople:  sanitizeTastePeople(req.GetLikedPeople()),
		MaxHeight:    height,
		Audio:        audio,
		SubtitleLang: sub,
		Subs:         subs,
	}, nil
}

func sanitizeTasteGenres(in []string) []string {
	out := make([]string, 0, len(in))
	seen := map[string]bool{}
	for _, g := range in {
		g = strings.TrimSpace(g)
		if g == "" || seen[g] || len(g) > 64 {
			continue
		}
		seen[g] = true
		out = append(out, g)
		if len(out) >= 32 {
			break
		}
	}
	return out
}

func sanitizeTasteTitles(in []*sushiv1.TasteTitle) []TasteTitle {
	out := make([]TasteTitle, 0, len(in))
	seen := map[string]bool{}
	for _, t := range in {
		if t == nil || t.GetTmdbId() == 0 {
			continue
		}
		kind, err := wireKind(t.GetKind())
		if err != nil {
			continue
		}
		key := fmt.Sprintf("%d:%d", t.GetTmdbId(), kind)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, TasteTitle{TMDBID: t.GetTmdbId(), Kind: kind})
		if len(out) >= 40 {
			break
		}
	}
	return out
}

func sanitizeTastePeople(in []uint64) []uint64 {
	out := make([]uint64, 0, len(in))
	seen := map[uint64]bool{}
	for _, id := range in {
		if id == 0 || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
		if len(out) >= 40 {
			break
		}
	}
	return out
}

func wireKind(k sushiv1.Kind) (catalog.Kind, error) {
	switch k {
	case sushiv1.Kind_KIND_MOVIE:
		return catalog.KindMovie, nil
	case sushiv1.Kind_KIND_SERIES:
		return catalog.KindSeries, nil
	default:
		return 0, fmt.Errorf("bad kind")
	}
}

func profileToRes(p TasteProfile) *sushiv1.TasteRes {
	out := &sushiv1.TasteRes{
		Complete:     p.TasteCompletedAt != nil,
		WatchKind:    p.WatchKind,
		Genres:       append([]string(nil), p.Genres...),
		LikedPeople:  append([]uint64(nil), p.LikedPeople...),
		MaxHeight:    p.MaxHeight,
		Audio:        p.Audio,
		SubtitleLang: p.SubtitleLang,
		Subs:         p.Subs,
	}
	for _, t := range p.LikedTitles {
		out.LikedTitles = append(out.LikedTitles, &sushiv1.TasteTitle{
			TmdbId: t.TMDBID,
			Kind:   kindToWire(t.Kind),
		})
	}
	return out
}

func kindToWire(k catalog.Kind) sushiv1.Kind {
	switch k {
	case catalog.KindMovie:
		return sushiv1.Kind_KIND_MOVIE
	case catalog.KindSeries:
		return sushiv1.Kind_KIND_SERIES
	default:
		return sushiv1.Kind_KIND_UNSPECIFIED
	}
}

func (a *API) loadSeedTitles(ctx context.Context, watchKind string, page int32) []*sushiv1.Row {
	if a.tasteSeeds == nil {
		return a.loadSeedTitlesLegacy(ctx, watchKind)
	}
	switch watchKind {
	case "movie":
		return catalogRowsToWire(a.tasteSeedRows(ctx, catalog.KindMovie, int(page)))
	case "series":
		return catalogRowsToWire(a.tasteSeedRows(ctx, catalog.KindSeries, int(page)))
	default:
		// Interleave movie + series (~10 each) so "both" still fits one Telegram reply.
		movies := a.tasteSeedRows(ctx, catalog.KindMovie, int(page))
		series := a.tasteSeedRows(ctx, catalog.KindSeries, int(page))
		half := tasteSeedPageSize / 2
		if len(movies) > half {
			movies = movies[:half]
		}
		if len(series) > half {
			series = series[:half]
		}
		out := make([]catalog.Row, 0, len(movies)+len(series))
		for i := 0; i < len(movies) || i < len(series); i++ {
			if i < len(movies) {
				out = append(out, movies[i])
			}
			if i < len(series) {
				out = append(out, series[i])
			}
		}
		return catalogRowsToWire(out)
	}
}

func (a *API) tasteSeedRows(ctx context.Context, kind catalog.Kind, page int) []catalog.Row {
	rows, err := a.tasteSeeds.TrendingTitles(ctx, kind, page)
	if err != nil || len(rows) == 0 {
		return nil
	}
	if len(rows) > tasteSeedPageSize {
		rows = rows[:tasteSeedPageSize]
	}
	return rows
}

func catalogRowsToWire(rows []catalog.Row) []*sushiv1.Row {
	out := make([]*sushiv1.Row, 0, len(rows))
	for _, item := range rows {
		if item.TMDBID == 0 || item.Title == "" {
			continue
		}
		out = append(out, &sushiv1.Row{
			TmdbId: item.TMDBID,
			Kind:   kindToWire(item.Kind),
			Title:  item.Title,
			Year:   item.Year,
			Rating: item.Rating,
			Poster: item.Poster,
		})
	}
	return out
}

// loadSeedTitlesLegacy is the old per-id hydrate path when TMDB trending is not wired.
func (a *API) loadSeedTitlesLegacy(ctx context.Context, watchKind string) []*sushiv1.Row {
	var seeds []seedTitle
	switch watchKind {
	case "movie":
		seeds = seedMovies
	case "series":
		seeds = seedSeries
	default:
		seeds = append(append([]seedTitle{}, seedMovies...), seedSeries...)
	}
	out := make([]*sushiv1.Row, 0, len(seeds))
	for _, s := range seeds {
		row, ok := a.seedTitleRow(ctx, s)
		if !ok {
			continue
		}
		out = append(out, row)
	}
	return out
}

func (a *API) seedTitleRow(ctx context.Context, s seedTitle) (*sushiv1.Row, bool) {
	if a.browse != nil {
		if item, ok, err := a.browse.BrowseItem(ctx, s.id, s.kind); err == nil && ok && item.TMDBID != 0 {
			return &sushiv1.Row{
				TmdbId: item.TMDBID,
				Kind:   kindToWire(item.Kind),
				Title:  item.Title,
				Year:   item.Year,
				Rating: item.Rating,
				Poster: item.Poster,
			}, true
		}
	}
	if a.hydrate == nil {
		return nil, false
	}
	item, err := a.hydrate.TitlePage(ctx, s.id, s.kind)
	if err != nil || item.TMDBID == 0 {
		return nil, false
	}
	return &sushiv1.Row{
		TmdbId: item.TMDBID,
		Kind:   kindToWire(item.Kind),
		Title:  item.Title,
		Year:   item.Year,
		Rating: item.Rating,
		Poster: item.Poster,
	}, true
}

func (a *API) loadSeedPeople(ctx context.Context, page int32) []*sushiv1.TastePerson {
	if a.peopleSearch == nil {
		return a.loadSeedPeopleLegacy(ctx)
	}
	if page < 1 {
		page = 1
	}
	hits, err := a.peopleSearch.PopularPeople(ctx, int(page))
	if err != nil || len(hits) == 0 {
		if page == 1 {
			return a.loadSeedPeopleLegacy(ctx)
		}
		return nil
	}
	out := make([]*sushiv1.TastePerson, 0, len(hits))
	for _, hit := range hits {
		if hit.TMDBID == 0 || hit.Name == "" {
			continue
		}
		out = append(out, &sushiv1.TastePerson{
			TmdbId: hit.TMDBID,
			Name:   hit.Name,
			Poster: hit.Poster,
		})
		if len(out) >= tasteSeedPageSize {
			break
		}
	}
	return out
}

// loadSeedPeopleLegacy is the old per-id PersonBrief path when popular is unavailable.
func (a *API) loadSeedPeopleLegacy(ctx context.Context) []*sushiv1.TastePerson {
	if a.peopleSearch == nil {
		return nil
	}
	out := make([]*sushiv1.TastePerson, 0, len(seedPeopleIDs))
	for _, id := range seedPeopleIDs {
		hit, err := a.peopleSearch.PersonBrief(ctx, id)
		if err != nil || hit.TMDBID == 0 || hit.Name == "" {
			continue
		}
		out = append(out, &sushiv1.TastePerson{
			TmdbId: hit.TMDBID,
			Name:   hit.Name,
			Poster: hit.Poster,
		})
	}
	return out
}

// MarshalLikedTitles encodes liked titles for jsonb storage.
func MarshalLikedTitles(titles []TasteTitle) ([]byte, error) {
	type row struct {
		TMDBID uint64 `json:"tmdb_id"`
		Kind   string `json:"kind"`
	}
	rows := make([]row, 0, len(titles))
	for _, t := range titles {
		k := "movie"
		if t.Kind == catalog.KindSeries {
			k = "series"
		}
		rows = append(rows, row{TMDBID: t.TMDBID, Kind: k})
	}
	return json.Marshal(rows)
}

// UnmarshalLikedTitles decodes liked titles from jsonb.
func UnmarshalLikedTitles(raw []byte) ([]TasteTitle, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	type row struct {
		TMDBID uint64 `json:"tmdb_id"`
		Kind   string `json:"kind"`
	}
	var rows []row
	if err := json.Unmarshal(raw, &rows); err != nil {
		return nil, err
	}
	out := make([]TasteTitle, 0, len(rows))
	for _, r := range rows {
		if r.TMDBID == 0 {
			continue
		}
		kind := catalog.KindMovie
		if r.Kind == "series" {
			kind = catalog.KindSeries
		}
		out = append(out, TasteTitle{TMDBID: r.TMDBID, Kind: kind})
	}
	return out, nil
}
