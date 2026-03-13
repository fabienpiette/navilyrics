package lyrics_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/user/navilyrics/internal/lyrics"
	"github.com/user/navilyrics/pkg/genius"
	"github.com/user/navilyrics/pkg/lrclib"
	"github.com/user/navilyrics/pkg/navidrome"
	"github.com/user/navilyrics/pkg/netease"
)

// stubProvider implements Provider for testing.
type stubProvider struct {
	name   string
	result lyrics.ProviderResult
	ok     bool
	mu     sync.Mutex
	calls  int
}

func (s *stubProvider) Name() string { return s.name }
func (s *stubProvider) Search(_ context.Context, _, _, _ string, _ float64) (lyrics.ProviderResult, bool, error) {
	s.mu.Lock()
	s.calls++
	s.mu.Unlock()
	return s.result, s.ok, nil
}

func TestProviderResult_fields(t *testing.T) {
	pr := lyrics.ProviderResult{
		PlainLyrics:  "plain",
		SyncedLyrics: "[00:01.00] synced",
		Instrumental: true,
	}
	if pr.PlainLyrics != "plain" {
		t.Errorf("PlainLyrics = %q", pr.PlainLyrics)
	}
	if !pr.Instrumental {
		t.Error("Instrumental should be true")
	}
}

func TestFetchLyricsOnly_filterByProvider(t *testing.T) {
	a := &stubProvider{name: "a", result: lyrics.ProviderResult{PlainLyrics: "from a"}, ok: true}
	b := &stubProvider{name: "b", result: lyrics.ProviderResult{PlainLyrics: "from b"}, ok: true}

	proc := lyrics.NewProcessor(nil, []lyrics.Provider{a, b}, nil, false)

	// Filter to "b" only — "a" must not be called.
	song := navidrome.Song{ID: "1", Title: "T", Artist: "A"}
	result := proc.FetchLyricsOnly(context.Background(), song, []string{"b"})
	if result.Status != "found" {
		t.Fatalf("want found, got %q", result.Status)
	}
	if result.PlainLyrics != "from b" {
		t.Errorf("PlainLyrics = %q, want %q", result.PlainLyrics, "from b")
	}
	if a.calls != 0 {
		t.Errorf("provider a should not have been called, got %d calls", a.calls)
	}
}

func TestFetchLyricsOnly_unknownFilterReturnsNotFound(t *testing.T) {
	a := &stubProvider{name: "a", ok: true}
	proc := lyrics.NewProcessor(nil, []lyrics.Provider{a}, nil, false)

	song := navidrome.Song{ID: "1", Title: "T", Artist: "A"}
	result := proc.FetchLyricsOnly(context.Background(), song, []string{"nonexistent"})
	if result.Status != "not_found" {
		t.Fatalf("want not_found, got %q", result.Status)
	}
	if a.calls != 0 {
		t.Errorf("provider a should not have been called, got %d calls", a.calls)
	}
}

func TestFetchLyricsOnly_nilFilterUsesAll(t *testing.T) {
	a := &stubProvider{name: "a", ok: false}
	b := &stubProvider{name: "b", result: lyrics.ProviderResult{PlainLyrics: "from b"}, ok: true}
	proc := lyrics.NewProcessor(nil, []lyrics.Provider{a, b}, nil, false)

	song := navidrome.Song{ID: "1", Title: "T", Artist: "A"}
	result := proc.FetchLyricsOnly(context.Background(), song, nil)
	if result.Status != "found" {
		t.Fatalf("want found, got %q", result.Status)
	}
	if a.calls != 1 {
		t.Errorf("provider a should have been called once, got %d", a.calls)
	}
}

// --- Adapter tests: each adapter drives a real pkg client via httptest server ---

func TestLRCLibProvider_foundViaGet(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/get" {
			json.NewEncoder(w).Encode(lrclib.Response{
				PlainLyrics:  "plain",
				SyncedLyrics: "[00:01.00] plain",
				Instrumental: false,
			})
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	p := lyrics.NewLRCLibProvider(lrclib.New(srv.URL))
	if p.Name() != "lrclib" {
		t.Errorf("Name = %q, want lrclib", p.Name())
	}
	res, ok, err := p.Search(context.Background(), "Artist", "Song", "Album", 200)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("want found")
	}
	if res.PlainLyrics != "plain" {
		t.Errorf("PlainLyrics = %q", res.PlainLyrics)
	}
	if res.SyncedLyrics != "[00:01.00] plain" {
		t.Errorf("SyncedLyrics = %q", res.SyncedLyrics)
	}
}

func TestLRCLibProvider_fallsBackToFuzzySearch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/get":
			http.NotFound(w, r) // exact get misses
		case "/api/search":
			json.NewEncoder(w).Encode([]lrclib.Response{
				{PlainLyrics: "fuzzy", SyncedLyrics: "[00:01.00] fuzzy", Duration: 200},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	p := lyrics.NewLRCLibProvider(lrclib.New(srv.URL))
	res, ok, err := p.Search(context.Background(), "Artist", "Song", "", 200)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("want found via fuzzy search")
	}
	if res.PlainLyrics != "fuzzy" {
		t.Errorf("PlainLyrics = %q, want fuzzy", res.PlainLyrics)
	}
}

func TestLRCLibProvider_notFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/get":
			http.NotFound(w, r)
		case "/api/search":
			json.NewEncoder(w).Encode([]lrclib.Response{}) // empty
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	p := lyrics.NewLRCLibProvider(lrclib.New(srv.URL))
	_, ok, err := p.Search(context.Background(), "Artist", "Song", "", 200)
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatal("want not found")
	}
}

func TestNetEaseProvider_found(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/search/get":
			json.NewEncoder(w).Encode(map[string]any{
				"result": map[string]any{
					"songs": []map[string]any{{"id": 1, "duration": 200000}},
				},
			})
		case "/api/song/lyric":
			json.NewEncoder(w).Encode(map[string]any{
				"lrc": map[string]any{"lyric": "[00:01.00] Hello"},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	p := lyrics.NewNetEaseProvider(netease.New(srv.URL))
	if p.Name() != "netease" {
		t.Errorf("Name = %q, want netease", p.Name())
	}
	res, ok, err := p.Search(context.Background(), "Artist", "Song", "", 200)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("want found")
	}
	if res.SyncedLyrics != "[00:01.00] Hello" {
		t.Errorf("SyncedLyrics = %q", res.SyncedLyrics)
	}
	if res.PlainLyrics != "Hello" {
		t.Errorf("PlainLyrics = %q", res.PlainLyrics)
	}
}

func TestNetEaseProvider_notFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"result": map[string]any{"songs": []any{}}})
	}))
	defer srv.Close()

	p := lyrics.NewNetEaseProvider(netease.New(srv.URL))
	_, ok, err := p.Search(context.Background(), "Artist", "Song", "", 200)
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatal("want not found")
	}
}

func TestGeniusProvider_found(t *testing.T) {
	mux := http.NewServeMux()
	var srv *httptest.Server
	srv = httptest.NewServer(mux)
	mux.HandleFunc("/search", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `{"response":{"hits":[{"result":{"title":"Song","url":"%s/song","primary_artist":{"name":"Artist"}}}]}}`, srv.URL)
	})
	mux.HandleFunc("/song", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `<div data-lyrics-container="true">Verse 1<br/>Verse 2</div>`)
	})
	defer srv.Close()

	p := lyrics.NewGeniusProvider(genius.New("token", srv.URL))
	if p.Name() != "genius" {
		t.Errorf("Name = %q, want genius", p.Name())
	}
	res, ok, err := p.Search(context.Background(), "Artist", "Song", "", 0)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("want found")
	}
	if res.PlainLyrics != "Verse 1\nVerse 2" {
		t.Errorf("PlainLyrics = %q", res.PlainLyrics)
	}
	if res.SyncedLyrics != "" {
		t.Errorf("SyncedLyrics should be empty, got %q", res.SyncedLyrics)
	}
}

func TestGeniusProvider_notFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"response":{"hits":[]}}`)
	}))
	defer srv.Close()

	p := lyrics.NewGeniusProvider(genius.New("token", srv.URL))
	_, ok, err := p.Search(context.Background(), "Artist", "Song", "", 0)
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatal("want not found")
	}
}
