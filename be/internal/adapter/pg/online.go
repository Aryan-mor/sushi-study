package pg

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/Aryan-mor/sushi-study/be/internal/app/onlineview"
)

// ListOnline returns people with playback, a search, or an app check-in in the last mins minutes.
func (s *Store) ListOnline(ctx context.Context, mins, limit, offset int32) ([]onlineview.Viewer, error) {
	rows, err := s.q.ListOnlineViewers(ctx, ListOnlineViewersParams{
		Mins: mins, RowLimit: limit, RowOffset: offset,
	})
	if err != nil {
		return nil, fmt.Errorf("list online: %w", err)
	}
	out := make([]onlineview.Viewer, 0, len(rows))
	for _, r := range rows {
		out = append(out, onlineview.Viewer{
			ID: r.ID, Name: r.TgName, Username: r.TgUsername, SeenAt: timeOf(r.SeenAt),
			Platforms: splitPlatforms(r.Platforms),
		})
	}
	return out, nil
}

// CountOnline is the size of the same window ListOnline pages through.
func (s *Store) CountOnline(ctx context.Context, mins int32) (int64, error) {
	n, err := s.q.CountOnlineViewers(ctx, mins)
	if err != nil {
		return 0, fmt.Errorf("count online: %w", err)
	}
	return n, nil
}

// WatchLog is one page of episode progress, newest first. Found is false when the user id is gone.
func (s *Store) WatchLog(ctx context.Context, userID int64, limit, offset int32) (onlineview.Person, []onlineview.Entry, error) {
	label, err := s.q.OnlineUserLabel(ctx, userID)
	if errors.Is(err, pgx.ErrNoRows) {
		return onlineview.Person{}, nil, nil
	}
	if err != nil {
		return onlineview.Person{}, nil, fmt.Errorf("online user %d: %w", userID, err)
	}
	rows, err := s.q.UserWatchLog(ctx, UserWatchLogParams{UserID: userID, RowLimit: limit, RowOffset: offset})
	if err != nil {
		return onlineview.Person{}, nil, fmt.Errorf("watch log %d: %w", userID, err)
	}
	entries := make([]onlineview.Entry, 0, len(rows))
	for _, r := range rows {
		entries = append(entries, watchEntry(r))
	}
	person := onlineview.Person{ID: label.ID, Name: label.TgName, Username: label.TgUsername, Found: true}
	return person, entries, nil
}

// LatestWatch is the newest progress row, for the "playing now" line on every history page.
func (s *Store) LatestWatch(ctx context.Context, userID int64) (onlineview.Entry, bool, error) {
	row, err := s.q.UserLatestWatch(ctx, userID)
	if errors.Is(err, pgx.ErrNoRows) {
		return onlineview.Entry{}, false, nil
	}
	if err != nil {
		return onlineview.Entry{}, false, fmt.Errorf("latest watch %d: %w", userID, err)
	}
	return watchEntry(UserWatchLogRow(row)), true, nil
}

// Devices are apps that checked in during the window, newest first.
func (s *Store) Devices(ctx context.Context, userID int64, mins int32) ([]onlineview.Device, error) {
	rows, err := s.q.ListUserDevices(ctx, ListUserDevicesParams{UserID: userID, Mins: mins})
	if err != nil {
		return nil, fmt.Errorf("user devices %d: %w", userID, err)
	}
	out := make([]onlineview.Device, 0, len(rows))
	for _, r := range rows {
		out = append(out, onlineview.Device{Platform: r.Platform, SeenAt: timeOf(r.SeenAt)})
	}
	return out, nil
}

// TouchDevice records a home check-in. Unknown platform strings are ignored.
func (s *Store) TouchDevice(ctx context.Context, userID int64, platform string) error {
	if userID == 0 || !knownPlatform(platform) {
		return nil
	}
	if err := s.q.TouchUserDevice(ctx, TouchUserDeviceParams{UserID: userID, Platform: platform}); err != nil {
		return fmt.Errorf("touch device %d: %w", userID, err)
	}
	return nil
}

// BumpLatestDevice refreshes the most recent device when playback or search has no platform of its own.
func (s *Store) BumpLatestDevice(ctx context.Context, userID int64) error {
	if userID == 0 {
		return nil
	}
	if err := s.q.BumpLatestUserDevice(ctx, userID); err != nil {
		return fmt.Errorf("bump device %d: %w", userID, err)
	}
	return nil
}

// WatchCount is every progress row for the user, including ones older than the log page.
func (s *Store) WatchCount(ctx context.Context, userID int64) (int64, error) {
	n, err := s.q.UserWatchCount(ctx, userID)
	if err != nil {
		return 0, fmt.Errorf("watch count %d: %w", userID, err)
	}
	return n, nil
}

func watchEntry(r UserWatchLogRow) onlineview.Entry {
	return onlineview.Entry{
		At:           timeOf(r.PlayedAt),
		PositionS:    r.PositionS,
		Done:         r.Done,
		TMDBID:       r.TmdbID,
		ItemTitle:    r.ItemTitle,
		Kind:         r.Kind,
		SeasonNo:     r.SeasonNo,
		EpisodeNo:    r.EpisodeNo,
		EpisodeTitle: r.EpisodeTitle,
	}
}

func splitPlatforms(raw string) []string {
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func knownPlatform(platform string) bool {
	switch platform {
	case "android_new", "android_old", "android_tv", "windows", "linux", "macos", "ios":
		return true
	default:
		return false
	}
}

func timeOf(t pgtype.Timestamptz) time.Time {
	if !t.Valid {
		return time.Time{}
	}
	return t.Time
}
