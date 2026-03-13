package netease_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/user/navilyrics/pkg/netease"
)

func TestSearch_found(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/search/get":
			json.NewEncoder(w).Encode(map[string]any{
				"result": map[string]any{
					"songs": []map[string]any{
						{"id": 123, "duration": 210000},
					},
				},
			})
		case "/api/song/lyric":
			json.NewEncoder(w).Encode(map[string]any{
				"lrc": map[string]any{"lyric": "[00:01.00] Hello world"},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	c := netease.New(srv.URL)
	resp, ok, err := c.Search(context.Background(), "Artist", "Song", 210.0)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("want found, got not found")
	}
	if resp.SyncedLyrics != "[00:01.00] Hello world" {
		t.Errorf("SyncedLyrics = %q", resp.SyncedLyrics)
	}
	if resp.PlainLyrics != "Hello world" {
		t.Errorf("PlainLyrics = %q", resp.PlainLyrics)
	}
}

func TestSearch_durationMismatch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/search/get":
			json.NewEncoder(w).Encode(map[string]any{
				"result": map[string]any{
					"songs": []map[string]any{
						// duration 300s, target 210s — delta 90s > 5s tolerance
						{"id": 123, "duration": 300000},
					},
				},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	c := netease.New(srv.URL)
	_, ok, err := c.Search(context.Background(), "Artist", "Song", 210.0)
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatal("want not found due to duration mismatch")
	}
}

func TestSearch_picksClosestDuration(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/search/get":
			json.NewEncoder(w).Encode(map[string]any{
				"result": map[string]any{
					"songs": []map[string]any{
						{"id": 1, "duration": 215000}, // delta 5s — within tolerance
						{"id": 2, "duration": 212000}, // delta 2s — closer, wins
					},
				},
			})
		case "/api/song/lyric":
			// verify id=2 was selected
			if r.URL.Query().Get("id") != "2" {
				http.Error(w, "wrong id", http.StatusBadRequest)
				return
			}
			json.NewEncoder(w).Encode(map[string]any{
				"lrc": map[string]any{"lyric": "[00:01.00] Close match"},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	c := netease.New(srv.URL)
	resp, ok, err := c.Search(context.Background(), "Artist", "Song", 210.0)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("want found")
	}
	if resp.SyncedLyrics != "[00:01.00] Close match" {
		t.Errorf("got %q", resp.SyncedLyrics)
	}
}

func TestSearch_emptyLyrics_notFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/search/get":
			json.NewEncoder(w).Encode(map[string]any{
				"result": map[string]any{
					"songs": []map[string]any{{"id": 1, "duration": 210000}},
				},
			})
		case "/api/song/lyric":
			json.NewEncoder(w).Encode(map[string]any{
				"lrc": map[string]any{"lyric": ""},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	c := netease.New(srv.URL)
	_, ok, err := c.Search(context.Background(), "Artist", "Song", 210.0)
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatal("want not found when lyrics are empty")
	}
}
