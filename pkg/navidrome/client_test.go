package navidrome_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/user/navilyrics/pkg/navidrome"
)

func TestAuthenticate(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/auth/login" {
			http.NotFound(w, r)
			return
		}
		json.NewEncoder(w).Encode(map[string]string{"token": "test-jwt"})
	}))
	defer srv.Close()

	c := navidrome.New(srv.URL, "user", "pass")
	if err := c.Authenticate(); err != nil {
		t.Fatalf("Authenticate: %v", err)
	}
}

func TestAllSongs(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/auth/login":
			json.NewEncoder(w).Encode(map[string]string{"token": "tok"})
		case "/api/song":
			start := r.URL.Query().Get("_start")
			if start == "0" {
				// lyrics:[] → HasLyrics=false; non-empty lyrics array → HasLyrics=true
				w.Write([]byte(`[
					{"id":"1","title":"Song A","lyrics":[]},
					{"id":"2","title":"Song B","lyrics":[{"lang":"xxx","value":"line"}]}
				]`))
			} else {
				w.Write([]byte(`[]`))
			}
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	c := navidrome.New(srv.URL, "user", "pass")
	_ = c.Authenticate()
	songs, err := c.AllSongs(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(songs) != 2 {
		t.Fatalf("want 2 songs, got %d", len(songs))
	}
	if songs[0].HasLyrics {
		t.Error("Song A should have HasLyrics=false")
	}
	if !songs[1].HasLyrics {
		t.Error("Song B should have HasLyrics=true")
	}
}

func TestListSongs(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/auth/login":
			json.NewEncoder(w).Encode(map[string]string{"token": "tok"})
		case "/api/song":
			q := r.URL.Query()
			if q.Get("_sort") != "artist" {
				t.Errorf("want _sort=artist, got %q", q.Get("_sort"))
			}
			if q.Get("_order") != "DESC" {
				t.Errorf("want _order=DESC, got %q", q.Get("_order"))
			}
			if q.Get("_start") != "0" {
				t.Errorf("want _start=0, got %q", q.Get("_start"))
			}
			if q.Get("_end") != "50" {
				t.Errorf("want _end=50, got %q", q.Get("_end"))
			}
			w.Write([]byte(`[{"id":"1","title":"T","lyrics":[]}]`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	c := navidrome.New(srv.URL, "u", "p")
	_ = c.Authenticate()
	songs, err := c.ListSongs(context.Background(), navidrome.SongQuery{
		Sort:  "artist",
		Dir:   "DESC",
		Limit: 50,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(songs) != 1 {
		t.Fatalf("want 1 song, got %d", len(songs))
	}
}
