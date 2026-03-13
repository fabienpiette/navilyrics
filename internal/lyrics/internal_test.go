package lyrics

import (
	"os"
	"path/filepath"
	"testing"
)

// minimalMP3 writes a minimal valid ID3v2+MPEG frame to a temp dir and returns its path.
func minimalMP3(t *testing.T) string {
	t.Helper()
	data := []byte{
		// ID3v2.3 header: "ID3", version 2.3.0, flags 0, syncsafe size=0
		0x49, 0x44, 0x33, 0x03, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
		// MPEG1 Layer3 frame sync (128kbps, 44100Hz, stereo)
		0xFF, 0xFB, 0x90, 0x00,
	}
	data = append(data, make([]byte, 417-len(data))...)
	path := filepath.Join(t.TempDir(), "test.mp3")
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestWriteLyrics_withSynced_mp3(t *testing.T) {
	audioPath := minimalMP3(t)
	synced := "[00:01.00] plain lyrics"
	if err := writeLyrics(audioPath, "plain lyrics", synced); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(lrcPathFor(audioPath))
	if err != nil {
		t.Fatalf("expected .lrc file: %v", err)
	}
	if string(got) != synced {
		t.Errorf("lrc = %q, want %q", got, synced)
	}
}

func TestWriteLyrics_noSynced_mp3(t *testing.T) {
	audioPath := minimalMP3(t)
	if err := writeLyrics(audioPath, "plain only", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(lrcPathFor(audioPath)); !os.IsNotExist(err) {
		t.Error("lrc file should not be created when synced is empty")
	}
}

func TestWriteLyrics_unsupportedFormat(t *testing.T) {
	dir := t.TempDir()
	audioPath := filepath.Join(dir, "song.wav")
	if err := os.WriteFile(audioPath, []byte("dummy"), 0644); err != nil {
		t.Fatal(err)
	}
	// writeLyrics must write the .lrc and return nil (soft error for unsupported tag format)
	if err := writeLyrics(audioPath, "plain", "[00:01.00] synced"); err != nil {
		t.Errorf("want nil (soft error), got %v", err)
	}
	if _, err := os.Stat(lrcPathFor(audioPath)); os.IsNotExist(err) {
		t.Error("expected .lrc file to be written for unsupported audio format")
	}
}

func TestMarkIsInstrumental_mp3(t *testing.T) {
	audioPath := minimalMP3(t)
	if isInstrumental(audioPath) {
		t.Fatal("expected file to not be instrumental initially")
	}
	if err := markInstrumental(audioPath); err != nil {
		t.Fatal(err)
	}
	if !isInstrumental(audioPath) {
		t.Error("expected file to be instrumental after marking")
	}
}

func TestIsInstrumental_unsupportedFormat(t *testing.T) {
	path := filepath.Join(t.TempDir(), "song.wav")
	if err := os.WriteFile(path, []byte("dummy"), 0644); err != nil {
		t.Fatal(err)
	}
	if isInstrumental(path) {
		t.Error("want false for unsupported audio format")
	}
}

func TestIsInstrumental_missingFile(t *testing.T) {
	// .mp3 so ForFile succeeds, but the underlying read fails
	if isInstrumental(filepath.Join(t.TempDir(), "nonexistent.mp3")) {
		t.Error("want false for missing file")
	}
}
