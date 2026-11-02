package main

import (
	"context"

	"github.com/Aryan-mor/sushi-study/be/internal/adapter/tmdb"
	"github.com/Aryan-mor/sushi-study/be/internal/app/api"
)

// tmdbPeople adapts tmdb.Client to api.PersonSearcher.
type tmdbPeople struct{ c *tmdb.Client }

func (t tmdbPeople) SearchPeople(ctx context.Context, query string) ([]api.TastePerson, error) {
	hits, err := t.c.SearchPeople(ctx, query)
	if err != nil {
		return nil, err
	}
	out := make([]api.TastePerson, 0, len(hits))
	for _, h := range hits {
		out = append(out, api.TastePerson{TMDBID: h.TMDBID, Name: h.Name, Poster: h.Poster})
	}
	return out, nil
}

func (t tmdbPeople) PersonBrief(ctx context.Context, personID uint64) (api.TastePerson, error) {
	h, err := t.c.PersonBrief(ctx, personID)
	if err != nil {
		return api.TastePerson{}, err
	}
	return api.TastePerson{TMDBID: h.TMDBID, Name: h.Name, Poster: h.Poster}, nil
}

func (t tmdbPeople) PopularPeople(ctx context.Context, page int) ([]api.TastePerson, error) {
	hits, err := t.c.PopularPeople(ctx, page)
	if err != nil {
		return nil, err
	}
	out := make([]api.TastePerson, 0, len(hits))
	for _, h := range hits {
		out = append(out, api.TastePerson{TMDBID: h.TMDBID, Name: h.Name, Poster: h.Poster})
	}
	return out, nil
}
