// Package subdl is a minimal client for subdl.com, a keyed subtitle API used as a fallback
// alongside sub-plus.ir (doc 15 §7).
//
// Server-side only, like subplus and tmdb: the client never sees the API key. subdl embeds its key
// directly in each search result's download url, so Search strips it before returning a Pack --
// FetchFile re-appends the adapter's own (possibly rotated) key when it resolves a tag.
package subdl

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/Aryan-mor/sushi-study/be/internal/adapter/subplus"
)

const (
	searchRoot = "https://api.subdl.com/api/v1/subtitles"
	dlRoot     = "https://dl.subdl.com"
	// Search/list responses are small JSON; zip downloads are the only large transfer.
	maxSearchRead = 1 << 20
	maxZipRead    = 8 << 20
	// DefaultLang matches SubDL's own two-letter code for Persian.
	DefaultLang = "FA"
)

// Pack is one subtitle hit. Tag is the download path only (no api_key) -- pass it back to
// FetchFile verbatim, never parse it.
type Pack struct {
	Tag         string
	ReleaseName string
	Releases    []string
	Lang        string
	Translator  string
	HI          bool
}

// Client talks to subdl.com. Round-robins across Keys so one rate-limited key does not stall every
// request -- mirrors internal/app/delivery.Runner.nextPushBot's atomic-cursor idiom. Zero value is
// not usable; call New.
type Client struct {
	http   *http.Client
	keys   []string
	cursor atomic.Uint64
}

// New builds a client over one or more API keys (SUBDL_API_KEYS, comma-separated). Panics on an
// empty slice -- callers should skip constructing a Client at all when no key is configured,
// exactly like apid's cfg.TMDBKey == "" check.
func New(keys []string) *Client {
	if len(keys) == 0 {
		panic("subdl: New called with no keys")
	}
	return &Client{
		http: &http.Client{Timeout: 12 * time.Second},
		keys: keys,
	}
}

// nextKey round-robins the configured pool, one pick per call.
func (c *Client) nextKey() string {
	i := int(c.cursor.Add(1)-1) % len(c.keys)
	return c.keys[i]
}

// Search returns candidate subtitle packs for a title already resolved to a TMDB id -- subdl
// matches tmdb_id directly, so (unlike subplus) no separate title-text query is needed. season/
// episode are 0 for a movie. lang "" means DefaultLang.
func (c *Client) Search(ctx context.Context, tmdbID uint64, isSeries bool, season, episode int, lang string) ([]Pack, error) {
	if tmdbID == 0 {
		return nil, fmt.Errorf("subdl search: tmdb_id required")
	}
	raw, err := c.get(ctx, searchRoot+"?"+searchQuery(tmdbID, isSeries, season, episode, lang, c.nextKey()).Encode(), maxSearchRead)
	if err != nil {
		return nil, err
	}
	return packsFromSearch(raw)
}

func searchQuery(tmdbID uint64, isSeries bool, season, episode int, lang, apiKey string) url.Values {
	q := url.Values{
		"api_key": {apiKey},
		"tmdb_id": {strconv.FormatUint(tmdbID, 10)},
	}
	if isSeries {
		q.Set("type", "tv")
		if season > 0 {
			q.Set("season_number", strconv.Itoa(season))
		}
		if episode > 0 {
			q.Set("episode_number", strconv.Itoa(episode))
		}
	} else {
		q.Set("type", "movie")
	}
	if l := strings.TrimSpace(lang); l != "" {
		q.Set("languages", l)
	} else {
		q.Set("languages", DefaultLang)
	}
	return q
}

type searchHit struct {
	ReleaseName string `json:"release_name"`
	Lang        string `json:"lang"`
	Author      string `json:"author"`
	URL         string `json:"url"`
	Language    string `json:"language"`
	HI          bool   `json:"hi"`
}

func packsFromSearch(raw []byte) ([]Pack, error) {
	var body struct {
		Status    bool        `json:"status"`
		Error     string      `json:"error"`
		Subtitles []searchHit `json:"subtitles"`
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		return nil, fmt.Errorf("subdl search: bad response: %w", err)
	}
	if !body.Status {
		return nil, fmt.Errorf("subdl search: %s", strings.TrimSpace(body.Error))
	}
	packs := make([]Pack, 0, len(body.Subtitles))
	for _, s := range body.Subtitles {
		tag := stripKey(s.URL)
		if tag == "" {
			continue
		}
		packs = append(packs, Pack{
			Tag:         tag,
			ReleaseName: strings.TrimSpace(s.ReleaseName),
			Releases:    []string{strings.TrimSpace(s.ReleaseName)},
			Lang:        strings.TrimSpace(s.Language),
			Translator:  strings.TrimSpace(s.Author),
			HI:          s.HI,
		})
	}
	return packs, nil
}

// PackFile is one subtitle inside a downloaded zip, with the season/episode parse and the
// SDH/forced flags the cache indexer needs. The zip is downloaded once; every entry is returned.
type PackFile struct {
	Name, Ext, Text       string
	Season, Episode       int
	HasSeason, HasEpisode bool
	SDH, Forced           bool
}

// FetchPack downloads a Pack.Tag and returns every .srt/.ass inside it. Re-appends this client's
// own (possibly different, round-robinned) key -- the tag itself carries no secret.
func (c *Client) FetchPack(ctx context.Context, tag string) ([]PackFile, error) {
	files, err := c.downloadSubs(ctx, tag)
	if err != nil {
		return nil, err
	}
	out := make([]PackFile, len(files))
	for i, f := range files {
		se := subplus.ParseSeasonEpisode(f.Name)
		out[i] = PackFile{
			Name: f.Name, Ext: f.Ext, Text: f.Text,
			Season: se.Season, Episode: se.Episode, HasSeason: se.HasS, HasEpisode: se.HasE,
			SDH: subplus.IsSDH(f.Name), Forced: subplus.IsForced(f.Name),
		}
	}
	return out, nil
}

// FetchFile resolves a Pack.Tag to the one subtitle for this episode.
func (c *Client) FetchFile(ctx context.Context, tag string, season, episode int) (text, ext string, err error) {
	files, err := c.downloadSubs(ctx, tag)
	if err != nil {
		return "", "", err
	}
	picked, err := pickFetchedSubs(files, season, episode)
	if err != nil {
		return "", "", err
	}
	return picked.Text, picked.Ext, nil
}

func (c *Client) downloadSubs(ctx context.Context, tag string) ([]subplus.SubFile, error) {
	t := strings.TrimSpace(tag)
	if t == "" || strings.Contains(t, "?") {
		return nil, fmt.Errorf("subdl fetch: invalid tag %q", tag)
	}
	q := url.Values{"api_key": {c.nextKey()}}
	zipBytes, err := c.get(ctx, dlRoot+t+"?"+q.Encode(), maxZipRead)
	if err != nil {
		return nil, err
	}
	files, err := subplus.ExtractSubs(zipBytes)
	if err != nil {
		return nil, fmt.Errorf("subdl fetch: %w", err)
	}
	return files, nil
}

// pickFetchedSubs chooses the ZIP entry for this episode. Serving files[0] was the Friends
// S3E12 miss: a season pack whose first entry was another episode's cues.
func pickFetchedSubs(files []subplus.SubFile, season, episode int) (subplus.SubFile, error) {
	if len(files) == 0 {
		return subplus.SubFile{}, fmt.Errorf("subdl fetch: zip has no .srt/.ass files")
	}
	if pick := subplus.PickSubFile(files, season, episode); pick != nil {
		return *pick, nil
	}
	if season > 0 && episode > 0 && len(files) > 1 {
		return subplus.SubFile{}, fmt.Errorf("subdl fetch: zip has no matching s%02de%02d file", season, episode)
	}
	return files[0], nil
}

// retry429Backoff is how long to wait before retrying a burst-rate-limited request. Observed live
// (2026-09-18): a Persian search 429'd, and an English search on the same single key succeeded
// ~800ms later with no other change -- this is SubDL throttling request bursts, not a hard quota,
// so one short wait-and-retry recovers the common case (two lookups issued back-to-back for the
// same title) instead of the caller silently losing the higher-priority language.
const retry429Backoff = 1200 * time.Millisecond

func (c *Client) get(ctx context.Context, target string, limit int64) ([]byte, error) {
	data, err := c.getOnce(ctx, target, limit)
	if err == nil || !errors.Is(err, errSubdlRateLimited) {
		return data, err
	}
	select {
	case <-time.After(retry429Backoff):
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	return c.getOnce(ctx, target, limit)
}

var errSubdlRateLimited = errors.New("subdl: rate limited")

func (c *Client) getOnce(ctx context.Context, target string, limit int64) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return nil, fmt.Errorf("subdl request: %w", err)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("subdl get: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusTooManyRequests {
		return nil, fmt.Errorf("subdl: HTTP %d: %w", resp.StatusCode, errSubdlRateLimited)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("subdl: HTTP %d", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, limit))
	if err != nil {
		return nil, fmt.Errorf("subdl read: %w", err)
	}
	return data, nil
}

// stripKey drops subdl's embedded ?api_key=... from a result's url, leaving the bare path
// (e.g. "/subtitle/3532652-8454512.zip") -- the client-safe tag. R-SEC-12: no provider secret may
// ever reach the client.
func stripKey(rawURL string) string {
	u, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || u.Path == "" {
		return ""
	}
	return u.Path
}
