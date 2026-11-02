package pg

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/Aryan-mor/sushi-study/be/internal/core/catalog"
)

const playEventSessionMins int32 = 30

// SetProgress upserts user_episode_state and, at most once per session, appends a play_event
// so most-watched has a number this environment owns (docs/06). Fire-and-forget: the caller
// never sees a reply. episode_id is the key; file_id is a preference.
func (s *Store) SetProgress(ctx context.Context, userID, episodeID, fileID int64, positionS, durationS int32) error {
	if userID == 0 || episodeID == 0 {
		return nil
	}
	if positionS < 0 {
		positionS = 0
	}

	var lastFile *int64
	if fileID > 0 {
		row, err := s.q.FileDurationForEpisode(ctx, FileDurationForEpisodeParams{
			FileID: fileID, EpisodeID: episodeID,
		})
		if err == nil {
			lastFile = &fileID
			if durationS <= 0 {
				durationS = row
			}
		} else if !errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("progress file %d/%d: %w", fileID, episodeID, err)
		}
	}

	done := catalog.EpisodeWatched(positionS, durationS)
	if err := s.q.UpsertUserEpisodeState(ctx, UpsertUserEpisodeStateParams{
		UserID:     userID,
		Kind:       int16(catalog.MediaCatalog),
		EpisodeID:  episodeID,
		PositionS:  positionS,
		Done:       done,
		LastFileID: lastFile,
	}); err != nil {
		return fmt.Errorf("progress upsert %d/%d: %w", userID, episodeID, err)
	}
	if err := s.BumpLatestDevice(ctx, userID); err != nil {
		return err
	}

	if !catalog.EpisodeStarted(positionS, durationS) {
		return nil
	}
	return s.recordPlayEvent(ctx, userID, episodeID, lastFile)
}

// SetEpisodeDone is a manual mark-as-watched (or unwatched). Position is cleared so resume does
// not stick at the 90% threshold, and no play_event is written — this is not a view.
func (s *Store) SetEpisodeDone(ctx context.Context, userID, episodeID int64, done bool) error {
	if userID == 0 || episodeID == 0 {
		return nil
	}
	if err := s.q.UpsertUserEpisodeState(ctx, UpsertUserEpisodeStateParams{
		UserID:    userID,
		Kind:      int16(catalog.MediaCatalog),
		EpisodeID: episodeID,
		PositionS: 0,
		Done:      done,
	}); err != nil {
		return fmt.Errorf("episode done %d/%d: %w", userID, episodeID, err)
	}
	return nil
}

// SetPersonalProgress is SetProgress for a personal video (ADR 0037 §5): the same resume and the
// same done threshold, in the same table under kind 2. It never writes a play_event -- those feed
// Most watched and the rails, which other people see.
func (s *Store) SetPersonalProgress(ctx context.Context, userID, videoID int64, positionS, durationS int32) error {
	if userID == 0 || videoID == 0 {
		return nil
	}
	if positionS < 0 {
		positionS = 0
	}
	if durationS <= 0 {
		d, err := s.q.PersonalDuration(ctx, videoID)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("personal duration %d: %w", videoID, err)
		}
		durationS = d
	}
	if err := s.q.UpsertUserEpisodeState(ctx, UpsertUserEpisodeStateParams{
		UserID:    userID,
		Kind:      int16(catalog.MediaPersonal),
		EpisodeID: videoID,
		PositionS: positionS,
		Done:      catalog.EpisodeWatched(positionS, durationS),
	}); err != nil {
		return fmt.Errorf("personal progress %d/%d: %w", userID, videoID, err)
	}
	return s.BumpLatestDevice(ctx, userID)
}

// SetPersonalDone is SetEpisodeDone for a personal video: a manual mark, position cleared.
func (s *Store) SetPersonalDone(ctx context.Context, userID, videoID int64, done bool) error {
	if userID == 0 || videoID == 0 {
		return nil
	}
	if err := s.q.UpsertUserEpisodeState(ctx, UpsertUserEpisodeStateParams{
		UserID:    userID,
		Kind:      int16(catalog.MediaPersonal),
		EpisodeID: videoID,
		PositionS: 0,
		Done:      done,
	}); err != nil {
		return fmt.Errorf("personal done %d/%d: %w", userID, videoID, err)
	}
	return nil
}

func (s *Store) recordPlayEvent(ctx context.Context, userID, episodeID int64, fileID *int64) error {
	recent, err := s.q.HasRecentPlayEvent(ctx, HasRecentPlayEventParams{
		UserID: userID, EpisodeID: &episodeID, Mins: playEventSessionMins,
	})
	if err != nil {
		return fmt.Errorf("progress recent play %d/%d: %w", userID, episodeID, err)
	}
	if recent {
		return nil
	}
	item, err := s.q.EpisodeItemKind(ctx, episodeID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("progress item for episode %d: %w", episodeID, err)
	}
	if err := s.q.InsertPlayEvent(ctx, InsertPlayEventParams{
		UserID:    userID,
		ItemID:    item.ItemID,
		EpisodeID: &episodeID,
		FileID:    fileID,
		Kind:      item.Kind,
	}); err != nil {
		return fmt.Errorf("progress play event %d/%d: %w", userID, episodeID, err)
	}
	return nil
}

// EpisodeProgress is the viewer's resume for one episode. Missing row is ok=false, not an error.
func (s *Store) EpisodeProgress(ctx context.Context, userID, episodeID int64) (catalog.EpisodeProgress, bool, error) {
	if userID == 0 || episodeID == 0 {
		return catalog.EpisodeProgress{}, false, nil
	}
	row, err := s.q.UserEpisodeProgress(ctx, UserEpisodeProgressParams{
		UserID: userID, EpisodeID: episodeID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return catalog.EpisodeProgress{}, false, nil
	}
	if err != nil {
		return catalog.EpisodeProgress{}, false, fmt.Errorf("episode progress %d/%d: %w", userID, episodeID, err)
	}
	var last int64
	if row.LastFileID != nil {
		last = *row.LastFileID
	}
	return catalog.EpisodeProgress{
		PositionS:  row.PositionS,
		Done:       row.Done,
		LastFileID: last,
	}, true, nil
}

// WatchedStateSince is the cross-device sync delta (docs/11 §6.1): user_episode_state rows this
// user has touched since watermark, oldest first, capped at limit so a heavy watcher's whole
// history still fits one reply.
func (s *Store) WatchedStateSince(ctx context.Context, userID, watermark int64, limit int32) ([]catalog.WatchedState, error) {
	if userID == 0 {
		return nil, nil
	}
	rows, err := s.q.WatchedStateSince(ctx, WatchedStateSinceParams{
		UserID: userID, Watermark: watermark, RowLimit: limit,
	})
	if err != nil {
		return nil, fmt.Errorf("watched state since %d/%d: %w", userID, watermark, err)
	}
	out := make([]catalog.WatchedState, len(rows))
	for i, r := range rows {
		out[i] = catalog.WatchedState{Kind: catalog.MediaKind(r.Kind), EpisodeID: r.EpisodeID, Done: r.Done, Seq: r.Seq}
	}
	return out, nil
}
