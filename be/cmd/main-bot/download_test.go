package main

import (
	"strings"
	"testing"

	"github.com/Aryan-mor/sushi-study/be/internal/app/identity"
)

func TestPlatformsForFileMapsCIAssets(t *testing.T) {
	t.Parallel()

	cases := []struct {
		file, caption string
		want          []string
	}{
		{"OXPlayer-Android-1.1.150-arm64-v8a.apk", "Sushi 1.1.150 · Android arm64-v8a", []string{platAndroidNew}},
		{"OXPlayer-Android-1.1.150-armeabi-v7a.apk", "Sushi 1.1.150 · Android armeabi-v7a", []string{platAndroidOld, platAndroidTV}},
		{"OXPlayer-Windows-1.1.150-Setup.exe", "Sushi 1.1.150 · Windows installer", []string{platWindows}},
		{"OXPlayer-iOS-1.1.150.ipa", "Sushi 1.1.150 · iOS", []string{platIOS}},
		{"OXPlayer-macOS-1.1.150.dmg", "Sushi 1.1.150 · macOS", []string{platMacOS}},
		{"OXPlayer-Linux-1.1.150.AppImage", "Sushi 1.1.150 · Linux AppImage", []string{platLinux}},
		{"OXPlayer-Android-1.1.150-x86_64.apk", "Sushi 1.1.150 · Android x86_64", nil},
		{"OXPlayer-Windows-1.1.150.zip", "Sushi 1.1.150 · Windows portable", nil},
		{"OXPlayer-Linux-1.1.150.zip", "Sushi 1.1.150 · Linux bundle", nil},
		{"OXPlayer-Linux-1.1.150.AppImage.zsync", "Sushi 1.1.150 · Linux AppImage zsync", nil},
		{"OXPlayer-Linux-1.1.150.flatpak", "Sushi 1.1.150 · Linux Flatpak", nil},
		{"", "news: a new movie dropped", nil},
	}
	for _, tc := range cases {
		got := platformsForFile(tc.file, tc.caption)
		if !stringSliceEqual(got, tc.want) {
			t.Errorf("platformsForFile(%q) = %v, want %v", tc.file, got, tc.want)
		}
	}
}

// A hashtag is the poster's own statement of what a file is, so it beats the file name — including
// when the file name would have said something else, or nothing at all.
func TestPlatformsFromTagsWinOverTheFileName(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name, file, caption string
		want                []string
	}{
		{"android tag is new phone only", "build.bin", "Sushi 1.2.0 #android", []string{platAndroidNew}},
		{"old android pairs tv", "build.bin", "Sushi 1.2.0 #android_old", []string{platAndroidOld, platAndroidTV}},
		{"tv only", "build.bin", "Sushi 1.2.0 #androidtv", []string{platAndroidTV}},
		{"windows", "whatever.dat", "Sushi 1.2.0 #windows", []string{platWindows}},
		{"linux", "whatever.dat", "Sushi 1.2.0 #linux", []string{platLinux}},
		{"macos short tag", "whatever.dat", "Sushi 1.2.0 #mac", []string{platMacOS}},
		{"ios", "whatever.dat", "Sushi 1.2.0 #ios", []string{platIOS}},
		{"tags are order-independent", "x", "#windows #android", []string{platAndroidNew, platWindows}},
		{"punctuation after a tag", "x", "Sushi 1.2.0 · #windows.", []string{platWindows}},
		{"unknown tags are ignored", "x", "#sushi #release", nil},
		{"tag overrides a contradicting name", "Sushi-1.2.0.dmg", "#windows", []string{platWindows}},
	}
	for _, tc := range cases {
		if got := platformsForFile(tc.file, tc.caption); !stringSliceEqual(got, tc.want) {
			t.Errorf("%s: platformsForFile(%q, %q) = %v, want %v", tc.name, tc.file, tc.caption, got, tc.want)
		}
	}
}

// The channel holds one release: registering a new post retires the message each platform pointed
// at before, once per message however many platforms shared it.
func TestSupersededIDs(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		replaced []int64
		current  int64
		want     []int64
	}{
		{"first ever post retires nothing", []int64{0, 0}, 41, nil},
		{"one shared upload is deleted once", []int64{31, 31}, 41, []int64{31}},
		{"two platforms, two old messages", []int64{31, 32}, 41, []int64{31, 32}},
		{"never deletes the post being registered", []int64{41, 41}, 41, nil},
		{"mixed: a new platform beside an updated one", []int64{0, 31}, 41, []int64{31}},
	}
	for _, tc := range cases {
		if got := supersededIDs(tc.replaced, tc.current); !int64SliceEqual(got, tc.want) {
			t.Errorf("%s: supersededIDs(%v, %d) = %v, want %v", tc.name, tc.replaced, tc.current, got, tc.want)
		}
	}
}

func TestReleaseTrack(t *testing.T) {
	t.Parallel()
	if got := releaseTrack("1.1.150-nightly"); got != "nightly" {
		t.Errorf("track = %q", got)
	}
	if got := releaseTrack("1.2.0"); got != "stable" {
		t.Errorf("track = %q", got)
	}
	if got := releaseTrack(""); got != "stable" {
		t.Errorf("empty track = %q", got)
	}
}

func TestVersionFromCaption(t *testing.T) {
	t.Parallel()
	if got := versionFromCaption("Sushi 1.1.150-nightly · Android arm64-v8a"); got != "1.1.150-nightly" {
		t.Errorf("version = %q", got)
	}
	if got := versionFromCaption(""); got != "" {
		t.Errorf("empty caption version = %q", got)
	}
}

// The picker offers the platforms that have a build, two to a row, Home last.
// macOS and iOS are always there: the tap says coming soon instead of copying a file.
func TestDownloadKeyboardOffersOnlyWhatExists(t *testing.T) {
	t.Parallel()

	kb := downloadKeyboard("en", map[string]string{
		platAndroidNew: "1.2.0",
		platAndroidOld: "1.2.0",
		platWindows:    "1.2.0",
	})
	if len(kb.Rows) != 4 {
		t.Fatalf("rows = %d, want 4 (two pairs, iOS alone, then home)", len(kb.Rows))
	}
	want := [][]string{
		{cbDlPrefix + platAndroidNew, cbDlPrefix + platAndroidOld},
		{cbDlPrefix + platWindows, cbDlPrefix + platMacOS},
		{cbDlPrefix + platIOS},
		{cbMenuHome},
	}
	for i, row := range want {
		if len(kb.Rows[i]) != len(row) {
			t.Fatalf("row %d holds %d buttons, want %d", i, len(kb.Rows[i]), len(row))
		}
		for j, data := range row {
			if kb.Rows[i][j].Data != data {
				t.Errorf("row %d col %d = %q, want %q", i, j, kb.Rows[i][j].Data, data)
			}
		}
	}

	// Nothing posted yet: macOS and iOS stay (coming soon), plus the way out.
	empty := downloadKeyboard("en", nil)
	if len(empty.Rows) != 2 || len(empty.Rows[0]) != 2 ||
		empty.Rows[0][0].Data != cbDlPrefix+platMacOS || empty.Rows[0][1].Data != cbDlPrefix+platIOS ||
		empty.Rows[1][0].Data != cbMenuHome {
		t.Fatalf("empty keyboard = %v, want macOS + iOS then Home", empty.Rows)
	}
}

func TestDownloadKeyboardKeepsPlatformOrder(t *testing.T) {
	t.Parallel()

	all := map[string]string{}
	for _, p := range dlPlatformOrder {
		all[p.platform] = "1.2.0"
	}
	kb := downloadKeyboard("en", all)
	var got []string
	for _, row := range kb.Rows {
		for _, btn := range row {
			got = append(got, btn.Data)
		}
	}
	want := []string{
		cbDlPrefix + platAndroidNew, cbDlPrefix + platAndroidOld,
		cbDlPrefix + platAndroidTV, cbDlPrefix + platWindows,
		cbDlPrefix + platLinux, cbDlPrefix + platMacOS,
		cbDlPrefix + platIOS, cbMenuHome,
	}
	if !stringSliceEqual(got, want) {
		t.Errorf("buttons = %v, want %v", got, want)
	}
}

// The title carries the version so somebody with an older build can tell this one apart.
func TestDownloadTitleNamesTheVersion(t *testing.T) {
	t.Parallel()

	withVersion := dlTitleFor("en", map[string]string{platWindows: "1.2.0"})
	if !strings.Contains(withVersion, "1.2.0") {
		t.Errorf("title = %q, want the version in it", withVersion)
	}
	bare := dlTitleFor("en", map[string]string{platWindows: ""})
	if bare != dlTitle.pick("en") {
		t.Errorf("title with no version = %q, want the plain title", bare)
	}
}

func TestSoonWantedNoticeNamesPlatformAndUser(t *testing.T) {
	t.Parallel()
	user := identity.User{TGUserID: 42, Name: "Ada", Username: "@ada"}
	for platform, want := range map[string]string{platMacOS: "macOS download wanted", platIOS: "iOS download wanted"} {
		got := soonWantedNotice(dlComingSoon[platform].label, user)
		for _, part := range []string{want, "id: 42", "name: Ada", "@ada"} {
			if !strings.Contains(got, part) {
				t.Errorf("%s notice = %q, want %q", platform, got, part)
			}
		}
	}
}

// A coming-soon tap answers in the user's language and names its own platform.
func TestComingSoonToastNamesThePlatform(t *testing.T) {
	t.Parallel()
	if got := dlComingSoon[platIOS].toast.pick("fa"); !strings.Contains(got, "آیفون") {
		t.Errorf("iOS fa toast = %q", got)
	}
	if got := dlComingSoon[platMacOS].toast.pick("en"); !strings.Contains(got, "macOS") {
		t.Errorf("macOS en toast = %q", got)
	}
	if isComingSoon(platWindows) {
		t.Error("windows must not be coming soon")
	}
}

func stringSliceEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func int64SliceEqual(a, b []int64) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
