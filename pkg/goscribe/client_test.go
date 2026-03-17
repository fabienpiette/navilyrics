package goscribe_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/user/navilyrics/pkg/goscribe"
)

func TestSubmitJob_success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/jobs" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		if err := r.ParseMultipartForm(10 << 20); err != nil {
			t.Fatal(err)
		}
		if r.MultipartForm.File["file"] == nil {
			t.Error("expected file field")
		}
		w.WriteHeader(http.StatusAccepted)
		json.NewEncoder(w).Encode(map[string]string{"job_id": "abc123", "status": "queued"})
	}))
	defer srv.Close()

	f := filepath.Join(t.TempDir(), "song.mp3")
	if err := os.WriteFile(f, []byte("fake audio"), 0644); err != nil {
		t.Fatal(err)
	}

	c := goscribe.New(srv.URL)
	jobID, err := c.SubmitJob(context.Background(), f)
	if err != nil {
		t.Fatalf("SubmitJob: %v", err)
	}
	if jobID != "abc123" {
		t.Errorf("want job_id abc123, got %q", jobID)
	}
}

func TestSubmitJob_fileNotFound(t *testing.T) {
	c := goscribe.New("http://localhost:9")
	_, err := c.SubmitJob(context.Background(), "/nonexistent/audio.mp3")
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestPollJob_completedImmediately(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"job_id":     "abc123",
			"status":     "completed",
			"transcript": "hello world",
		})
	}))
	defer srv.Close()

	c := goscribe.New(srv.URL)
	transcript, err := c.PollJob(context.Background(), "abc123", 10*time.Millisecond)
	if err != nil {
		t.Fatalf("PollJob: %v", err)
	}
	if transcript != "hello world" {
		t.Errorf("want transcript 'hello world', got %q", transcript)
	}
}

func TestPollJob_failedJob(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"job_id": "abc123",
			"status": "failed",
			"error":  "transcription error",
		})
	}))
	defer srv.Close()

	c := goscribe.New(srv.URL)
	_, err := c.PollJob(context.Background(), "abc123", 10*time.Millisecond)
	if err == nil {
		t.Fatal("expected error for failed job")
	}
}

func TestPollJob_contextCancelled(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"job_id": "abc123",
			"status": "processing",
		})
	}))
	defer srv.Close()

	c := goscribe.New(srv.URL)
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	_, err := c.PollJob(ctx, "abc123", 10*time.Millisecond)
	if err == nil {
		t.Fatal("expected error when context cancelled")
	}
}
