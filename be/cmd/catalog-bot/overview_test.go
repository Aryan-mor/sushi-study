package main

import (
	"context"
	"log/slog"
	"strings"
	"sync"
	"testing"

	"github.com/Aryan-mor/sushi-study/be/internal/adapter/tg"
	"github.com/Aryan-mor/sushi-study/be/internal/app/newsroom"
	"github.com/Aryan-mor/sushi-study/be/internal/core/catalog"
)

type fakeOverviewTranslator struct {
	out string
	err error
	n   int
}

func (f *fakeOverviewTranslator) TranslateOverview(context.Context, newsroom.Announcement) (string, error) {
	f.n++
	return f.out, f.err
}

func TestWithPersianTranslatesOnceAndCaptionsInPersian(t *testing.T) {
	t.Parallel()
	const (
		english = "In a dystopian 1970s America, fifty teenage boys take part in a deadly contest."
		persian = "در آمریکای دیستوپیایی دههٔ هفتاد، پنجاه پسر نوجوان در یک مسابقهٔ مرگبار راه می‌روند."
	)
	tr := &fakeOverviewTranslator{out: persian}
	b := &bot{log: slog.New(slog.DiscardHandler), translate: tr}
	item := catalog.Item{
		Row:      catalog.Row{TMDBID: 604079, Kind: catalog.KindMovie, Title: "The Long Walk", Year: 2025},
		Overview: english,
	}
	got := b.withPersian(context.Background(), item)
	if got.OverviewFa != "" {
		t.Fatalf("blocked on translate: %q", got.OverviewFa)
	}
	<-b.overviewReady(imdbCacheKey(item.Kind, item.TMDBID))
	again := b.withPersian(context.Background(), item)
	if again.OverviewFa != persian {
		t.Fatalf("fa %q", again.OverviewFa)
	}
	if tr.n != 1 {
		t.Fatalf("translated %d times", tr.n)
	}
	caption := catalogCaption(again, shareFooter, "exampleCatalogBot")
	if !strings.Contains(caption, "دیستوپیایی") || strings.Contains(caption, "dystopian") {
		t.Fatalf("caption still English:\n%s", caption)
	}
}

type gateTranslator struct {
	out     string
	started chan struct{}
	release chan struct{}
	once    sync.Once
}

func (g *gateTranslator) TranslateOverview(context.Context, newsroom.Announcement) (string, error) {
	g.once.Do(func() { close(g.started) })
	<-g.release
	return g.out, nil
}

func TestOverviewPatchEditsOpenCard(t *testing.T) {
	t.Parallel()
	const (
		english = "In a dystopian 1970s America, fifty teenage boys take part in a deadly contest."
		persian = "در آمریکای دیستوپیایی دههٔ هفتاد، پنجاه پسر نوجوان در یک مسابقهٔ مرگبار راه می‌روند."
	)
	tr := &gateTranslator{out: persian, started: make(chan struct{}), release: make(chan struct{})}
	var mu sync.Mutex
	var caption string
	var chatID, msgID int64
	b := &bot{
		log:       slog.New(slog.DiscardHandler),
		translate: tr,
		patchCaption: func(_ context.Context, c, m int64, text string, _ tg.InlineKeyboard) error {
			mu.Lock()
			chatID, msgID, caption = c, m, text
			mu.Unlock()
			return nil
		},
	}
	item := catalog.Item{
		Row:      catalog.Row{TMDBID: 604079, Kind: catalog.KindMovie, Title: "The Long Walk", Year: 2025},
		Overview: english,
	}
	got := b.withPersian(context.Background(), item)
	b.armOverviewPatch(context.Background(), got, 9, 42, shareFooter, 0, 0, "", tg.InlineKeyboard{})
	<-tr.started
	close(tr.release)
	<-b.overviewReady(imdbCacheKey(item.Kind, item.TMDBID))
	mu.Lock()
	defer mu.Unlock()
	if chatID != 9 || msgID != 42 {
		t.Fatalf("edit target %d/%d", chatID, msgID)
	}
	if !strings.Contains(caption, "دیستوپیایی") || strings.Contains(caption, "dystopian") {
		t.Fatalf("caption not patched:\n%s", caption)
	}
}

func TestWithPersianKeepsStoredTranslation(t *testing.T) {
	t.Parallel()
	tr := &fakeOverviewTranslator{out: "نباید صدا زده شود"}
	b := &bot{log: slog.New(slog.DiscardHandler), translate: tr}
	item := catalog.Item{
		Row:        catalog.Row{TMDBID: 1, Kind: catalog.KindMovie, Title: "X"},
		Overview:   "English.",
		OverviewFa: "فارسی ذخیره‌شده.",
	}
	got := b.withPersian(context.Background(), item)
	if got.OverviewFa != "فارسی ذخیره‌شده." || tr.n != 0 {
		t.Fatalf("fa %q calls %d", got.OverviewFa, tr.n)
	}
}
