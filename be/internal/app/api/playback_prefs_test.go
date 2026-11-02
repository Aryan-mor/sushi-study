package api_test

import (
	"context"
	"encoding/hex"
	"errors"
	"testing"

	"google.golang.org/protobuf/proto"

	"github.com/Aryan-mor/sushi-study/be/internal/app/api"
	sushiv1 "github.com/Aryan-mor/sushi-study/be/internal/protocol/sushi/v1"
)

type fakePlaybackPrefs struct {
	audio        string
	maxHeight    int32
	subtitleLang string
	subs         string
	err          error
	gotUser      int64
}

func (f *fakePlaybackPrefs) PlaybackPrefs(_ context.Context, userID int64) (string, int32, string, string, error) {
	f.gotUser = userID
	return f.audio, f.maxHeight, f.subtitleLang, f.subs, f.err
}

func newSyncAPI() *api.API {
	return api.New(&fakeRails{}, &fakeDeliveries{}, &fakeCatalog{}, &fakeSearch{},
		api.RateLimits{PerHour: 30, MaxConcurrent: 2, SearchPerHour: 60})
}

// Regression: a user who told the bot "I prefer subtitles" still got the dubbed file by default,
// because the answer never left the server. It rides on Sync, the call every launch already makes.
func TestSyncCarriesPlaybackPrefs(t *testing.T) {
	t.Parallel()
	prefs := &fakePlaybackPrefs{audio: "original", maxHeight: 1080, subtitleLang: "fa", subs: "on"}
	a := newSyncAPI().WithBrowse(&fakeBrowse{}).WithPlaybackPrefs(prefs)

	res, err := a.Sync(context.Background(), api.Principal{UserID: 7}, &sushiv1.SyncReq{})
	if err != nil {
		t.Fatalf("Sync: %v", err)
	}
	if res.GetAudioPref() != "original" || res.GetMaxHeight() != 1080 || res.GetSubtitleLang() != "fa" || res.GetSubs() != "on" {
		t.Fatalf("prefs = (%q, %d, %q, %q), want (original, 1080, fa, on)", res.GetAudioPref(), res.GetMaxHeight(), res.GetSubtitleLang(), res.GetSubs())
	}
	if prefs.gotUser != 7 {
		t.Fatalf("prefs read for user %d, want the principal 7", prefs.gotUser)
	}
}

func TestSyncWithoutBrowseStillCarriesPrefs(t *testing.T) {
	t.Parallel()
	a := newSyncAPI().WithPlaybackPrefs(&fakePlaybackPrefs{audio: "dub"})

	res, err := a.Sync(context.Background(), api.Principal{UserID: 7}, &sushiv1.SyncReq{WatchedWatermark: 5})
	if err != nil {
		t.Fatalf("Sync: %v", err)
	}
	if res.GetAudioPref() != "dub" || res.GetWatchedWatermark() != 5 {
		t.Fatalf("res = %+v, want dub and watermark 5", res)
	}
}

func TestSyncSurfacesPrefsError(t *testing.T) {
	t.Parallel()
	a := newSyncAPI().WithBrowse(&fakeBrowse{}).WithPlaybackPrefs(&fakePlaybackPrefs{err: errors.New("db down")})

	if _, err := a.Sync(context.Background(), api.Principal{UserID: 7}, &sushiv1.SyncReq{}); err == nil {
		t.Fatal("Sync should fail when the prefs read fails")
	}
}

func TestSyncWithoutPrefsStoreSendsNone(t *testing.T) {
	t.Parallel()
	res, err := newSyncAPI().WithBrowse(&fakeBrowse{}).Sync(context.Background(), api.Principal{UserID: 7}, &sushiv1.SyncReq{})
	if err != nil {
		t.Fatalf("Sync: %v", err)
	}
	if res.GetAudioPref() != "" || res.GetMaxHeight() != 0 {
		t.Fatalf("prefs = (%q, %d), want none", res.GetAudioPref(), res.GetMaxHeight())
	}
}

// syncPrefsVector is SyncRes{watched_watermark: 30, audio_pref: "original", max_height: 1080} on
// the wire. sushi-app/test/sushi/sushi_sync_pb_test.dart decodes the same bytes (R-PROTO-2).
const syncPrefsVector = "101e22086f726967696e616c28b808"

func TestSyncPrefsWireVector(t *testing.T) {
	t.Parallel()
	b, err := proto.Marshal(&sushiv1.SyncRes{WatchedWatermark: 30, AudioPref: "original", MaxHeight: 1080})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if got := hex.EncodeToString(b); got != syncPrefsVector {
		t.Fatalf("wire = %s, want %s", got, syncPrefsVector)
	}
}
