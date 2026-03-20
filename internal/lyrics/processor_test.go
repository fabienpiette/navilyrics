package lyrics_test

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/user/navilyrics/internal/lyrics"
	"github.com/user/navilyrics/pkg/navidrome"
)

// minimalMP3Bytes returns a minimal valid ID3v2+MPEG frame.
func minimalMP3Bytes() []byte {
	data := []byte{
		0x49, 0x44, 0x33, 0x03, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
		0xFF, 0xFB, 0x90, 0x00,
	}
	return append(data, make([]byte, 417-len(data))...)
}

func TestProcessor_skipsHasLyrics(t *testing.T) {
	p := lyrics.NewProcessor(nil, []lyrics.Provider{&stubProvider{name: "lrclib", ok: true}}, nil, []string{"/music"}, true)
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
	p := lyrics.NewProcessor(nil, []lyrics.Provider{stub}, nil, []string{"/music"}, true)
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
	p := lyrics.NewProcessor(nil, []lyrics.Provider{&stubProvider{name: "lrclib", ok: false}}, nil, []string{"/music"}, false)
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
	proc := lyrics.NewProcessor(nil, []lyrics.Provider{primary, secondary}, nil, []string{"/music"}, true)
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
	proc := lyrics.NewProcessor(nil, nil, nil, []string{dir}, false)
	got := proc.ResolveLRCPath("song.mp3")
	want := filepath.Join(dir, "song.lrc")
	if got != want {
		t.Errorf("want %q, got %q", want, got)
	}
}

func TestProcessor_ResolveLRCPath_notFound(t *testing.T) {
	proc := lyrics.NewProcessor(nil, nil, nil, []string{"/nonexistent"}, false)
	if got := proc.ResolveLRCPath("song.mp3"); got != "" {
		t.Errorf("want empty, got %q", got)
	}
}

func TestProcessor_instrumentalResult(t *testing.T) {
	stub := &stubProvider{
		name:   "lrclib",
		ok:     true,
		result: lyrics.ProviderResult{Instrumental: true},
	}
	// dryRun=true so markInstrumental is not called (no audio file needed)
	proc := lyrics.NewProcessor(nil, []lyrics.Provider{stub}, nil, nil, true)
	song := navidrome.Song{ID: "4", Title: "Ambient", Artist: "Artist"}
	r := proc.ProcessSong(context.Background(), song)
	if r.Status != "instrumental" {
		t.Errorf("want instrumental, got %q", r.Status)
	}
	if !r.Instrumental {
		t.Error("want Instrumental=true")
	}
}

func TestProcessor_saveError(t *testing.T) {
	stub := &stubProvider{
		name:   "lrclib",
		ok:     true,
		result: lyrics.ProviderResult{PlainLyrics: "plain", SyncedLyrics: "[00:01.00] plain"},
	}
	// Empty musicDirs → resolveAudioPath always returns "" → SaveLyrics errors
	proc := lyrics.NewProcessor(nil, []lyrics.Provider{stub}, nil, nil, false)
	song := navidrome.Song{ID: "5", Title: "Song", Artist: "Artist", Path: "song.mp3"}
	r := proc.ProcessSong(context.Background(), song)
	if r.Status != "error" {
		t.Errorf("want error, got %q", r.Status)
	}
	if r.Err == "" {
		t.Error("want non-empty Err")
	}
}

func TestProcessor_savesLyrics(t *testing.T) {
	dir := t.TempDir()
	audioPath := filepath.Join(dir, "song.mp3")
	if err := os.WriteFile(audioPath, minimalMP3Bytes(), 0644); err != nil {
		t.Fatal(err)
	}
	stub := &stubProvider{
		name:   "lrclib",
		ok:     true,
		result: lyrics.ProviderResult{PlainLyrics: "plain", SyncedLyrics: "[00:01.00] plain"},
	}
	proc := lyrics.NewProcessor(nil, []lyrics.Provider{stub}, nil, []string{dir}, false)
	song := navidrome.Song{ID: "6", Title: "Song", Artist: "Artist", Path: "song.mp3"}
	r := proc.ProcessSong(context.Background(), song)
	if r.Status != "found" {
		t.Fatalf("want found, got %q (err: %s)", r.Status, r.Err)
	}
	lrcPath := filepath.Join(dir, "song.lrc")
	if _, err := os.Stat(lrcPath); os.IsNotExist(err) {
		t.Error("expected .lrc file to be written")
	}
}

func TestProcessor_RunSongs(t *testing.T) {
	stub := &stubProvider{name: "lrclib", ok: false}
	proc := lyrics.NewProcessor(nil, []lyrics.Provider{stub}, nil, nil, true)

	songs := []navidrome.Song{
		{ID: "a", Title: "A", Artist: "X"},
		{ID: "b", Title: "B", Artist: "X"},
		{ID: "c", Title: "C", Artist: "X"},
	}

	var mu sync.Mutex
	var results []lyrics.Result
	if err := proc.RunSongs(context.Background(), songs, func(r lyrics.Result) {
		mu.Lock()
		results = append(results, r)
		mu.Unlock()
	}); err != nil {
		t.Fatal(err)
	}
	if len(results) != len(songs) {
		t.Errorf("want %d results, got %d", len(songs), len(results))
	}
}
