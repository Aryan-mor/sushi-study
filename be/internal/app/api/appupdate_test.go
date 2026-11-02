package api_test

import (
	"context"
	"errors"
	"testing"

	"github.com/Aryan-mor/sushi-study/be/internal/app/api"
	sushiv1 "github.com/Aryan-mor/sushi-study/be/internal/protocol/sushi/v1"
)

type stubReleases struct {
	info    api.AppReleaseInfo
	nightly api.AppReleaseInfo
	ok      bool
	err     error
}

func (s stubReleases) AppReleaseInfo(_ context.Context, _, track string) (api.AppReleaseInfo, bool, error) {
	if track == api.AppReleaseNightly {
		if s.nightly.Version == "" && s.nightly.MessageID == 0 {
			return api.AppReleaseInfo{}, false, s.err
		}
		return s.nightly, true, s.err
	}
	return s.info, s.ok, s.err
}

func TestLookupAppReleaseReturnsTheShelfRow(t *testing.T) {
	t.Parallel()

	want := api.AppReleaseInfo{ChannelID: -100, MessageID: 9, Version: "1.2.0", FileName: "app.apk"}
	svc := newAPI(&fakeRails{}).WithAppReleases(stubReleases{info: want, ok: true})
	got, err := svc.LookupAppRelease(context.Background(), "android_new", api.Principal{})
	if err != nil {
		t.Fatalf("LookupAppRelease: %v", err)
	}
	if got != want {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

func TestLookupAppReleaseNotFoundWhenMissing(t *testing.T) {
	t.Parallel()

	svc := newAPI(&fakeRails{}).WithAppReleases(stubReleases{})
	_, err := svc.LookupAppRelease(context.Background(), "windows", api.Principal{})
	if !errors.Is(err, api.ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

func TestLookupAppReleaseRejectsAnEmptyPlatform(t *testing.T) {
	t.Parallel()

	svc := newAPI(&fakeRails{}).WithAppReleases(stubReleases{ok: true})
	if _, err := svc.LookupAppRelease(context.Background(), "  ", api.Principal{}); err == nil {
		t.Fatal("empty platform should error")
	}
}

func TestHomeAttachesLatestApp(t *testing.T) {
	t.Parallel()

	svc := newAPI(&fakeRails{seq: 3}).WithAppReleases(stubReleases{
		info: api.AppReleaseInfo{ChannelID: -1, MessageID: 2, Version: "1.1.150"},
		ok:   true,
	})
	res, err := svc.Home(context.Background(), api.Principal{}, &sushiv1.HomeReq{
		Tab:      sushiv1.Tab_TAB_MOVIES,
		Platform: "android_tv",
	})
	if err != nil {
		t.Fatalf("Home: %v", err)
	}
	if res.GetLatestApp().GetVersion() != "1.1.150" || res.GetLatestApp().GetPlatform() != "android_tv" {
		t.Fatalf("latest_app = %+v", res.GetLatestApp())
	}
}

func TestHomeCacheHitStillCarriesLatestApp(t *testing.T) {
	t.Parallel()

	svc := newAPI(&fakeRails{seq: 5}).WithAppReleases(stubReleases{
		info: api.AppReleaseInfo{Version: "2.0.0", ChannelID: -1, MessageID: 1},
		ok:   true,
	})
	res, err := svc.Home(context.Background(), api.Principal{}, &sushiv1.HomeReq{
		Tab:      sushiv1.Tab_TAB_MOVIES,
		SinceSeq: 5,
		Platform: "windows",
	})
	if err != nil {
		t.Fatalf("Home: %v", err)
	}
	if len(res.GetRails()) != 0 {
		t.Fatalf("cache-hit should skip rails, got %d", len(res.GetRails()))
	}
	if res.GetLatestApp().GetVersion() != "2.0.0" {
		t.Fatalf("latest_app.version = %q", res.GetLatestApp().GetVersion())
	}
}

func TestHomeOmitsLatestAppWhenPlatformEmpty(t *testing.T) {
	t.Parallel()

	svc := newAPI(&fakeRails{seq: 1}).WithAppReleases(stubReleases{
		info: api.AppReleaseInfo{Version: "9.9.9"},
		ok:   true,
	})
	res, err := svc.Home(context.Background(), api.Principal{}, &sushiv1.HomeReq{
		Tab: sushiv1.Tab_TAB_MOVIES,
	})
	if err != nil {
		t.Fatalf("Home: %v", err)
	}
	if res.GetLatestApp() != nil {
		t.Fatalf("old client sent no platform; latest_app should be omitted")
	}
}

func TestHomeOffersNightlyOnlyToBetaTesters(t *testing.T) {
	t.Parallel()

	svc := newAPI(&fakeRails{seq: 1}).WithAppReleases(stubReleases{
		info:    api.AppReleaseInfo{ChannelID: -1, MessageID: 2, Version: "1.2.0"},
		nightly: api.AppReleaseInfo{ChannelID: -1, MessageID: 9, Version: "1.2.0-nightly"},
		ok:      true,
	})
	beta, err := svc.Home(context.Background(), api.Principal{BetaTester: true}, &sushiv1.HomeReq{
		Tab:      sushiv1.Tab_TAB_MOVIES,
		Platform: "android_new",
	})
	if err != nil {
		t.Fatalf("Home beta: %v", err)
	}
	if beta.GetLatestApp().GetVersion() != "1.2.0-nightly" {
		t.Fatalf("beta latest_app = %q", beta.GetLatestApp().GetVersion())
	}
	regular, err := svc.Home(context.Background(), api.Principal{}, &sushiv1.HomeReq{
		Tab:      sushiv1.Tab_TAB_MOVIES,
		Platform: "android_new",
	})
	if err != nil {
		t.Fatalf("Home: %v", err)
	}
	if regular.GetLatestApp().GetVersion() != "1.2.0" {
		t.Fatalf("stable latest_app = %q", regular.GetLatestApp().GetVersion())
	}
}

func TestHomeOffersNightlyToStaffWithoutTheFlag(t *testing.T) {
	t.Parallel()

	svc := newAPI(&fakeRails{seq: 1}).WithAppReleases(stubReleases{
		info:    api.AppReleaseInfo{ChannelID: -1, MessageID: 2, Version: "1.2.0"},
		nightly: api.AppReleaseInfo{ChannelID: -1, MessageID: 9, Version: "1.2.0-nightly"},
		ok:      true,
	})
	for _, role := range []string{"admin", "superadmin"} {
		res, err := svc.Home(context.Background(), api.Principal{Role: role}, &sushiv1.HomeReq{
			Tab:      sushiv1.Tab_TAB_MOVIES,
			Platform: "android_new",
		})
		if err != nil {
			t.Fatalf("Home %s: %v", role, err)
		}
		if res.GetLatestApp().GetVersion() != "1.2.0-nightly" {
			t.Fatalf("%s latest_app = %q", role, res.GetLatestApp().GetVersion())
		}
	}
}

func TestBetaFallsBackToStableWhenNightlyMissing(t *testing.T) {
	t.Parallel()

	svc := newAPI(&fakeRails{seq: 1}).WithAppReleases(stubReleases{
		info: api.AppReleaseInfo{ChannelID: -1, MessageID: 2, Version: "1.2.0"},
		ok:   true,
	})
	res, err := svc.Home(context.Background(), api.Principal{BetaTester: true}, &sushiv1.HomeReq{
		Tab:      sushiv1.Tab_TAB_MOVIES,
		Platform: "android_new",
	})
	if err != nil {
		t.Fatalf("Home: %v", err)
	}
	if res.GetLatestApp().GetVersion() != "1.2.0" {
		t.Fatalf("latest_app = %q", res.GetLatestApp().GetVersion())
	}
	got, err := svc.LookupAppRelease(context.Background(), "android_new", api.Principal{BetaTester: true})
	if err != nil {
		t.Fatalf("LookupAppRelease: %v", err)
	}
	if got.Version != "1.2.0" || got.MessageID != 2 {
		t.Fatalf("fallback = %+v", got)
	}
}

func TestBetaGetsStableWhenStableSemverIsNewer(t *testing.T) {
	t.Parallel()

	svc := newAPI(&fakeRails{seq: 1}).WithAppReleases(stubReleases{
		info:    api.AppReleaseInfo{ChannelID: -1, MessageID: 2, Version: "1.1.205", FileName: "stable.apk"},
		nightly: api.AppReleaseInfo{ChannelID: -1, MessageID: 9, Version: "1.1.204-nightly.15234", FileName: "nightly.apk"},
		ok:      true,
	})
	res, err := svc.Home(context.Background(), api.Principal{BetaTester: true}, &sushiv1.HomeReq{
		Tab:      sushiv1.Tab_TAB_MOVIES,
		Platform: "android_new",
	})
	if err != nil {
		t.Fatalf("Home: %v", err)
	}
	if res.GetLatestApp().GetVersion() != "1.1.205" {
		t.Fatalf("beta latest_app = %q", res.GetLatestApp().GetVersion())
	}
	got, err := svc.LookupAppRelease(context.Background(), "android_new", api.Principal{Role: "admin"})
	if err != nil {
		t.Fatalf("LookupAppRelease: %v", err)
	}
	if got.MessageID != 2 || got.Version != "1.1.205" {
		t.Fatalf("got %+v", got)
	}
}

func TestBetaKeepsNightlyWhenNightlySemverIsNewer(t *testing.T) {
	t.Parallel()

	svc := newAPI(&fakeRails{seq: 1}).WithAppReleases(stubReleases{
		info:    api.AppReleaseInfo{ChannelID: -1, MessageID: 2, Version: "1.1.205"},
		nightly: api.AppReleaseInfo{ChannelID: -1, MessageID: 9, Version: "1.1.206-nightly.16000"},
		ok:      true,
	})
	res, err := svc.Home(context.Background(), api.Principal{BetaTester: true}, &sushiv1.HomeReq{
		Tab:      sushiv1.Tab_TAB_MOVIES,
		Platform: "android_new",
	})
	if err != nil {
		t.Fatalf("Home: %v", err)
	}
	if res.GetLatestApp().GetVersion() != "1.1.206-nightly.16000" {
		t.Fatalf("beta latest_app = %q", res.GetLatestApp().GetVersion())
	}
}

func TestLookupAppReleaseGivesBetaTheNightlyFile(t *testing.T) {
	t.Parallel()

	svc := newAPI(&fakeRails{}).WithAppReleases(stubReleases{
		info:    api.AppReleaseInfo{ChannelID: -1, MessageID: 2, Version: "1.2.0", FileName: "stable.apk"},
		nightly: api.AppReleaseInfo{ChannelID: -1, MessageID: 9, Version: "1.2.0-nightly", FileName: "nightly.apk"},
		ok:      true,
	})
	got, err := svc.LookupAppRelease(context.Background(), "android_new", api.Principal{BetaTester: true})
	if err != nil {
		t.Fatalf("LookupAppRelease: %v", err)
	}
	if got.MessageID != 9 || got.Version != "1.2.0-nightly" {
		t.Fatalf("got %+v", got)
	}
	stable, err := svc.LookupAppRelease(context.Background(), "android_new", api.Principal{})
	if err != nil {
		t.Fatalf("stable lookup: %v", err)
	}
	if stable.MessageID != 2 {
		t.Fatalf("non-beta got message %d", stable.MessageID)
	}
}

func TestAppUpdateLocator(t *testing.T) {
	t.Parallel()
	if got := api.AppUpdateLocator("android_new", ""); got != "app_android_new" {
		t.Fatalf("locator = %q", got)
	}
	if got := api.AppUpdateLocator("windows", "1.1.231"); got != "app_windows_1.1.231_" {
		t.Fatalf("versioned locator = %q", got)
	}
}
