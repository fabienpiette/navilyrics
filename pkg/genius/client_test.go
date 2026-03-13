package genius_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/user/navilyrics/pkg/genius"
)

// newMux returns a test server where /search returns Genius API JSON
// pointing to /song, and /song returns a Genius-style lyrics page.
func newMux(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	var srv *httptest.Server
	srv = httptest.NewServer(mux)
	mux.HandleFunc("/search", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `{"response":{"hits":[{"result":{"title":"Song","url":"%s/song","primary_artist":{"name":"Artist"}}}]}}`, srv.URL)
	})
	mux.HandleFunc("/song", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `<div data-lyrics-container="true">Line 1<br/>Line 2</div>`)
	})
	return srv
}

func TestSearch_found(t *testing.T) {
	srv := newMux(t)
	defer srv.Close()

	c := genius.New("token", srv.URL)
	resp, ok, err := c.Search(context.Background(), "Artist", "Song")
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("want found, got not found")
	}
	if resp.PlainLyrics != "Line 1\nLine 2" {
		t.Errorf("PlainLyrics = %q", resp.PlainLyrics)
	}
	if resp.SyncedLyrics != "" {
		t.Errorf("SyncedLyrics should always be empty, got %q", resp.SyncedLyrics)
	}
}

func TestSearch_noArtistMatch_notFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Hit exists but primary_artist doesn't match
		fmt.Fprint(w, `{"response":{"hits":[{"result":{"title":"Song","url":"http://x/song","primary_artist":{"name":"Somebody Else"}}}]}}`)
	}))
	defer srv.Close()

	c := genius.New("token", srv.URL)
	_, ok, err := c.Search(context.Background(), "Artist", "Song")
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatal("want not found when artist doesn't match")
	}
}

func TestSearch_noHits_notFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"response":{"hits":[]}}`)
	}))
	defer srv.Close()

	c := genius.New("token", srv.URL)
	_, ok, err := c.Search(context.Background(), "Artist", "Song")
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatal("want not found")
	}
}

func TestSearch_noLyricsContainer_notFound(t *testing.T) {
	mux := http.NewServeMux()
	var srv *httptest.Server
	srv = httptest.NewServer(mux)
	mux.HandleFunc("/search", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `{"response":{"hits":[{"result":{"title":"Song","url":"%s/song","primary_artist":{"name":"Artist"}}}]}}`, srv.URL)
	})
	mux.HandleFunc("/song", func(w http.ResponseWriter, r *http.Request) {
		// No data-lyrics-container div
		fmt.Fprint(w, `<html><body><p>No lyrics here</p></body></html>`)
	})
	defer srv.Close()

	c := genius.New("token", srv.URL)
	_, ok, err := c.Search(context.Background(), "Artist", "Song")
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatal("want not found when no lyrics container found")
	}
}

func TestSearch_nestedDivs_extractsFullLyrics(t *testing.T) {
	// Real Genius pages wrap section headers in inner <div> elements.
	// extractLyrics must handle these without truncating.
	mux := http.NewServeMux()
	var srv *httptest.Server
	srv = httptest.NewServer(mux)
	mux.HandleFunc("/search", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `{"response":{"hits":[{"result":{"title":"Song","url":"%s/song","primary_artist":{"name":"Artist"}}}]}}`, srv.URL)
	})
	mux.HandleFunc("/song", func(w http.ResponseWriter, r *http.Request) {
		// Lyrics container with a nested section-header div
		fmt.Fprint(w, `<div data-lyrics-container="true"><div class="section">Verse 1</div>Line 1<br/>Line 2</div>`)
	})
	defer srv.Close()

	c := genius.New("token", srv.URL)
	resp, ok, err := c.Search(context.Background(), "Artist", "Song")
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("want found")
	}
	// Should contain content from both inside and outside the nested div
	if resp.PlainLyrics == "" {
		t.Fatal("want non-empty PlainLyrics")
	}
	if !strings.Contains(resp.PlainLyrics, "Line 1") {
		t.Errorf("expected Line 1 in lyrics, got %q", resp.PlainLyrics)
	}
}
