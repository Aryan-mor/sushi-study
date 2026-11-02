package tg

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

func TestSendDocumentPostsMultipart(t *testing.T) {
	t.Parallel()

	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/bottest-token/sendDocument" {
			t.Errorf("path %s", r.URL.Path)
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		if !strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/form-data") {
			t.Errorf("content-type %s", r.Header.Get("Content-Type"))
		}
		calls.Add(1)
		_, _ = w.Write([]byte(`{"ok":true,"result":{}}`))
	}))
	t.Cleanup(srv.Close)

	path := filepath.Join(t.TempDir(), "part.7z.001")
	if err := os.WriteFile(path, []byte("archive"), 0o600); err != nil {
		t.Fatal(err)
	}

	c := New("test-token")
	c.baseURL = srv.URL
	if _, err := c.SendDocument(context.Background(), -100123, path, "part.7z.001", "caption"); err != nil {
		t.Fatalf("SendDocument: %v", err)
	}
	if calls.Load() != 1 {
		t.Fatalf("calls = %d", calls.Load())
	}
}

func TestDocumentBodyHTMLParseMode(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "a.srt")
	if err := os.WriteFile(path, []byte("1"), 0o600); err != nil {
		t.Fatal(err)
	}
	caption := `💬 Matrix` + "\n" + `<a href="https://www.themoviedb.org/movie/603">tmdb</a>`
	body, _, err := documentBody(-100, path, "a.srt", caption, "HTML")
	if err != nil {
		t.Fatal(err)
	}
	raw := body.String()
	if !strings.Contains(raw, "parse_mode") || !strings.Contains(raw, "HTML") {
		t.Fatalf("HTML caption missing parse_mode:\n%s", raw)
	}
	if !strings.Contains(raw, caption) {
		t.Fatalf("caption missing:\n%s", raw)
	}

	plain, _, err := documentBody(-100, path, "a.srt", "backup", "")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(plain.String(), "parse_mode") {
		t.Fatal("plain caption set parse_mode")
	}
}

func TestSendDocumentRejectsTelegramError(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"ok":false,"description":"chat not found"}`))
	}))
	t.Cleanup(srv.Close)

	path := filepath.Join(t.TempDir(), "a.7z")
	if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}

	c := New("tok")
	c.baseURL = srv.URL
	_, err := c.SendDocument(context.Background(), -1, path, "a.7z", "")
	if err == nil || !strings.Contains(err.Error(), "chat not found") {
		t.Fatalf("got %v", err)
	}
}

func TestSendDocumentRetries429(t *testing.T) {
	t.Parallel()

	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		n := calls.Add(1)
		if n == 1 {
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(`{"ok":false,"error_code":429,"description":"Too Many Requests: retry after 1","parameters":{"retry_after":1}}`))
			return
		}
		_, _ = w.Write([]byte(`{"ok":true,"result":{"message_id":5}}`))
	}))
	t.Cleanup(srv.Close)

	path := filepath.Join(t.TempDir(), "a.srt")
	if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}

	c := New("tok")
	c.baseURL = srv.URL
	id, err := c.SendDocument(context.Background(), -1, path, "a.srt", "")
	if err != nil {
		t.Fatalf("SendDocument: %v", err)
	}
	if id != 5 || calls.Load() != 2 {
		t.Fatalf("id=%d calls=%d, want id 5 after one retry", id, calls.Load())
	}
}
