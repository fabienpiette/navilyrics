# Features Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add NetEase as a fallback lyrics source, a raw `.lrc` preview endpoint, and SSE live-progress for batch runs.

**Architecture:** Three independent features. NetEase is a new `pkg/netease/` package satisfying `LRCFetcher`; the processor gains a `fallback` field. LRC preview is a single new handler + Navidrome `GetSong`. SSE progress replaces the blocking `RunBatch`/`RunFiltered` with a background goroutine + channel drained by an SSE handler.

**Tech Stack:** Go stdlib `net/http` SSE (no library), HTMX 2.x `sse` extension, chi v5 URL params.

---

## Context you must understand before starting

### Key interfaces and types

`internal/lyrics/processor.go` — `LRCFetcher` interface:
```go
type LRCFetcher interface {
    Get(ctx context.Context, artist, title, album string, duration float64) (lrclib.Response, bool, error)
    Search(ctx context.Context, artist, title string, duration float64) (lrclib.Response, bool, error)
}
```

`Processor` struct fields: `nd *navidrome.Client`, `lrc LRCFetcher`, `musicDirs []string`, `dryRun bool`.

`Result.Source` is currently `"lrclib"` or `""`. We add `"netease"`.

`internal/lyrics/writer.go` — `lrcPathFor(audioPath string) string` is unexported; we expose it via the new `Processor.ResolveLRCPath` method in the same package.

`internal/handlers/songs.go` — `RunBatch` and `RunFiltered` currently block synchronously (collect all results into a slice, then render). We replace them with background goroutines.

`internal/handlers/handler.go` — `Handler` struct, `New(nd, proc, tmpls, partials, version)`. We add a `runs *RunStore` field.

Routes currently in `cmd/navilyrics/main.go`:
```
GET  /           → h.Dashboard
GET  /songs      → h.Songs
GET  /songs/rows → h.SongsRows
POST /run        → h.RunBatch
POST /run/filtered → h.RunFiltered
```

### SSE wire format (Go stdlib)

```go
w.Header().Set("Content-Type", "text/event-stream")
w.Header().Set("Cache-Control", "no-cache")
fmt.Fprintf(w, "event: result\ndata: <html fragment>\n\n")
w.(http.Flusher).Flush()
```

Multi-line `data:` lines are joined with `\n` by the browser. It is simpler to write one-line HTML per event. Escape `<`, `>`, `&` in user content before embedding in HTML.

### HTMX SSE extension

```html
<div hx-ext="sse" sse-connect="/run/{id}/events">
  <div id="run-log" sse-swap="result" hx-swap="beforeend"></div>
  <div id="run-summary" sse-swap="done" hx-swap="outerHTML"></div>
</div>
```

The `sse` extension is bundled in htmx via CDN (`/static/htmx.min.js` already included). Check that the existing `htmx.min.js` includes SSE support — if not, add `<script src="https://unpkg.com/htmx-ext-sse@2.2.2/sse.js" defer></script>` to `base.html`.

### Run commands

```
make test       # go test -v -race ./...
make build      # go build ./cmd/navilyrics
make vet        # go vet ./...
```

---

## Task 1: NetEase client

**Files:**
- Create: `pkg/netease/client.go`
- Create: `pkg/netease/client_test.go`

### Step 1: Write the failing test

```go
// pkg/netease/client_test.go
package netease_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/user/navilyrics/pkg/netease"
)

func TestSearch_found(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/search/get":
			json.NewEncoder(w).Encode(map[string]any{
				"result": map[string]any{
					"songs": []map[string]any{
						{"id": 123, "duration": 210000, "name": "Song", "artists": []map[string]any{{"name": "Artist"}}},
					},
				},
			})
		case "/api/song/lyric":
			json.NewEncoder(w).Encode(map[string]any{
				"lrc": map[string]any{"lyric": "[00:01.00] Hello world"},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	c := netease.New(srv.URL)
	resp, ok, err := c.Search(context.Background(), "Artist", "Song", 210.0)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("want found, got not found")
	}
	if resp.SyncedLyrics != "[00:01.00] Hello world" {
		t.Errorf("got SyncedLyrics %q", resp.SyncedLyrics)
	}
}

func TestSearch_durationMismatch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"result": map[string]any{
				"songs": []map[string]any{
					// duration 300s, target 210s — delta 90s > 5s tolerance
					{"id": 123, "duration": 300000, "name": "Song", "artists": []map[string]any{{"name": "Artist"}}},
				},
			},
		})
	}))
	defer srv.Close()

	c := netease.New(srv.URL)
	_, ok, err := c.Search(context.Background(), "Artist", "Song", 210.0)
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatal("want not found due to duration mismatch")
	}
}

func TestGet_delegatesToSearch(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/search/get" {
			calls++
		}
		json.NewEncoder(w).Encode(map[string]any{"result": map[string]any{"songs": []any{}}})
	}))
	defer srv.Close()

	c := netease.New(srv.URL)
	c.Get(context.Background(), "A", "T", "L", 200.0)
	if calls != 1 {
		t.Errorf("want 1 search call, got %d", calls)
	}
}
```

### Step 2: Run test to verify it fails

```
go test ./pkg/netease/... -v
```
Expected: FAIL — package does not exist yet.

### Step 3: Implement `pkg/netease/client.go`

```go
package netease

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/user/navilyrics/pkg/lrclib"
)

const defaultBaseURL = "https://music.163.com"

// Client is a NetEase Cloud Music API client.
type Client struct {
	baseURL    string
	httpClient *http.Client
}

// New creates a Client. Pass "" to use the default base URL.
func New(baseURL string) *Client {
	if baseURL == "" {
		baseURL = defaultBaseURL
	}
	return &Client{
		baseURL:    baseURL,
		httpClient: &http.Client{Timeout: 10 * time.Second},
	}
}

// Get delegates to Search (NetEase has no exact-match endpoint).
func (c *Client) Get(ctx context.Context, artist, title, album string, duration float64) (lrclib.Response, bool, error) {
	return c.Search(ctx, artist, title, duration)
}

// Search searches for a track and returns the lyrics if a duration match is found.
// Uses ±5 s tolerance (same as lrclib).
func (c *Client) Search(ctx context.Context, artist, title string, targetDuration float64) (lrclib.Response, bool, error) {
	id, dur, ok, err := c.searchSong(ctx, artist, title, targetDuration)
	if err != nil || !ok {
		return lrclib.Response{}, false, err
	}
	synced, err := c.fetchLyrics(ctx, id)
	if err != nil {
		return lrclib.Response{}, false, err
	}
	if synced == "" {
		return lrclib.Response{}, false, nil
	}
	return lrclib.Response{
		TrackName:    title,
		ArtistName:   artist,
		Duration:     dur,
		SyncedLyrics: synced,
	}, true, nil
}

type neSearchResp struct {
	Result struct {
		Songs []struct {
			ID       int64 `json:"id"`
			Duration int64 `json:"duration"` // milliseconds
		} `json:"songs"`
	} `json:"result"`
}

func (c *Client) searchSong(ctx context.Context, artist, title string, targetDuration float64) (id int64, dur float64, ok bool, err error) {
	body := url.Values{
		"s":      {artist + " " + title},
		"type":   {"1"},
		"limit":  {"10"},
		"offset": {"0"},
	}.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		c.baseURL+"/api/search/get", strings.NewReader(body))
	if err != nil {
		return 0, 0, false, err
	}
	c.setHeaders(req)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return 0, 0, false, fmt.Errorf("netease search: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return 0, 0, false, fmt.Errorf("netease search: status %d", resp.StatusCode)
	}

	var sr neSearchResp
	if err := json.NewDecoder(resp.Body).Decode(&sr); err != nil {
		return 0, 0, false, fmt.Errorf("netease search decode: %w", err)
	}

	const maxDelta = 5.0
	bestID, bestDur, bestDelta := int64(0), 0.0, math.MaxFloat64
	for _, s := range sr.Result.Songs {
		secs := float64(s.Duration) / 1000.0
		if d := math.Abs(secs - targetDuration); d < bestDelta && d <= maxDelta {
			bestDelta, bestID, bestDur = d, s.ID, secs
		}
	}
	if bestID == 0 {
		return 0, 0, false, nil
	}
	return bestID, bestDur, true, nil
}

type neLyricResp struct {
	Lrc struct{ Lyric string } `json:"lrc"`
}

func (c *Client) fetchLyrics(ctx context.Context, id int64) (synced string, err error) {
	q := url.Values{"id": {fmt.Sprintf("%d", id)}, "lv": {"-1"}, "kv": {"-1"}, "tv": {"-1"}}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		c.baseURL+"/api/song/lyric?"+q.Encode(), nil)
	if err != nil {
		return "", err
	}
	c.setHeaders(req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("netease lyrics: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("netease lyrics: status %d", resp.StatusCode)
	}

	var lr neLyricResp
	if err := json.NewDecoder(resp.Body).Decode(&lr); err != nil {
		return "", fmt.Errorf("netease lyrics decode: %w", err)
	}
	return strings.TrimSpace(lr.Lrc.Lyric), nil
}

func (c *Client) setHeaders(req *http.Request) {
	req.Header.Set("Referer", "https://music.163.com/")
	req.Header.Set("Cookie", "os=pc; appver=2.0.2")
	req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36")
}
```

### Step 4: Run tests to verify they pass

```
go test ./pkg/netease/... -v -race
```
Expected: all 3 tests PASS.

### Step 5: Commit

```
git add pkg/netease/
git commit -m "feat(netease): add NetEase Cloud Music lyrics client"
```

---

## Task 2: Processor fallback integration

**Files:**
- Modify: `internal/lyrics/processor.go`
- Modify: `internal/lyrics/processor_test.go`

### Step 1: Write the failing test

Add to `internal/lyrics/processor_test.go` (after existing tests):

```go
// stubFetcher is already defined in the test file (search for it).
// Add a second stub for the fallback and a test that uses it.

type stubFetcher struct {
    resp lrclib.Response
    ok   bool
    err  error
}

func (s *stubFetcher) Get(_ context.Context, _, _, _ string, _ float64) (lrclib.Response, bool, error) {
    return s.resp, s.ok, s.err
}
func (s *stubFetcher) Search(_ context.Context, _, _ string, _ float64) (lrclib.Response, bool, error) {
    return s.resp, s.ok, s.err
}

func TestProcessor_fallbackUsedWhenLrclibMisses(t *testing.T) {
    primary := &stubFetcher{ok: false}
    fallback := &stubFetcher{ok: true, resp: lrclib.Response{PlainLyrics: "fallback", SyncedLyrics: "[00:01.00] fallback"}}

    proc := NewProcessor(nil, primary, []string{"/music"}, true)
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
```

NOTE: Check the existing test file first — `stubFetcher` may already be defined. If so, skip redefining it. Add only the new `TestProcessor_fallbackUsedWhenLrclibMisses` test.

### Step 2: Run test to verify it fails

```
go test ./internal/lyrics/... -run TestProcessor_fallbackUsedWhenLrclibMisses -v
```
Expected: FAIL — `SetFallback` undefined.

### Step 3: Update `processor.go`

Add `fallback LRCFetcher` to `Processor` struct and expose a setter:

```go
type Processor struct {
    nd        *navidrome.Client
    lrc       LRCFetcher
    fallback  LRCFetcher // optional; nil = disabled
    musicDirs []string
    dryRun    bool
}

// SetFallback sets an optional secondary lyrics source tried after lrc fails.
func (p *Processor) SetFallback(f LRCFetcher) { p.fallback = f }
```

Update `ProcessSong` — replace the current `if !ok { r.Status = "not_found" ... }` block with:

```go
if !ok && p.fallback != nil {
    resp, ok, err = p.fallback.Search(ctx, song.Artist, song.Title, song.Duration)
    if err != nil {
        log.Printf("netease search %q: %v", song.Title, err)
    }
    if ok {
        r.Source = "netease"
    }
}

if !ok {
    log.Printf("[not_found] %s — %s", song.Artist, song.Title)
    r.Status = "not_found"
    return r
}

r.PlainLyrics = resp.PlainLyrics
r.SyncedLyrics = resp.SyncedLyrics
if r.Source == "" {
    r.Source = "lrclib"
}
```

(Remove the existing `r.Source = "lrclib"` line that comes after the `if !ok` block, since source assignment now happens inline.)

### Step 4: Run all lyrics tests

```
go test ./internal/lyrics/... -v -race
```
Expected: all tests PASS.

### Step 5: Commit

```
git add internal/lyrics/processor.go internal/lyrics/processor_test.go
git commit -m "feat(lyrics): add optional fallback LRCFetcher to processor"
```

---

## Task 3: Navidrome GetSong + Processor ResolveLRCPath

**Files:**
- Modify: `pkg/navidrome/songs.go`
- Modify: `pkg/navidrome/client_test.go`
- Modify: `internal/lyrics/processor.go`

### Step 1: Write the failing tests

Add to `pkg/navidrome/client_test.go`:

```go
func TestGetSong(t *testing.T) {
    srv := testServer(t) // uses the existing helper in the test file
    c := clientFor(srv)  // uses the existing helper

    song, err := c.GetSong(context.Background(), "abc123")
    if err != nil {
        t.Fatal(err)
    }
    if song.ID != "abc123" {
        t.Errorf("want id=abc123, got %q", song.ID)
    }
}
```

NOTE: Look at the existing `client_test.go` to find how `testServer` and `clientFor` are defined (or what the test helpers are called). The test server likely handles `/api/song` — add a case for `/api/song/abc123` returning `{"id":"abc123","title":"T","artist":"A","album":"L","duration":200}`.

For `ResolveLRCPath`, test it in `internal/lyrics/processor_test.go` (it's internal to the lyrics package, so test it there directly):

```go
func TestProcessor_ResolveLRCPath(t *testing.T) {
    dir := t.TempDir()
    // create a dummy mp3 so os.Stat finds it
    audioPath := filepath.Join(dir, "song.mp3")
    os.WriteFile(audioPath, []byte{}, 0644)

    proc := NewProcessor(nil, &stubFetcher{}, []string{dir}, false)
    got := proc.ResolveLRCPath("song.mp3")
    want := filepath.Join(dir, "song.lrc")
    if got != want {
        t.Errorf("want %q, got %q", want, got)
    }
}

func TestProcessor_ResolveLRCPath_notFound(t *testing.T) {
    proc := NewProcessor(nil, &stubFetcher{}, []string{"/nonexistent"}, false)
    if got := proc.ResolveLRCPath("song.mp3"); got != "" {
        t.Errorf("want empty, got %q", got)
    }
}
```

### Step 2: Run tests to verify they fail

```
go test ./pkg/navidrome/... ./internal/lyrics/... -run "TestGetSong|TestProcessor_ResolveLRCPath" -v
```
Expected: FAIL.

### Step 3: Add `GetSong` to `pkg/navidrome/songs.go`

```go
// GetSong fetches a single song by ID.
func (c *Client) GetSong(ctx context.Context, id string) (Song, error) {
    resp, err := c.Do(ctx, http.MethodGet, "/api/song/"+id, nil)
    if err != nil {
        return Song{}, fmt.Errorf("get song: %w", err)
    }
    defer resp.Body.Close()
    if resp.StatusCode == http.StatusNotFound {
        return Song{}, fmt.Errorf("song %s not found", id)
    }
    if resp.StatusCode != http.StatusOK {
        return Song{}, fmt.Errorf("get song: status %d", resp.StatusCode)
    }
    var s Song
    if err := json.NewDecoder(resp.Body).Decode(&s); err != nil {
        return Song{}, fmt.Errorf("get song decode: %w", err)
    }
    return s, nil
}
```

### Step 4: Add `ResolveLRCPath` to `internal/lyrics/processor.go`

```go
// ResolveLRCPath returns the full .lrc sidecar path for a relative song path,
// or "" if the audio file is not found in any music dir.
func (p *Processor) ResolveLRCPath(relPath string) string {
    audioPath := p.resolveAudioPath(relPath)
    if audioPath == "" {
        return ""
    }
    return lrcPathFor(audioPath)
}
```

### Step 5: Run all tests

```
go test ./pkg/navidrome/... ./internal/lyrics/... -v -race
```
Expected: all PASS.

### Step 6: Commit

```
git add pkg/navidrome/songs.go pkg/navidrome/client_test.go internal/lyrics/processor.go internal/lyrics/processor_test.go
git commit -m "feat(navidrome,lyrics): add GetSong and ResolveLRCPath"
```

---

## Task 4: `/songs/{id}/lrc` handler + route

**Files:**
- Create: `internal/handlers/lrc.go`
- Modify: `cmd/navilyrics/main.go`

No test file for this handler — the logic is thin (fetch song path, read file, serve). The integration is tested by running the server.

### Step 1: Create `internal/handlers/lrc.go`

```go
package handlers

import (
	"html"
	"net/http"
	"os"

	"github.com/go-chi/chi/v5"
)

// SongLRC serves the raw .lrc sidecar for a song as plain text.
// GET /songs/{id}/lrc
func (h *Handler) SongLRC(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	song, err := h.nd.GetSong(r.Context(), id)
	if err != nil {
		http.Error(w, html.EscapeString(err.Error()), http.StatusBadGateway)
		return
	}

	lrcPath := h.proc.ResolveLRCPath(song.Path)
	if lrcPath == "" {
		http.NotFound(w, r)
		return
	}

	data, err := os.ReadFile(lrcPath)
	if err != nil {
		if os.IsNotExist(err) {
			http.NotFound(w, r)
			return
		}
		http.Error(w, "read lrc: "+html.EscapeString(err.Error()), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Write(data)
}
```

### Step 2: Add route in `cmd/navilyrics/main.go`

In `runServer`, after the existing routes:
```go
r.Get("/songs/{id}/lrc", h.SongLRC)
```

### Step 3: Build and verify no errors

```
make build
```
Expected: PASS.

### Step 4: Commit

```
git add internal/handlers/lrc.go cmd/navilyrics/main.go
git commit -m "feat(handlers): add GET /songs/{id}/lrc endpoint"
```

---

## Task 5: RunStore + progress handler infrastructure

**Files:**
- Create: `internal/handlers/runstore.go`
- Modify: `internal/handlers/handler.go`

### Step 1: Create `internal/handlers/runstore.go`

```go
package handlers

import (
	"sync"

	"github.com/user/navilyrics/internal/lyrics"
)

// RunStore manages active batch runs keyed by a unique run ID.
// Each run sends Result values to its channel; the channel is closed when done.
type RunStore struct {
	mu   sync.Mutex
	runs map[string]chan lyrics.Result
}

func newRunStore() *RunStore {
	return &RunStore{runs: make(map[string]chan lyrics.Result)}
}

func (rs *RunStore) create(id string) chan lyrics.Result {
	ch := make(chan lyrics.Result, 64)
	rs.mu.Lock()
	rs.runs[id] = ch
	rs.mu.Unlock()
	return ch
}

func (rs *RunStore) get(id string) (chan lyrics.Result, bool) {
	rs.mu.Lock()
	ch, ok := rs.runs[id]
	rs.mu.Unlock()
	return ch, ok
}

func (rs *RunStore) delete(id string) {
	rs.mu.Lock()
	delete(rs.runs, id)
	rs.mu.Unlock()
}
```

### Step 2: Update `internal/handlers/handler.go`

Add `runs *RunStore` to `Handler`:

```go
type Handler struct {
    nd       *navidrome.Client
    proc     *lyrics.Processor
    tmpls    map[string]*template.Template
    partials map[string]*template.Template
    version  string
    runs     *RunStore
}
```

Update `New`:

```go
func New(nd *navidrome.Client, proc *lyrics.Processor, tmpls, partials map[string]*template.Template, version string) *Handler {
    return &Handler{nd: nd, proc: proc, tmpls: tmpls, partials: partials, version: version, runs: newRunStore()}
}
```

### Step 3: Build to check no errors

```
make build
```

### Step 4: Commit

```
git add internal/handlers/runstore.go internal/handlers/handler.go
git commit -m "feat(handlers): add RunStore for SSE run management"
```

---

## Task 6: SSE live-progress — template + handler + update RunBatch/RunFiltered

**Files:**
- Create: `web/templates/run_progress.html`
- Create: `internal/handlers/run_progress.go`
- Modify: `internal/handlers/songs.go` (replace RunBatch + RunFiltered)
- Modify: `web/templates/base.html` (add SSE extension script if needed)
- Modify: `cmd/navilyrics/main.go` (add SSE route)

### Step 1: Check htmx SSE support

Open `web/static/htmx.min.js` and search for `"sse"`. If the string is not present, the bundled HTMX does not include the SSE extension. In that case, add the following line to `web/templates/base.html` immediately after the existing `<script src="/static/htmx.min.js" defer></script>`:

```html
<script src="https://unpkg.com/htmx-ext-sse@2.2.2/sse.js" defer></script>
```

If `"sse"` is already present in `htmx.min.js`, skip this step.

### Step 2: Create `web/templates/run_progress.html`

```html
{{define "title"}}navilyrics — running…{{end}}

{{define "content"}}
<h2>Batch run</h2>

<div class="stats-grid" id="run-summary" style="margin-bottom:1.5rem">
    <div class="stat-cell"><div class="stat-num" id="cnt-found">—</div><div class="stat-label">found</div></div>
    <div class="stat-cell"><div class="stat-num" id="cnt-notfound">—</div><div class="stat-label">not found</div></div>
    <div class="stat-cell"><div class="stat-num" id="cnt-skipped">—</div><div class="stat-label">skipped</div></div>
    <div class="stat-cell"><div class="stat-num" id="cnt-errors">—</div><div class="stat-label">errors</div></div>
</div>

<div class="run-log"
     hx-ext="sse"
     sse-connect="/run/{{.RunID}}/events"
     id="run-log">
    <div sse-swap="result" hx-swap="beforeend" id="run-log-inner"></div>
    <div sse-swap="done" hx-swap="none" id="run-done-trigger"></div>
</div>

<div class="form-actions" style="margin-top:1rem">
    <a href="/" class="btn secondary">Dashboard</a>
    <a href="/songs" class="btn secondary">Songs</a>
</div>

<script>
document.addEventListener('htmx:sseMessage', function(e) {
    if (e.detail.type === 'done') {
        try {
            var d = JSON.parse(e.detail.data);
            document.getElementById('cnt-found').textContent    = d.found;
            document.getElementById('cnt-notfound').textContent = d.not_found;
            document.getElementById('cnt-skipped').textContent  = d.skipped;
            document.getElementById('cnt-errors').textContent   = d.errors;
        } catch(ex) {}
    }
});
</script>
{{end}}
```

### Step 3: Create `internal/handlers/run_progress.go`

```go
package handlers

import (
	"context"
	"fmt"
	"html"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/user/navilyrics/internal/lyrics"
	"github.com/user/navilyrics/pkg/navidrome"
)

type runProgressData struct {
	ActiveTab string
	Version   string
	RunID     string
}

// RunBatch starts a background run for all songs and returns a live-progress page.
func (h *Handler) RunBatch(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	id := fmt.Sprintf("%x", time.Now().UnixNano())
	ch := h.runs.create(id)
	go func() {
		defer close(ch)
		_ = h.proc.Run(context.Background(), func(res lyrics.Result) { ch <- res })
	}()
	h.render(w, "run_progress.html", runProgressData{ActiveTab: "songs", Version: h.version, RunID: id})
}

// RunFiltered starts a background run for matching songs and returns a live-progress page.
func (h *Handler) RunFiltered(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	query := r.FormValue("q")
	filter := r.FormValue("filter")
	switch filter {
	case "missing", "has":
	default:
		filter = "all"
	}

	songs, err := h.allMatchingSongs(r.Context(), query, filter)
	if err != nil {
		http.Error(w, "navidrome: "+err.Error(), http.StatusBadGateway)
		return
	}

	id := fmt.Sprintf("%x", time.Now().UnixNano())
	ch := h.runs.create(id)
	go func() {
		defer close(ch)
		_ = h.proc.RunSongs(context.Background(), songs, func(res lyrics.Result) { ch <- res })
	}()
	h.render(w, "run_progress.html", runProgressData{ActiveTab: "songs", Version: h.version, RunID: id})
}

// RunEvents streams per-song results for a run as Server-Sent Events.
// GET /run/{id}/events
func (h *Handler) RunEvents(w http.ResponseWriter, r *http.Request) {
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

	var found, notFound, skipped, errors int
	for res := range ch {
		line := resultLineHTML(res)
		fmt.Fprintf(w, "event: result\ndata: %s\n\n", line)
		flusher.Flush()
		switch res.Status {
		case "found", "dry_run":
			found++
		case "not_found":
			notFound++
		case "skipped":
			skipped++
		case "error":
			errors++
		}
	}

	summary := fmt.Sprintf(`{"found":%d,"not_found":%d,"skipped":%d,"errors":%d}`,
		found, notFound, skipped, errors)
	fmt.Fprintf(w, "event: done\ndata: %s\n\n", summary)
	flusher.Flush()
	h.runs.delete(id)
}

// resultLineHTML builds an HTML fragment for one run result line.
// All user-supplied strings are escaped to prevent XSS.
func resultLineHTML(r lyrics.Result) string {
	errPart := ""
	if r.Err != "" {
		errPart = ` <span class="muted">(` + html.EscapeString(r.Err) + `)</span>`
	}
	return fmt.Sprintf(
		`<div class="run-log-line"><span class="badge badge-%s">%s</span> <span class="muted">%s</span> — %s%s</div>`,
		html.EscapeString(r.Status), html.EscapeString(r.Status),
		html.EscapeString(r.Artist), html.EscapeString(r.Title), errPart,
	)
}

// allSongsForRun fetches all library songs for RunBatch (alias used in goroutine).
// This helper ensures the Navidrome call happens before the response is sent,
// so the goroutine only processes the already-fetched list.
func allSongsForBatch(ctx context.Context, nd interface {
	AllSongs(context.Context) ([]navidrome.Song, error)
}) ([]navidrome.Song, error) {
	return nd.AllSongs(ctx)
}
```

Note: `allSongsForBatch` helper is not needed — `proc.Run` calls `nd.AllSongs` internally. Remove it from the file. The goroutine for `RunBatch` just calls `h.proc.Run(context.Background(), ...)`.

### Step 4: Remove old RunBatch and RunFiltered from `internal/handlers/songs.go`

Delete the following from `songs.go`:
- `RunFiltered` function (lines ~204–254)
- `runResult` struct (lines ~256–264)
- `RunBatch` function (lines ~266–310)

They are now in `run_progress.go`. The `allMatchingSongs` helper stays in `songs.go` since `RunFiltered` in `run_progress.go` calls it.

### Step 5: Add route in `cmd/navilyrics/main.go`

```go
r.Get("/run/{id}/events", h.RunEvents)
```

Also ensure `run_progress.html` is parsed. Open `internal/handlers/handler.go` — `ParseTemplates` globs all `templates/*.html`, so `run_progress.html` will be picked up automatically. No change needed there.

### Step 6: Build and verify

```
make build && make vet
```
Expected: PASS with no errors.

### Step 7: Smoke-test manually

```
make run-server
```
Open browser at `http://localhost:8080/songs`, click **Run all**. Verify:
- Page immediately shows "Batch run" with `—` placeholders in the stats grid
- Log lines appear one by one as songs are processed
- Stats grid fills in with final counts when done

### Step 8: Commit

```
git add web/templates/run_progress.html internal/handlers/run_progress.go internal/handlers/songs.go web/templates/base.html cmd/navilyrics/main.go
git commit -m "feat(handlers): replace blocking run with SSE live-progress"
```

---

## Task 7: Wire NetEase into main.go

**Files:**
- Modify: `cmd/navilyrics/main.go`

### Step 1: Update both `runCLI` and `runServer`

In both functions, after constructing `lrc`, add:

```go
import "github.com/user/navilyrics/pkg/netease"

// ...
lrc := lrclib.New("https://lrclib.net")
ne  := netease.New("")           // uses default music.163.com
proc := lyrics.NewProcessor(nd, lrc, strings.Split(musicDir, ":"), *dryRun)
proc.SetFallback(ne)
```

(In `runServer` the musicDir variable is named `musicDir` too; `dryRun` is a plain `bool` there, not a pointer — adjust accordingly.)

### Step 2: Build and run all tests

```
make build && make test
```
Expected: all PASS.

### Step 3: Commit

```
git add cmd/navilyrics/main.go
git commit -m "feat(main): wire netease fallback into cli and server processors"
```

---

## Done

Run `make test && make build` one final time to confirm everything is green. The three features are complete:

- `GET /songs/{id}/lrc` serves the raw `.lrc` file
- `POST /run` / `POST /run/filtered` return immediately with a live-progress page
- NetEase is tried automatically when lrclib returns nothing
