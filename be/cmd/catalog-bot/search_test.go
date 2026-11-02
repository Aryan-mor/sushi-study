package main

import (
	"strings"
	"testing"

	"github.com/Aryan-mor/sushi-study/be/internal/core/catalog"
)

func TestInlineArticlePostsCaptionNotStub(t *testing.T) {
	t.Parallel()
	b := &bot{mainUser: "exampleMainBot"}
	item := catalog.Item{Row: catalog.Row{
		TMDBID: 196322,
		Kind:   catalog.KindSeries,
		Title:  "Dark Matter",
		Year:   2024,
	}}
	art := b.inlineArticle(item, true)
	text := art.InputMessageContent.MessageText
	if strings.Contains(text, "#tmdb:") {
		t.Fatalf("stub leaked into shared card: %s", text)
	}
	if !strings.Contains(text, "Dark Matter") || !strings.Contains(text, "سریال") {
		t.Fatalf("caption = %s", text)
	}
	if art.InputMessageContent.ParseMode != "HTML" {
		t.Fatalf("parse mode %q", art.InputMessageContent.ParseMode)
	}
	if art.ID != "t:196322" {
		t.Fatalf("id %q", art.ID)
	}
	if art.ReplyMarkup == nil {
		t.Fatal("missing keyboard")
	}
	var labels []string
	for _, row := range art.ReplyMarkup.Rows {
		for _, btn := range row {
			labels = append(labels, btn.Text)
		}
	}
	joined := strings.Join(labels, ",")
	if !strings.Contains(joined, playInAppBtn) || !strings.Contains(joined, shareBtn) {
		t.Fatalf("buttons %s", joined)
	}
}

func TestShareKeyboardDownload(t *testing.T) {
	t.Parallel()
	kb := shareInlineKeyboard(catalog.KindSeries, 196322, "exampleCatalogBot", "exampleMainBot")
	var download string
	for _, row := range kb.Rows {
		for _, btn := range row {
			if btn.Text == "📥 دانلود" {
				download = btn.URL
			}
		}
	}
	if download != "https://t.me/exampleCatalogBot?start=t_196322" {
		t.Fatalf("download url %q", download)
	}
}
