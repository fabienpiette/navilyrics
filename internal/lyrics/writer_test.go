package lyrics_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/user/navilyrics/internal/lyrics"
)

func TestWriteLRCFile(t *testing.T) {
	dir := t.TempDir()
	audioPath := filepath.Join(dir, "song.mp3")
	if err := os.WriteFile(audioPath, []byte{0xFF, 0xFB, 0x90, 0x00}, 0644); err != nil {
		t.Fatal(err)
	}

	const synced = "[00:01.00] Hello\n[00:02.00] World"
	if err := lyrics.WriteLRCFile(audioPath, synced); err != nil {
		t.Fatalf("WriteLRCFile: %v", err)
	}

	lrcPath := filepath.Join(dir, "song.lrc")
	data, err := os.ReadFile(lrcPath)
	if err != nil {
		t.Fatalf("read lrc: %v", err)
	}
	if string(data) != synced {
		t.Errorf("lrc content: want %q, got %q", synced, string(data))
	}
}

func TestWriteLRCFile_empty(t *testing.T) {
	dir := t.TempDir()
	audioPath := filepath.Join(dir, "song.mp3")
	// Empty synced string should produce no .lrc file
	if err := lyrics.WriteLRCFile(audioPath, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "song.lrc")); !os.IsNotExist(err) {
		t.Error("lrc file should not be created for empty synced lyrics")
	}
}

func TestWriteRawLRC(t *testing.T) {
	lrcPath := filepath.Join(t.TempDir(), "song.lrc")
	content := "[00:01.00] Hello\n[00:02.00] World"
	if err := lyrics.WriteRawLRC(lrcPath, content); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(lrcPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != content {
		t.Errorf("content = %q, want %q", got, content)
	}
}
