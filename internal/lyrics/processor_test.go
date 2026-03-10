package lyrics_test

import (
	"context"
	"testing"

	"github.com/user/navilyrics/internal/lyrics"
	"github.com/user/navilyrics/pkg/lrclib"
	"github.com/user/navilyrics/pkg/navidrome"
)

type stubLRCLib struct {
	response lrclib.Response
	found    bool
}

func (s *stubLRCLib) Get(_ context.Context, _, _, _ string, _ float64) (lrclib.Response, bool, error) {
	return s.response, s.found, nil
}

func (s *stubLRCLib) Search(_ context.Context, _, _ string, _ float64) (lrclib.Response, bool, error) {
	return s.response, s.found, nil
}

func TestProcessor_skipsHasLyrics(t *testing.T) {
	p := lyrics.NewProcessor(nil, &stubLRCLib{found: true}, "/music", true)
	song := navidrome.Song{ID: "1", HasLyrics: true, Path: "/music/song.mp3"}
	result := p.ProcessSong(context.Background(), song)
	if result.Status != "skipped" {
		t.Errorf("want skipped, got %q", result.Status)
	}
}

func TestProcessor_dryRunWhenFound(t *testing.T) {
	stub := &stubLRCLib{
		found: true,
		response: lrclib.Response{
			PlainLyrics:  "Line one",
			SyncedLyrics: "[00:01.00] Line one",
		},
	}
	p := lyrics.NewProcessor(nil, stub, "/music", true)
	song := navidrome.Song{ID: "2", Title: "Song", Artist: "Artist", HasLyrics: false, Path: "/music/song.mp3"}
	result := p.ProcessSong(context.Background(), song)
	if result.Status != "dry_run" {
		t.Errorf("want dry_run, got %q", result.Status)
	}
	if result.PlainLyrics != "Line one" {
		t.Errorf("unexpected lyrics: %q", result.PlainLyrics)
	}
	if result.Source != "lrclib" {
		t.Errorf("want source=lrclib, got %q", result.Source)
	}
}

func TestProcessor_notFound(t *testing.T) {
	p := lyrics.NewProcessor(nil, &stubLRCLib{found: false}, "/music", false)
	song := navidrome.Song{ID: "3", HasLyrics: false, Path: "/music/song.flac"}
	result := p.ProcessSong(context.Background(), song)
	if result.Status != "not_found" {
		t.Errorf("want not_found, got %q", result.Status)
	}
}
