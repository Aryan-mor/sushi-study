package pg

import (
	"context"
	"fmt"

	"github.com/Aryan-mor/sushi-study/be/internal/app/api"
)

// ListCachedSubtitles returns every archived cut of this episode+language. The caller picks
// the row whose type and minutes match the file being watched. An empty slice is a miss.
func (s *Store) ListCachedSubtitles(ctx context.Context, tmdbID uint64, kind int16, seasonNo, episodeNo int32, lang string) ([]api.CachedSubtitle, error) {
	rows, err := s.q.ListCachedSubtitles(ctx, ListCachedSubtitlesParams{
		TmdbID:    int64(tmdbID),
		Kind:      kind,
		SeasonNo:  seasonNo,
		EpisodeNo: episodeNo,
		Lang:      lang,
	})
	if err != nil {
		return nil, fmt.Errorf("cached subtitles: %w", err)
	}
	out := make([]api.CachedSubtitle, 0, len(rows))
	for _, row := range rows {
		out = append(out, api.CachedSubtitle{
			ChannelID:   row.ChannelID,
			MessageID:   row.MessageID,
			Ext:         row.Ext,
			Provider:    row.Provider,
			SubType:     row.SubType,
			DurationMin: row.DurationMin,
		})
	}
	return out, nil
}

// PutCachedSubtitle records one cut (type + minutes). A different cut of the same episode
// inserts another row instead of replacing this one.
func (s *Store) PutCachedSubtitle(ctx context.Context, tmdbID uint64, kind int16, seasonNo, episodeNo int32, lang, provider, ext, subType string, durationMin int32, channelID, messageID int64) error {
	if err := s.q.PutCachedSubtitle(ctx, PutCachedSubtitleParams{
		TmdbID:      int64(tmdbID),
		Kind:        kind,
		SeasonNo:    seasonNo,
		EpisodeNo:   episodeNo,
		Lang:        lang,
		Provider:    provider,
		Ext:         ext,
		ChannelID:   channelID,
		MessageID:   messageID,
		SubType:     subType,
		DurationMin: durationMin,
	}); err != nil {
		return fmt.Errorf("put cached subtitle: %w", err)
	}
	return nil
}
