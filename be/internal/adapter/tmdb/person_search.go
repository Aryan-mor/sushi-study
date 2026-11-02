package tmdb

import (
	"context"
	"encoding/json"
	"net/url"
	"strconv"
	"strings"

	"github.com/Aryan-mor/sushi-study/be/internal/core/catalog"
)

const personSearchCap = 20

// PersonHit is a compact person card for the taste wizard search.
type PersonHit struct {
	TMDBID uint64
	Name   string
	Poster string
}

// SearchPeople runs TMDB GET /search/person. Empty key or query yields nil.
func (c *Client) SearchPeople(ctx context.Context, query string) ([]PersonHit, error) {
	if c == nil || c.key == "" {
		return nil, nil
	}
	q := strings.TrimSpace(query)
	if q == "" {
		return nil, nil
	}
	raw, err := c.get(ctx, "search/person", url.Values{"query": {q}})
	if err != nil {
		return nil, err
	}
	return parsePersonSearch(raw), nil
}

// PopularPeople loads TMDB GET /person/popular (one page, up to 20). page is 1-based.
func (c *Client) PopularPeople(ctx context.Context, page int) ([]PersonHit, error) {
	if c == nil || c.key == "" {
		return nil, nil
	}
	if page < 1 {
		page = 1
	}
	raw, err := c.get(ctx, "person/popular", url.Values{"page": {strconv.Itoa(page)}})
	if err != nil {
		return nil, err
	}
	return parsePersonSearch(raw), nil
}

// PersonBrief loads name + profile for one person id (taste seeds).
func (c *Client) PersonBrief(ctx context.Context, personID uint64) (PersonHit, error) {
	if c == nil || c.key == "" || personID == 0 {
		return PersonHit{}, nil
	}
	raw, err := c.get(ctx, "person/"+strconv.FormatUint(personID, 10), nil)
	if err != nil {
		return PersonHit{}, err
	}
	if len(raw) == 0 {
		return PersonHit{}, nil
	}
	var root struct {
		ID          uint64 `json:"id"`
		Name        string `json:"name"`
		ProfilePath string `json:"profile_path"`
	}
	if err := json.Unmarshal(raw, &root); err != nil || root.ID == 0 {
		// Soft-miss: seed hydration skips unknown/deleted TMDB people.
		return PersonHit{}, nil //nolint:nilerr
	}
	return PersonHit{
		TMDBID: root.ID,
		Name:   strings.TrimSpace(root.Name),
		Poster: catalog.PosterPath(root.ProfilePath),
	}, nil
}

func parsePersonSearch(raw []byte) []PersonHit {
	var wrap struct {
		Results []struct {
			ID          uint64 `json:"id"`
			Name        string `json:"name"`
			ProfilePath string `json:"profile_path"`
		} `json:"results"`
	}
	if json.Unmarshal(raw, &wrap) != nil {
		return nil
	}
	out := make([]PersonHit, 0, personSearchCap)
	for _, r := range wrap.Results {
		if r.ID == 0 || len(out) >= personSearchCap {
			continue
		}
		out = append(out, PersonHit{
			TMDBID: r.ID,
			Name:   strings.TrimSpace(r.Name),
			Poster: catalog.PosterPath(r.ProfilePath),
		})
	}
	return out
}
