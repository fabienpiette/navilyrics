package lrclib_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/user/navilyrics/pkg/lrclib"
)

func TestGet_found(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/get" {
			http.NotFound(w, r)
			return
		}
		json.NewEncoder(w).Encode(lrclib.Response{
			TrackName:    "Bohemian Rhapsody",
			ArtistName:   "Queen",
			PlainLyrics:  "Is this the real life?",
			SyncedLyrics: "[00:01.00] Is this the real life?",
		})
	}))
	defer srv.Close()

	c := lrclib.New(srv.URL)
	got, ok, err := c.Get(context.Background(), "Queen", "Bohemian Rhapsody", "A Night at the Opera", 354)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("want found, got not found")
	}
	if got.PlainLyrics != "Is this the real life?" {
		t.Errorf("unexpected plain lyrics: %q", got.PlainLyrics)
	}
	if got.SyncedLyrics != "[00:01.00] Is this the real life?" {
		t.Errorf("unexpected synced lyrics: %q", got.SyncedLyrics)
	}
}

func TestGet_notFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer srv.Close()

	c := lrclib.New(srv.URL)
	_, ok, err := c.Get(context.Background(), "Unknown", "Unknown", "", 0)
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatal("want not found")
	}
}

func TestSearch_picksBestDurationMatch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/search" {
			http.NotFound(w, r)
			return
		}
		json.NewEncoder(w).Encode([]lrclib.Response{
			{TrackName: "Song", ArtistName: "Artist", Duration: 300, PlainLyrics: "wrong"},
			{TrackName: "Song", ArtistName: "Artist", Duration: 182, PlainLyrics: "correct"},
		})
	}))
	defer srv.Close()

	c := lrclib.New(srv.URL)
	got, ok, err := c.Search(context.Background(), "Artist", "Song", 180)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("want found")
	}
	if got.PlainLyrics != "correct" {
		t.Errorf("unexpected lyrics: %q", got.PlainLyrics)
	}
}

func TestSearch_noMatchWithinTolerance(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode([]lrclib.Response{
			{Duration: 300, PlainLyrics: "way off"},
		})
	}))
	defer srv.Close()

	c := lrclib.New(srv.URL)
	_, ok, err := c.Search(context.Background(), "Artist", "Song", 180)
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatal("want not found — duration delta >5s")
	}
}
