package api

import (
	"context"
	"fmt"

	sushiv1 "github.com/Aryan-mor/sushi-study/be/internal/protocol/sushi/v1"
)

// PlaybackPrefsStore reads the taste answers the version picker needs. Declared here, by the
// consumer (R-GO-5).
type PlaybackPrefsStore interface {
	PlaybackPrefs(ctx context.Context, userID int64) (audio string, maxHeight int32, subtitleLang, subs string, err error)
}

// WithPlaybackPrefs attaches the taste-answer read Sync forwards to the client. Absent, Sync
// sends none and the client keeps its cold-start default.
func (a *API) WithPlaybackPrefs(s PlaybackPrefsStore) *API {
	a.playbackPrefs = s
	return a
}

// addPlaybackPrefs puts the user's audio / max-height / subtitle-lang answers on a sync reply: the
// one call every launch already makes, so honouring "I prefer subtitles" costs no message
// (R-PROTO-3).
func (a *API) addPlaybackPrefs(ctx context.Context, userID int64, out *sushiv1.SyncRes) error {
	if a.playbackPrefs == nil {
		return nil
	}
	audio, maxHeight, subtitleLang, subs, err := a.playbackPrefs.PlaybackPrefs(ctx, userID)
	if err != nil {
		return fmt.Errorf("sync playback prefs: %w", err)
	}
	out.AudioPref = audio
	out.MaxHeight = maxHeight
	out.SubtitleLang = subtitleLang
	out.Subs = subs
	return nil
}
