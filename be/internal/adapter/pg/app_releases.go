package pg

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/Aryan-mor/sushi-study/be/internal/app/api"
)

// UpsertAppRelease records the latest channel message for one client platform and track
// (stable or nightly) and returns the message that track replaced. Zero means this track had no
// build before. The other track's message is left alone.
func (s *Store) UpsertAppRelease(
	ctx context.Context, platform, track string, channelID, messageID int64, version, fileName string,
) (prevMessageID int64, err error) {
	prevMessageID, err = s.q.UpsertAppRelease(ctx, UpsertAppReleaseParams{
		Platform:  platform,
		Channel:   track,
		ChannelID: channelID,
		MessageID: messageID,
		Version:   version,
		FileName:  fileName,
	})
	if err != nil {
		return 0, fmt.Errorf("upsert app release %s %s: %w", platform, track, err)
	}
	return prevMessageID, nil
}

// AppRelease is the latest stable channel message for a platform. ok is false when CI has not
// posted a stable build yet. Download never reads the nightly track.
func (s *Store) AppRelease(ctx context.Context, platform string) (channelID, messageID int64, ok bool, err error) {
	info, ok, err := s.AppReleaseInfo(ctx, platform, api.AppReleaseStable)
	return info.ChannelID, info.MessageID, ok, err
}

// AppReleaseInfo is one shelf row apid needs for HomeRes.latest_app and /appupdate (ADR 0019).
// track is api.AppReleaseStable or api.AppReleaseNightly.
func (s *Store) AppReleaseInfo(ctx context.Context, platform, track string) (api.AppReleaseInfo, bool, error) {
	row, err := s.q.AppReleaseByPlatform(ctx, AppReleaseByPlatformParams{
		Platform: platform,
		Channel:  track,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return api.AppReleaseInfo{}, false, nil
	}
	if err != nil {
		return api.AppReleaseInfo{}, false, fmt.Errorf("app release %s %s: %w", platform, track, err)
	}
	return api.AppReleaseInfo{
		ChannelID: row.ChannelID,
		MessageID: row.MessageID,
		Version:   row.Version,
		FileName:  row.FileName,
	}, true, nil
}

// AppReleasePlatforms lists the platforms that actually have a build, with the version each is at.
// The Download screen offers exactly these.
func (s *Store) AppReleasePlatforms(ctx context.Context) (map[string]string, error) {
	rows, err := s.q.AppReleasePlatforms(ctx)
	if err != nil {
		return nil, fmt.Errorf("app release platforms: %w", err)
	}
	out := make(map[string]string, len(rows))
	for _, r := range rows {
		out[r.Platform] = r.Version
	}
	return out, nil
}
