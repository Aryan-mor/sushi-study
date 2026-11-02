package pg

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/Aryan-mor/sushi-study/be/internal/app/newsroom"
	"github.com/Aryan-mor/sushi-study/be/internal/core/catalog"
)

// The announce-channel queue (ADR 0017). Discovery reads the replicated catalog; the queue itself is
// env-local, and main-bot is its only writer.

// MovieDebuts returns movies that became playable inside the window and pass the freshness rule.
func (s *Store) MovieDebuts(
	ctx context.Context, since, premieredAfter time.Time, limit int32,
) ([]newsroom.Debut, error) {
	rows, err := s.q.DebutMovies(ctx, DebutMoviesParams{
		Since:          stamp(since),
		PremieredAfter: pgtype.Date{Time: premieredAfter, Valid: true},
		Lim:            limit,
	})
	if err != nil {
		return nil, fmt.Errorf("debut movies: %w", err)
	}
	out := make([]newsroom.Debut, 0, len(rows))
	for _, r := range rows {
		out = append(out, newsroom.Debut{
			TMDBID:           uint64(r.TmdbID),
			Kind:             catalog.KindMovie,
			Title:            r.Title,
			Poster:           r.Poster,
			Genres:           r.Genres,
			Overview:         r.Overview,
			RuntimeS:         r.RuntimeS,
			Year:             r.Year,
			Rating:           r.Rating,
			Backdrop:         r.Backdrop,
			PremiereOn:       dateOrZero(r.ReleasedOn),
			FirstAvailableAt: timeOrZero(r.FirstAvailableAt),
		})
	}
	return out, nil
}

// EpisodeDebuts returns episodes that became playable inside the window, newest series activity
// first. Year is the freshness filter: series first-air is not an episode premiere (ADR 0017 §4).
func (s *Store) EpisodeDebuts(ctx context.Context, since, premieredAfter time.Time, limit int32) ([]newsroom.Debut, error) {
	rows, err := s.q.DebutEpisodes(ctx, DebutEpisodesParams{
		Since:          stamp(since),
		PremieredAfter: pgtype.Date{Time: premieredAfter, Valid: true},
		Lim:            limit,
	})
	if err != nil {
		return nil, fmt.Errorf("debut episodes: %w", err)
	}
	out := make([]newsroom.Debut, 0, len(rows))
	for _, r := range rows {
		out = append(out, newsroom.Debut{
			TMDBID:           uint64(r.TmdbID),
			Kind:             catalog.KindSeries,
			Title:            r.SeriesTitle,
			Subtitle:         r.EpisodeTitle,
			Season:           r.SeasonNo,
			Episode:          r.EpisodeNo,
			Poster:           r.Poster,
			Genres:           r.Genres,
			Overview:         r.Overview,
			RuntimeS:         r.RuntimeS,
			Year:             r.Year,
			Rating:           r.Rating,
			Backdrop:         r.Backdrop,
			PremiereOn:       dateOrZero(r.ReleasedOn),
			FirstAvailableAt: timeOrZero(r.FirstAvailableAt),
		})
	}
	return out, nil
}

// QueueAnnouncement inserts one debut. A live row for the same thing means no row comes back, which
// is "already known", not a failure.
func (s *Store) QueueAnnouncement(
	ctx context.Context, d newsroom.Debut, announceAfter time.Time,
) (bool, error) {
	_, err := s.q.InsertAnnouncement(ctx, announcementParams(d, announceAfter))
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("queue announcement: %w", err)
	}
	return true, nil
}

// InsertManualAnnouncement queues a staff draft for an immediate preview. inserted is false when
// a live row already covers the same title (queued, awaiting approval, or posted).
func (s *Store) InsertManualAnnouncement(ctx context.Context, d newsroom.Debut) (int64, bool, error) {
	d.Manual = true
	if d.FirstAvailableAt.IsZero() {
		d.FirstAvailableAt = time.Now()
	}
	id, err := s.q.InsertAnnouncement(ctx, announcementParams(d, time.Now()))
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, fmt.Errorf("manual announcement: %w", err)
	}
	return id, true, nil
}

// LiveAnnouncement is the queued or awaiting-approval row for this title, if one exists.
// A posted row does not block a second draft.
func (s *Store) LiveAnnouncement(
	ctx context.Context, tmdbID uint64, kind catalog.Kind, season, episode int32,
) (int64, int16, bool, error) {
	row, err := s.q.LiveAnnouncement(ctx, LiveAnnouncementParams{
		TmdbID:    int64(tmdbID),
		Kind:      int16(kind),
		SeasonNo:  &season,
		EpisodeNo: &episode,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, 0, false, nil
	}
	if err != nil {
		return 0, 0, false, fmt.Errorf("live announcement: %w", err)
	}
	return row.ID, row.Status, true, nil
}

// AnnouncementWasPosted reports a channel post that already went out for this title.
// A manual draft still proceeds; the bot only warns.
func (s *Store) AnnouncementWasPosted(
	ctx context.Context, tmdbID uint64, kind catalog.Kind, season, episode int32,
) (bool, error) {
	posted, err := s.q.AnnouncementWasPosted(ctx, AnnouncementWasPostedParams{
		TmdbID:    int64(tmdbID),
		Kind:      int16(kind),
		SeasonNo:  &season,
		EpisodeNo: &episode,
	})
	if err != nil {
		return false, fmt.Errorf("announcement posted before: %w", err)
	}
	return posted, nil
}

// QueueStaleSeriesEpisode inserts an episode, then keeps only the latest queued episode of that
// series. Indexing a whole season therefore leaves one hold on the highest (season, episode).
// The inserted row's announce_after is the new hold; skipped rows never reach staff.
func (s *Store) QueueStaleSeriesEpisode(
	ctx context.Context, d newsroom.Debut, announceAfter time.Time,
) (bool, error) {
	if d.Kind != catalog.KindSeries {
		return false, fmt.Errorf("queue stale series: kind %d", d.Kind)
	}
	var queued bool
	err := s.InTx(ctx, func(tx *Store) error {
		id, err := tx.q.InsertAnnouncement(ctx, announcementParams(d, announceAfter))
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		if err := tx.q.DropOlderQueuedSeries(ctx, int64(d.TMDBID)); err != nil {
			return err
		}
		if _, err := tx.q.AnnouncementQueued(ctx, id); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return nil
			}
			return err
		}
		queued = true
		return nil
	})
	if err != nil {
		return false, fmt.Errorf("queue stale series: %w", err)
	}
	return queued, nil
}

func announcementParams(d newsroom.Debut, announceAfter time.Time) InsertAnnouncementParams {
	var season, episode *int32
	if d.Kind == catalog.KindSeries {
		// A movie leaves both null so the live-row index treats every movie row for a title as the
		// same thing; a series addresses one episode.
		season, episode = &d.Season, &d.Episode
	}
	return InsertAnnouncementParams{
		TmdbID:           int64(d.TMDBID),
		Kind:             int16(d.Kind),
		SeasonNo:         season,
		EpisodeNo:        episode,
		Title:            d.Title,
		Subtitle:         d.Subtitle,
		Poster:           d.Poster,
		Genres:           d.Genres,
		Overview:         d.Overview,
		RuntimeS:         d.RuntimeS,
		Year:             d.Year,
		Rating:           d.Rating,
		PremiereOn:       dateOrNull(d.PremiereOn),
		FirstAvailableAt: stamp(d.FirstAvailableAt),
		AnnounceAfter:    stamp(announceAfter),
		Manual:           d.Manual,
	}
}

// DueAnnouncements returns queued rows whose delay has elapsed.
func (s *Store) DueAnnouncements(ctx context.Context, limit int32) ([]newsroom.Announcement, error) {
	rows, err := s.q.DueAnnouncements(ctx, limit)
	if err != nil {
		return nil, fmt.Errorf("due announcements: %w", err)
	}
	out := make([]newsroom.Announcement, 0, len(rows))
	for _, r := range rows {
		out = append(out, newsroom.Announcement{
			ID:         r.ID,
			TMDBID:     uint64(r.TmdbID),
			Kind:       catalog.Kind(r.Kind),
			Season:     deref(r.SeasonNo),
			Episode:    deref(r.EpisodeNo),
			Title:      r.Title,
			Subtitle:   r.Subtitle,
			Poster:     r.Poster,
			Genres:     r.Genres,
			Overview:   r.Overview,
			OverviewFa: r.OverviewFa,
			RuntimeS:   r.RuntimeS,
			Year:       r.Year,
			Rating:     r.Rating,
			Backdrop:   r.Backdrop,
			Country:    r.Country,
			Logo:       r.Logo,
			Network:    r.Network,
			TrailerKey: r.TrailerKey,
			PremiereOn: dateOrZero(r.PremiereOn),
			Manual:     r.Manual,
		})
		stems, known := catalog.ParseStoredBackdrops(r.Backdrops)
		out[len(out)-1].Backdrops = stems
		out[len(out)-1].BackdropsKnown = known
	}
	return out, nil
}

// MarkAwaitingApproval records which preview message an approve press will edit.
func (s *Store) MarkAwaitingApproval(ctx context.Context, id, adminChatID, adminMessageID int64) error {
	err := s.q.MarkAnnouncementAwaitingApproval(ctx, MarkAnnouncementAwaitingApprovalParams{
		ID:             id,
		AdminChatID:    &adminChatID,
		AdminMessageID: &adminMessageID,
	})
	if err != nil {
		return fmt.Errorf("mark announcement awaiting approval: %w", err)
	}
	return nil
}

// MarkSkipped parks a row nobody can review.
func (s *Store) MarkSkipped(ctx context.Context, id int64) error {
	if err := s.q.MarkAnnouncementSkipped(ctx, id); err != nil {
		return fmt.Errorf("skip announcement: %w", err)
	}
	return nil
}

// NewsAdminRecipients reuses the request panel's recipient query — the same staff review both, and
// one list is one place to add somebody.
func (s *Store) NewsAdminRecipients(ctx context.Context) ([]newsroom.Admin, error) {
	rows, err := s.q.AdminAlertRecipients(ctx)
	if err != nil {
		return nil, fmt.Errorf("news admin recipients: %w", err)
	}
	out := make([]newsroom.Admin, 0, len(rows))
	for _, r := range rows {
		out = append(out, newsroom.Admin{TelegramID: r.TgUserID, Locale: r.Locale})
	}
	return out, nil
}

// AnnouncementForDecision is the approve/reject read: the row plus its status, so a stale keyboard
// can be told apart from a live one. found is false for an id that no longer exists.
func (s *Store) AnnouncementForDecision(
	ctx context.Context, id int64,
) (a newsroom.Announcement, status int16, found bool, err error) {
	r, err := s.q.AnnouncementForDecision(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return newsroom.Announcement{}, 0, false, nil
	}
	if err != nil {
		return newsroom.Announcement{}, 0, false, fmt.Errorf("announcement %d: %w", id, err)
	}
	stems, known := catalog.ParseStoredBackdrops(r.Backdrops)
	return newsroom.Announcement{
		ID:             r.ID,
		TMDBID:         uint64(r.TmdbID),
		Kind:           catalog.Kind(r.Kind),
		Season:         deref(r.SeasonNo),
		Episode:        deref(r.EpisodeNo),
		Title:          r.Title,
		Subtitle:       r.Subtitle,
		Poster:         r.Poster,
		Genres:         r.Genres,
		Overview:       r.Overview,
		OverviewFa:     r.OverviewFa,
		RuntimeS:       r.RuntimeS,
		Year:           r.Year,
		Rating:         r.Rating,
		Backdrop:       r.Backdrop,
		Backdrops:      stems,
		BackdropsKnown: known,
		Country:        r.Country,
		Logo:           r.Logo,
		Network:        r.Network,
		TrailerKey:     r.TrailerKey,
		PremiereOn:     dateOrZero(r.PremiereOn),
		Manual:         r.Manual,
	}, r.Status, true, nil
}

// MarkAnnouncementPosted records the published message. wasPending is false when somebody already
// decided this one — two admins pressing approve must post once.
func (s *Store) MarkAnnouncementPosted(ctx context.Context, id, channelMessageID int64) (bool, error) {
	n, err := s.q.MarkAnnouncementPosted(ctx, MarkAnnouncementPostedParams{
		ID:               id,
		ChannelMessageID: &channelMessageID,
	})
	if err != nil {
		return false, fmt.Errorf("mark announcement posted: %w", err)
	}
	return n > 0, nil
}

// RejectAnnouncement declines it. wasPending is false for a stale button.
func (s *Store) RejectAnnouncement(ctx context.Context, id int64) (bool, error) {
	n, err := s.q.RejectAnnouncement(ctx, id)
	if err != nil {
		return false, fmt.Errorf("reject announcement: %w", err)
	}
	return n > 0, nil
}

func dateOrZero(d pgtype.Date) time.Time {
	if !d.Valid {
		return time.Time{}
	}
	return d.Time
}

func timeOrZero(t pgtype.Timestamptz) time.Time {
	if !t.Valid {
		return time.Time{}
	}
	return t.Time
}

// dateOrNull maps Go's zero time to SQL NULL: "we do not know the premiere" is a null date, not
// year 1 (R-META-2 — render the card without it rather than inventing one).
func dateOrNull(t time.Time) pgtype.Date {
	if t.IsZero() {
		return pgtype.Date{}
	}
	return pgtype.Date{Time: t, Valid: true}
}
