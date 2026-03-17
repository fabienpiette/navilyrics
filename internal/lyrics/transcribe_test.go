package lyrics_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/user/navilyrics/internal/lyrics"
	"github.com/user/navilyrics/pkg/navidrome"
)

type stubTranscriber struct {
	name       string
	transcript string
	err        error
}

func (s *stubTranscriber) Name() string { return s.name }
func (s *stubTranscriber) Transcribe(_ context.Context, _ string) (string, error) {
	return s.transcript, s.err
}

func TestTranscribeSong_noTranscribers(t *testing.T) {
	proc := lyrics.NewProcessor(nil, nil, nil, []string{"/music"}, false)
	_, _, err := proc.TranscribeSong(context.Background(), navidrome.Song{ID: "1", Path: "song.mp3"})
	if err == nil {
		t.Fatal("expected error when no transcribers configured")
	}
}

func TestTranscribeSong_fileNotFound(t *testing.T) {
	stub := &stubTranscriber{name: "goscribe", transcript: "lyrics"}
	proc := lyrics.NewProcessor(nil, nil, []lyrics.Transcriber{stub}, []string{"/nonexistent"}, false)
	_, _, err := proc.TranscribeSong(context.Background(), navidrome.Song{ID: "1", Path: "song.mp3"})
	if err == nil {
		t.Fatal("expected error when audio file not found")
	}
}

func TestTranscribeSong_success(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "song.mp3"), []byte("audio"), 0644); err != nil {
		t.Fatal(err)
	}
	stub := &stubTranscriber{name: "goscribe", transcript: "verse one\nverse two"}
	proc := lyrics.NewProcessor(nil, nil, []lyrics.Transcriber{stub}, []string{dir}, false)
	gotAudio, gotTranscript, err := proc.TranscribeSong(context.Background(), navidrome.Song{ID: "1", Path: "song.mp3"})
	if err != nil {
		t.Fatalf("TranscribeSong: %v", err)
	}
	if gotAudio != filepath.Join(dir, "song.mp3") {
		t.Errorf("unexpected audioPath: %q", gotAudio)
	}
	if gotTranscript != "verse one\nverse two" {
		t.Errorf("unexpected transcript: %q", gotTranscript)
	}
}

func TestTranscribeSong_transcriberError(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "song.mp3"), []byte("audio"), 0644); err != nil {
		t.Fatal(err)
	}
	stub := &stubTranscriber{name: "goscribe", err: errors.New("upstream error")}
	proc := lyrics.NewProcessor(nil, nil, []lyrics.Transcriber{stub}, []string{dir}, false)
	_, _, err := proc.TranscribeSong(context.Background(), navidrome.Song{ID: "1", Path: "song.mp3"})
	if err == nil {
		t.Fatal("expected error from transcriber")
	}
}

func TestTranscribeAll_emitsResults(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"a.mp3", "b.mp3"} {
		if err := os.WriteFile(filepath.Join(dir, name), minimalMP3Bytes(), 0644); err != nil {
			t.Fatal(err)
		}
	}
	stub := &stubTranscriber{name: "goscribe", transcript: "lyrics text"}
	proc := lyrics.NewProcessor(nil, nil, []lyrics.Transcriber{stub}, []string{dir}, false)

	var mu sync.Mutex
	var results []lyrics.Result
	err := proc.TranscribeAll(context.Background(), []navidrome.Song{
		{ID: "1", Path: "a.mp3"},
		{ID: "2", Path: "b.mp3"},
	}, func(r lyrics.Result) {
		mu.Lock()
		results = append(results, r)
		mu.Unlock()
	})
	if err != nil {
		t.Fatalf("TranscribeAll: %v", err)
	}
	if len(results) != 2 {
		t.Errorf("want 2 results, got %d", len(results))
	}
	for _, r := range results {
		if r.Status != "transcribed" {
			t.Errorf("want status transcribed, got %q (err: %s)", r.Status, r.Err)
		}
	}
}

func TestTranscribeAll_continuesPastErrors(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.mp3"), minimalMP3Bytes(), 0644); err != nil {
		t.Fatal(err)
	}
	stub := &stubTranscriber{name: "goscribe", transcript: "lyrics text"}
	proc := lyrics.NewProcessor(nil, nil, []lyrics.Transcriber{stub}, []string{dir}, false)

	var mu sync.Mutex
	var results []lyrics.Result
	_ = proc.TranscribeAll(context.Background(), []navidrome.Song{
		{ID: "1", Path: "a.mp3"},
		{ID: "2", Path: "missing.mp3"},
	}, func(r lyrics.Result) {
		mu.Lock()
		defer mu.Unlock()
		results = append(results, r)
	})

	statuses := map[string]int{}
	for _, r := range results {
		statuses[r.Status]++
	}
	if statuses["transcribed"] != 1 || statuses["error"] != 1 {
		t.Errorf("want 1 transcribed + 1 error, got %v", statuses)
	}
}
