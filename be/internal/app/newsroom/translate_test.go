package newsroom

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Aryan-mor/sushi-study/be/internal/core/catalog"
)

const (
	englishOverview = "A retired hitman is pulled back in for one last job."
	persianOverview = "یک آدمکش بازنشسته برای آخرین مأموریت دوباره به میدان کشیده می‌شود."
)

type fakeTranslator struct {
	out   string
	err   error
	calls int
	got   Announcement
}

func (f *fakeTranslator) TranslateOverview(_ context.Context, a Announcement) (string, error) {
	f.calls++
	f.got = a
	return f.out, f.err
}

type savedOverview struct {
	a  Announcement
	fa string
}

type fakeOverviews struct {
	saved []savedOverview
	err   error
}

func (f *fakeOverviews) SaveOverviewFa(_ context.Context, a Announcement, fa string) error {
	f.saved = append(f.saved, savedOverview{a, fa})
	return f.err
}

func TestFillOverviewUsesTheStoredTranslation(t *testing.T) {
	t.Parallel()
	tr := &fakeTranslator{out: "should not be asked"}
	store := &fakeOverviews{}
	got := FillOverview(context.Background(), tr, store, quietLog(),
		Announcement{Overview: englishOverview, OverviewFa: persianOverview})
	if got.Overview != persianOverview {
		t.Fatalf("overview %q", got.Overview)
	}
	if tr.calls != 0 || len(store.saved) != 0 {
		t.Fatalf("translated %d, saved %d — the stored one should have been enough", tr.calls, len(store.saved))
	}
}

func TestFillOverviewTranslatesAndSavesAgainstTheEnglish(t *testing.T) {
	t.Parallel()
	tr := &fakeTranslator{out: persianOverview}
	store := &fakeOverviews{}
	in := Announcement{TMDBID: 9, Kind: catalog.KindSeries, Season: 2, Episode: 5, Overview: englishOverview}
	got := FillOverview(context.Background(), tr, store, quietLog(), in)

	if got.Overview != persianOverview || got.OverviewFa != persianOverview {
		t.Fatalf("got overview %q / fa %q", got.Overview, got.OverviewFa)
	}
	if len(store.saved) != 1 {
		t.Fatalf("saved %d", len(store.saved))
	}
	// The save is keyed on the text that was translated, not on the Persian that replaced it.
	if s := store.saved[0]; s.a.Overview != englishOverview || s.fa != persianOverview || s.a.Episode != 5 {
		t.Fatalf("saved %+v", s)
	}
}

func TestFillOverviewKeepsTheEnglishWhenTheLLMFails(t *testing.T) {
	t.Parallel()
	tr := &fakeTranslator{err: errors.New("quota exceeded")}
	store := &fakeOverviews{}
	got := FillOverview(context.Background(), tr, store, quietLog(), Announcement{Overview: englishOverview})
	if got.Overview != englishOverview || got.OverviewFa != "" {
		t.Fatalf("got %q / %q", got.Overview, got.OverviewFa)
	}
	if len(store.saved) != 0 {
		t.Fatalf("saved a failed translation")
	}
}

func TestFillOverviewStillPostsWhenTheSaveFails(t *testing.T) {
	t.Parallel()
	tr := &fakeTranslator{out: persianOverview}
	store := &fakeOverviews{err: errors.New("library down")}
	got := FillOverview(context.Background(), tr, store, quietLog(), Announcement{Overview: englishOverview})
	if got.Overview != persianOverview {
		t.Fatalf("overview %q", got.Overview)
	}
}

func TestFillOverviewSkipsTextThatIsAlreadyPersianOrEmpty(t *testing.T) {
	t.Parallel()
	for _, overview := range []string{persianOverview, "", "   "} {
		tr := &fakeTranslator{out: persianOverview}
		got := FillOverview(context.Background(), tr, &fakeOverviews{}, quietLog(), Announcement{Overview: overview})
		if tr.calls != 0 {
			t.Errorf("%q: translator called", overview)
		}
		if got.Overview != overview {
			t.Errorf("%q: changed to %q", overview, got.Overview)
		}
	}
}

func TestFillOverviewWithNoTranslatorIsANoOp(t *testing.T) {
	t.Parallel()
	got := FillOverview(context.Background(), nil, nil, quietLog(), Announcement{Overview: englishOverview})
	if got.Overview != englishOverview {
		t.Fatalf("overview %q", got.Overview)
	}
	if NewLLMTranslator(nil) != nil {
		t.Fatal("NewLLMTranslator(nil) must be nil so callers need no check")
	}
}

func TestPreviewShowsStaffThePersianSynopsis(t *testing.T) {
	t.Parallel()
	store := &fakeStore{
		due: []Announcement{{
			ID: 7, TMDBID: 1752176, Kind: catalog.KindMovie, Title: "Blackmail", Year: 2026,
			Overview: englishOverview, PremiereOn: time.Date(2026, 8, 20, 0, 0, 0, 0, time.UTC),
		}},
		admins: []Admin{{TelegramID: 111, Locale: "fa"}},
	}
	dm := &fakeMessenger{}
	tr := &fakeTranslator{out: persianOverview}
	overviews := &fakeOverviews{}
	newTestReconciler(store, dm).WithTranslator(tr, overviews).Tick(context.Background())

	if len(dm.sends) != 1 {
		t.Fatalf("sent %d", len(dm.sends))
	}
	p := dm.sends[0].preview
	if !strings.Contains(p.Rich, persianOverview) || strings.Contains(p.Rich, englishOverview) {
		t.Errorf("preview should carry only the Persian synopsis:\n%s", p.Rich)
	}
	if len(overviews.saved) != 1 {
		t.Errorf("saved %d", len(overviews.saved))
	}
}

type fakeCompleter struct {
	out    string
	system string
	user   string
	schema map[string]any
}

func (f *fakeCompleter) Complete(_ context.Context, system, user string, schema map[string]any) (string, error) {
	f.system, f.user, f.schema = system, user, schema
	return f.out, nil
}

func TestLLMTranslatorSendsTitleContextAndReadsTheJSON(t *testing.T) {
	t.Parallel()
	llm := &fakeCompleter{out: "```json\n{\"overview\": \"" + persianOverview + "\"}\n```"}
	got, err := NewLLMTranslator(llm).TranslateOverview(context.Background(), Announcement{
		Title: "Slow Horses", Year: 2022, Kind: catalog.KindSeries, Season: 5, Episode: 2,
		Subtitle: "Missing", Genres: "Drama, Thriller", Overview: englishOverview,
	})
	if err != nil {
		t.Fatal(err)
	}
	if got != persianOverview {
		t.Fatalf("got %q", got)
	}
	for _, want := range []string{"Slow Horses (2022)", "season 5 episode 2", "Missing", "Drama, Thriller", englishOverview} {
		if !strings.Contains(llm.user, want) {
			t.Errorf("input missing %q:\n%s", want, llm.user)
		}
	}
	if llm.schema == nil || !strings.Contains(llm.system, "Persian") {
		t.Error("prompt or schema missing")
	}
}

func TestParseTranslationRejectsAnswersThatAreNotPersian(t *testing.T) {
	t.Parallel()
	for _, raw := range []string{
		`{"overview": "` + englishOverview + `"}`, // echoed back
		`{"overview": "  "}`,
		`not json`,
	} {
		if _, err := parseTranslation(raw); err == nil {
			t.Errorf("accepted %q", raw)
		}
	}
}

func TestIsPersian(t *testing.T) {
	t.Parallel()
	cases := map[string]bool{
		persianOverview: true,
		englishOverview: false,
		"John Wick در نیویورک برمی‌گردد و این بار انتقام می‌گیرد": true,
		"2026 — 123": false,
		"":           false,
	}
	for in, want := range cases {
		if got := isPersian(in); got != want {
			t.Errorf("isPersian(%q) = %v, want %v", in, got, want)
		}
	}
}
