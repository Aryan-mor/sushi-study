package tg

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func TestSendRichKeyboardJSONWithoutHero(t *testing.T) {
	t.Parallel()

	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/bottest-token/sendRichMessage" {
			t.Errorf("path %s", r.URL.Path)
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		if ct := r.Header.Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
			t.Errorf("content-type %s", ct)
		}
		raw, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatal(err)
		}
		var body sendRichBody
		if err := json.Unmarshal(raw, &body); err != nil {
			t.Fatal(err)
		}
		if body.ChatID != 42 || body.RichMessage.HTML != "<p>hi</p>" {
			t.Errorf("body %+v", body)
		}
		if len(body.RichMessage.Media) != 0 {
			t.Errorf("media %v", body.RichMessage.Media)
		}
		calls.Add(1)
		_, _ = w.Write([]byte(`{"ok":true,"result":{"message_id":9}}`))
	}))
	t.Cleanup(srv.Close)

	c := New("test-token")
	c.baseURL = srv.URL
	msg, err := c.SendRichKeyboard(t.Context(), 42, "<p>hi</p>", InlineKeyboard{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if msg.ID != 9 {
		t.Fatalf("id %d", msg.ID)
	}
	if calls.Load() != 1 {
		t.Fatalf("calls = %d", calls.Load())
	}
}

func TestSendRichKeyboardMultipartHero(t *testing.T) {
	t.Parallel()

	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/bottest-token/sendRichMessage" {
			t.Errorf("path %s", r.URL.Path)
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		if !strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/form-data") {
			t.Errorf("content-type %s", r.Header.Get("Content-Type"))
		}
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			t.Fatal(err)
		}
		if r.FormValue("chat_id") != "7" {
			t.Errorf("chat_id %s", r.FormValue("chat_id"))
		}
		var rich richMessage
		if err := json.Unmarshal([]byte(r.FormValue("rich_message")), &rich); err != nil {
			t.Fatal(err)
		}
		if rich.HTML != `<img src="tg://photo?id=hero"/>` {
			t.Errorf("html %q", rich.HTML)
		}
		if len(rich.Media) != 1 || rich.Media[0].ID != "hero" || rich.Media[0].Media.Media != "attach://hero" {
			t.Errorf("media %+v", rich.Media)
		}
		file, _, err := r.FormFile("hero")
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = file.Close() }()
		got, err := io.ReadAll(file)
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != "JPEGBYTES" {
			t.Errorf("file %q", got)
		}
		calls.Add(1)
		_, _ = w.Write([]byte(`{"ok":true,"result":{"message_id":3}}`))
	}))
	t.Cleanup(srv.Close)

	c := New("test-token")
	c.baseURL = srv.URL
	kb := InlineKeyboard{Rows: [][]InlineButton{{{Text: "ok", Data: "x"}}}}
	msg, err := c.SendRichKeyboard(t.Context(), 7, `<img src="tg://photo?id=hero"/>`, kb, []byte("JPEGBYTES"))
	if err != nil {
		t.Fatal(err)
	}
	if msg.ID != 3 {
		t.Fatalf("id %d", msg.ID)
	}
	if calls.Load() != 1 {
		t.Fatalf("calls = %d", calls.Load())
	}
}
