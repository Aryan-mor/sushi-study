package tmdb

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

func TestGetRetriesOn5xxThenSucceeds(t *testing.T) {
	t.Parallel()

	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if atomic.AddInt32(&calls, 1) <= tmdb5xxRetries {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	t.Cleanup(srv.Close)

	c := New("k")
	c.apiRoot = srv.URL
	body, err := c.get(context.Background(), "movie/1", nil)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if string(body) != `{"ok":true}` {
		t.Fatalf("body = %q", body)
	}
	if got := atomic.LoadInt32(&calls); got != tmdb5xxRetries+1 {
		t.Fatalf("calls = %d, want %d", got, tmdb5xxRetries+1)
	}
}

func TestGetGivesUpAfterRepeated5xx(t *testing.T) {
	t.Parallel()

	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)

	c := New("k")
	c.apiRoot = srv.URL
	if _, err := c.get(context.Background(), "movie/1", nil); err == nil {
		t.Fatal("get: want error after exhausting retries")
	}
	if got := atomic.LoadInt32(&calls); got != tmdb5xxRetries+1 {
		t.Fatalf("calls = %d, want %d", got, tmdb5xxRetries+1)
	}
}

func TestGetDoesNotRetryOn4xx(t *testing.T) {
	t.Parallel()

	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.WriteHeader(http.StatusUnauthorized)
	}))
	t.Cleanup(srv.Close)

	c := New("k")
	c.apiRoot = srv.URL
	if _, err := c.get(context.Background(), "movie/1", nil); err == nil {
		t.Fatal("get: want error on 401")
	}
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Fatalf("calls = %d, want 1 (no retry on 4xx)", got)
	}
}
