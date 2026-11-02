package api

import "testing"

func TestSubtitleCutType(t *testing.T) {
	t.Parallel()
	cases := []struct{ label, want string }{
		{"1080p SoftSub", "softsub"},
		{"Friends.S03.Complete.1080p.10bit.BluRay.x265", "bluray"},
		{"friends s03 e12 UNCUT.DVDRip.XviD", "dvd"},
		{"Show.2024.1080p.WEB-DL.x264", "web"},
		{"Movie.2026.AMZN.WEB.Fa", "web"},
		{"Movie.2026.TELESYNC.1080p", "telesync"},
		{"Movie.2026.CAM-Rip.720p", "cam"},
		{"1080p", ""},
		{"webcam", ""},
		{"BluRay SoftSub", "bluray"},
	}
	for _, tc := range cases {
		if got := subtitleCutType(tc.label); got != tc.want {
			t.Errorf("subtitleCutType(%q) = %q, want %q", tc.label, got, tc.want)
		}
	}
}

func TestSubtitleDurationMin(t *testing.T) {
	t.Parallel()
	text := "1\n00:00:01,000 --> 00:00:02,000\nhi\n\n2\n00:22:30,000 --> 00:22:32,500\nbye\n"
	if got := subtitleDurationMin(text); got != 23 {
		t.Fatalf("duration min = %d, want 23", got)
	}
	if subtitleDurationMin("no cues") != 0 {
		t.Fatal("expected 0")
	}
}

func TestMatchCachedSubtitle(t *testing.T) {
	t.Parallel()
	rows := []CachedSubtitle{
		{ChannelID: 1, SubType: "bluray", DurationMin: 23},
		{ChannelID: 2, SubType: "softsub", DurationMin: 23},
		{ChannelID: 3, SubType: "", DurationMin: 0},
		{ChannelID: 4, SubType: "", DurationMin: 23},
	}
	got, ok := matchCachedSubtitle(rows, "softsub", 23)
	if !ok || got.ChannelID != 2 {
		t.Fatalf("softsub hit = %+v ok=%v", got, ok)
	}
	if _, ok := matchCachedSubtitle(rows, "softsub", 22); ok {
		t.Fatal("different minute must miss")
	}
	if _, ok := matchCachedSubtitle(rows, "web", 23); ok {
		t.Fatal("different type must miss")
	}
	if _, ok := matchCachedSubtitle(rows, "", 0); ok {
		t.Fatal("legacy request must not take a random row")
	}
	if _, ok := matchCachedSubtitle(rows, "", 23); ok {
		t.Fatal("duration without a playback type must miss")
	}
	if _, ok := matchCachedSubtitle([]CachedSubtitle{{ChannelID: 4, SubType: "", DurationMin: 23}}, "softsub", 23); ok {
		t.Fatal("typeless row must miss even when minutes match")
	}
}
