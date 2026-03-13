package lyrics_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/user/navilyrics/internal/lyrics"
	"github.com/user/navilyrics/pkg/navidrome"
)

func TestProcessor_skipsHasLyrics(t *testing.T) {
	p := lyrics.NewProcessor(nil, []lyrics.Provider{&stubProvider{name: "lrclib", ok: true}}, []string{"/music"}, true)
	song := navidrome.Song{ID: "1", HasLyrics: true, Path: "/music/song.mp3"}
	result := p.ProcessSong(context.Background(), song)
	if result.Status != "skipped" {
		t.Errorf("want skipped, got %q", result.Status)
	}
}

func TestProcessor_dryRunWhenFound(t *testing.T) {
	stub := &stubProvider{
		name:   "lrclib",
		ok:     true,
		result: lyrics.ProviderResult{PlainLyrics: "Line one", SyncedLyrics: "[00:01.00] Line one"},
	}
	p := lyrics.NewProcessor(nil, []lyrics.Provider{stub}, []string{"/music"}, true)
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
	p := lyrics.NewProcessor(nil, []lyrics.Provider{&stubProvider{name: "lrclib", ok: false}}, []string{"/music"}, false)
	song := navidrome.Song{ID: "3", HasLyrics: false, Path: "/music/song.flac"}
	result := p.ProcessSong(context.Background(), song)
	if result.Status != "not_found" {
		t.Errorf("want not_found, got %q", result.Status)
	}
}

func TestProcessor_secondProviderUsedWhenFirstMisses(t *testing.T) {
	primary := &stubProvider{name: "lrclib", ok: false}
	secondary := &stubProvider{
		name:   "netease",
		ok:     true,
		result: lyrics.ProviderResult{PlainLyrics: "fallback", SyncedLyrics: "[00:01.00] fallback"},
	}
	proc := lyrics.NewProcessor(nil, []lyrics.Provider{primary, secondary}, []string{"/music"}, true)
	song := navidrome.Song{Title: "T", Artist: "A", Duration: 200}
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
	proc := lyrics.NewProcessor(nil, nil, []string{dir}, false)
	got := proc.ResolveLRCPath("song.mp3")
	want := filepath.Join(dir, "song.lrc")
	if got != want {
		t.Errorf("want %q, got %q", want, got)
	}
}

func TestProcessor_ResolveLRCPath_notFound(t *testing.T) {
	proc := lyrics.NewProcessor(nil, nil, []string{"/nonexistent"}, false)
	if got := proc.ResolveLRCPath("song.mp3"); got != "" {
		t.Errorf("want empty, got %q", got)
	}
}
