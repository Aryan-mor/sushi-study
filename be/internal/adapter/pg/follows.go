package pg

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/Aryan-mor/sushi-study/be/internal/app/contentnotify"
)

// SetFollow toggles a series follow (ADR 0014 §D3). Turning it on seeds the follow's high-water
// mark — and the shared debounce row — to the series' current max serveable episode, so a brand
// new follow never fires for episodes that already exist. Turning it off deletes the row and drops
// the shared state once the last follower is gone.
func (s *Store) SetFollow(ctx context.Context, userID int64, tmdbID uint64, on bool) error {
	if !on {
		if err := s.q.DeleteSeriesFollow(ctx, DeleteSeriesFollowParams{UserID: userID, TmdbID: int64(tmdbID)}); err != nil {
			return fmt.Errorf("unfollow %d/%d: %w", userID, tmdbID, err)
		}
		if err := s.q.GCFollowedSeriesState(ctx, int64(tmdbID)); err != nil {
			return fmt.Errorf("unfollow gc %d: %w", tmdbID, err)
		}
		return nil
	}
	maxEp, err := s.q.SeriesMaxServeableEpisode(ctx, int64(tmdbID))
	if err != nil {
		return fmt.Errorf("follow max %d: %w", tmdbID, err)
	}
	if err := s.q.EnsureFollowedSeriesState(ctx, EnsureFollowedSeriesStateParams{
		TmdbID: int64(tmdbID), MaxSeason: maxEp.SeasonNo, MaxEpisode: maxEp.EpisodeNo,
	}); err != nil {
		return fmt.Errorf("follow ensure state %d: %w", tmdbID, err)
	}
	if err := s.q.UpsertSeriesFollow(ctx, UpsertSeriesFollowParams{
		UserID: userID, TmdbID: int64(tmdbID), SeenSeason: maxEp.SeasonNo, SeenEpisode: maxEp.EpisodeNo,
	}); err != nil {
		return fmt.Errorf("follow %d/%d: %w", userID, tmdbID, err)
	}
	return nil
}

// --- contentnotify.FollowStore (ADR 0014 §D3) ---

// FollowedSeriesToScan lists series with a follower plus their recorded max episode.
func (s *Store) FollowedSeriesToScan(ctx context.Context, limit int32) ([]contentnotify.FollowedSeries, error) {
	rows, err := s.q.FollowedSeriesForScan(ctx, limit)
	if err != nil {
		return nil, fmt.Errorf("followed series to scan: %w", err)
	}
	out := make([]contentnotify.FollowedSeries, len(rows))
	for i, r := range rows {
		out[i] = contentnotify.FollowedSeries{
			TMDBID:    uint64(r.TmdbID),
			MaxSeason: r.MaxSeasonNo,
			MaxEp:     r.MaxEpisodeNo,
		}
	}
	return out, nil
}

// BumpFollowedSeriesMax records a newly-serveable episode and resets the shared debounce clock.
// Returns whether the max actually moved.
func (s *Store) BumpFollowedSeriesMax(ctx context.Context, tmdbID uint64, season, episode int32) (bool, error) {
	n, err := s.q.BumpFollowedSeriesMax(ctx, BumpFollowedSeriesMaxParams{
		TmdbID: int64(tmdbID), MaxSeasonNo: season, MaxEpisodeNo: episode,
	})
	if err != nil {
		return false, fmt.Errorf("bump followed series max %d: %w", tmdbID, err)
	}
	return n > 0, nil
}

// SeriesMaxServeableEpisode is the highest (season, episode) of a series that has a ready file.
func (s *Store) SeriesMaxServeableEpisode(ctx context.Context, tmdbID uint64) (season, episode int32, err error) {
	row, err := s.q.SeriesMaxServeableEpisode(ctx, int64(tmdbID))
	if err != nil {
		return 0, 0, fmt.Errorf("series max serveable %d: %w", tmdbID, err)
	}
	return row.SeasonNo, row.EpisodeNo, nil
}

// FollowNoticesDue lists (follower, series) pairs past their high-water mark and past the quiet
// period cutoff.
func (s *Store) FollowNoticesDue(ctx context.Context, cutoff time.Time, limit int32) ([]contentnotify.FollowNotice, error) {
	rows, err := s.q.FollowNoticesDue(ctx, FollowNoticesDueParams{
		Cutoff: pgtype.Timestamptz{Time: cutoff, Valid: true},
		Limit:  limit,
	})
	if err != nil {
		return nil, fmt.Errorf("follow notices due: %w", err)
	}
	out := make([]contentnotify.FollowNotice, len(rows))
	for i, r := range rows {
		out[i] = contentnotify.FollowNotice{
			UserID:      r.UserID,
			RequesterTG: r.TgUserID,
			TMDBID:      uint64(r.TmdbID),
			MaxSeason:   r.MaxSeasonNo,
			MaxEp:       r.MaxEpisodeNo,
			Title:       r.Title,
			Poster:      r.Poster,
			Year:        r.Year,
			NewCount:    r.NewCount,
			Locale:      r.Locale,
		}
	}
	return out, nil
}

// AdvanceFollowSeen catches one follow up to the episode the follower has now been told about.
func (s *Store) AdvanceFollowSeen(ctx context.Context, userID int64, tmdbID uint64, season, episode int32) error {
	if err := s.q.AdvanceFollowSeen(ctx, AdvanceFollowSeenParams{
		UserID: userID, TmdbID: int64(tmdbID), SeenSeasonNo: season, SeenEpisodeNo: episode,
	}); err != nil {
		return fmt.Errorf("advance follow seen %d/%d: %w", userID, tmdbID, err)
	}
	return nil
}

// SyncFollowSeenToSeriesMax catches a requester's follow up to the series max after a
// "your request is ready" DM, so the follow reconciler does not send a near-duplicate.
func (s *Store) SyncFollowSeenToSeriesMax(ctx context.Context, userID int64, tmdbID uint64) error {
	if err := s.q.SyncFollowSeenToSeriesMax(ctx, SyncFollowSeenToSeriesMaxParams{
		UserID: userID, TmdbID: int64(tmdbID),
	}); err != nil {
		return fmt.Errorf("sync follow seen %d/%d: %w", userID, tmdbID, err)
	}
	return nil
}
