// Package subplus is a minimal client for sub-plus.ir, a public keyless Persian subtitle API.
//
// Server-side only. Like tmdb, this is called from a backend worker (a subtitle sync/hydration
// job), never from the app — the client talks to nothing but its bot (docs/00 the invisibility
// rule, doc 15 §7). The whole surface is two POSTs to one endpoint: search and download-link.
//
// Hand-written rather than pulled in: there is no library, the contract is tiny, and it needs
// wrapping anyway for the quirks in doc 15 §6 (POST-only, tt-prefixed IMDb ids as the only id
// lookup, a leaked-path bug on a bad tag, URL-safe base64 tags).
package subplus

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	apiRoot = "https://sub-plus.ir/api.php"
	// Search responses run ~50-150 KB (count capped at 100 upstream, no pagination).
	maxRead = 1 << 20
	// DefaultLang is what sub-plus itself defaults to when l= is omitted.
	DefaultLang = "persian"
)

// Client talks to sub-plus.ir. Zero value is not usable; call New.
type Client struct {
	http    *http.Client
	apiRoot string
}

// New builds a client with a conservative timeout. sub-plus generates the ZIP on demand but the
// api.php calls themselves return in well under a second (doc 15 §6).
func New() *Client {
	return &Client{
		http:    &http.Client{Timeout: 12 * time.Second},
		apiRoot: apiRoot,
	}
}

// Pack is one search hit: a bundle (usually a ZIP) of subtitle files for a title. Tag is an
// opaque encrypted blob — pass it back to DownloadURL verbatim, never parse or re-encode it.
type Pack struct {
	Tag        string   // -> DownloadURL; also the stable per-pack id
	Title      string   // "News of the World (2020)"
	IMDb       string   // "tt6878306"
	Year       string   // "2020"
	Series     bool     // season pack vs movie
	Translator string   // free-text uploader/translator credit, as-is
	Releases   []string // release names this pack claims to match, one per line of `info`
	Poster     string   // isubcdn.com URL, advisory only
}

// Search runs the sub-plus search. query is either free text or an IMDb id as "tt#######"
// (case-insensitive) — with the tt prefix sub-plus does an exact match on its imdb field, which
// is what a caller should almost always pass (use IMDbQuery). lang "" means DefaultLang.
//
// A well-formed upstream error ({"ok":false,...}) is returned as an error carrying its
// description. An empty body yields (nil, nil); an unparseable one is an error.
func (c *Client) Search(ctx context.Context, query, lang string) ([]Pack, error) {
	q := strings.TrimSpace(query)
	if c == nil || q == "" {
		return nil, nil
	}
	form := url.Values{"q": {q}}
	if l := strings.TrimSpace(lang); l != "" {
		form.Set("l", l)
	}
	raw, err := c.post(ctx, form)
	if err != nil {
		return nil, err
	}
	if len(raw) == 0 {
		return nil, nil
	}
	var body struct {
		OK          bool   `json:"ok"`
		Description string `json:"description"`
		Count       int    `json:"count"`
		Result      []struct {
			Title   string `json:"title"`
			Tag     string `json:"tag"`
			Series  bool   `json:"series"`
			Comment string `json:"comment"`
			Info    string `json:"info"`
			Pic     string `json:"pic"`
			IMDb    string `json:"imdb"`
			Year    string `json:"year"`
		} `json:"result"`
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		return nil, fmt.Errorf("subplus search: bad response: %w", err)
	}
	if !body.OK {
		return nil, fmt.Errorf("subplus search: %s", strings.TrimSpace(body.Description))
	}
	packs := make([]Pack, 0, len(body.Result))
	for _, r := range body.Result {
		if strings.TrimSpace(r.Tag) == "" {
			continue
		}
		packs = append(packs, Pack{
			Tag:        r.Tag,
			Title:      strings.TrimSpace(r.Title),
			IMDb:       strings.TrimSpace(r.IMDb),
			Year:       strings.TrimSpace(r.Year),
			Series:     r.Series,
			Translator: strings.TrimSpace(r.Comment),
			Releases:   splitLines(r.Info),
			Poster:     strings.TrimSpace(r.Pic),
		})
	}
	return packs, nil
}

// DownloadURL resolves a Pack.Tag to a ZIP URL. The link is a plain http:// URL served from the
// sub-plus origin (doc 15 §6) — the caller fetches and delivers the bytes itself.
//
// Guards the doc 15 §6 bug: a bad/short tag returns {"ok":true} with a link containing the
// server's absolute filesystem path. Anything that is not a real /download/*.zip URL is rejected.
func (c *Client) DownloadURL(ctx context.Context, tag string) (string, error) {
	t := strings.TrimSpace(tag)
	if c == nil || t == "" {
		return "", fmt.Errorf("subplus download: empty tag")
	}
	raw, err := c.post(ctx, url.Values{"dl": {t}})
	if err != nil {
		return "", err
	}
	var body struct {
		OK          bool   `json:"ok"`
		Description string `json:"description"`
		Download    string `json:"download"`
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		return "", fmt.Errorf("subplus download: bad response: %w", err)
	}
	if !body.OK {
		return "", fmt.Errorf("subplus download: %s", strings.TrimSpace(body.Description))
	}
	link := strings.TrimSpace(body.Download)
	if !validDownloadURL(link) {
		return "", fmt.Errorf("subplus download: unusable link %q (bad tag?)", link)
	}
	return link, nil
}

func (c *Client) post(ctx context.Context, form url.Values) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.apiRoot, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, fmt.Errorf("subplus request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("subplus post: %w", err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxRead))
	if err != nil {
		return nil, fmt.Errorf("subplus read: %w", err)
	}
	// api.php answers 405 for GET but 200 for every POST outcome, error or not, so the JSON
	// `ok` field is the real signal — only bail here on a genuine transport-level status.
	if resp.StatusCode >= http.StatusInternalServerError {
		return nil, fmt.Errorf("subplus: HTTP %d", resp.StatusCode)
	}
	return data, nil
}

// IMDbQuery normalises an IMDb id ("tt6878306", "6878306", " TT6878306 ") into the "tt#######"
// form Search matches exactly. Returns "" if it is not a plausible IMDb id.
func IMDbQuery(id string) string {
	s := strings.ToLower(strings.TrimSpace(id))
	s = strings.TrimPrefix(s, "tt")
	if s == "" {
		return ""
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return ""
		}
	}
	return "tt" + s
}

func validDownloadURL(link string) bool {
	u, err := url.Parse(link)
	if err != nil || u.Host == "" {
		return false
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return false
	}
	// The bug case is "http://sub-plus.ir//home2/subplusi/public_html/modules/temp/-.zip".
	if strings.Contains(u.Path, "//") || !strings.HasPrefix(u.Path, "/download/") {
		return false
	}
	return strings.HasSuffix(strings.ToLower(u.Path), ".zip")
}

func splitLines(s string) []string {
	raw := strings.FieldsFunc(s, func(r rune) bool { return r == '\n' || r == '\r' })
	out := make([]string, 0, len(raw))
	for _, line := range raw {
		if l := strings.TrimSpace(line); l != "" {
			out = append(out, l)
		}
	}
	return out
}
