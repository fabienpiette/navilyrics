package lyrics_test

import (
	"context"
	"os"
	"path/filepath"
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
	p := lyrics.NewProcessor(nil, &stubLRCLib{found: true}, []string{"/music"}, true)
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
	p := lyrics.NewProcessor(nil, stub, []string{"/music"}, true)
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
	p := lyrics.NewProcessor(nil, &stubLRCLib{found: false}, []string{"/music"}, false)
	song := navidrome.Song{ID: "3", HasLyrics: false, Path: "/music/song.flac"}
	result := p.ProcessSong(context.Background(), song)
	if result.Status != "not_found" {
		t.Errorf("want not_found, got %q", result.Status)
	}
}

type stubFetcher struct {
	ok   bool
	resp lrclib.Response
}

func (s *stubFetcher) Get(_ context.Context, _, _, _ string, _ float64) (lrclib.Response, bool, error) {
	return s.resp, s.ok, nil
}

func (s *stubFetcher) Search(_ context.Context, _, _ string, _ float64) (lrclib.Response, bool, error) {
	return s.resp, s.ok, nil
}

func TestProcessor_fallbackUsedWhenLrclibMisses(t *testing.T) {
	primary := &stubFetcher{ok: false}
	fallback := &stubFetcher{ok: true, resp: lrclib.Response{PlainLyrics: "fallback", SyncedLyrics: "[00:01.00] fallback"}}

	proc := lyrics.NewProcessor(nil, primary, []string{"/music"}, true)
	proc.SetFallback(fallback)

	song := navidrome.Song{Title: "T", Artist: "A", Album: "L", Duration: 200}
	r := proc.ProcessSong(context.Background(), song)
	if r.Status != "dry_run" {
		t.Fatalf("want dry_run, got %s", r.Status)
	}
	if r.Source != "netease" {
		t.Errorf("want source=netease, got %q", r.Source)
	}
}

func TestProcessor_ResolveLRCPath(t *testing.T) {
	dir := t.TempDir()
	audioPath := filepath.Join(dir, "song.mp3")
	if err := os.WriteFile(audioPath, []byte{}, 0644); err != nil {
		t.Fatal(err)
	}

	proc := lyrics.NewProcessor(nil, &stubFetcher{}, []string{dir}, false)
	got := proc.ResolveLRCPath("song.mp3")
	want := filepath.Join(dir, "song.lrc")
	if got != want {
		t.Errorf("want %q, got %q", want, got)
	}
}

func TestProcessor_ResolveLRCPath_notFound(t *testing.T) {
	proc := lyrics.NewProcessor(nil, &stubFetcher{}, []string{"/nonexistent"}, false)
	if got := proc.ResolveLRCPath("song.mp3"); got != "" {
		t.Errorf("want empty, got %q", got)
	}
}
