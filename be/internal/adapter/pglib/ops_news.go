package pglib

import (
	"context"
	"fmt"

	"github.com/Aryan-mor/sushi-study/be/internal/app/newsroom"
	"github.com/Aryan-mor/sushi-study/be/internal/core/catalog"
)

// SaveOverviewFa keeps a news post's Persian synopsis beside the TMDB text it translates
// (library/022, ADR 0032). Another narrow main-bot write, like the probe card's rename: one
// derived text column, never the TMDB fields themselves; it reaches every env by replication.
//
// An episode debut is captioned with the episode's own synopsis when it has one, else the
// series'. Both updates are guarded on the English, so whichever row a.Overview came from is the
// one that matches — and a row whose text changed since the preview matches neither and stores
// nothing.
func (o *Ops) SaveOverviewFa(ctx context.Context, a newsroom.Announcement, fa string) error {
	if a.Kind == catalog.KindSeries && a.Episode > 0 {
		n, err := o.q.SetEpisodeOverviewFa(ctx, SetEpisodeOverviewFaParams{
			OverviewFa: fa,
			TmdbID:     int64(a.TMDBID),
			SeasonNo:   a.Season,
			EpisodeNo:  a.Episode,
			Overview:   a.Overview,
		})
		if err != nil {
			return fmt.Errorf("set episode overview_fa %d s%de%d: %w", a.TMDBID, a.Season, a.Episode, err)
		}
		if n > 0 {
			return nil
		}
	}
	_, err := o.q.SetItemOverviewFa(ctx, SetItemOverviewFaParams{
		OverviewFa: fa,
		TmdbID:     int64(a.TMDBID),
		Kind:       int16(a.Kind),
		Overview:   a.Overview,
	})
	if err != nil {
		return fmt.Errorf("set item overview_fa %d: %w", a.TMDBID, err)
	}
	return nil
}

// SaveBackdrops stores extra TMDB backdrop stems on items.detail for the news slideshow.
func (o *Ops) SaveBackdrops(ctx context.Context, tmdbID uint64, kind catalog.Kind, stems []string) error {
	err := o.q.SetItemBackdrops(ctx, SetItemBackdropsParams{
		TmdbID:    int64(tmdbID),
		Kind:      int16(kind),
		Backdrops: newsroom.BackdropJSON(stems),
	})
	if err != nil {
		return fmt.Errorf("set item backdrops %d: %w", tmdbID, err)
	}
	return nil
}
