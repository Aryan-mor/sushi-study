package main

import (
	"strings"
	"testing"
	"time"

	"github.com/Aryan-mor/sushi-study/be/internal/app/onlineview"
	"github.com/Aryan-mor/sushi-study/be/internal/core/catalog"
)

func TestFormatOnlineUserOrdersOldToNewAndMarksCurrent(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 9, 27, 12, 0, 0, 0, time.UTC)
	entries := []onlineview.Entry{
		{At: now.Add(-time.Minute), ItemTitle: "Live", PositionS: 3661},
		{At: now.Add(-time.Hour), ItemTitle: "Mid", Kind: int16(catalog.KindSeries), SeasonNo: 1, EpisodeNo: 3, EpisodeTitle: "Pilot", PositionS: 125},
		{At: now.Add(-2 * time.Hour), ItemTitle: "Old", PositionS: 90, Done: true},
	}
	person := onlineview.Person{ID: 7, Name: "Arya <b>", Username: "arya", Found: true}
	entries[0].TMDBID = 550
	entries[1].TMDBID = 1396
	latest := entries[0]

	got := formatOnlineUser("fa", onlineWatchView{
		Person: person, Entries: entries, Total: 3, Devices: []onlineview.Device{
			{Platform: "android_tv"}, {Platform: "windows"},
		}, Latest: &latest,
	}, now)

	histAt := strings.Index(got, "دیده‌ها")
	if histAt < 0 {
		t.Fatalf("history heading missing:\n%s", got)
	}
	hist := got[histAt:]
	old := strings.Index(hist, "Old")
	mid := strings.Index(hist, "S01E03")
	live := strings.Index(hist, "Live")
	if old < 0 || mid < 0 || live < 0 || !(live < mid && mid < old) {
		t.Fatalf("want newest → oldest, got:\n%s", got)
	}
	if !strings.Contains(got, "Arya &lt;b&gt;") || !strings.Contains(got, "(@arya)") {
		t.Fatalf("header = %s", got)
	}
	if !strings.Contains(got, "الان") || !strings.Contains(got, "1:01:01") || !strings.Contains(got, "themoviedb.org/movie/550") {
		t.Fatalf("current missing:\n%s", got)
	}
	if !strings.Contains(got, "themoviedb.org/tv/1396") || !strings.Contains(got, "تلویزیون") || !strings.Contains(got, "Windows") {
		t.Fatalf("series link or devices missing:\n%s", got)
	}
	if strings.Count(got, "▶️") < 2 {
		t.Fatalf("current should be in the header and the list:\n%s", got)
	}
	if !strings.Contains(got, "تمام") || !strings.Contains(got, "2:05") {
		t.Fatalf("progress labels:\n%s", got)
	}
	if !strings.Contains(got, "2026-09-27 15:29 · ") || !strings.Contains(got, "2026-09-27 14:30 · ") || !strings.Contains(got, "2026-09-27 13:30 · ") {
		t.Fatalf("watch stamps (Tehran) missing:\n%s", got)
	}
	if strings.Contains(got, "قدیمی‌تر") {
		t.Fatalf("a short page must not mention clipped rows:\n%s", got)
	}
}

func TestFormatOnlineUserIdleWhenLastPlayIsStale(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 9, 27, 12, 0, 0, 0, time.UTC)
	stale := onlineview.Entry{At: now.Add(-time.Hour), ItemTitle: "Yesterday", Done: true}
	got := formatOnlineUser("en", onlineWatchView{
		Person: onlineview.Person{Name: "Sam", Found: true}, Entries: []onlineview.Entry{stale}, Total: 1, Latest: &stale,
	}, now)
	if !strings.Contains(got, "not playing") {
		t.Fatalf("stale play must not be Now:\n%s", got)
	}
	if strings.Contains(got, "▶️") {
		t.Fatalf("stale row must not be marked current:\n%s", got)
	}
	if !strings.Contains(got, "2026-09-27 14:30 · ") {
		t.Fatalf("stale row still needs a watch stamp:\n%s", got)
	}
}

func TestOnlineWatchPager(t *testing.T) {
	t.Parallel()

	live := onlineSpans[0]
	mid := onlineUserKeyboard(7, 2, 1, 20, live, "fa")
	if !rowsContain(mid.Rows, onlineUserData(7, 2, 0, live)) || !rowsContain(mid.Rows, onlineUserData(7, 2, 2, live)) {
		t.Fatal("a middle history page needs previous and next")
	}
	first := onlineUserKeyboard(7, 2, 0, 20, live, "fa")
	if rowsContain(first.Rows, onlineUserData(7, 2, -1, live)) {
		t.Fatal("the newest page must not offer previous")
	}
	if !rowsContain(first.Rows, onlineUserData(7, 2, 1, live)) {
		t.Fatal("the newest page must offer next")
	}
	last := onlineUserKeyboard(7, 2, 2, 20, live, "en")
	if rowsContain(last.Rows, onlineUserData(7, 2, 3, live)) {
		t.Fatal("the oldest page must not offer next")
	}
}

func TestOnlineListKeyboardPagesAndFitsCallback(t *testing.T) {
	t.Parallel()

	viewers := make([]onlineview.Viewer, onlinePageSize)
	for i := range viewers {
		viewers[i] = onlineview.Viewer{ID: int64(100 + i), Name: "Person", Username: "u"}
	}
	live := onlineSpans[0]
	kb := onlineListKeyboard(viewers, 0, 20, live, "fa")
	if !rowsContain(kb.Rows, onlineUserData(100, 0, -1, live)) {
		t.Fatal("first user button must carry id, page, and window")
	}
	if !rowsContain(kb.Rows, onlinePageData(1, live)) {
		t.Fatal("page 0 with a remainder must offer next in the same window")
	}
	if rowsContain(kb.Rows, onlinePageData(0, onlineSpans[len(onlineSpans)-1])) {
		t.Fatal("user list must not keep the time buttons")
	}
	if !rowsContain(kb.Rows, cbMenuOnline) || !rowsContain(kb.Rows, cbMenuHome) {
		t.Fatal("user list footer must go back to the time picker and Home")
	}
	pick := onlinePickKeyboard("fa")
	if !rowsContain(pick.Rows, onlinePageData(0, onlineSpans[len(onlineSpans)-1])) {
		t.Fatal("time picker must include 1 month")
	}
	if !rowsContain(pick.Rows, cbMenuActions) {
		t.Fatal("time picker must go back to Actions")
	}

	week, ok := onlineSpanByCode("1w")
	if !ok {
		t.Fatal("1 week window missing")
	}
	back := onlineUserKeyboard(9, 3, 0, 1, week, "en")
	if !rowsContain(back.Rows, onlinePageData(3, week)) {
		t.Fatal("user panel must return to the same list page and window")
	}
	for _, row := range kb.Rows {
		for _, btn := range row {
			if len(btn.Data) > 64 {
				t.Fatalf("callback %q is %d bytes", btn.Data, len(btn.Data))
			}
		}
	}
}

func TestParseOnlineCallback(t *testing.T) {
	t.Parallel()

	kind, page, logPage, id, span, ok := parseOnlineCallback(cbMenuOnline)
	if !ok || kind != onlineCbPick || page != 0 || logPage != -1 || id != 0 || span.code != "" {
		t.Fatalf("menu = %d %d %d %d %s %v", kind, page, logPage, id, span.code, ok)
	}
	kind, page, _, _, span, ok = parseOnlineCallback(cbOnlinePagePrefix + "2")
	if !ok || kind != onlineCbList || page != 2 || span.code != "0" {
		t.Fatalf("page = %d %d %s %v", kind, page, span.code, ok)
	}
	_, page, _, _, span, ok = parseOnlineCallback(cbOnlinePagePrefix + "2:1w")
	if !ok || page != 2 || span.mins != 7*24*60 {
		t.Fatalf("week page = %d %d %v", page, span.mins, ok)
	}
	kind, page, logPage, id, span, ok = parseOnlineCallback(cbOnlineUserPrefix + "42:3:1m")
	if !ok || kind != onlineCbUser || id != 42 || page != 3 || logPage != -1 || span.code != "1m" {
		t.Fatalf("user = %d %d %d %d %s %v", kind, page, logPage, id, span.code, ok)
	}
	_, _, logPage, id, _, ok = parseOnlineCallback(cbOnlineUserPrefix + "42:3:1m:2")
	if !ok || id != 42 || logPage != 2 {
		t.Fatalf("history page = %d %d %v", id, logPage, ok)
	}
	if _, _, _, _, _, ok = parseOnlineCallback("on:p:nope"); ok {
		t.Fatal("bad page must not route")
	}
	if _, _, _, _, _, ok = parseOnlineCallback("on:p:1:nope"); ok {
		t.Fatal("unknown window must not route")
	}
	if _, _, _, _, _, ok = parseOnlineCallback(cbOnlineUserPrefix + "0:1"); ok {
		t.Fatal("id 0 must not route")
	}
}

func TestOnlineSpanLabels(t *testing.T) {
	t.Parallel()

	want := []string{
		"آنلاین", "30 دقیقه", "3 ساعت", "6 ساعت", "12 ساعت",
		"1 روز", "2 روز", "3 روز", "5 روز", "1 هفته", "2 هفته", "1 ماه",
	}
	if len(onlineSpans) != len(want) {
		t.Fatalf("spans = %d, want %d", len(onlineSpans), len(want))
	}
	for i, s := range onlineSpans {
		if s.fa != want[i] || s.mins <= 0 || s.code == "" {
			t.Fatalf("span %d = %+v, want fa %q", i, s, want[i])
		}
	}
	if onlineSpans[0].mins != onlineWindowMins || onlineSpans[len(onlineSpans)-1].mins != 30*24*60 {
		t.Fatal("live window must stay 5 minutes and the last window 30 days")
	}
}
