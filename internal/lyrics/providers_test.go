package lyrics_test

import (
	"context"
	"testing"

	"github.com/user/navilyrics/internal/lyrics"
	"github.com/user/navilyrics/pkg/navidrome"
)

// stubProvider implements Provider for testing.
type stubProvider struct {
	name   string
	result lyrics.ProviderResult
	ok     bool
	calls  int
}

func (s *stubProvider) Name() string { return s.name }
func (s *stubProvider) Search(_ context.Context, _, _, _ string, _ float64) (lyrics.ProviderResult, bool, error) {
	s.calls++
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
