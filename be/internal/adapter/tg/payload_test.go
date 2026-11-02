package tg

import (
	"testing"

	"github.com/Aryan-mor/sushi-study/be/internal/core/catalog"
)

func TestWatchLaterPayloadRoundTrip(t *testing.T) {
	t.Parallel()

	if got := WatchLaterPayload(catalog.KindMovie, 550); got != "wl_m_550" {
		t.Errorf("movie payload = %q", got)
	}
	if got := WatchLaterPayload(catalog.KindSeries, 1399); got != "wl_t_1399" {
		t.Errorf("series payload = %q", got)
	}
	kind, id, ok := ParseWatchLaterPayload("wl_m_550")
	if !ok || kind != catalog.KindMovie || id != 550 {
		t.Errorf("parse movie = %d/%d/%v", kind, id, ok)
	}
	kind, id, ok = ParseWatchLaterPayload("wl_t_1399")
	if !ok || kind != catalog.KindSeries || id != 1399 {
		t.Errorf("parse series = %d/%d/%v", kind, id, ok)
	}
	if _, _, ok := ParseWatchLaterPayload("m_550"); ok {
		t.Error("catalog payload must not parse as watch-later")
	}
}

func TestCatalogPayloadRoundTrip(t *testing.T) {
	t.Parallel()

	if got := CatalogPayload(catalog.KindMovie, 550); got != "m_550" {
		t.Errorf("movie payload = %q", got)
	}
	kind, id, ok := ParseCatalogPayload("t_1399")
	if !ok || kind != catalog.KindSeries || id != 1399 {
		t.Errorf("parse series = %d/%d/%v", kind, id, ok)
	}
	if _, _, ok := ParseCatalogPayload("wl_m_550"); ok {
		t.Error("watch-later payload must not parse as catalog start")
	}
	if _, _, ok := ParseCatalogPayload(""); ok {
		t.Error("empty accepted")
	}
}

func TestAppWatchPayloadRoundTrip(t *testing.T) {
	t.Parallel()

	if got := AppWatchPayload(catalog.KindMovie, 550); got != "app_m_550" {
		t.Errorf("movie payload = %q", got)
	}
	kind, id, ok := ParseAppWatchPayload("app_t_1399")
	if !ok || kind != catalog.KindSeries || id != 1399 {
		t.Errorf("parse series = %d/%d/%v", kind, id, ok)
	}
	if _, _, ok := ParseAppWatchPayload("wl_m_550"); ok {
		t.Error("watch-later payload must not parse as app-watch")
	}
	if _, _, ok := ParseAppWatchPayload("m_550"); ok {
		t.Error("catalog payload must not parse as app-watch")
	}
}
