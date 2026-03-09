package tagger_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/user/navilyrics/pkg/tagger"
)

func TestMain(m *testing.M) {
	if err := os.MkdirAll("testdata", 0755); err != nil {
		panic(err)
	}
	generateMP3Fixture()
	os.Exit(m.Run())
}

func generateMP3Fixture() {
	// Minimal valid ID3v2.3 header + one silent MPEG1 Layer3 frame
	data := []byte{
		// ID3v2.3 header: "ID3", version 2.3.0, flags 0, syncsafe size=0
		0x49, 0x44, 0x33, 0x03, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
		// MPEG1 Layer3 frame sync (128kbps, 44100Hz, stereo)
		0xFF, 0xFB, 0x90, 0x00,
	}
	// Pad to 417 bytes (minimum MP3 frame size at 128kbps)
	data = append(data, make([]byte, 417-len(data))...)
	_ = os.WriteFile("testdata/sample.mp3", data, 0644)
}

func TestMP3Tagger_RoundTrip(t *testing.T) {
	src, err := os.ReadFile("testdata/sample.mp3")
	if err != nil {
		t.Fatalf("read sample.mp3: %v", err)
	}
	tmp := filepath.Join(t.TempDir(), "test.mp3")
	if err := os.WriteFile(tmp, src, 0644); err != nil {
		t.Fatal(err)
	}

	tgr, err := tagger.ForFile(tmp)
	if err != nil {
		t.Fatalf("ForFile: %v", err)
	}

	const plain = "Line one\nLine two"
	const synced = "[00:01.00] Line one\n[00:02.00] Line two"

	if err := tgr.WriteLyrics(tmp, plain, synced); err != nil {
		t.Fatalf("WriteLyrics: %v", err)
	}

	tgr2, err := tagger.ForFile(tmp)
	if err != nil {
		t.Fatal(err)
	}
	gotPlain, gotSynced, err := tgr2.ReadLyrics(tmp)
	if err != nil {
		t.Fatal(err)
	}
	if gotPlain != plain {
		t.Errorf("plain: want %q, got %q", plain, gotPlain)
	}
	if gotSynced != synced {
		t.Errorf("synced: want %q, got %q", synced, gotSynced)
	}
}

func TestMP3Tagger_UnsupportedFormat(t *testing.T) {
	_, err := tagger.ForFile("song.ogg")
	if err == nil {
		t.Fatal("want error for unsupported format")
	}
}
