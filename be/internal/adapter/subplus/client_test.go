package subplus

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func writeBody(t *testing.T, w http.ResponseWriter, body string) {
	t.Helper()
	if _, err := io.WriteString(w, body); err != nil {
		t.Errorf("write response: %v", err)
	}
}

func parseForm(t *testing.T, r *http.Request) {
	t.Helper()
	if err := r.ParseForm(); err != nil {
		t.Fatalf("parse form: %v", err)
	}
}

func newTestClient(t *testing.T, handler http.HandlerFunc) *Client {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	c := New()
	c.apiRoot = srv.URL
	return c
}

const searchBody = `{"ok":true,"count":2,"result":[
 {"title":"News of the World (2020)","tag":"TAG-A","series":false,
  "comment":"By\nmeisam_t72\n zirnevis","info":"News.Of.The.World.2020.1080p.BluRay.x264-YTS\nNews.of.the.World.2020.720p.BluRay-RARBG",
  "pic":"https://isubcdn.com/x.jpg","imdb":"tt6878306","year":"2020"},
 {"title":"News of the World (2020)","tag":"","series":false,"comment":"","info":"","imdb":"tt6878306","year":"2020"}
]}`

func TestSearchParsesPacks(t *testing.T) {
	t.Parallel()
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", r.Method)
		}
		parseForm(t, r)
		if got := r.PostForm.Get("q"); got != "tt6878306" {
			t.Errorf("q = %q", got)
		}
		if got := r.PostForm.Get("l"); got != "persian" {
			t.Errorf("l = %q", got)
		}
		writeBody(t, w, searchBody)
	})

	packs, err := c.Search(context.Background(), "tt6878306", DefaultLang)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(packs) != 1 { // the tag-less second row is dropped
		t.Fatalf("got %d packs, want 1: %+v", len(packs), packs)
	}
	p := packs[0]
	if p.Tag != "TAG-A" || p.IMDb != "tt6878306" || p.Year != "2020" || p.Series {
		t.Fatalf("pack fields %+v", p)
	}
	if len(p.Releases) != 2 || p.Releases[0] != "News.Of.The.World.2020.1080p.BluRay.x264-YTS" {
		t.Fatalf("releases %+v", p.Releases)
	}
	if p.Translator == "" {
		t.Fatalf("translator not carried")
	}
}

func TestSearchUpstreamError(t *testing.T) {
	t.Parallel()
	c := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		writeBody(t, w, `{"ok":false,"description":"Bad Request: Parameter not found"}`)
	})
	_, err := c.Search(context.Background(), "tt1", "")
	if err == nil || !strings.Contains(err.Error(), "Parameter not found") {
		t.Fatalf("err = %v, want upstream description", err)
	}
}

func TestSearchEmptyQueryNoCall(t *testing.T) {
	t.Parallel()
	c := newTestClient(t, func(_ http.ResponseWriter, _ *http.Request) {
		t.Fatal("must not hit the network for an empty query")
	})
	packs, err := c.Search(context.Background(), "   ", "")
	if err != nil || packs != nil {
		t.Fatalf("got (%v, %v)", packs, err)
	}
}

func TestDownloadURL(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		resp string
		want string
		ok   bool
	}{
		{"good", `{"ok":true,"download":"http://sub-plus.ir/download/news-2466179.zip"}`, "http://sub-plus.ir/download/news-2466179.zip", true},
		{"leaked path bug", `{"ok":true,"download":"http://sub-plus.ir//home2/subplusi/public_html/modules/temp/-.zip"}`, "", false},
		{"not a zip", `{"ok":true,"download":"http://sub-plus.ir/download/index.html"}`, "", false},
		{"upstream error", `{"ok":false,"description":"nope"}`, "", false},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
				parseForm(t, r)
				if r.PostForm.Get("dl") != "TAG-A" {
					t.Errorf("dl = %q", r.PostForm.Get("dl"))
				}
				writeBody(t, w, tc.resp)
			})
			got, err := c.DownloadURL(context.Background(), "TAG-A")
			if tc.ok && (err != nil || got != tc.want) {
				t.Fatalf("got (%q, %v), want (%q, nil)", got, err, tc.want)
			}
			if !tc.ok && err == nil {
				t.Fatalf("want error, got %q", got)
			}
		})
	}
}

func TestIMDbQuery(t *testing.T) {
	t.Parallel()
	cases := []struct{ in, want string }{
		{"tt6878306", "tt6878306"},
		{"6878306", "tt6878306"},
		{" TT1160419 ", "tt1160419"}, // trims and case-folds
		{"dune", ""},
		{"", ""},
		{"tt", ""},
		{"tt12a", ""},
	}
	for _, tc := range cases {
		if got := IMDbQuery(tc.in); got != tc.want {
			t.Errorf("IMDbQuery(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
