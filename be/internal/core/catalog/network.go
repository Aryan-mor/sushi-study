package catalog

import (
	"encoding/json"
	"strings"
)

// NetworkNone is persisted when TMDB was checked and had no broadcast network and no
// streaming studio. Empty still means unknown, so a news post may ask TMDB once.
const NetworkNone = "-"

// maxNetworkNames caps the news-table cell. Three is a co-production; past that is a credit dump.
const maxNetworkNames = 3

// JoinNetworks reads TMDB `networks` (TV: Netflix, Disney+). When that list is empty it
// keeps production companies that are themselves a streamer (a Netflix film has no networks
// array, but the company is named Netflix). Empty means there is nothing to print.
func JoinNetworks(networksRaw, companiesRaw json.RawMessage) string {
	names := namedList(networksRaw, nil)
	if len(names) == 0 {
		names = namedList(companiesRaw, streamerCompany)
	}
	return strings.Join(names, ", ")
}

func namedList(raw json.RawMessage, keep func(string) bool) []string {
	var rows []struct {
		Name string `json:"name"`
	}
	if len(raw) == 0 || json.Unmarshal(raw, &rows) != nil {
		return nil
	}
	seen := map[string]struct{}{}
	out := make([]string, 0, maxNetworkNames)
	for _, r := range rows {
		n := strings.TrimSpace(r.Name)
		if n == "" || n == NetworkNone {
			continue
		}
		if keep != nil && !keep(n) {
			continue
		}
		key := strings.ToLower(n)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, n)
		if len(out) == maxNetworkNames {
			break
		}
	}
	return out
}

// streamerCompany is the movie-side fallback. A theatrical studio (Warner Bros.) is not a
// network; Netflix and Walt Disney Pictures are what the news row is for.
func streamerCompany(name string) bool {
	n := strings.ToLower(strings.TrimSpace(name))
	if n == "" {
		return false
	}
	for _, needle := range streamerNeedles {
		if strings.Contains(n, needle) {
			return true
		}
	}
	return false
}

var streamerNeedles = []string{
	"netflix",
	"disney",
	"hulu",
	"amazon studios",
	"amazon prime",
	"prime video",
	"apple tv",
	"hbo",
	"paramount+",
	"paramount plus",
	"peacock",
	"crunchyroll",
	"showtime",
	"starz",
}
