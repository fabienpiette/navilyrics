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
				json.NewEncoder(w).Encode([]navidrome.Song{
					{ID: "1", Title: "Song A", HasLyrics: false},
					{ID: "2", Title: "Song B", HasLyrics: true},
				})
			} else {
				json.NewEncoder(w).Encode([]navidrome.Song{})
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
