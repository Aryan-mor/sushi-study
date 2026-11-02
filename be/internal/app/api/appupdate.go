package api

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	sushiv1 "github.com/Aryan-mor/sushi-study/be/internal/protocol/sushi/v1"
)

// Tracks on app_releases. Stable is what everyone gets, including main-bot Download.
// Nightly is the in-app shelf for beta testers and for admin/superadmin.
const (
	AppReleaseStable  = "stable"
	AppReleaseNightly = "nightly"
)

// AppReleaseInfo is one RELEASE_CHANNEL shelf row (ADR 0019).
type AppReleaseInfo struct {
	ChannelID int64
	MessageID int64
	Version   string
	FileName  string
}

// AppReleaseStore is what Home and AppUpdate need from storage. Declared here (R-GO-5).
// track is AppReleaseStable or AppReleaseNightly.
type AppReleaseStore interface {
	AppReleaseInfo(ctx context.Context, platform, track string) (AppReleaseInfo, bool, error)
}

// AppUpdateLocator is the caption stamped on the protocol-chat copy so the client can verify
// the document. Mirror of play's `plm_<fileId>`.
//
// Version is part of the caption. Every platform used to share one caption (`app_windows`),
// and the client resolves that caption from DM history before the new copy arrives — so an
// update installed whatever setup was already in the chat. A trailing underscore stops
// `1.1.23` from matching `1.1.231` via substring.
func AppUpdateLocator(platform, version string) string {
	loc := "app_" + platform
	version = strings.TrimSpace(version)
	if version == "" {
		return loc
	}
	return loc + "_" + version + "_"
}

// WithAppReleases attaches the client-binary shelf (ADR 0019). Absent, Home omits latest_app
// and AppUpdate returns an error rather than claiming nothing is posted.
func (a *API) WithAppReleases(s AppReleaseStore) *API {
	a.releases = s
	return a
}

// offersNightly is who may see the nightly shelf. Staff are beta testers by role. Anyone else
// needs users.beta_tester set by hand. main-bot Download never calls this.
func offersNightly(p Principal) bool {
	return p.BetaTester || isStaff(p.Role)
}

// LookupAppRelease is the DB half of /appupdate. Transport copies; this package never talks
// to Telegram (R-API-1).
func (a *API) LookupAppRelease(ctx context.Context, platform string, p Principal) (AppReleaseInfo, error) {
	platform = strings.TrimSpace(platform)
	if platform == "" {
		return AppReleaseInfo{}, fmt.Errorf("appupdate: platform not specified")
	}
	if a.releases == nil {
		return AppReleaseInfo{}, fmt.Errorf("appupdate: not configured")
	}
	info, ok, err := a.releaseFor(ctx, platform, offersNightly(p))
	if err != nil {
		return AppReleaseInfo{}, fmt.Errorf("appupdate: %w", err)
	}
	if !ok || info.ChannelID == 0 || info.MessageID == 0 {
		return AppReleaseInfo{}, ErrNotFound
	}
	return info, nil
}

// releaseFor picks the shelf a person may install. Beta testers get nightly when that track has
// a file and its semver is at least the stable semver. A tagged release that moved past the
// last nightly (stable 1.1.205, nightly 1.1.204) is what they install, so the prompt does not
// stick on the older build. Same numbers stay on nightly. Everyone else gets stable.
// A nightly lookup error is returned so /appupdate fails closed. Home treats that error as
// "no banner". A stable lookup error while nightly is usable keeps the nightly.
func (a *API) releaseFor(ctx context.Context, platform string, beta bool) (AppReleaseInfo, bool, error) {
	if beta {
		nightly, ok, err := a.releases.AppReleaseInfo(ctx, platform, AppReleaseNightly)
		if err != nil {
			return AppReleaseInfo{}, false, err
		}
		if ok && nightly.MessageID != 0 && nightly.Version != "" {
			stable, stableOK, stableErr := a.releases.AppReleaseInfo(ctx, platform, AppReleaseStable)
			if stableErr == nil && stableOK && stable.MessageID != 0 && stable.Version != "" &&
				appReleaseNewer(stable.Version, nightly.Version) {
				return stable, true, nil
			}
			return nightly, true, nil
		}
	}
	return a.releases.AppReleaseInfo(ctx, platform, AppReleaseStable)
}

// appReleaseNewer reports whether candidate's semver is strictly above baseline.
// The -nightly suffix and build metadata are ignored, matching sushiIsNewerApp.
// An unparseable string is not newer.
func appReleaseNewer(candidate, baseline string) bool {
	c, okC := parseAppSemver(candidate)
	b, okB := parseAppSemver(baseline)
	if !okC || !okB {
		return false
	}
	if c[0] != b[0] {
		return c[0] > b[0]
	}
	if c[1] != b[1] {
		return c[1] > b[1]
	}
	return c[2] > b[2]
}

func parseAppSemver(raw string) ([3]int, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return [3]int{}, false
	}
	if i := strings.IndexByte(raw, '+'); i >= 0 {
		raw = raw[:i]
	}
	if i := strings.IndexByte(raw, '-'); i >= 0 {
		raw = raw[:i]
	}
	parts := strings.Split(raw, ".")
	var out [3]int
	n := 0
	for _, part := range parts {
		if part == "" {
			return [3]int{}, false
		}
		v, err := strconv.Atoi(part)
		if err != nil || v < 0 {
			return [3]int{}, false
		}
		if n < 3 {
			out[n] = v
		}
		n++
	}
	if n == 0 {
		return [3]int{}, false
	}
	return out, true
}

// attachLatestApp is best-effort: a shelf miss or a lookup error leaves latest_app unset
// rather than failing Home. A blank home is worse than a missing update banner.
func (a *API) attachLatestApp(ctx context.Context, p Principal, req *sushiv1.HomeReq, res *sushiv1.HomeRes) {
	if a.releases == nil {
		return
	}
	platform := strings.TrimSpace(req.GetPlatform())
	if platform == "" {
		return
	}
	info, ok, err := a.releaseFor(ctx, platform, offersNightly(p))
	if err != nil || !ok || info.Version == "" {
		return
	}
	res.LatestApp = &sushiv1.LatestApp{Platform: platform, Version: info.Version}
}
