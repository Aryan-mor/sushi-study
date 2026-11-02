package tmdb

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Aryan-mor/sushi-study/be/internal/core/catalog"
)

const (
	apiRoot = "https://api.themoviedb.org/3"
	maxRead = 1 << 20
)

// Client talks to TMDB from apid on-demand misses (ADR 0009). Never the client.
type Client struct {
	key     string
	http    *http.Client
	apiRoot string
}

// New uses TMDB's v3 API key query param.
func New(apiKey string) *Client {
	return &Client{
		key:     strings.TrimSpace(apiKey),
		apiRoot: apiRoot,
		http:    &http.Client{Timeout: 12 * time.Second},
	}
}

// TitleDetail loads logo, cast, genres, runtime, collection and similar cards.
func (c *Client) TitleDetail(ctx context.Context, tmdbID uint64, kind catalog.Kind) (catalog.Detail, error) {
	item, err := c.TitlePage(ctx, tmdbID, kind)
	if err != nil {
		return catalog.Detail{}, err
	}
	return item.Detail, nil
}

// OriginCountry is the cheap movie/tv GET newsroom uses when items.detail has no country yet.
func (c *Client) OriginCountry(ctx context.Context, tmdbID uint64, kind catalog.Kind) (string, error) {
	raw, err := c.titleJSON(ctx, tmdbID, kind)
	if err != nil || len(raw) == 0 {
		return "", err
	}
	return parseTitlePage(raw, tmdbID, kind).Detail.Country, nil
}

// Network is the cheap movie/tv GET newsroom uses when items.detail has no network yet.
// TV answers with networks (Netflix, Disney+). A movie falls back to a streaming production company.
func (c *Client) Network(ctx context.Context, tmdbID uint64, kind catalog.Kind) (string, error) {
	raw, err := c.titleJSON(ctx, tmdbID, kind)
	if err != nil || len(raw) == 0 {
		return "", err
	}
	return parseTitlePage(raw, tmdbID, kind).Detail.Network, nil
}

func (c *Client) titleJSON(ctx context.Context, tmdbID uint64, kind catalog.Kind) ([]byte, error) {
	if c == nil || c.key == "" || tmdbID == 0 {
		return nil, nil
	}
	path := "movie/" + strconv.FormatUint(tmdbID, 10)
	if kind == catalog.KindSeries {
		path = "tv/" + strconv.FormatUint(tmdbID, 10)
	}
	return c.get(ctx, path, nil)
}

// TitlePage loads a full title card + extras for on-demand Item (ADR 0009).
func (c *Client) TitlePage(ctx context.Context, tmdbID uint64, kind catalog.Kind) (catalog.Item, error) {
	if c == nil || c.key == "" || tmdbID == 0 {
		return catalog.Item{}, nil
	}
	path := "movie/" + strconv.FormatUint(tmdbID, 10)
	if kind == catalog.KindSeries {
		path = "tv/" + strconv.FormatUint(tmdbID, 10)
	}
	raw, err := c.get(ctx, path, url.Values{
		"append_to_response":     {"credits,images,similar,external_ids,videos"},
		"include_image_language": {"en,null"},
	})
	if err != nil {
		return catalog.Item{}, err
	}
	if len(raw) == 0 {
		return catalog.Item{}, nil
	}
	return parseTitlePage(raw, tmdbID, kind), nil
}

// PersonPage loads identity + combined credit cards for /person (ADR 0009).
func (c *Client) PersonPage(ctx context.Context, personID uint64) (catalog.PersonPage, error) {
	if c == nil || c.key == "" || personID == 0 {
		return catalog.PersonPage{}, nil
	}
	raw, err := c.get(ctx, "person/"+strconv.FormatUint(personID, 10), url.Values{
		"append_to_response": {"combined_credits"},
	})
	if err != nil {
		return catalog.PersonPage{}, err
	}
	if len(raw) == 0 {
		return catalog.PersonPage{}, nil
	}
	return parsePersonPage(raw, personID), nil
}

// tmdb5xxRetries covers a transient upstream blip (e.g. ingest 38571's search/multi 500): TMDB's
// 5xx are almost always momentary, so a couple of short retries clear them without a human having
// to click Retry on the review card. 4xx (bad request, 404) never gets retried -- it won't change.
const tmdb5xxRetries = 2

func (c *Client) get(ctx context.Context, path string, extra url.Values) ([]byte, error) {
	u, err := buildURL(c.apiRoot, path, c.key, extra)
	if err != nil {
		return nil, err
	}

	var lastErr error
	for attempt := 0; attempt <= tmdb5xxRetries; attempt++ {
		if attempt > 0 {
			if err := sleepBackoff(ctx, attempt); err != nil {
				return nil, err
			}
		}
		body, status, err := c.getOnce(ctx, u)
		if err != nil {
			return nil, fmt.Errorf("tmdb get %s: %w", path, err)
		}
		switch {
		case status == http.StatusNotFound:
			return nil, nil
		case status == http.StatusOK:
			return body, nil
		case status < 500:
			return nil, fmt.Errorf("tmdb %s: HTTP %d", path, status)
		default:
			lastErr = fmt.Errorf("tmdb %s: HTTP %d", path, status)
		}
	}
	return nil, lastErr
}

func buildURL(apiRoot, path, key string, extra url.Values) (string, error) {
	u, err := url.Parse(apiRoot + "/" + path)
	if err != nil {
		return "", fmt.Errorf("tmdb url: %w", err)
	}
	q := u.Query()
	q.Set("api_key", key)
	for k, vs := range extra {
		q[k] = vs
	}
	u.RawQuery = q.Encode()
	return u.String(), nil
}

func sleepBackoff(ctx context.Context, attempt int) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(time.Duration(attempt) * 500 * time.Millisecond):
		return nil
	}
}

func (c *Client) getOnce(ctx context.Context, u string) ([]byte, int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, 0, fmt.Errorf("request: %w", err)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxRead))
	if err != nil {
		return nil, 0, fmt.Errorf("read: %w", err)
	}
	return body, resp.StatusCode, nil
}

func parseTitle(raw []byte) catalog.Detail {
	return parseTitlePage(raw, 0, catalog.KindMovie).Detail
}

func parseTitlePage(raw []byte, tmdbID uint64, kind catalog.Kind) catalog.Item {
	var root map[string]json.RawMessage
	if json.Unmarshal(raw, &root) != nil {
		return catalog.Item{}
	}
	title := strings.TrimSpace(jsonString(root["title"]))
	if title == "" {
		title = strings.TrimSpace(jsonString(root["name"]))
	}
	orig := distinctOrig(title, jsonString(root["original_title"]), jsonString(root["original_name"]))
	date := jsonString(root["release_date"])
	if date == "" {
		date = jsonString(root["first_air_date"])
	}
	d, similar := titlePageDetail(root, kind)
	var released time.Time
	if len(date) >= 10 {
		if t, err := time.Parse("2006-01-02", date[:10]); err == nil {
			released = t
		}
	}
	id := tmdbID
	if id == 0 {
		id = jsonUint64(root["id"])
	}
	return catalog.Item{
		Row: catalog.Row{
			TMDBID:    id,
			Kind:      kind,
			Title:     title,
			OrigTitle: orig,
			Year:      catalog.Year(nil, date),
			Rating:    catalog.Rating(voteAverage(root)),
			Poster:    catalog.PosterPath(jsonString(root["poster_path"])),
		},
		Overview:   strings.TrimSpace(jsonString(root["overview"])),
		ReleasedOn: released,
		Detail:     d,
		Related:    similar,
		IMDBID:     imdbID(root),
	}
}

func voteAverage(root map[string]json.RawMessage) string {
	vote := jsonString(root["vote_average"])
	if vote != "" {
		return vote
	}
	var n float64
	if json.Unmarshal(root["vote_average"], &n) == nil {
		return strconv.FormatFloat(n, 'f', 1, 64)
	}
	return ""
}

func titlePageDetail(root map[string]json.RawMessage, kind catalog.Kind) (catalog.Detail, []catalog.Row) {
	d := catalog.Detail{
		Backdrop:       catalog.PosterPath("https://image.tmdb.org/t/p/w1280" + jsonString(root["backdrop_path"])),
		Genres:         joinNamed(root["genres"]),
		RuntimeS:       runtimeSeconds(root, kind),
		People:         parseCredits(root["credits"]),
		Logo:           pickLogo(root["images"]),
		Backdrops:      pickBackdrops(root["images"], catalog.PosterPath(jsonString(root["backdrop_path"]))),
		CollectionID:   collectionID(root["belongs_to_collection"]),
		CollectionName: collectionName(root["belongs_to_collection"]),
		Country:        catalog.OriginCountryJSON(root["origin_country"], root["production_countries"]),
		TrailerKey:     pickTrailer(root["videos"]),
		Network:        catalog.JoinNetworks(root["networks"], root["production_companies"]),
	}
	similar, ids := parseSimilar(root["similar"], kind)
	d.RelatedPreview = similar
	d.SimilarTMDB = ids
	if kind == catalog.KindSeries {
		d.LastAirDate = seriesLastAirDate(root)
	}
	return d, similar
}

// seriesLastAirDate prefers last_episode_to_air.air_date, then last_air_date. Future dates are
// dropped so an unaired TMDB row does not float a 1989 show to the top of /list.
func seriesLastAirDate(root map[string]json.RawMessage) string {
	var ep struct {
		AirDate string `json:"air_date"`
	}
	if json.Unmarshal(root["last_episode_to_air"], &ep) == nil {
		if d := tmdbDay(ep.AirDate); d != "" {
			return d
		}
	}
	return tmdbDay(jsonString(root["last_air_date"]))
}

func tmdbDay(s string) string {
	s = strings.TrimSpace(s)
	if len(s) < 10 {
		return ""
	}
	t, err := time.Parse("2006-01-02", s[:10])
	if err != nil {
		return ""
	}
	today := time.Now().UTC().Truncate(24 * time.Hour)
	if t.After(today) {
		return ""
	}
	return t.Format("2006-01-02")
}

// imdbID reads the "tt…" id: a top-level field on a /movie response, and under external_ids on
// /tv (added to append_to_response for exactly this).
func imdbID(root map[string]json.RawMessage) string {
	if s := strings.TrimSpace(jsonString(root["imdb_id"])); s != "" {
		return s
	}
	var ext map[string]json.RawMessage
	if json.Unmarshal(root["external_ids"], &ext) == nil {
		return strings.TrimSpace(jsonString(ext["imdb_id"]))
	}
	return ""
}

func parsePersonPage(raw []byte, personID uint64) catalog.PersonPage {
	var root map[string]json.RawMessage
	if json.Unmarshal(raw, &root) != nil {
		return catalog.PersonPage{}
	}
	movies, series := parseCombinedCredits(root["combined_credits"])
	return catalog.PersonPage{
		TMDBID:    personID,
		Name:      strings.TrimSpace(jsonString(root["name"])),
		Profile:   catalog.PosterPath(jsonString(root["profile_path"])),
		Biography: strings.TrimSpace(jsonString(root["biography"])),
		Movies:    movies,
		Series:    series,
	}
}

func parseCombinedCredits(raw json.RawMessage) (movies, series []catalog.Row) {
	var wrap struct {
		Cast []struct {
			ID          uint64  `json:"id"`
			MediaType   string  `json:"media_type"`
			Title       string  `json:"title"`
			Name        string  `json:"name"`
			PosterPath  string  `json:"poster_path"`
			ReleaseDate string  `json:"release_date"`
			FirstAir    string  `json:"first_air_date"`
			Vote        float64 `json:"vote_average"`
		} `json:"cast"`
	}
	if json.Unmarshal(raw, &wrap) != nil {
		return nil, nil
	}
	seenM := map[uint64]struct{}{}
	seenS := map[uint64]struct{}{}
	for _, c := range wrap.Cast {
		if c.ID == 0 {
			continue
		}
		title := strings.TrimSpace(c.Title)
		if title == "" {
			title = strings.TrimSpace(c.Name)
		}
		date := c.ReleaseDate
		if date == "" {
			date = c.FirstAir
		}
		row := catalog.Row{
			TMDBID: c.ID,
			Title:  title,
			Year:   catalog.Year(nil, date),
			Rating: catalog.Rating(strconv.FormatFloat(c.Vote, 'f', 1, 64)),
			Poster: catalog.PosterPath(c.PosterPath),
		}
		switch c.MediaType {
		case "movie":
			movies = appendUniqueCredit(movies, seenM, row, catalog.KindMovie)
		case "tv":
			series = appendUniqueCredit(series, seenS, row, catalog.KindSeries)
		}
	}
	return movies, series
}

func appendUniqueCredit(dst []catalog.Row, seen map[uint64]struct{}, row catalog.Row, kind catalog.Kind) []catalog.Row {
	// 12 matches api.personFilmographyCap so browse_people cache stays text-sized.
	if _, ok := seen[row.TMDBID]; ok || len(dst) >= 12 {
		return dst
	}
	seen[row.TMDBID] = struct{}{}
	row.Kind = kind
	return append(dst, row)
}

func jsonUint64(raw json.RawMessage) uint64 {
	if len(raw) == 0 {
		return 0
	}
	var n float64
	if json.Unmarshal(raw, &n) == nil && n > 0 {
		return uint64(n)
	}
	return 0
}

func runtimeSeconds(root map[string]json.RawMessage, kind catalog.Kind) int32 {
	key := "runtime"
	if kind == catalog.KindSeries {
		key = "episode_run_time"
		var mins []float64
		if json.Unmarshal(root[key], &mins) == nil && len(mins) > 0 && mins[0] > 0 {
			return int32(mins[0] * 60)
		}
		return 0
	}
	var mins float64
	if json.Unmarshal(root[key], &mins) != nil || mins <= 0 {
		return 0
	}
	return int32(mins * 60)
}

func jsonString(raw json.RawMessage) string {
	if len(raw) == 0 || string(raw) == "null" {
		return ""
	}
	var s string
	if json.Unmarshal(raw, &s) != nil {
		return ""
	}
	return strings.TrimSpace(s)
}

func joinNamed(raw json.RawMessage) string {
	var rows []struct {
		Name string `json:"name"`
	}
	if json.Unmarshal(raw, &rows) != nil {
		return ""
	}
	names := make([]string, 0, len(rows))
	for _, r := range rows {
		n := strings.TrimSpace(r.Name)
		if n != "" {
			names = append(names, n)
		}
	}
	return strings.Join(names, ", ")
}

func parseCredits(raw json.RawMessage) []catalog.Person {
	var wrap struct {
		Cast []struct {
			ID          uint64 `json:"id"`
			Name        string `json:"name"`
			Character   string `json:"character"`
			ProfilePath string `json:"profile_path"`
		} `json:"cast"`
	}
	if json.Unmarshal(raw, &wrap) != nil {
		return nil
	}
	out := make([]catalog.Person, 0, 20)
	for _, c := range wrap.Cast {
		if len(out) >= 20 {
			break
		}
		name := strings.TrimSpace(c.Name)
		if name == "" {
			continue
		}
		out = append(out, catalog.Person{
			Name:    name,
			Role:    strings.TrimSpace(c.Character),
			Profile: catalog.PosterPath(c.ProfilePath),
			TMDBID:  c.ID,
		})
	}
	return out
}

func pickLogo(raw json.RawMessage) string {
	var wrap struct {
		Logos []struct {
			FilePath string `json:"file_path"`
			Lang     string `json:"iso_639_1"`
		} `json:"logos"`
	}
	if json.Unmarshal(raw, &wrap) != nil {
		return ""
	}
	var fallback string
	for _, logo := range wrap.Logos {
		stem := catalog.PosterPath(logo.FilePath)
		if stem == "" || strings.HasSuffix(strings.ToLower(logo.FilePath), ".svg") {
			continue
		}
		if logo.Lang == "en" || logo.Lang == "" {
			return stem
		}
		if fallback == "" {
			fallback = stem
		}
	}
	return fallback
}

// maxExtraBackdrops is how many gallery stills we keep beside the primary backdrop.
// The news slideshow caps at five frames (hero or poster, primary backdrop, then these).
const maxExtraBackdrops = 4

// ExtraBackdrops loads gallery stills for a news post whose items.detail has none stored yet.
func (c *Client) ExtraBackdrops(ctx context.Context, tmdbID uint64, kind catalog.Kind, primary string) ([]string, error) {
	if c == nil || c.key == "" || tmdbID == 0 {
		return nil, nil
	}
	path := "movie/" + strconv.FormatUint(tmdbID, 10) + "/images"
	if kind == catalog.KindSeries {
		path = "tv/" + strconv.FormatUint(tmdbID, 10) + "/images"
	}
	raw, err := c.get(ctx, path, url.Values{
		"include_image_language": {"en,null"},
	})
	if err != nil {
		return nil, err
	}
	stems := pickBackdrops(raw, primary)
	if stems == nil {
		stems = []string{}
	}
	return stems, nil
}

// pickBackdrops keeps text-free stills first (iso_639_1 empty), then the rest, highest vote first.
// primary is the stem already stored as backdrop_path, so the slideshow does not repeat it.
// raw is either a TMDB images object or the append_to_response "images" blob.
func pickBackdrops(raw json.RawMessage, primary string) []string {
	shots := decodeBackdropShots(raw)
	if len(shots) == 0 {
		return nil
	}
	primary = backdropStem(primary)
	seen := map[string]struct{}{}
	if primary != "" {
		seen[primary] = struct{}{}
	}
	plain, other := splitBackdropShots(shots, seen)
	out := takeBackdrops(plain, maxExtraBackdrops)
	if room := maxExtraBackdrops - len(out); room > 0 {
		out = append(out, takeBackdrops(other, room)...)
	}
	return out
}

type backdropShot struct {
	stem  string
	vote  float64
	plain bool
}

func decodeBackdropShots(raw json.RawMessage) []backdropShot {
	var wrap struct {
		Backdrops []struct {
			FilePath string  `json:"file_path"`
			Lang     *string `json:"iso_639_1"`
			Vote     float64 `json:"vote_average"`
		} `json:"backdrops"`
	}
	if json.Unmarshal(raw, &wrap) != nil || wrap.Backdrops == nil {
		return nil
	}
	out := make([]backdropShot, 0, len(wrap.Backdrops))
	for _, b := range wrap.Backdrops {
		if strings.HasSuffix(strings.ToLower(b.FilePath), ".svg") {
			continue
		}
		stem := catalog.PosterPath(b.FilePath)
		if stem == "" {
			continue
		}
		plain := b.Lang == nil || strings.TrimSpace(*b.Lang) == ""
		out = append(out, backdropShot{stem: stem, vote: b.Vote, plain: plain})
	}
	return out
}

func splitBackdropShots(shots []backdropShot, seen map[string]struct{}) (plain, other []backdropShot) {
	for _, s := range shots {
		if _, ok := seen[s.stem]; ok {
			continue
		}
		seen[s.stem] = struct{}{}
		if s.plain {
			plain = append(plain, s)
		} else {
			other = append(other, s)
		}
	}
	return plain, other
}

func backdropStem(raw string) string {
	if stem := catalog.PosterPath(raw); stem != "" {
		return stem
	}
	return strings.TrimSpace(raw)
}

func takeBackdrops(shots []backdropShot, n int) []string {
	if n <= 0 || len(shots) == 0 {
		return nil
	}
	order := append([]backdropShot(nil), shots...)
	sort.SliceStable(order, func(i, j int) bool { return order[i].vote > order[j].vote })
	if len(order) > n {
		order = order[:n]
	}
	out := make([]string, 0, len(order))
	for _, s := range order {
		out = append(out, s.stem)
	}
	return out
}

// pickTrailer picks one YouTube video id from TMDB's videos.results: an official trailer first,
// any trailer next, a teaser last resort. Non-YouTube sites (Vimeo etc.) are skipped -- there is
// nowhere in the client to play them (docs/09 R-SEC-12).
func pickTrailer(raw json.RawMessage) string {
	official, trailer, teaser := classifyTrailers(raw)
	switch {
	case official != "":
		return official
	case trailer != "":
		return trailer
	default:
		return teaser
	}
}

func classifyTrailers(raw json.RawMessage) (official, trailer, teaser string) {
	var wrap struct {
		Results []struct {
			Key      string `json:"key"`
			Site     string `json:"site"`
			Type     string `json:"type"`
			Official bool   `json:"official"`
		} `json:"results"`
	}
	if json.Unmarshal(raw, &wrap) != nil {
		return "", "", ""
	}
	for _, v := range wrap.Results {
		if v.Site != "YouTube" || v.Key == "" {
			continue
		}
		official, trailer, teaser = noteTrailer(v.Type, v.Key, v.Official, official, trailer, teaser)
	}
	return official, trailer, teaser
}

func noteTrailer(kind, key string, isOfficial bool, official, trailer, teaser string) (string, string, string) {
	switch kind {
	case "Trailer":
		if isOfficial && official == "" {
			official = key
		}
		if trailer == "" {
			trailer = key
		}
	case "Teaser":
		if teaser == "" {
			teaser = key
		}
	}
	return official, trailer, teaser
}

func collectionID(raw json.RawMessage) uint64 {
	var wrap struct {
		ID uint64 `json:"id"`
	}
	if json.Unmarshal(raw, &wrap) != nil {
		return 0
	}
	return wrap.ID
}

func collectionName(raw json.RawMessage) string {
	var wrap struct {
		Name string `json:"name"`
	}
	if json.Unmarshal(raw, &wrap) != nil {
		return ""
	}
	return strings.TrimSpace(wrap.Name)
}

func parseSimilar(raw json.RawMessage, kind catalog.Kind) ([]catalog.Row, []uint64) {
	var wrap struct {
		Results []struct {
			ID          uint64  `json:"id"`
			Title       string  `json:"title"`
			Name        string  `json:"name"`
			PosterPath  string  `json:"poster_path"`
			ReleaseDate string  `json:"release_date"`
			FirstAir    string  `json:"first_air_date"`
			Vote        float64 `json:"vote_average"`
		} `json:"results"`
	}
	if json.Unmarshal(raw, &wrap) != nil {
		return nil, nil
	}
	rows := make([]catalog.Row, 0, 16)
	ids := make([]uint64, 0, 16)
	seen := map[uint64]struct{}{}
	for _, r := range wrap.Results {
		if r.ID == 0 {
			continue
		}
		if _, ok := seen[r.ID]; ok {
			continue
		}
		seen[r.ID] = struct{}{}
		title := strings.TrimSpace(r.Title)
		if title == "" {
			title = strings.TrimSpace(r.Name)
		}
		date := r.ReleaseDate
		if date == "" {
			date = r.FirstAir
		}
		row := catalog.Row{
			TMDBID: r.ID,
			Kind:   kind,
			Title:  title,
			Year:   catalog.Year(nil, date),
			Rating: catalog.Rating(strconv.FormatFloat(r.Vote, 'f', 1, 64)),
			Poster: catalog.PosterPath(r.PosterPath),
		}
		rows = append(rows, row)
		ids = append(ids, r.ID)
		if len(rows) >= 16 {
			break
		}
	}
	return rows, ids
}
