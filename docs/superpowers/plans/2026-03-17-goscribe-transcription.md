# Goscribe Audio Transcription Integration — Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Integrate the separately-running goscribe service into navilyrics so users can transcribe audio to plain-text lyrics for songs that have no lyrics from existing providers.

**Architecture:** A new `pkg/goscribe` HTTP client submits audio files to goscribe and polls for results. A `Transcriber` interface (mirroring the existing `Provider` pattern) wraps this client in `internal/lyrics`. The `Processor` gains `TranscribeSong` and `TranscribeAll` methods. Five new HTTP handlers serve single-song and batch flows. The UI adds a "Transcribe" button to the song preview panel and a "Transcribe all missing" batch button to the toolbar. Feature is opt-in via `GOSCRIBE_URL` env var.

**Tech Stack:** Go 1.23, stdlib `net/http` only (no new deps), HTMX 2.x SSE extension, html/template, chi v5.

---

## UI Architecture Note

The spec described per-row buttons in the songs table. After reviewing the actual `songs.html`, the existing UX places all per-song actions in a **preview panel** (right-side aside) that opens when a row is clicked. Adding the "Transcribe" button to the preview panel's action-group is consistent with this architecture. The `hx-include` attribute on the button will include a hidden `<input name="song_id">` that JavaScript updates on row selection.

---

## Chunk 1: `pkg/goscribe` — HTTP Client

**Files:**
- Create: `pkg/goscribe/client.go`
- Create: `pkg/goscribe/client_test.go`

### Task 1: Write the goscribe client tests

- [ ] **Step 1: Create `pkg/goscribe/client_test.go` with failing tests**

```go
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
```

- [ ] **Step 2: Run tests to confirm they fail (package does not exist yet)**

```bash
cd /home/gndm/Projects/navilyrics && go test ./pkg/goscribe/... 2>&1 | head -5
```

Expected: `cannot find package` or build error.

### Task 2: Implement the goscribe client

- [ ] **Step 3: Create `pkg/goscribe/client.go`**

```go
package goscribe

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

// Client is an HTTP client for the goscribe transcription service.
type Client struct {
	baseURL string
	http    *http.Client
}

// New creates a Client targeting the given base URL (e.g. "http://goscribe:8080").
func New(baseURL string) *Client {
	return &Client{
		baseURL: baseURL,
		http:    &http.Client{Timeout: 30 * time.Second},
	}
}

type submitResponse struct {
	JobID  string `json:"job_id"`
	Status string `json:"status"`
}

type pollResponse struct {
	JobID      string `json:"job_id"`
	Status     string `json:"status"`
	Transcript string `json:"transcript"`
	Error      string `json:"error"`
}

// SubmitJob uploads the audio file at audioPath to goscribe and returns the job ID.
func (c *Client) SubmitJob(ctx context.Context, audioPath string) (string, error) {
	f, err := os.Open(audioPath)
	if err != nil {
		return "", fmt.Errorf("goscribe: open audio: %w", err)
	}
	defer f.Close()

	pr, pw := io.Pipe()
	mw := multipart.NewWriter(pw)
	go func() {
		part, err := mw.CreateFormFile("file", filepath.Base(audioPath))
		if err != nil {
			pw.CloseWithError(err)
			return
		}
		if _, err := io.Copy(part, f); err != nil {
			pw.CloseWithError(err)
			return
		}
		pw.CloseWithError(mw.Close())
	}()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/jobs", pr)
	if err != nil {
		return "", fmt.Errorf("goscribe: build request: %w", err)
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())

	resp, err := c.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("goscribe: submit job: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusAccepted {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return "", fmt.Errorf("goscribe: submit job: status %d: %s", resp.StatusCode, body)
	}

	var result submitResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("goscribe: decode submit response: %w", err)
	}
	return result.JobID, nil
}

// PollJob polls GET /jobs/{id} at the given interval until the job reaches
// completed or failed status, or ctx is cancelled.
// Returns the transcript text on success.
func (c *Client) PollJob(ctx context.Context, jobID string, interval time.Duration) (string, error) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return "", fmt.Errorf("goscribe: poll job %s: %w", jobID, ctx.Err())
		case <-ticker.C:
			transcript, done, err := c.checkJob(ctx, jobID)
			if err != nil {
				return "", err
			}
			if done {
				return transcript, nil
			}
		}
	}
}

// checkJob fetches job status once. Returns (transcript, true, nil) when complete,
// ("", false, nil) when still pending, or ("", false, err) on error/failure.
func (c *Client) checkJob(ctx context.Context, jobID string) (string, bool, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/jobs/"+jobID, nil)
	if err != nil {
		return "", false, fmt.Errorf("goscribe: build poll request: %w", err)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return "", false, fmt.Errorf("goscribe: poll job: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", false, fmt.Errorf("goscribe: poll job: status %d", resp.StatusCode)
	}

	var result pollResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", false, fmt.Errorf("goscribe: decode poll response: %w", err)
	}

	switch result.Status {
	case "completed":
		return result.Transcript, true, nil
	case "failed":
		return "", false, fmt.Errorf("goscribe: job %s failed: %s", jobID, result.Error)
	default:
		return "", false, nil
	}
}
```

- [ ] **Step 4: Run tests — expect pass**

```bash
cd /home/gndm/Projects/navilyrics && go test -v -race ./pkg/goscribe/...
```

Expected: all 4 tests pass.

- [ ] **Step 5: Vet**

```bash
cd /home/gndm/Projects/navilyrics && go vet ./pkg/goscribe/...
```

- [ ] **Step 6: Commit**

```bash
cd /home/gndm/Projects/navilyrics
git add pkg/goscribe/client.go pkg/goscribe/client_test.go
git commit -m "feat(goscribe): add http client for job submission and polling"
```

---

## Chunk 2: `internal/lyrics` — Transcriber Interface + Processor

**Files:**
- Create: `internal/lyrics/transcribers.go`
- Create: `internal/lyrics/transcribe.go`
- Create: `internal/lyrics/transcribe_test.go`
- Modify: `internal/lyrics/processor.go`
- Modify: `internal/lyrics/processor_test.go`

### Task 3: Transcriber interface + goscribeTranscriber

- [ ] **Step 1: Create `internal/lyrics/transcribers.go`**

```go
package lyrics

import (
	"context"
	"time"

	"github.com/user/navilyrics/pkg/goscribe"
)

// Transcriber transcribes an audio file into plain-text lyrics.
// Defined in the consumer package, mirroring the Provider pattern.
type Transcriber interface {
	// Name returns the transcriber's identifier (e.g. "goscribe").
	Name() string
	// Transcribe transcribes the audio file at audioPath.
	// Returns the plain-text transcript or an error.
	Transcribe(ctx context.Context, audioPath string) (string, error)
}

type goscribeTranscriber struct{ c *goscribe.Client }

// NewGoscribeTranscriber wraps a goscribe.Client as a Transcriber.
func NewGoscribeTranscriber(c *goscribe.Client) Transcriber {
	return &goscribeTranscriber{c: c}
}

func (t *goscribeTranscriber) Name() string { return "goscribe" }

func (t *goscribeTranscriber) Transcribe(ctx context.Context, audioPath string) (string, error) {
	jobID, err := t.c.SubmitJob(ctx, audioPath)
	if err != nil {
		return "", err
	}
	return t.c.PollJob(ctx, jobID, 3*time.Second)
}
```

- [ ] **Step 2: Verify compilation**

```bash
cd /home/gndm/Projects/navilyrics && go build ./internal/lyrics/...
```

### Task 4: Update `processor.go`

- [ ] **Step 3: Edit `internal/lyrics/processor.go` — add `transcribers` field and update `NewProcessor`**

The `Processor` struct (around line 31) becomes:

```go
type Processor struct {
	nd           *navidrome.Client
	providers    []Provider
	transcribers []Transcriber
	musicDirs    []string
	dryRun       bool
}
```

`NewProcessor` (around line 41) becomes:

```go
// NewProcessor creates a Processor. nd may be nil when using ProcessSong directly.
// musicDirs is a list of base directories to search for audio files.
// transcribers is the list of audio transcription backends; nil is valid.
func NewProcessor(nd *navidrome.Client, providers []Provider, transcribers []Transcriber, musicDirs []string, dryRun bool) *Processor {
	return &Processor{nd: nd, providers: providers, transcribers: transcribers, musicDirs: musicDirs, dryRun: dryRun}
}
```

- [ ] **Step 4: Fix `NewProcessor` call sites in `internal/lyrics/processor_test.go`**

Insert `nil,` as the third argument in every `lyrics.NewProcessor(` call in that file. There are ~8 call sites. Each call changes like this:

Before: `lyrics.NewProcessor(nil, []lyrics.Provider{stub}, []string{"/music"}, true)`
After:  `lyrics.NewProcessor(nil, []lyrics.Provider{stub}, nil, []string{"/music"}, true)`

Before: `lyrics.NewProcessor(nil, nil, []string{dir}, false)`
After:  `lyrics.NewProcessor(nil, nil, nil, []string{dir}, false)`

- [ ] **Step 5: Run existing tests — no regressions**

```bash
cd /home/gndm/Projects/navilyrics && go test -v -race ./internal/lyrics/...
```

Expected: all existing tests pass.

### Task 5: `TranscribeSong` + `TranscribeAll`

- [ ] **Step 6: Create `internal/lyrics/transcribe_test.go`**

```go
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

	var results []lyrics.Result
	_ = proc.TranscribeAll(context.Background(), []navidrome.Song{
		{ID: "1", Path: "a.mp3"},
		{ID: "2", Path: "missing.mp3"},
	}, func(r lyrics.Result) { results = append(results, r) })

	statuses := map[string]int{}
	for _, r := range results {
		statuses[r.Status]++
	}
	if statuses["transcribed"] != 1 || statuses["error"] != 1 {
		t.Errorf("want 1 transcribed + 1 error, got %v", statuses)
	}
}
```

- [ ] **Step 7: Run test — expect failure (TranscribeSong undefined)**

```bash
cd /home/gndm/Projects/navilyrics && go test ./internal/lyrics/... 2>&1 | head -5
```

- [ ] **Step 8: Create `internal/lyrics/transcribe.go`**

```go
package lyrics

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/user/navilyrics/pkg/navidrome"
)

// TranscribeSong resolves the audio path for song and calls the first configured
// transcriber. Returns the resolved audioPath and transcript text.
func (p *Processor) TranscribeSong(ctx context.Context, song navidrome.Song) (audioPath, transcript string, err error) {
	if len(p.transcribers) == 0 {
		return "", "", fmt.Errorf("no transcribers configured")
	}
	audioPath = p.resolveAudioPath(song.Path)
	if audioPath == "" {
		return "", "", fmt.Errorf("audio file not found in any music dir: %s", song.Path)
	}
	transcript, err = p.transcribers[0].Transcribe(ctx, audioPath)
	if err != nil {
		return audioPath, "", fmt.Errorf("transcribe %q: %w", song.Title, err)
	}
	return audioPath, transcript, nil
}

// TranscribeAll transcribes every song in the list with a worker pool of 2,
// auto-saving each result. progress is called once per result from any goroutine.
func (p *Processor) TranscribeAll(ctx context.Context, songs []navidrome.Song, progress func(Result)) error {
	log.Printf("transcribe: starting %d songs", len(songs))
	start := time.Now()

	const workers = 2
	jobs := make(chan navidrome.Song, workers)
	var wg sync.WaitGroup

	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for song := range jobs {
				if ctx.Err() != nil {
					return
				}
				result := p.transcribeOne(ctx, song)
				if progress != nil {
					progress(result)
				}
			}
		}()
	}

	for _, s := range songs {
		select {
		case jobs <- s:
		case <-ctx.Done():
			close(jobs)
			wg.Wait()
			return ctx.Err()
		}
	}
	close(jobs)
	wg.Wait()

	log.Printf("transcribe: finished %d songs in %s", len(songs), time.Since(start).Round(time.Millisecond))
	return ctx.Err()
}

func (p *Processor) transcribeOne(ctx context.Context, song navidrome.Song) Result {
	base := Result{SongID: song.ID, SongPath: song.Path, Title: song.Title, Artist: song.Artist}

	tCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()

	audioPath, transcript, err := p.TranscribeSong(tCtx, song)
	if err != nil {
		log.Printf("[transcribe:error] %s — %s: %v", song.Artist, song.Title, err)
		base.Status = "error"
		base.Err = err.Error()
		return base
	}

	if err := writeLyrics(audioPath, transcript, ""); err != nil {
		log.Printf("[transcribe:error] save %s — %s: %v", song.Artist, song.Title, err)
		base.Status = "error"
		base.Err = err.Error()
		return base
	}

	log.Printf("[transcribed]  %s — %s", song.Artist, song.Title)
	base.PlainLyrics = transcript
	base.Source = "goscribe"
	base.Status = "transcribed"
	return base
}
```

- [ ] **Step 9: Run all lyrics tests**

```bash
cd /home/gndm/Projects/navilyrics && go test -v -race ./internal/lyrics/...
```

Expected: all tests pass.

- [ ] **Step 10: Vet and commit**

```bash
cd /home/gndm/Projects/navilyrics && go vet ./internal/lyrics/...
git add internal/lyrics/transcribers.go internal/lyrics/transcribe.go \
        internal/lyrics/transcribe_test.go internal/lyrics/processor.go \
        internal/lyrics/processor_test.go
git commit -m "feat(lyrics): add Transcriber interface and TranscribeSong/TranscribeAll"
```

---

## Chunk 3: HTTP Handlers

**Files:**
- Modify: `internal/handlers/handler.go`
- Modify: `internal/handlers/songs.go`
- Create: `internal/handlers/transcribe.go`

### Task 6: Add `goscribeEnabled` to Handler

- [ ] **Step 1: Edit `internal/handlers/handler.go`**

Add field to the struct:

```go
type Handler struct {
	nd                 *navidrome.Client
	proc               *lyrics.Processor
	tmpls              map[string]*template.Template
	partials           map[string]*template.Template
	version            string
	runs               *RunStore
	stats              *statsCache
	availableProviders []string
	goscribeEnabled    bool
}
```

Update `New` signature and body:

```go
func New(nd *navidrome.Client, proc *lyrics.Processor, tmpls, partials map[string]*template.Template, version string, availableProviders []string, goscribeEnabled bool) *Handler {
	h := &Handler{
		nd: nd, proc: proc, tmpls: tmpls, partials: partials,
		version: version, runs: newRunStore(), stats: &statsCache{},
		availableProviders: availableProviders,
		goscribeEnabled:    goscribeEnabled,
	}
	go func() {
		if err := h.stats.refresh(context.Background(), nd); err != nil {
			log.Printf("stats: boot refresh failed: %v", err)
		}
	}()
	return h
}
```

- [ ] **Step 2: Add `GoscribeEnabled` to `songsData` in `internal/handlers/songs.go`**

```go
type songsData struct {
	ActiveTab          string
	Version            string
	AvailableProviders []string
	GoscribeEnabled    bool
	songsRowsData
}
```

In the `Songs` handler, include it:

```go
h.render(w, "songs.html", songsData{
    ActiveTab:          "songs",
    Version:            h.version,
    AvailableProviders: h.availableProviders,
    GoscribeEnabled:    h.goscribeEnabled,
    songsRowsData:      rows,
})
```

- [ ] **Step 3: Build — expect error only in main.go (handlers.New arity)**

```bash
cd /home/gndm/Projects/navilyrics && go build ./... 2>&1 | grep -v "main.go"
```

Expected: no errors outside `main.go`.

### Task 7: Create transcription handlers

- [ ] **Step 4: Create `internal/handlers/transcribe.go`**

The handler uses a package-level `sync.Map` to track in-progress single-song transcriptions. The polling flow is: first poll starts a background goroutine (job=nil sentinel), subsequent polls check the map for completion.

```go
package handlers

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/user/navilyrics/internal/lyrics"
)

// transcribeJobs stores in-progress single-song transcription results.
// Key: song_id (string). Value: nil while running, *transcribeJobResult when done.
var transcribeJobs sync.Map

type transcribeJobResult struct {
	transcript string
	err        error
}

// transcribePollData is the template data for the poll spinner partial.
type transcribePollData struct {
	SongID  string
	Attempt int
	JobID   string
}

// transcribePreviewData is the template data for the transcript preview partial.
type transcribePreviewData struct {
	SongID     string
	Transcript string
}

// transcribeResultData is the template data for the result/error partial.
type transcribeResultData struct {
	Message string
	Error   string
}

// TranscribeSong handles POST /transcribe/song.
// Resolves the song, checks the audio path exists, returns the poll partial.
func (h *Handler) TranscribeSong(w http.ResponseWriter, r *http.Request) {
	songID := r.FormValue("song_id")
	if songID == "" {
		http.Error(w, "song_id required", http.StatusBadRequest)
		return
	}
	song, err := h.nd.GetSong(r.Context(), songID)
	if err != nil {
		h.renderPartial(w, "transcribe_result.html", transcribeResultData{Error: "song not found: " + err.Error()})
		return
	}
	if h.proc.ResolveAudioPath(song.Path) == "" {
		h.renderPartial(w, "transcribe_result.html", transcribeResultData{Error: "audio file not found on disk"})
		return
	}
	h.renderPartial(w, "transcribe_poll.html", transcribePollData{SongID: songID, Attempt: 0, JobID: ""})
}

// TranscribeSongPoll handles GET /transcribe/song/poll.
// On attempt=0 (no job_id) it starts a background transcription goroutine.
// On subsequent attempts it checks the result map and returns preview or poll partial.
func (h *Handler) TranscribeSongPoll(w http.ResponseWriter, r *http.Request) {
	songID := r.URL.Query().Get("song_id")
	jobID := r.URL.Query().Get("job_id")
	attempt, _ := strconv.Atoi(r.URL.Query().Get("attempt"))

	if attempt >= 100 {
		h.renderPartial(w, "transcribe_result.html", transcribeResultData{Error: "transcription timed out after 5 minutes"})
		return
	}

	if jobID == "" {
		// First poll: start transcription in a background goroutine.
		h.startTranscribeJob(songID)
		h.renderPartial(w, "transcribe_poll.html", transcribePollData{SongID: songID, Attempt: 1, JobID: songID})
		return
	}

	// Subsequent polls: check the map.
	result, ready := h.checkTranscribeJob(jobID)
	if !ready {
		h.renderPartial(w, "transcribe_poll.html", transcribePollData{SongID: songID, Attempt: attempt + 1, JobID: jobID})
		return
	}
	transcribeJobs.Delete(jobID)

	if result.err != nil {
		h.renderPartial(w, "transcribe_result.html", transcribeResultData{Error: result.err.Error()})
		return
	}
	h.renderPartial(w, "transcript_preview.html", transcribePreviewData{SongID: songID, Transcript: result.transcript})
}

// TranscribeSongSave handles POST /transcribe/song/save.
// Saves the user-approved transcript as plain lyrics for the song.
func (h *Handler) TranscribeSongSave(w http.ResponseWriter, r *http.Request) {
	songID := r.FormValue("song_id")
	transcript := r.FormValue("transcript")
	song, err := h.nd.GetSong(r.Context(), songID)
	if err != nil {
		h.renderPartial(w, "transcribe_result.html", transcribeResultData{Error: "song not found: " + err.Error()})
		return
	}
	if err := h.proc.SaveLyrics(song, transcript, ""); err != nil {
		h.renderPartial(w, "transcribe_result.html", transcribeResultData{Error: "save failed: " + err.Error()})
		return
	}
	h.renderPartial(w, "transcribe_result.html", transcribeResultData{Message: "Lyrics saved."})
}

// TranscribeBatch handles POST /transcribe/batch.
// Starts batch transcription for all missing-lyrics songs.
func (h *Handler) TranscribeBatch(w http.ResponseWriter, r *http.Request) {
	songs, err := h.allMatchingSongs(r.Context(), "", "missing")
	if err != nil {
		http.Error(w, "navidrome: "+err.Error(), http.StatusBadGateway)
		return
	}
	id := fmt.Sprintf("%x", time.Now().UnixNano())
	ch := h.runs.create(id)
	go func() {
		defer close(ch)
		_ = h.proc.TranscribeAll(context.Background(), songs, func(res lyrics.Result) { ch <- res })
	}()
	h.render(w, "transcribe_progress.html", runProgressData{ActiveTab: "songs", Version: h.version, RunID: id})
}

// TranscribeBatchEvents handles GET /transcribe/batch/{id}/events.
// Streams transcription results as SSE events.
func (h *Handler) TranscribeBatchEvents(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	ch, ok := h.runs.get(id)
	if !ok {
		http.NotFound(w, r)
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	defer h.runs.delete(id)

	var transcribed, errors int
loop:
	for {
		select {
		case res, ok := <-ch:
			if !ok {
				break loop
			}
			fmt.Fprintf(w, "event: result\ndata: %s\n\n", resultLineHTML(res))
			flusher.Flush()
			switch res.Status {
			case "transcribed":
				transcribed++
			case "error":
				errors++
			}
		case <-r.Context().Done():
			go func() {
				for range ch {
				}
			}()
			return
		}
	}

	fmt.Fprintf(w, "event: done\ndata: {\"transcribed\":%d,\"errors\":%d}\n\n", transcribed, errors)
	flusher.Flush()

	if transcribed > 0 {
		go func() {
			if err := h.nd.TriggerScan(context.Background()); err != nil {
				_ = err
			}
		}()
	}
}

// startTranscribeJob launches a background goroutine for song_id if one is not already running.
func (h *Handler) startTranscribeJob(songID string) {
	if _, loaded := transcribeJobs.LoadOrStore(songID, nil); loaded {
		return // already running
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()
		song, err := h.nd.GetSong(ctx, songID)
		if err != nil {
			transcribeJobs.Store(songID, &transcribeJobResult{err: err})
			return
		}
		_, transcript, err := h.proc.TranscribeSong(ctx, song)
		transcribeJobs.Store(songID, &transcribeJobResult{transcript: transcript, err: err})
	}()
}

// checkTranscribeJob returns the result for a job if it has completed.
// ready is false while the goroutine is still running.
func (h *Handler) checkTranscribeJob(songID string) (*transcribeJobResult, bool) {
	v, ok := transcribeJobs.Load(songID)
	if !ok {
		return nil, false
	}
	if v == nil {
		return nil, false // sentinel: still running
	}
	r, _ := v.(*transcribeJobResult)
	return r, r != nil
}
```

- [ ] **Step 5: Build (expect error only in main.go)**

```bash
cd /home/gndm/Projects/navilyrics && go build ./internal/... 2>&1
```

Expected: clean.

- [ ] **Step 6: Commit**

```bash
cd /home/gndm/Projects/navilyrics
git add internal/handlers/transcribe.go internal/handlers/handler.go internal/handlers/songs.go
git commit -m "feat(handlers): add transcription handlers for single-song and batch flows"
```

---

## Chunk 4: Web Templates

**Files:**
- Create: `web/templates/partials/transcribe_poll.html`
- Create: `web/templates/partials/transcript_preview.html`
- Create: `web/templates/partials/transcribe_result.html`
- Create: `web/templates/transcribe_progress.html`
- Modify: `web/templates/songs.html`
- Modify: `web/static/style.css` (or wherever badge styles live — check with `grep -r badge- web/static/`)

### Task 8: New partials

- [ ] **Step 1: Create `web/templates/partials/transcribe_poll.html`**

```html
<div id="transcribe-area" class="transcribe-area">
  <span class="muted">Transcribing&#8230;</span>
  <div hx-get="/transcribe/song/poll?song_id={{.SongID}}&amp;job_id={{.JobID}}&amp;attempt={{.Attempt}}"
       hx-trigger="load delay:3s"
       hx-target="#transcribe-area"
       hx-swap="outerHTML"></div>
</div>
```

- [ ] **Step 2: Create `web/templates/partials/transcript_preview.html`**

The Discard button uses a safe JS function (`clearTranscribeArea`) defined in `songs.html` — see Task 9 Step 1d.

```html
<div id="transcribe-area" class="transcribe-area">
  <label style="font-size:.75rem;color:var(--muted)">Transcript — review and edit before saving:</label>
  <form hx-post="/transcribe/song/save"
        hx-target="#transcribe-area"
        hx-swap="outerHTML">
    <input type="hidden" name="song_id" value="{{.SongID}}">
    <textarea name="transcript" rows="8"
              style="width:100%;box-sizing:border-box;font-family:monospace;font-size:.8rem;resize:vertical">{{.Transcript}}</textarea>
    <div class="action-group" style="margin-top:.25rem">
      <button type="submit" class="action-btn">Approve &amp; save</button>
      <button type="button" class="action-btn" onclick="clearTranscribeArea()">Discard</button>
    </div>
  </form>
</div>
```

- [ ] **Step 3: Create `web/templates/partials/transcribe_result.html`**

```html
<div id="transcribe-area" class="transcribe-area">
  {{if .Error -}}
  <span class="badge badge-error">error</span> <span class="muted">{{.Error}}</span>
  {{- else -}}
  <span class="badge badge-transcribed">{{.Message}}</span>
  {{- end}}
</div>
```

### Task 9: Progress page + songs.html updates

- [ ] **Step 4: Create `web/templates/transcribe_progress.html`**

```html
{{define "title"}}navilyrics &#8212; transcribing&#8230;{{end}}

{{define "content"}}
<h2>Transcribe missing lyrics</h2>

<div class="stats-grid" id="transcribe-summary" style="margin-bottom:1.5rem">
    <div class="stat-cell"><div class="stat-num" id="cnt-transcribed">&#8212;</div><div class="stat-label">transcribed</div></div>
    <div class="stat-cell"><div class="stat-num" id="cnt-errors">&#8212;</div><div class="stat-label">errors</div></div>
</div>

<div class="run-log-toolbar" id="transcribe-toolbar" style="display:none">
    <div class="filter-pills">
        <button class="filter-pill active" data-status="transcribed" onclick="toggleFilter(this,'transcribe-log')">transcribed</button>
        <button class="filter-pill active" data-status="error"       onclick="toggleFilter(this,'transcribe-log')">errors</button>
    </div>
    <button class="btn secondary small" onclick="exportCSV('transcribe-log','transcribe_results.csv')">Export CSV</button>
</div>

<div class="run-log streaming"
     hx-ext="sse"
     sse-connect="/transcribe/batch/{{.RunID}}/events"
     id="transcribe-log">
    <div sse-swap="result" hx-swap="afterbegin" id="transcribe-log-inner"></div>
    <div sse-swap="done"   hx-swap="none"       id="transcribe-done-trigger"></div>
</div>

<div class="form-actions" style="margin-top:1rem">
    <button class="btn secondary" id="btn-scroll-top"
            onclick="document.getElementById('transcribe-log').scrollTo({top:0,behavior:'smooth'})"
            style="display:none" title="Scroll to top of log">&#8593; Top</button>
</div>

<script>
function countUp(id, target) {
    var el = document.getElementById(id);
    if (!target) { el.textContent = '0'; return; }
    var n = 0, inc = Math.max(1, Math.ceil(target / 40));
    var t = setInterval(function() {
        n = Math.min(n + inc, target);
        el.textContent = n;
        if (n >= target) clearInterval(t);
    }, 16);
}
function toggleFilter(pill, logId) {
    document.getElementById(logId).classList.toggle('hide-' + pill.dataset.status);
    pill.classList.toggle('active');
}
function exportCSV(logId, filename) {
    var rows = [['status','artist','title','error']];
    document.getElementById(logId).querySelectorAll('.run-log-line[data-status]').forEach(function(el) {
        rows.push([el.dataset.status||'', el.dataset.artist||'', el.dataset.title||'', el.dataset.err||'']);
    });
    var csv = rows.map(function(r) {
        return r.map(function(v) { return '"' + v.replace(/"/g,'""') + '"'; }).join(',');
    }).join('\n');
    var a = document.createElement('a');
    a.href = 'data:text/csv;charset=utf-8,' + encodeURIComponent(csv);
    a.download = filename;
    document.body.appendChild(a); a.click(); document.body.removeChild(a);
}
document.addEventListener('htmx:sseMessage', function(e) {
    if (e.detail.type === 'result') {
        document.getElementById('btn-scroll-top').style.display = '';
        document.getElementById('transcribe-toolbar').style.display = 'flex';
    }
    if (e.detail.type === 'done') {
        document.getElementById('transcribe-log').classList.remove('streaming');
        try {
            var d = JSON.parse(e.detail.data);
            countUp('cnt-transcribed', d.transcribed);
            countUp('cnt-errors',      d.errors);
        } catch(ex) {}
    }
});
</script>
{{end}}
```

- [ ] **Step 5: Edit `web/templates/songs.html` — add Transcribe button + hidden input + transcribe-area + batch button + helper JS**

**a) Hidden input and transcribe area — add inside `<div class="preview-inner">`, after `<div class="preview-actions">...</div>` closing tag:**

```html
      {{if .GoscribeEnabled}}
      <input type="hidden" id="transcribe-song-id" name="song_id" value="">
      <div id="transcribe-area"></div>
      {{end}}
```

**b) "Transcribe" action button — add inside `<div class="action-group">` after the Tags button:**

```html
          {{if .GoscribeEnabled}}<button class="action-btn" id="btn-transcribe"
            hx-post="/transcribe/song"
            hx-target="#transcribe-area"
            hx-swap="outerHTML"
            hx-include="#transcribe-song-id"
            title="Transcribe audio using AI">Transcribe</button>{{end}}
```

**c) "Transcribe missing" batch button — add in the toolbar button group after the sync form closing `</form>`:**

```html
      {{if .GoscribeEnabled}}
      <form method="post" action="/transcribe/batch">
        <button type="submit" class="btn secondary small"
                title="Transcribe audio for all songs without lyrics">Transcribe missing</button>
      </form>
      {{end}}
```

**d) JS helpers — add inside the `<script>` block, before the closing `</script>`, after the existing `htmx:afterSettle` listener:**

```javascript
// Update the hidden song_id input used by the Transcribe HTMX button,
// and reset the transcription area when switching songs.
var _origPreviewSong = previewSong; // eslint-disable-line no-unused-vars
function previewSong(id, row) {
  var tsi = document.getElementById('transcribe-song-id');
  if (tsi) { tsi.value = id; }
  var ta = document.getElementById('transcribe-area');
  if (ta) { while (ta.firstChild) ta.removeChild(ta.firstChild); }
  _origPreviewSong(id, row);
}
// clearTranscribeArea is called by the Discard button in transcript_preview.html
function clearTranscribeArea() {
  var el = document.getElementById('transcribe-area');
  if (!el) return;
  var empty = document.createElement('div');
  empty.id = 'transcribe-area';
  el.parentNode.replaceChild(empty, el);
}
```

Note: this wraps `previewSong` to add transcription state management without modifying the existing function body.

- [ ] **Step 6: Find badge styles and add `badge-transcribed`**

```bash
grep -r "badge-found\|badge-error" /home/gndm/Projects/navilyrics/web/static/ | head -5
```

Then add to the stylesheet (same location as other `.badge-*` rules):

```css
.badge-transcribed { background: var(--fg); color: var(--bg); }
```

- [ ] **Step 7: Build — templates are embedded, parse errors surface here**

```bash
cd /home/gndm/Projects/navilyrics && go build ./... 2>&1 | grep -v "main.go"
```

Expected: clean (only `main.go` arity error remains).

- [ ] **Step 8: Commit**

```bash
cd /home/gndm/Projects/navilyrics
git add web/templates/partials/transcribe_poll.html \
        web/templates/partials/transcript_preview.html \
        web/templates/partials/transcribe_result.html \
        web/templates/transcribe_progress.html \
        web/templates/songs.html \
        web/static/
git commit -m "feat(ui): add transcription templates and songs page integration"
```

---

## Chunk 5: Wiring + Final Verification

**Files:**
- Modify: `cmd/navilyrics/main.go`

### Task 10: Wire GOSCRIBE_URL in `main.go`

- [ ] **Step 1: Add `pkg/goscribe` import to `cmd/navilyrics/main.go`**

In the import block, add:

```go
"github.com/user/navilyrics/pkg/goscribe"
```

- [ ] **Step 2: Add `buildTranscribers` helper after `buildProviders`**

```go
// buildTranscribers constructs the transcriber list from env config.
// Returns nil if GOSCRIBE_URL is not set (feature disabled).
func buildTranscribers() []lyrics.Transcriber {
	if url := os.Getenv("GOSCRIBE_URL"); url != "" {
		return []lyrics.Transcriber{lyrics.NewGoscribeTranscriber(goscribe.New(url))}
	}
	return nil
}
```

- [ ] **Step 3: Update `runCLI` — pass transcribers to NewProcessor**

```go
proc := lyrics.NewProcessor(nd, providers, buildTranscribers(), strings.Split(musicDir, ":"), *dryRun)
```

- [ ] **Step 4: Update `runServer` — pass transcribers + goscribeEnabled to handlers.New**

```go
transcribers := buildTranscribers()
proc := lyrics.NewProcessor(nd, providers, transcribers, strings.Split(musicDir, ":"), dryRun)
// ...
goscribeEnabled := os.Getenv("GOSCRIBE_URL") != ""
h := handlers.New(nd, proc, tmpls, partials, "dev", providerNames(providers), goscribeEnabled)
```

- [ ] **Step 5: Update `runSync` — pass nil transcribers**

```go
proc := lyrics.NewProcessor(nd, nil, nil, strings.Split(musicDir, ":"), false)
```

- [ ] **Step 6: Register the 5 new routes in `runServer`**

After `r.Get("/sync/{id}/events", h.SyncEvents)`, add:

```go
r.Post("/transcribe/song",             h.TranscribeSong)
r.Get("/transcribe/song/poll",         h.TranscribeSongPoll)
r.Post("/transcribe/song/save",        h.TranscribeSongSave)
r.Post("/transcribe/batch",            h.TranscribeBatch)
r.Get("/transcribe/batch/{id}/events", h.TranscribeBatchEvents)
```

### Task 11: Full build, test, and smoke test

- [ ] **Step 7: Build — must be clean**

```bash
cd /home/gndm/Projects/navilyrics && go build ./...
```

Expected: no errors.

- [ ] **Step 8: Full test suite**

```bash
cd /home/gndm/Projects/navilyrics && go test -v -race ./...
```

Expected: all tests pass.

- [ ] **Step 9: Format and vet**

```bash
cd /home/gndm/Projects/navilyrics && go fmt ./... && go vet ./...
```

- [ ] **Step 10: Smoke test — feature absent when GOSCRIBE_URL unset**

```bash
cd /home/gndm/Projects/navilyrics
make build
# Start server in background (requires .env or inline env vars)
NAVIDROME_URL=http://localhost:4533 NAVIDROME_USER=x NAVIDROME_PASS=x \
  MUSIC_DIR=/tmp ./navilyrics serve &
SERVER_PID=$!
sleep 1
# The /songs page should not contain "Transcribe" text when GOSCRIBE_URL is unset
RESULT=$(curl -s http://localhost:8080/songs 2>/dev/null | grep -c "Transcribe" || echo "0")
kill $SERVER_PID 2>/dev/null
echo "Transcribe occurrences (expect 0): $RESULT"
```

- [ ] **Step 11: Final commit**

```bash
cd /home/gndm/Projects/navilyrics
git add cmd/navilyrics/main.go
git commit -m "feat: wire goscribe transcription into serve and run commands"
```
