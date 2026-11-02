package tmdb

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestTVFindByAirDatePrefersGuestOnAmbiguousDay(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/tv/59941":
			_, _ = w.Write([]byte(`{"seasons":[
				{"season_number":7,"air_date":"2019-01-02"},
				{"season_number":8,"air_date":"2020-01-06"}
			]}`))
		case "/tv/59941/season/8":
			_, _ = w.Write([]byte(`{"season_number":8,"episodes":[
				{"episode_number":86,"air_date":"2021-02-08","name":"Someone Else","overview":""},
				{"episode_number":87,"air_date":"2021-02-09","name":"Priyanka Chopra Jonas","overview":"Priyanka Chopra Jonas, guest host."},
				{"episode_number":200,"air_date":"2021-09-02","name":"Other Guest","overview":""}
			]}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)

	c := New("k")
	c.apiRoot = srv.URL
	s, e, ok, err := c.TVFindByAirDate(context.Background(), 59941,
		[]string{"2021-02-09", "2021-09-02"}, "Priyanka Chopra")
	if err != nil || !ok {
		t.Fatalf("ok=%v err=%v", ok, err)
	}
	if s != 8 || e != 87 {
		t.Fatalf("S%dE%d, want S08E87 (Feb 9 + Priyanka, not Sep 2)", s, e)
	}
}

func TestTVFindByAirDateNoHit(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/tv/59941":
			_, _ = w.Write([]byte(`{"seasons":[{"season_number":8,"air_date":"2020-01-06"}]}`))
		case "/tv/59941/season/8":
			_, _ = w.Write([]byte(`{"season_number":8,"episodes":[{"episode_number":1,"air_date":"2020-01-06","name":"x"}]}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	c := New("k")
	c.apiRoot = srv.URL
	_, _, ok, err := c.TVFindByAirDate(context.Background(), 59941, []string{"2021-02-09"}, "")
	if err != nil || ok {
		t.Fatalf("ok=%v err=%v, want miss", ok, err)
	}
}
