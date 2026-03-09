# navilyrics Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Build a single-binary Go tool (`navilyrics`) that fetches missing lyrics from lrclib.net, writes `.lrc` sidecar files next to audio files, and embeds `LYRICS`/`SYNCEDLYRICS` tags — with both a `run` CLI batch mode and a `serve` HTMX web UI.

**Architecture:** Single binary (`cmd/navilyrics/main.go`) with two subcommands parsed via `os.Args`. `pkg/navidrome/` (Navidrome REST API client), `pkg/lrclib/` (lrclib.net client), and `pkg/tagger/` (audio tag read/write) are standalone reusable packages. `internal/lyrics/` ties them together with a worker-pool `Processor`. The web UI uses chi + HTMX 2.x with `html/template` embedded via `//go:embed`. Config comes from env vars only.

**Tech Stack:** Go 1.23, `github.com/go-chi/chi/v5`, HTMX 2.x (CDN), `html/template`, `github.com/bogem/id3v2/v2` (MP3 tagging), `github.com/mewkiz/flac` (FLAC Vorbis comments).

**Reference project:** `/home/gndm/Projects/navidrome-playlists/` — copy patterns exactly (Makefile, Dockerfile, CLAUDE.md, handler structure, template system, CSS design system).

---

## Task 1: Project Scaffold

**Files:**
- Create: `go.mod`
- Create: `Makefile`
- Create: `.gitignore`
- Create: `CLAUDE.md`
- Create: `.env.example`
- Create: `cmd/navilyrics/.gitkeep`, `pkg/navidrome/.gitkeep`, `pkg/lrclib/.gitkeep`, `pkg/tagger/.gitkeep`, `internal/lyrics/.gitkeep`, `internal/handlers/.gitkeep`, `web/templates/.gitkeep`, `web/static/.gitkeep`

**Step 1: Initialize Go module**
```bash
cd /home/gndm/Projects/navilyrics
go mod init github.com/user/navilyrics
```
Expected: `go.mod` created with `module github.com/user/navilyrics` and `go 1.23`.

**Step 2: Create Makefile**

```makefile
BINARY_NAME ?= navilyrics
BIN_DIR     ?= bin
VERSION     ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
LDFLAGS     := -ldflags="-s -w -X main.version=$(VERSION)"

.PHONY: build test test-coverage clean run-server run-cli fmt vet \
        build-all docker-build up down logs

## Build the binary
build:
	go build $(LDFLAGS) -o $(BINARY_NAME) ./cmd/navilyrics

## Run all tests with race detector
test:
	go test -v -race ./...

## Run tests and produce an HTML coverage report
test-coverage:
	go test -v -race -coverprofile=coverage.out ./...
	go tool cover -html=coverage.out -o coverage.html

## Remove build artifacts
clean:
	rm -f $(BINARY_NAME) coverage.out coverage.html
	rm -rf $(BIN_DIR)

## Run the web server locally (loads .env if present)
run-server:
	@set -a && [ -f .env ] && . ./.env; set +a && go run $(LDFLAGS) ./cmd/navilyrics serve

## Run the CLI batch processor in dry-run mode locally
run-cli:
	@set -a && [ -f .env ] && . ./.env; set +a && go run $(LDFLAGS) ./cmd/navilyrics run --dry-run

## Format all Go source files
fmt:
	go fmt ./...

## Run go vet
vet:
	go vet ./...

## Cross-compile for Linux amd64, macOS amd64/arm64, Windows amd64
build-all:
	mkdir -p $(BIN_DIR)
	GOOS=linux   GOARCH=amd64 CGO_ENABLED=0 go build $(LDFLAGS) -o $(BIN_DIR)/$(BINARY_NAME)-linux-amd64    ./cmd/navilyrics
	GOOS=darwin  GOARCH=amd64 CGO_ENABLED=0 go build $(LDFLAGS) -o $(BIN_DIR)/$(BINARY_NAME)-darwin-amd64   ./cmd/navilyrics
	GOOS=darwin  GOARCH=arm64 CGO_ENABLED=0 go build $(LDFLAGS) -o $(BIN_DIR)/$(BINARY_NAME)-darwin-arm64   ./cmd/navilyrics
	GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build $(LDFLAGS) -o $(BIN_DIR)/$(BINARY_NAME)-windows-amd64.exe ./cmd/navilyrics

## Build the Docker image
docker-build:
	docker build --build-arg VERSION=$(VERSION) -t $(BINARY_NAME):$(VERSION) -t $(BINARY_NAME):latest .

## Start the stack with Docker Compose
up:
	VERSION=$(VERSION) docker compose up -d --build

## Stop the stack
down:
	docker compose down

## Tail Docker Compose logs
logs:
	docker compose logs -f
```

**Step 3: Create `.gitignore`**
```
navilyrics
bin/
coverage.out
coverage.html
.env
*.lrc.tmp
```

**Step 4: Create `CLAUDE.md`**
```markdown
# CLAUDE.md

## Git Commit Style
- Conventional Commits: `<type>[scope]: <description>`
- Lowercase imperative, no trailing period, max 50 chars
- One-line only; no body unless necessary
- No Claude/AI attribution in commit messages

## Commands

```bash
make build          # build binary
make run-server     # run web UI locally (reads env from .env)
make run-cli        # run CLI batch in dry-run mode
make test           # go test -v -race ./...
make test-coverage  # produces coverage.html
make fmt            # gofmt -w
make vet            # go vet ./...
make build-all      # cross-compile
make docker-build
make up             # docker compose up -d --build
```

Required env vars: `NAVIDROME_URL`, `NAVIDROME_USER`, `NAVIDROME_PASS`, `MUSIC_DIR`.
Optional: `PORT` (default 8080), `DRY_RUN` (default false).

## Architecture

Single binary with two subcommands: `navilyrics serve` (web UI) and `navilyrics run [--dry-run]` (CLI batch).

`pkg/navidrome/` — Navidrome REST API client (JWT auth, song listing).
`pkg/lrclib/` — lrclib.net HTTP client (exact get + fuzzy search).
`pkg/tagger/` — Audio tag reader/writer (MP3 ID3v2, FLAC Vorbis comments).
`internal/lyrics/` — Processor: fetches lyrics, writes .lrc, embeds tags, worker pool.
`internal/handlers/` — HTTP handlers (chi, html/template).
`web/` — Embedded templates and static assets.

`pkg/` packages never import `internal/`. `internal/lyrics/` is the only package that imports multiple `pkg/` packages.
```

**Step 5: Create `.env.example`**
```
NAVIDROME_URL=http://localhost:4533
NAVIDROME_USER=admin
NAVIDROME_PASS=changeme
MUSIC_DIR=/music
PORT=8080
DRY_RUN=false
```

**Step 6: Create directory structure**
```bash
mkdir -p cmd/navilyrics pkg/navidrome pkg/lrclib pkg/tagger internal/lyrics internal/handlers web/templates/partials web/static
```

**Step 7: Commit**
```bash
git add go.mod Makefile .gitignore CLAUDE.md .env.example
git commit -m "chore: project scaffold"
```

---

## Task 2: pkg/navidrome — Client + Song Listing

**Files:**
- Create: `pkg/navidrome/client.go`
- Create: `pkg/navidrome/types.go`
- Create: `pkg/navidrome/songs.go`
- Create: `pkg/navidrome/client_test.go`

**Step 1: Write failing test for authentication**

```go
// pkg/navidrome/client_test.go
package navidrome_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/user/navilyrics/pkg/navidrome"
)

func TestAuthenticate(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/auth/login" {
			http.NotFound(w, r)
			return
		}
		json.NewEncoder(w).Encode(map[string]string{"token": "test-jwt"})
	}))
	defer srv.Close()

	c := navidrome.New(srv.URL+"/api", "user", "pass")
	if err := c.Authenticate(); err != nil {
		t.Fatalf("Authenticate: %v", err)
	}
}

func TestAllSongs(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/auth/login":
			json.NewEncoder(w).Encode(map[string]string{"token": "tok"})
		case "/api/song":
			calls++
			start := r.URL.Query().Get("_start")
			if start == "0" {
				json.NewEncoder(w).Encode([]navidrome.Song{
					{ID: "1", Title: "Song A", HasLyrics: false},
					{ID: "2", Title: "Song B", HasLyrics: true},
				})
			} else {
				json.NewEncoder(w).Encode([]navidrome.Song{})
			}
		}
	}))
	defer srv.Close()

	c := navidrome.New(srv.URL+"/api", "user", "pass")
	_ = c.Authenticate()
	songs, err := c.AllSongs(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(songs) != 2 {
		t.Fatalf("want 2 songs, got %d", len(songs))
	}
}
```

**Step 2: Run test to verify it fails**
```bash
go test ./pkg/navidrome/ -v -run TestAuthenticate
```
Expected: compile error (package doesn't exist yet).

**Step 3: Create `pkg/navidrome/types.go`**

```go
package navidrome

// Song represents a Navidrome track.
type Song struct {
	ID        string  `json:"id"`
	Title     string  `json:"title"`
	Artist    string  `json:"artist"`
	Album     string  `json:"album"`
	Duration  float64 `json:"duration"`
	Path      string  `json:"path"`       // absolute path on Navidrome's filesystem
	HasLyrics bool    `json:"hasLyrics"`  // true if Navidrome found any lyrics
}

type authRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type authResponse struct {
	Token string `json:"token"`
}
```

**Step 4: Create `pkg/navidrome/client.go`**

Copy `/home/gndm/Projects/navidrome-playlists/pkg/navidrome/client.go` verbatim, then change the package import path comment at top to reflect this project. The only change needed is the package declaration stays `package navidrome` — no other changes required since the client is identical.

**Step 5: Create `pkg/navidrome/songs.go`**

```go
package navidrome

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
)

const songPageSize = 500

// AllSongs fetches every song in the Navidrome library, paginating automatically.
func (c *Client) AllSongs(ctx context.Context) ([]Song, error) {
	var all []Song
	for start := 0; ; start += songPageSize {
		q := url.Values{
			"_start": []string{strconv.Itoa(start)},
			"_end":   []string{strconv.Itoa(start + songPageSize)},
			"_sort":  []string{"title"},
			"_order": []string{"ASC"},
		}
		resp, err := c.Do(ctx, http.MethodGet, "/api/song?"+q.Encode(), nil)
		if err != nil {
			return nil, fmt.Errorf("list songs (start=%d): %w", start, err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("list songs: status %d", resp.StatusCode)
		}
		var page []Song
		if err := json.NewDecoder(resp.Body).Decode(&page); err != nil {
			return nil, fmt.Errorf("decode songs: %w", err)
		}
		all = append(all, page...)
		if len(page) < songPageSize {
			break
		}
	}
	return all, nil
}

// TriggerScan requests a Navidrome library rescan.
func (c *Client) TriggerScan(ctx context.Context) error {
	resp, err := c.Do(ctx, http.MethodGet, "/api/scanner/trigger", nil)
	if err != nil {
		return fmt.Errorf("trigger scan: %w", err)
	}
	defer resp.Body.Close()
	return nil
}
```

**Step 6: Run tests**
```bash
go test ./pkg/navidrome/ -v -race
```
Expected: PASS for both TestAuthenticate and TestAllSongs.

**Step 7: Commit**
```bash
git add pkg/navidrome/
git commit -m "feat(navidrome): add client and song listing with pagination"
```

---

## Task 3: pkg/lrclib — API Client

**Files:**
- Create: `pkg/lrclib/client.go`
- Create: `pkg/lrclib/client_test.go`

**Step 1: Write failing tests**

```go
// pkg/lrclib/client_test.go
package lrclib_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/user/navilyrics/pkg/lrclib"
)

func TestGet_found(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/get" {
			http.NotFound(w, r)
			return
		}
		json.NewEncoder(w).Encode(lrclib.Response{
			TrackName:    "Bohemian Rhapsody",
			ArtistName:   "Queen",
			PlainLyrics:  "Is this the real life?",
			SyncedLyrics: "[00:01.00] Is this the real life?",
		})
	}))
	defer srv.Close()

	c := lrclib.New(srv.URL)
	got, ok, err := c.Get(context.Background(), "Queen", "Bohemian Rhapsody", "A Night at the Opera", 354)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("want found, got not found")
	}
	if got.PlainLyrics != "Is this the real life?" {
		t.Errorf("unexpected lyrics: %q", got.PlainLyrics)
	}
}

func TestGet_notFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer srv.Close()

	c := lrclib.New(srv.URL)
	_, ok, err := c.Get(context.Background(), "Unknown", "Unknown", "", 0)
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatal("want not found")
	}
}

func TestSearch_picksBestDurationMatch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/search" {
			http.NotFound(w, r)
			return
		}
		json.NewEncoder(w).Encode([]lrclib.Response{
			{TrackName: "Song", ArtistName: "Artist", Duration: 300, PlainLyrics: "wrong"},
			{TrackName: "Song", ArtistName: "Artist", Duration: 182, PlainLyrics: "correct"},
		})
	}))
	defer srv.Close()

	c := lrclib.New(srv.URL)
	got, ok, err := c.Search(context.Background(), "Artist", "Song", 180)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("want found")
	}
	if got.PlainLyrics != "correct" {
		t.Errorf("unexpected lyrics: %q", got.PlainLyrics)
	}
}
```

**Step 2: Run test to verify it fails**
```bash
go test ./pkg/lrclib/ -v
```
Expected: compile error.

**Step 3: Create `pkg/lrclib/client.go`**

```go
package lrclib

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

const defaultBaseURL = "https://lrclib.net"

// Response is a lrclib.net track result.
type Response struct {
	ID           int     `json:"id"`
	TrackName    string  `json:"trackName"`
	ArtistName   string  `json:"artistName"`
	AlbumName    string  `json:"albumName"`
	Duration     float64 `json:"duration"`
	Instrumental bool    `json:"instrumental"`
	PlainLyrics  string  `json:"plainLyrics"`
	SyncedLyrics string  `json:"syncedLyrics"`
}

// Client is a lrclib.net API client.
type Client struct {
	baseURL    string
	httpClient *http.Client
}

// New creates a Client. Pass "" for baseURL to use the default (https://lrclib.net).
func New(baseURL string) *Client {
	if baseURL == "" {
		baseURL = defaultBaseURL
	}
	return &Client{
		baseURL:    baseURL,
		httpClient: &http.Client{Timeout: 10 * time.Second},
	}
}

// Get performs an exact-match lookup by artist, title, album, and duration (seconds).
// Returns (result, true, nil) if found, (zero, false, nil) if 404, or (zero, false, err) on error.
func (c *Client) Get(ctx context.Context, artist, title, album string, duration float64) (Response, bool, error) {
	q := url.Values{
		"artist_name": []string{artist},
		"track_name":  []string{title},
		"album_name":  []string{album},
		"duration":    []string{strconv.FormatFloat(duration, 'f', 0, 64)},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/api/get?"+q.Encode(), nil)
	if err != nil {
		return Response{}, false, err
	}
	req.Header.Set("User-Agent", "navilyrics/1.0 (https://github.com/user/navilyrics)")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return Response{}, false, fmt.Errorf("lrclib get: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return Response{}, false, nil
	}
	if resp.StatusCode != http.StatusOK {
		return Response{}, false, fmt.Errorf("lrclib get: status %d", resp.StatusCode)
	}

	var r Response
	if err := json.NewDecoder(resp.Body).Decode(&r); err != nil {
		return Response{}, false, fmt.Errorf("lrclib get decode: %w", err)
	}
	return r, true, nil
}

// Search performs a fuzzy search and picks the result whose duration is closest
// to targetDuration (within ±5 seconds). Returns (zero, false, nil) if no match.
func (c *Client) Search(ctx context.Context, artist, title string, targetDuration float64) (Response, bool, error) {
	q := url.Values{
		"artist_name": []string{artist},
		"track_name":  []string{title},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/api/search?"+q.Encode(), nil)
	if err != nil {
		return Response{}, false, err
	}
	req.Header.Set("User-Agent", "navilyrics/1.0 (https://github.com/user/navilyrics)")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return Response{}, false, fmt.Errorf("lrclib search: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return Response{}, false, fmt.Errorf("lrclib search: status %d", resp.StatusCode)
	}

	var results []Response
	if err := json.NewDecoder(resp.Body).Decode(&results); err != nil {
		return Response{}, false, fmt.Errorf("lrclib search decode: %w", err)
	}

	const maxDelta = 5.0
	best := Response{}
	bestDelta := math.MaxFloat64
	for _, r := range results {
		delta := math.Abs(r.Duration - targetDuration)
		if delta < bestDelta && delta <= maxDelta {
			bestDelta = delta
			best = r
		}
	}
	if bestDelta == math.MaxFloat64 {
		return Response{}, false, nil
	}
	return best, true, nil
}
```

**Step 4: Run tests**
```bash
go test ./pkg/lrclib/ -v -race
```
Expected: PASS.

**Step 5: Commit**
```bash
git add pkg/lrclib/
git commit -m "feat(lrclib): add lrclib.net API client with exact get and fuzzy search"
```

---

## Task 4: pkg/tagger — Interface + MP3 Implementation

**Files:**
- Create: `pkg/tagger/tagger.go`
- Create: `pkg/tagger/mp3.go`
- Create: `pkg/tagger/mp3_test.go`
- Create: `pkg/tagger/testdata/sample.mp3` (fixture — see step 1)

**Step 1: Add dependencies**
```bash
go get github.com/bogem/id3v2/v2
```

**Step 2: Write failing test for MP3 tagger**

For the test fixture, create a minimal valid MP3 file programmatically using id3v2:

```go
// pkg/tagger/mp3_test.go
package tagger_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/user/navilyrics/pkg/tagger"
)

func TestMP3Tagger_RoundTrip(t *testing.T) {
	// Create a temp copy of the sample MP3 (never mutate testdata)
	src, err := os.ReadFile("testdata/sample.mp3")
	if err != nil {
		t.Fatalf("read sample.mp3: %v (run: make testdata)", err)
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
```

**Step 3: Create `pkg/tagger/tagger.go`**

```go
package tagger

import (
	"fmt"
	"path/filepath"
	"strings"
)

// Tagger can read and write LYRICS and SYNCEDLYRICS tags in an audio file.
type Tagger interface {
	// ReadLyrics returns the plain and synced lyrics stored in the file.
	// Returns ("", "", nil) if no lyrics are present.
	ReadLyrics(path string) (plain, synced string, err error)

	// WriteLyrics writes the plain and synced lyrics to the file.
	// An empty string for either value means "don't touch that tag".
	WriteLyrics(path, plain, synced string) error
}

// ForFile returns the appropriate Tagger for the given file based on extension.
func ForFile(path string) (Tagger, error) {
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".mp3":
		return &mp3Tagger{}, nil
	case ".flac":
		return &flacTagger{}, nil
	default:
		return nil, fmt.Errorf("unsupported audio format: %q", ext)
	}
}
```

**Step 4: Create `pkg/tagger/mp3.go`**

```go
package tagger

import (
	"fmt"
	"strings"

	id3 "github.com/bogem/id3v2/v2"
)

type mp3Tagger struct{}

func (t *mp3Tagger) ReadLyrics(path string) (plain, synced string, err error) {
	tag, err := id3.Open(path, id3.Options{Parse: true})
	if err != nil {
		return "", "", fmt.Errorf("mp3 open %s: %w", path, err)
	}
	defer tag.Close()

	// USLT frame holds unsynced (plain) lyrics
	frames := tag.GetFrames(tag.CommonID("Unsynchronised lyrics/text transcription"))
	for _, f := range frames {
		if ulf, ok := f.(id3.UnsynchronisedLyricsFrame); ok {
			plain = ulf.Lyrics
			break
		}
	}

	// SYLT frame holds synced lyrics — stored as comment for portability
	// We use a TXXX frame with description "SYNCEDLYRICS" to store the LRC string.
	for _, f := range tag.GetFrames("TXXX") {
		if tf, ok := f.(id3.UserDefinedTextFrame); ok {
			if strings.EqualFold(tf.Description, "SYNCEDLYRICS") {
				synced = tf.Value
				break
			}
		}
	}
	return plain, synced, nil
}

func (t *mp3Tagger) WriteLyrics(path, plain, synced string) error {
	tag, err := id3.Open(path, id3.Options{Parse: true})
	if err != nil {
		return fmt.Errorf("mp3 open %s: %w", path, err)
	}
	defer tag.Close()

	if plain != "" {
		tag.DeleteFrames(tag.CommonID("Unsynchronised lyrics/text transcription"))
		tag.AddUnsynchronisedLyricsFrame(id3.UnsynchronisedLyricsFrame{
			Encoding: id3.EncodingUTF8,
			Language: "eng",
			Lyrics:   plain,
		})
	}

	if synced != "" {
		// Remove any existing SYNCEDLYRICS TXXX frame before adding new one
		existing := tag.GetFrames("TXXX")
		tag.DeleteFrames("TXXX")
		for _, f := range existing {
			if tf, ok := f.(id3.UserDefinedTextFrame); ok {
				if !strings.EqualFold(tf.Description, "SYNCEDLYRICS") {
					tag.AddFrame("TXXX", tf)
				}
			}
		}
		tag.AddFrame("TXXX", id3.UserDefinedTextFrame{
			Encoding:    id3.EncodingUTF8,
			Description: "SYNCEDLYRICS",
			Value:       synced,
		})
	}

	if err := tag.Save(); err != nil {
		return fmt.Errorf("mp3 save %s: %w", path, err)
	}
	return nil
}
```

**Step 5: Create a minimal MP3 fixture**

Create `pkg/tagger/testdata/` and add a 1-second silent MP3 fixture. The simplest way is to use a Go test helper that generates a minimal ID3-tagged file:

```bash
mkdir -p pkg/tagger/testdata
```

Then create `pkg/tagger/testdata_generate_test.go`:

```go
//go:build ignore

package main

import (
	"os"
	id3 "github.com/bogem/id3v2/v2"
)

func main() {
	// Create a minimal MP3 file with ID3 header only (no audio frames — enough for tag testing)
	f, _ := os.Create("testdata/sample.mp3")
	defer f.Close()
	// Write minimal ID3v2 header + empty MP3 frame
	// ID3v2 header: "ID3" + version 2.3.0 + flags 0x00 + syncsafe size 0
	f.Write([]byte{0x49, 0x44, 0x33, 0x03, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00})
	// Minimal MP3 frame (silence): sync word + MPEG1 Layer3 128kbps 44100Hz stereo
	f.Write([]byte{0xFF, 0xFB, 0x90, 0x00})
	_ = id3.Open // ensure dependency
}
```

Actually, the simplest approach: create the fixture with bogem/id3v2 itself in a TestMain:

```go
// pkg/tagger/mp3_test.go — add TestMain
func TestMain(m *testing.M) {
	// Create a minimal MP3 fixture for tests
	if err := os.MkdirAll("testdata", 0755); err != nil {
		panic(err)
	}
	// Write a bare-minimum MP3: ID3v2 header + one silent MP3 frame
	data := []byte{
		// ID3v2.3 header: "ID3", version 2.3.0, flags 0, size 0 (syncsafe)
		0x49, 0x44, 0x33, 0x03, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
		// A minimal MPEG1 Layer3 frame (128kbps, 44100Hz, stereo, no padding)
		0xFF, 0xFB, 0x90, 0x00,
	}
	// Pad to 417 bytes (one full MP3 frame at 128kbps)
	data = append(data, make([]byte, 417-len(data))...)
	if err := os.WriteFile("testdata/sample.mp3", data, 0644); err != nil {
		panic(err)
	}
	os.Exit(m.Run())
}
```

**Step 6: Run tests**
```bash
go test ./pkg/tagger/ -v -race -run TestMP3
```
Expected: PASS.

**Step 7: Commit**
```bash
git add pkg/tagger/
git commit -m "feat(tagger): add Tagger interface and MP3 implementation"
```

---

## Task 5: pkg/tagger — FLAC Implementation

**Files:**
- Create: `pkg/tagger/flac.go`
- Create: `pkg/tagger/flac_test.go`

**Step 1: Add FLAC dependency**
```bash
go get github.com/mewkiz/flac
```

**Step 2: Write failing test**

```go
// pkg/tagger/flac_test.go
package tagger_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/user/navilyrics/pkg/tagger"
)

func TestFLACTagger_RoundTrip(t *testing.T) {
	src, err := os.ReadFile("testdata/sample.flac")
	if err != nil {
		t.Skipf("no FLAC fixture: %v", err)
	}
	tmp := filepath.Join(t.TempDir(), "test.flac")
	if err := os.WriteFile(tmp, src, 0644); err != nil {
		t.Fatal(err)
	}

	tgr, err := tagger.ForFile(tmp)
	if err != nil {
		t.Fatal(err)
	}

	const plain = "FLAC line one\nFLAC line two"
	const synced = "[00:01.00] FLAC line one\n[00:02.00] FLAC line two"

	if err := tgr.WriteLyrics(tmp, plain, synced); err != nil {
		t.Fatalf("WriteLyrics: %v", err)
	}

	tgr2, _ := tagger.ForFile(tmp)
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
```

**Step 3: Create `pkg/tagger/flac.go`**

FLAC Vorbis comments are stored as `KEY=VALUE` pairs in the VORBIS_COMMENT metadata block. The keys `LYRICS` and `SYNCEDLYRICS` are the standard tags.

```go
package tagger

import (
	"fmt"
	"os"
	"strings"

	"github.com/mewkiz/flac"
	"github.com/mewkiz/flac/meta"
)

type flacTagger struct{}

func (t *flacTagger) ReadLyrics(path string) (plain, synced string, err error) {
	f, err := flac.ParseFile(path)
	if err != nil {
		return "", "", fmt.Errorf("flac parse %s: %w", path, err)
	}
	for _, block := range f.Blocks {
		vc, ok := block.Body.(*meta.VorbisComment)
		if !ok {
			continue
		}
		for _, tag := range vc.Tags {
			key, val, found := strings.Cut(tag, "=")
			if !found {
				continue
			}
			switch strings.ToUpper(key) {
			case "LYRICS":
				plain = val
			case "SYNCEDLYRICS":
				synced = val
			}
		}
	}
	return plain, synced, nil
}

func (t *flacTagger) WriteLyrics(path, plain, synced string) error {
	f, err := flac.ParseFile(path)
	if err != nil {
		return fmt.Errorf("flac parse %s: %w", path, err)
	}

	// Find or create VorbisComment block
	var vc *meta.VorbisComment
	for _, block := range f.Blocks {
		if v, ok := block.Body.(*meta.VorbisComment); ok {
			vc = v
			break
		}
	}
	if vc == nil {
		vc = &meta.VorbisComment{}
		f.Blocks = append(f.Blocks, &meta.Block{Header: &meta.BlockHeader{Type: meta.TypeVorbisComment}, Body: vc})
	}

	// Remove existing LYRICS / SYNCEDLYRICS tags, then add new ones
	filtered := vc.Tags[:0]
	for _, tag := range vc.Tags {
		key, _, found := strings.Cut(tag, "=")
		if !found {
			filtered = append(filtered, tag)
			continue
		}
		upper := strings.ToUpper(key)
		if upper != "LYRICS" && upper != "SYNCEDLYRICS" {
			filtered = append(filtered, tag)
		}
	}
	if plain != "" {
		filtered = append(filtered, "LYRICS="+plain)
	}
	if synced != "" {
		filtered = append(filtered, "SYNCEDLYRICS="+synced)
	}
	vc.Tags = filtered

	// Write to temp file then rename (atomic)
	tmp := path + ".tmp"
	out, err := os.Create(tmp)
	if err != nil {
		return fmt.Errorf("flac create tmp: %w", err)
	}
	if err := f.Write(out); err != nil {
		out.Close()
		os.Remove(tmp)
		return fmt.Errorf("flac write %s: %w", path, err)
	}
	out.Close()
	return os.Rename(tmp, path)
}
```

**Step 4: Generate a minimal FLAC fixture**

The easiest way is to use `go generate` with `ffmpeg` or include a pre-generated binary fixture. Since we can't assume ffmpeg, generate the fixture in `TestMain` using the `mewkiz/flac` encoder:

Add to `mp3_test.go` TestMain (or create a new `testmain_test.go`):

```go
// pkg/tagger/testmain_test.go
package tagger_test

import (
	"os"
	"testing"

	"github.com/mewkiz/flac"
	"github.com/mewkiz/flac/frame"
	"github.com/mewkiz/flac/meta"
)

func TestMain(m *testing.M) {
	if err := os.MkdirAll("testdata", 0755); err != nil {
		panic(err)
	}
	generateMP3Fixture()
	generateFLACFixture()
	os.Exit(m.Run())
}

func generateMP3Fixture() {
	data := []byte{
		0x49, 0x44, 0x33, 0x03, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
		0xFF, 0xFB, 0x90, 0x00,
	}
	data = append(data, make([]byte, 417-len(data))...)
	_ = os.WriteFile("testdata/sample.mp3", data, 0644)
}

func generateFLACFixture() {
	// Minimal FLAC: StreamInfo + one silent frame
	enc, err := flac.NewEncoder("testdata/sample.flac", &meta.StreamInfo{
		BlockSizeMin:  4096,
		BlockSizeMax:  4096,
		SampleRate:    44100,
		NChannels:     2,
		BitsPerSample: 16,
	})
	if err != nil {
		panic(err)
	}
	// One silent frame (all zeros)
	samples := make([]int32, 4096*2)
	f := &frame.Frame{
		Header: frame.Header{
			HasFixedBlockSize: true,
			BlockSize:         4096,
			SampleRate:        44100,
			Channels:          frame.ChannelsLR,
			BitsPerSample:     16,
		},
		Subframes: []*frame.Subframe{
			{Header: frame.SubHeader{Pred: frame.PredConstant}, Samples: samples[:4096]},
			{Header: frame.SubHeader{Pred: frame.PredConstant}, Samples: samples[4096:]},
		},
	}
	if err := enc.WriteFrame(f); err != nil {
		panic(err)
	}
	enc.Close()
}
```

**Step 5: Run tests**
```bash
go test ./pkg/tagger/ -v -race
```
Expected: PASS for both MP3 and FLAC round-trip tests.

**Step 6: Commit**
```bash
git add pkg/tagger/
git commit -m "feat(tagger): add FLAC Vorbis comment implementation"
```

---

## Task 6: internal/lyrics — Processor

**Files:**
- Create: `internal/lyrics/processor.go`
- Create: `internal/lyrics/processor_test.go`

**Step 1: Write failing tests**

```go
// internal/lyrics/processor_test.go
package lyrics_test

import (
	"context"
	"testing"

	"github.com/user/navilyrics/internal/lyrics"
	"github.com/user/navilyrics/pkg/lrclib"
	"github.com/user/navilyrics/pkg/navidrome"
)

// stubLRCLib implements a fake lrclib that always returns a fixed result.
type stubLRCLib struct {
	response lrclib.Response
	found    bool
}

func (s *stubLRCLib) Get(ctx context.Context, artist, title, album string, duration float64) (lrclib.Response, bool, error) {
	return s.response, s.found, nil
}

func (s *stubLRCLib) Search(ctx context.Context, artist, title string, duration float64) (lrclib.Response, bool, error) {
	return s.response, s.found, nil
}

func TestProcessor_skipsHasLyrics(t *testing.T) {
	p := lyrics.NewProcessor(nil, &stubLRCLib{found: true}, "/music", true)
	song := navidrome.Song{ID: "1", HasLyrics: true, Path: "/music/song.mp3"}
	result := p.ProcessSong(context.Background(), song)
	if result.Status != "skipped" {
		t.Errorf("want skipped, got %q", result.Status)
	}
}

func TestProcessor_fetchesWhenMissing(t *testing.T) {
	stub := &stubLRCLib{
		found: true,
		response: lrclib.Response{
			PlainLyrics:  "Line one",
			SyncedLyrics: "[00:01.00] Line one",
		},
	}
	p := lyrics.NewProcessor(nil, stub, "/music", true /* dry run */)
	song := navidrome.Song{ID: "2", Title: "Song", Artist: "Artist", HasLyrics: false, Path: "/music/song.mp3"}
	result := p.ProcessSong(context.Background(), song)
	if result.Status != "dry_run" {
		t.Errorf("want dry_run, got %q", result.Status)
	}
	if result.PlainLyrics != "Line one" {
		t.Errorf("unexpected lyrics: %q", result.PlainLyrics)
	}
}

func TestProcessor_notFound(t *testing.T) {
	p := lyrics.NewProcessor(nil, &stubLRCLib{found: false}, "/music", false)
	song := navidrome.Song{ID: "3", HasLyrics: false, Path: "/music/song.flac"}
	result := p.ProcessSong(context.Background(), song)
	if result.Status != "not_found" {
		t.Errorf("want not_found, got %q", result.Status)
	}
}
```

**Step 2: Run to verify it fails**
```bash
go test ./internal/lyrics/ -v
```
Expected: compile error.

**Step 3: Create `internal/lyrics/processor.go`**

```go
package lyrics

import (
	"context"
	"log"
	"sync"

	"github.com/user/navilyrics/pkg/lrclib"
	"github.com/user/navilyrics/pkg/navidrome"
	"github.com/user/navilyrics/pkg/tagger"
)

// LRCFetcher abstracts lrclib lookups (makes testing easier).
type LRCFetcher interface {
	Get(ctx context.Context, artist, title, album string, duration float64) (lrclib.Response, bool, error)
	Search(ctx context.Context, artist, title string, duration float64) (lrclib.Response, bool, error)
}

// Result holds the outcome of processing a single song.
type Result struct {
	SongID       string
	SongPath     string
	Title        string
	Artist       string
	PlainLyrics  string
	SyncedLyrics string
	Source       string // "lrclib" | ""
	Status       string // "found" | "not_found" | "skipped" | "error" | "dry_run"
	Err          string
}

// Processor fetches and writes lyrics for Navidrome songs.
type Processor struct {
	nd       *navidrome.Client // may be nil in tests
	lrc      LRCFetcher
	musicDir string
	dryRun   bool
}

// NewProcessor creates a Processor. nd may be nil when using ProcessSong directly.
func NewProcessor(nd *navidrome.Client, lrc LRCFetcher, musicDir string, dryRun bool) *Processor {
	return &Processor{nd: nd, lrc: lrc, musicDir: musicDir, dryRun: dryRun}
}

// ProcessSong fetches and writes lyrics for a single song.
func (p *Processor) ProcessSong(ctx context.Context, song navidrome.Song) Result {
	r := Result{
		SongID:   song.ID,
		SongPath: song.Path,
		Title:    song.Title,
		Artist:   song.Artist,
	}

	if song.HasLyrics {
		r.Status = "skipped"
		return r
	}

	// Strategy 1: exact get
	resp, ok, err := p.lrc.Get(ctx, song.Artist, song.Title, song.Album, song.Duration)
	if err != nil {
		log.Printf("lrclib get %q: %v", song.Title, err)
	}
	// Strategy 2: fuzzy search if exact failed
	if !ok {
		resp, ok, err = p.lrc.Search(ctx, song.Artist, song.Title, song.Duration)
		if err != nil {
			log.Printf("lrclib search %q: %v", song.Title, err)
		}
	}

	if !ok {
		r.Status = "not_found"
		return r
	}

	r.PlainLyrics = resp.PlainLyrics
	r.SyncedLyrics = resp.SyncedLyrics
	r.Source = "lrclib"

	if p.dryRun {
		r.Status = "dry_run"
		return r
	}

	if err := writeLyrics(song.Path, r.PlainLyrics, r.SyncedLyrics); err != nil {
		r.Status = "error"
		r.Err = err.Error()
		return r
	}

	r.Status = "found"
	return r
}

// Run processes all songs from Navidrome with a worker pool of 4.
// progress is called once per result (from any goroutine).
func (p *Processor) Run(ctx context.Context, progress func(Result)) error {
	songs, err := p.nd.AllSongs(ctx)
	if err != nil {
		return err
	}

	const workers = 4
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
				result := p.ProcessSong(ctx, song)
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
			break
		}
	}
	close(jobs)
	wg.Wait()
	return ctx.Err()
}
```

**Step 4: Run tests**
```bash
go test ./internal/lyrics/ -v -race
```
Expected: PASS (writeLyrics stub will be needed — see next task).

Note: `writeLyrics` is defined in `writer.go` (next task). For the tests to compile now, add a stub:

```go
// internal/lyrics/writer.go — temporary stub, replaced in Task 7
package lyrics

func writeLyrics(path, plain, synced string) error { return nil }
```

**Step 5: Commit**
```bash
git add internal/lyrics/
git commit -m "feat(lyrics): add Processor with worker pool and dry-run support"
```

---

## Task 7: internal/lyrics — Writer

**Files:**
- Modify: `internal/lyrics/writer.go` (replace stub)
- Create: `internal/lyrics/writer_test.go`

**Step 1: Write failing tests**

```go
// internal/lyrics/writer_test.go
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
	// Create a placeholder audio file
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
```

**Step 2: Run to verify it fails**
```bash
go test ./internal/lyrics/ -v -run TestWriteLRC
```
Expected: FAIL (WriteLRCFile undefined).

**Step 3: Replace `internal/lyrics/writer.go`**

```go
package lyrics

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/user/navilyrics/pkg/tagger"
)

// WriteLRCFile writes the synced lyrics as a .lrc sidecar next to the audio file.
// Uses atomic write (temp file → rename) to avoid corrupt files on failure.
func WriteLRCFile(audioPath, synced string) error {
	if synced == "" {
		return nil
	}
	lrcPath := lrcPathFor(audioPath)
	tmp := lrcPath + ".tmp"
	if err := os.WriteFile(tmp, []byte(synced), 0644); err != nil {
		return fmt.Errorf("write lrc tmp: %w", err)
	}
	if err := os.Rename(tmp, lrcPath); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("rename lrc: %w", err)
	}
	return nil
}

// writeLyrics writes both the .lrc sidecar and the embedded audio tags.
// On any error it rolls back (removes .lrc if it was just written) and returns the error.
func writeLyrics(audioPath, plain, synced string) error {
	lrcWritten := false

	if synced != "" {
		if err := WriteLRCFile(audioPath, synced); err != nil {
			return err
		}
		lrcWritten = true
	}

	tgr, err := tagger.ForFile(audioPath)
	if err != nil {
		// Unsupported format — skip tag embedding but keep .lrc
		return nil
	}

	if err := tgr.WriteLyrics(audioPath, plain, synced); err != nil {
		if lrcWritten {
			os.Remove(lrcPathFor(audioPath)) // rollback
		}
		return fmt.Errorf("embed tags %s: %w", audioPath, err)
	}
	return nil
}

// lrcPathFor returns the .lrc sidecar path for a given audio file path.
func lrcPathFor(audioPath string) string {
	ext := filepath.Ext(audioPath)
	return strings.TrimSuffix(audioPath, ext) + ".lrc"
}
```

**Step 4: Run tests**
```bash
go test ./internal/lyrics/ -v -race
```
Expected: PASS.

**Step 5: Commit**
```bash
git add internal/lyrics/writer.go internal/lyrics/writer_test.go
git commit -m "feat(lyrics): add atomic .lrc writer and tag embedder"
```

---

## Task 8: cmd/navilyrics — CLI run subcommand

**Files:**
- Create: `cmd/navilyrics/main.go`

**Step 1: Create `cmd/navilyrics/main.go`** with the `run` subcommand wired up. The `serve` subcommand will be a stub returning an error until Task 11.

```go
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"strconv"

	"github.com/user/navilyrics/internal/lyrics"
	"github.com/user/navilyrics/pkg/lrclib"
	"github.com/user/navilyrics/pkg/navidrome"
)

var version = "dev"

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(1)
	}
	switch os.Args[1] {
	case "run":
		runCLI(os.Args[2:])
	case "serve":
		runServer(os.Args[2:])
	default:
		usage()
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: navilyrics <run|serve> [flags]")
	fmt.Fprintln(os.Stderr, "  run    batch-fetch missing lyrics")
	fmt.Fprintln(os.Stderr, "  serve  start the web UI")
}

func runCLI(args []string) {
	fs := flag.NewFlagSet("run", flag.ExitOnError)
	dryRun := fs.Bool("dry-run", envBool("DRY_RUN", false), "preview without writing files")
	_ = fs.Parse(args)

	nd := navidrome.New(mustEnv("NAVIDROME_URL")+"/api", mustEnv("NAVIDROME_USER"), mustEnv("NAVIDROME_PASS"))
	if err := nd.Authenticate(); err != nil {
		log.Fatalf("navidrome auth: %v", err)
	}

	lrc := lrclib.New("")
	musicDir := mustEnv("MUSIC_DIR")
	proc := lyrics.NewProcessor(nd, lrc, musicDir, *dryRun)

	counts := struct{ found, notFound, skipped, errors int }{}
	err := proc.Run(context.Background(), func(r lyrics.Result) {
		switch r.Status {
		case "found", "dry_run":
			counts.found++
			log.Printf("[found] %s — %s (%s)", r.Artist, r.Title, r.Source)
		case "not_found":
			counts.notFound++
			log.Printf("[not_found] %s — %s", r.Artist, r.Title)
		case "skipped":
			counts.skipped++
		case "error":
			counts.errors++
			log.Printf("[error] %s — %s: %s", r.Artist, r.Title, r.Err)
		}
	})
	if err != nil {
		log.Fatalf("run: %v", err)
	}

	log.Printf("done: %d found, %d not_found, %d skipped, %d errors",
		counts.found, counts.notFound, counts.skipped, counts.errors)

	if !*dryRun {
		if err := nd.TriggerScan(context.Background()); err != nil {
			log.Printf("trigger scan: %v (non-fatal)", err)
		}
	}
}

func runServer(_ []string) {
	log.Fatal("serve not yet implemented — coming in Task 11")
}

func mustEnv(key string) string {
	v := os.Getenv(key)
	if v == "" {
		log.Fatalf("required env var %s is not set", key)
	}
	return v
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func envBool(key string, def bool) bool {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		return def
	}
	return b
}
```

**Step 2: Build**
```bash
make build
```
Expected: binary `./navilyrics` built without errors.

**Step 3: Smoke-test the CLI help**
```bash
./navilyrics
```
Expected: usage message printed.

**Step 4: Commit**
```bash
git add cmd/navilyrics/
git commit -m "feat(cmd): add run subcommand with worker pool and dry-run flag"
```

---

## Task 9: web/ — Embed Setup + Base Template + CSS

**Files:**
- Create: `web/web.go`
- Create: `web/templates/base.html`
- Create: `web/static/app.css`

**Step 1: Create `web/web.go`**

```go
package web

import "embed"

//go:embed templates static
var Files embed.FS
```

**Step 2: Create `web/templates/base.html`**

Copy the base.html from navilist and adapt:
- Change `navilist` → `navilyrics` in title and h1
- Update nav tabs: `Dashboard` (`/`), `Songs` (`/songs`)
- Keep identical CSS vars, theme toggle, toast system, HTMX CDN link

**Step 3: Create `web/static/app.css`**

Copy `/home/gndm/Projects/navidrome-playlists/web/static/app.css` verbatim — the design system (black/white, CSS vars, 1px borders) is identical.

**Step 4: Verify embed compiles**
```bash
go build ./web/
```
Expected: no errors.

**Step 5: Commit**
```bash
git add web/
git commit -m "feat(web): add embed setup, base template, and CSS"
```

---

## Task 10: internal/handlers — Handler Base + Dashboard

**Files:**
- Create: `internal/handlers/handler.go`
- Create: `internal/handlers/dashboard.go`
- Create: `web/templates/dashboard.html`

**Step 1: Create `internal/handlers/handler.go`**

Copy from navilist's `internal/handlers/handler.go` verbatim, then:
- Change import path to `github.com/user/navilyrics/pkg/navidrome`
- Add `proc *lyrics.Processor` field to `Handler`
- Update `New()` to accept both `*navidrome.Client` and `*lyrics.Processor`

```go
package handlers

import (
	"fmt"
	"html/template"
	"io"
	"log"
	"net/http"

	"github.com/user/navilyrics/internal/lyrics"
	"github.com/user/navilyrics/pkg/navidrome"
)

type Templates struct {
	sets map[string]*template.Template
}

func NewTemplates(sets map[string]*template.Template) *Templates {
	return &Templates{sets: sets}
}

func (ts *Templates) ExecuteTemplate(w io.Writer, name string, data any) error {
	t, ok := ts.sets[name]
	if !ok {
		return fmt.Errorf("no template registered for %q", name)
	}
	return t.ExecuteTemplate(w, name, data)
}

type Handler struct {
	nd      *navidrome.Client
	proc    *lyrics.Processor
	tpl     *Templates
	version string
}

func New(nd *navidrome.Client, proc *lyrics.Processor, tpl *Templates, version string) *Handler {
	return &Handler{nd: nd, proc: proc, tpl: tpl, version: version}
}

func (h *Handler) baseData(activeTab string) map[string]any {
	return map[string]any{"ActiveTab": activeTab, "Version": h.version}
}

func (h *Handler) renderError(w http.ResponseWriter, r *http.Request, msg string, code int) {
	if code >= 400 {
		log.Printf("error %d %s %s: %s", code, r.Method, r.URL.Path, msg)
	}
	if r.Header.Get("HX-Request") != "" {
		w.Header().Set("HX-Reswap", "none")
		w.Header().Set("HX-Trigger", `{"showToast":"`+msg+`"}`)
		w.WriteHeader(code)
		return
	}
	http.Error(w, msg, code)
}
```

**Step 2: Create `internal/handlers/dashboard.go`**

```go
package handlers

import (
	"net/http"
)

// DashboardData is the view model for the dashboard page.
type DashboardData struct {
	ActiveTab  string
	Version    string
	Total      int
	WithLyrics int
	Missing    int
}

// Dashboard renders the main stats page.
func (h *Handler) Dashboard(w http.ResponseWriter, r *http.Request) {
	songs, err := h.nd.AllSongs(r.Context())
	if err != nil {
		h.renderError(w, r, "failed to load songs", http.StatusBadGateway)
		return
	}

	data := DashboardData{
		ActiveTab: "dashboard",
		Version:   h.version,
		Total:     len(songs),
	}
	for _, s := range songs {
		if s.HasLyrics {
			data.WithLyrics++
		} else {
			data.Missing++
		}
	}

	if err := h.tpl.ExecuteTemplate(w, "dashboard.html", data); err != nil {
		log.Printf("template dashboard: %v", err)
	}
}
```

**Step 3: Create `web/templates/dashboard.html`**

```html
{{template "base" .}}
{{define "title"}}navilyrics — dashboard{{end}}
{{define "content"}}
<section class="stats">
  <div class="stat-card">
    <span class="stat-label">Total songs</span>
    <span class="stat-value">{{.Total}}</span>
  </div>
  <div class="stat-card">
    <span class="stat-label">With lyrics</span>
    <span class="stat-value">{{.WithLyrics}}</span>
  </div>
  <div class="stat-card stat-card--missing">
    <span class="stat-label">Missing lyrics</span>
    <span class="stat-value">{{.Missing}}</span>
  </div>
</section>

<div class="actions">
  <a href="/songs?filter=missing" class="btn">View missing</a>
  <form hx-post="/run" hx-target="#run-output" hx-swap="innerHTML">
    <button type="submit" class="btn btn--primary">Fetch all missing</button>
  </form>
</div>

<div id="run-output"></div>
{{end}}
```

**Step 4: Commit**
```bash
git add internal/handlers/ web/templates/dashboard.html
git commit -m "feat(handlers): add Handler base and Dashboard"
```

---

## Task 11: internal/handlers — Songs List + Per-Song Actions

**Files:**
- Create: `internal/handlers/songs.go`
- Create: `internal/handlers/run.go`
- Create: `web/templates/songs.html`
- Create: `web/templates/partials/songs_table.html`
- Create: `web/templates/partials/lyrics_preview.html`
- Create: `web/templates/partials/run_result.html`

**Step 1: Create `internal/handlers/songs.go`**

```go
package handlers

import (
	"log"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/user/navilyrics/pkg/navidrome"
)

type SongsData struct {
	ActiveTab string
	Version   string
	Songs     []navidrome.Song
	Filter    string // "all" | "missing" | "complete"
}

// Songs renders the paginated song list, with optional filter.
func (h *Handler) Songs(w http.ResponseWriter, r *http.Request) {
	filter := r.URL.Query().Get("filter")
	if filter == "" {
		filter = "missing"
	}

	all, err := h.nd.AllSongs(r.Context())
	if err != nil {
		h.renderError(w, r, "failed to load songs", http.StatusBadGateway)
		return
	}

	var filtered []navidrome.Song
	for _, s := range all {
		switch filter {
		case "missing":
			if !s.HasLyrics {
				filtered = append(filtered, s)
			}
		case "complete":
			if s.HasLyrics {
				filtered = append(filtered, s)
			}
		default:
			filtered = append(filtered, s)
		}
	}

	data := SongsData{
		ActiveTab: "songs",
		Version:   h.version,
		Songs:     filtered,
		Filter:    filter,
	}

	name := "songs.html"
	if r.Header.Get("HX-Request") != "" {
		name = "songs_table"
	}
	if err := h.tpl.ExecuteTemplate(w, name, data); err != nil {
		log.Printf("template songs: %v", err)
	}
}

// SongPreview fetches lyrics for a single song from lrclib and returns a preview partial.
func (h *Handler) SongPreview(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	songs, err := h.nd.AllSongs(r.Context())
	if err != nil {
		h.renderError(w, r, "load songs", http.StatusBadGateway)
		return
	}

	var song navidrome.Song
	for _, s := range songs {
		if s.ID == id {
			song = s
			break
		}
	}
	if song.ID == "" {
		h.renderError(w, r, "song not found", http.StatusNotFound)
		return
	}

	result := h.proc.ProcessSong(r.Context(), song)

	if err := h.tpl.ExecuteTemplate(w, "lyrics_preview", result); err != nil {
		log.Printf("template lyrics_preview: %v", err)
	}
}
```

**Step 2: Create `internal/handlers/run.go`**

```go
package handlers

import (
	"context"
	"fmt"
	"log"
	"net/http"

	"github.com/user/navilyrics/internal/lyrics"
)

// RunBatch triggers the full batch processor and streams a result summary.
func (h *Handler) RunBatch(w http.ResponseWriter, r *http.Request) {
	counts := struct{ found, notFound, skipped, errors int }{}

	err := h.proc.Run(context.Background(), func(res lyrics.Result) {
		switch res.Status {
		case "found":
			counts.found++
		case "not_found":
			counts.notFound++
		case "skipped":
			counts.skipped++
		case "error":
			counts.errors++
			log.Printf("[error] %s — %s: %s", res.Artist, res.Title, res.Err)
		}
	})

	msg := fmt.Sprintf("Done: %d found, %d not found, %d skipped, %d errors",
		counts.found, counts.notFound, counts.skipped, counts.errors)
	if err != nil {
		msg = "Run failed: " + err.Error()
	}

	w.Header().Set("HX-Trigger", `{"showToast":"`+msg+`"}`)
	if err := h.tpl.ExecuteTemplate(w, "run_result", map[string]any{
		"Found":    counts.found,
		"NotFound": counts.notFound,
		"Skipped":  counts.skipped,
		"Errors":   counts.errors,
	}); err != nil {
		log.Printf("template run_result: %v", err)
	}
}
```

**Step 3: Create templates**

`web/templates/songs.html` — full page wrapping the table partial.
`web/templates/partials/songs_table.html` — the `{{define "songs_table"}}` partial.
`web/templates/partials/lyrics_preview.html` — shows fetched plain + synced lyrics with a "Write" button.
`web/templates/partials/run_result.html` — shows batch result counts.

Follow navilist's partial pattern (see `/home/gndm/Projects/navidrome-playlists/web/templates/partials/`).

**Step 4: Commit**
```bash
git add internal/handlers/songs.go internal/handlers/run.go web/templates/
git commit -m "feat(handlers): add songs list, lyrics preview, and batch run"
```

---

## Task 12: cmd/navilyrics — serve subcommand + routes

**Files:**
- Modify: `cmd/navilyrics/main.go` (replace `runServer` stub)

**Step 1: Replace the `runServer` stub in `main.go`**

```go
func runServer(_ []string) {
	nd := navidrome.New(mustEnv("NAVIDROME_URL")+"/api", mustEnv("NAVIDROME_USER"), mustEnv("NAVIDROME_PASS"))
	if err := nd.Authenticate(); err != nil {
		log.Fatalf("navidrome auth: %v", err)
	}

	lrc := lrclib.New("")
	musicDir := mustEnv("MUSIC_DIR")
	dryRun := envBool("DRY_RUN", false)
	proc := lyrics.NewProcessor(nd, lrc, musicDir, dryRun)

	tpl := buildTemplates()
	h := handlers.New(nd, proc, tpl, version)

	r := chi.NewRouter()
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)

	staticSub, _ := fs.Sub(web.Files, "static")
	r.Handle("/static/*", http.StripPrefix("/static/", http.FileServer(http.FS(staticSub))))

	r.Get("/", h.Dashboard)
	r.Get("/songs", h.Songs)
	r.Get("/songs/{id}/preview", h.SongPreview)
	r.Post("/run", h.RunBatch)

	port := envOr("PORT", "8080")
	log.Printf("navilyrics %s listening on :%s", version, port)
	log.Fatal(http.ListenAndServe(":"+port, r))
}

func buildTemplates() *handlers.Templates {
	base := template.Must(template.New("").ParseFS(web.Files,
		"templates/base.html",
		"templates/partials/toast.html",
	))
	newPage := func(page string) *template.Template {
		c := template.Must(base.Clone())
		return template.Must(c.ParseFS(web.Files, "templates/"+page))
	}
	songsPage := newPage("songs.html")
	return handlers.NewTemplates(map[string]*template.Template{
		"dashboard.html":   newPage("dashboard.html"),
		"songs.html":       songsPage,
		"songs_table":      songsPage,
		"lyrics_preview":   base,
		"run_result":       base,
	})
}
```

Add required imports: `chi`, `middleware`, `fs`, `html/template`, `handlers`, `lyrics`, `web`, `lrclib`.

**Step 2: Build and smoke-test**
```bash
make build
./navilyrics serve  # should print "listening on :8080"
```
Expected: server starts (will fail to reach Navidrome without .env, but binary compiles and routes register).

**Step 3: Commit**
```bash
git add cmd/navilyrics/main.go
git commit -m "feat(cmd): wire serve subcommand with chi routes"
```

---

## Task 13: Dockerfile + docker-compose.yml

**Files:**
- Create: `Dockerfile`
- Create: `docker-compose.yml`

**Step 1: Create `Dockerfile`**

```dockerfile
FROM golang:1.23-alpine AS builder
ARG VERSION=dev
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -ldflags="-s -w -X main.version=${VERSION}" -o navilyrics ./cmd/navilyrics

FROM alpine:3.20
RUN apk add --no-cache ca-certificates tzdata
WORKDIR /app
COPY --from=builder /app/navilyrics .
EXPOSE 8080
ENTRYPOINT ["/app/navilyrics"]
CMD ["serve"]
```

**Step 2: Create `docker-compose.yml`**

```yaml
services:
  navilyrics:
    build:
      context: .
      args:
        VERSION: ${VERSION:-dev}
    image: navilyrics:${VERSION:-dev}
    restart: unless-stopped
    environment:
      NAVIDROME_URL: ${NAVIDROME_URL}
      NAVIDROME_USER: ${NAVIDROME_USER}
      NAVIDROME_PASS: ${NAVIDROME_PASS}
      MUSIC_DIR: /music
      PORT: 8080
      DRY_RUN: ${DRY_RUN:-false}
    volumes:
      - ${MUSIC_DIR_HOST:-/tmp/music}:/music:rw
    ports:
      - "${PORT:-8080}:8080"
```

**Step 3: Build Docker image**
```bash
make docker-build
```
Expected: image `navilyrics:dev` built successfully.

**Step 4: Commit**
```bash
git add Dockerfile docker-compose.yml
git commit -m "chore: add Dockerfile and docker-compose"
```

---

## Task 14: README

**Files:**
- Create: `README.md`

**Step 1: Write `README.md`** covering:
- What it does (one paragraph)
- Quick start (env vars → `make up` → visit `:8080`)
- CLI usage: `docker compose run navilyrics run --dry-run`
- Configuration table (all env vars + defaults)
- Supported audio formats (MP3, FLAC)
- Development: `make test`, `make run-server`, `make run-cli`
- Architecture (one paragraph)

**Step 2: Commit**
```bash
git add README.md
git commit -m "docs: add README"
```

---

## Verification Checklist

After all tasks complete:

```bash
# All tests pass
make test

# Binary builds
make build
./navilyrics          # prints usage
./navilyrics run --help

# Docker image builds
make docker-build

# Dry-run against real Navidrome (set .env first)
make run-cli          # navilyrics run --dry-run
```
