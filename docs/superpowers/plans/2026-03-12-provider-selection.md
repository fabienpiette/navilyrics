# Provider Selection Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add NetEase and Genius as lyrics providers alongside lrclib, and let the user choose which providers to query per individual fetch in the UI.

**Architecture:** A generic `Provider` interface (`Name() string`, `Search(...)`) lives in `internal/lyrics/providers.go` alongside thin adapters for each `pkg/` client. The processor holds a `[]Provider` slice; `FetchLyricsOnly` accepts an optional name filter. The UI search form renders provider checkboxes server-side and includes checked names in the POST body.

**Tech Stack:** Go stdlib only — no new dependencies. Genius HTML scraping uses `regexp` + `html.UnescapeString`. Tests use `httptest.NewServer`.

---

## Chunk 1: pkg/netease refactor + pkg/genius new

### Task 1: Refactor pkg/netease to own Response type

**Context:** `pkg/netease/client.go` currently imports `pkg/lrclib` and returns `lrclib.Response`. This coupling must be removed. The client gets its own `Response` type and adds `PlainLyrics` (stripped from LRC). The `Get` method is removed (it only existed to satisfy the old `LRCFetcher` interface).

**Files:**
- Modify: `pkg/netease/client.go`
- Modify: `pkg/netease/client_test.go`

- [ ] **Step 1: Replace the lrclib import and define own Response**

Replace the top of `pkg/netease/client.go` — change imports and add `Response` and `stripTimestamps`:

```go
package netease

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

const defaultBaseURL = "https://music.163.com"

// Response holds lyrics fetched from NetEase Cloud Music.
type Response struct {
	SyncedLyrics string // LRC format with timestamps
	PlainLyrics  string // timestamps stripped
}

var lrcTagRe = regexp.MustCompile(`\[[^\]]*\]`)

// stripTimestamps removes [mm:ss.xx] and metadata tags from LRC lines.
func stripTimestamps(lrc string) string {
	lines := strings.Split(lrc, "\n")
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		stripped := lrcTagRe.ReplaceAllString(line, "")
		stripped = strings.TrimSpace(stripped)
		if stripped != "" {
			out = append(out, stripped)
		}
	}
	return strings.Join(out, "\n")
}
```

- [ ] **Step 2: Update Search to return netease.Response (not lrclib.Response)**

In `pkg/netease/client.go`, update the `Search` and `fetchLyrics` methods:

```go
// Search searches for a track and returns the lyrics if a duration match is found.
// Uses ±5 s tolerance (same as lrclib). Returns (zero, false, nil) if not found.
func (c *Client) Search(ctx context.Context, artist, title string, targetDuration float64) (Response, bool, error) {
	id, _, ok, err := c.searchSong(ctx, artist, title, targetDuration)
	if err != nil || !ok {
		return Response{}, false, err
	}
	synced, err := c.fetchLyrics(ctx, id)
	if err != nil {
		return Response{}, false, err
	}
	if synced == "" {
		return Response{}, false, nil
	}
	return Response{
		SyncedLyrics: synced,
		PlainLyrics:  stripTimestamps(synced),
	}, true, nil
}
```

Also remove the `Get` method entirely (deleted, not replaced).

- [ ] **Step 3: Update client_test.go**

Remove `TestGet_delegatesToSearch` (the `Get` method no longer exists). Update `TestSearch_found` to also assert `PlainLyrics`:

```go
func TestSearch_found(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/search/get":
			json.NewEncoder(w).Encode(map[string]any{
				"result": map[string]any{
					"songs": []map[string]any{
						{"id": 123, "duration": 210000},
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
		t.Errorf("SyncedLyrics = %q", resp.SyncedLyrics)
	}
	if resp.PlainLyrics != "Hello world" {
		t.Errorf("PlainLyrics = %q", resp.PlainLyrics)
	}
}

func TestSearch_emptyLyrics_notFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/search/get":
			json.NewEncoder(w).Encode(map[string]any{
				"result": map[string]any{
					"songs": []map[string]any{{"id": 1, "duration": 210000}},
				},
			})
		case "/api/song/lyric":
			json.NewEncoder(w).Encode(map[string]any{
				"lrc": map[string]any{"lyric": ""},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	c := netease.New(srv.URL)
	_, ok, err := c.Search(context.Background(), "Artist", "Song", 210.0)
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatal("want not found when lyrics are empty")
	}
}
```

Keep `TestSearch_durationMismatch` and `TestSearch_picksClosestDuration` unchanged.

- [ ] **Step 4: Run tests**

```
go test ./pkg/netease/...
```
Expected: all pass.

- [ ] **Step 5: Verify build still compiles (main.go will fail — that's expected)**

```
go build ./pkg/... 2>&1 | head -20
```
`main.go` will have compile errors (uses `Get` and `lrclib.Response`) — document them as "will be fixed in Task 8". `pkg/netease` itself must build cleanly.

- [ ] **Step 6: Commit**

```
git add pkg/netease/client.go pkg/netease/client_test.go
git commit -m "refactor(netease): own Response type, remove lrclib coupling"
```

---

### Task 2: Create pkg/genius

**Context:** Genius has an official search API (requires token) but returns no lyrics directly — they must be scraped from the song page HTML. `data-lyrics-container="true"` divs hold the content. Uses stdlib `regexp` and `html` packages only — no new dependencies.

**Files:**
- Create: `pkg/genius/client.go`
- Create: `pkg/genius/client_test.go`

- [ ] **Step 1: Write failing tests first**

Create `pkg/genius/client_test.go` using a single mux server so both the API search and page scrape are served by one `httptest.Server` (the `/search` response embeds the server's own URL for the song page):

```go
package genius_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/user/navilyrics/pkg/genius"
)

// newMux returns a test server where /search returns Genius API JSON
// pointing to /song, and /song returns a Genius-style lyrics page.
func newMux(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	var srv *httptest.Server
	srv = httptest.NewServer(mux)
	mux.HandleFunc("/search", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `{"response":{"hits":[{"result":{"title":"Song","url":"%s/song","primary_artist":{"name":"Artist"}}}]}}`, srv.URL)
	})
	mux.HandleFunc("/song", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `<div data-lyrics-container="true">Line 1<br/>Line 2</div>`)
	})
	return srv
}

func TestSearch_found(t *testing.T) {
	srv := newMux(t)
	defer srv.Close()

	c := genius.New("token", srv.URL)
	resp, ok, err := c.Search(context.Background(), "Artist", "Song")
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("want found, got not found")
	}
	if resp.PlainLyrics != "Line 1\nLine 2" {
		t.Errorf("PlainLyrics = %q", resp.PlainLyrics)
	}
	if resp.SyncedLyrics != "" {
		t.Errorf("SyncedLyrics should always be empty, got %q", resp.SyncedLyrics)
	}
}

func TestSearch_noArtistMatch_notFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Hit exists but primary_artist doesn't match
		fmt.Fprint(w, `{"response":{"hits":[{"result":{"title":"Song","url":"http://x/song","primary_artist":{"name":"Somebody Else"}}}]}}`)
	}))
	defer srv.Close()

	c := genius.New("token", srv.URL)
	_, ok, err := c.Search(context.Background(), "Artist", "Song")
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatal("want not found when artist doesn't match")
	}
}

func TestSearch_noHits_notFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"response":{"hits":[]}}`)
	}))
	defer srv.Close()

	c := genius.New("token", srv.URL)
	_, ok, err := c.Search(context.Background(), "Artist", "Song")
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatal("want not found")
	}
}

func TestSearch_noLyricsContainer_notFound(t *testing.T) {
	mux := http.NewServeMux()
	var srv *httptest.Server
	srv = httptest.NewServer(mux)
	mux.HandleFunc("/search", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `{"response":{"hits":[{"result":{"title":"Song","url":"%s/song","primary_artist":{"name":"Artist"}}}]}}`, srv.URL)
	})
	mux.HandleFunc("/song", func(w http.ResponseWriter, r *http.Request) {
		// No data-lyrics-container div
		fmt.Fprint(w, `<html><body><p>No lyrics here</p></body></html>`)
	})
	defer srv.Close()

	c := genius.New("token", srv.URL)
	_, ok, err := c.Search(context.Background(), "Artist", "Song")
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatal("want not found when no lyrics container found")
	}
}

func TestSearch_nestedDivs_extractsFullLyrics(t *testing.T) {
	// Real Genius pages wrap section headers in inner <div> elements.
	// extractLyrics must handle these without truncating.
	mux := http.NewServeMux()
	var srv *httptest.Server
	srv = httptest.NewServer(mux)
	mux.HandleFunc("/search", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `{"response":{"hits":[{"result":{"title":"Song","url":"%s/song","primary_artist":{"name":"Artist"}}}]}}`, srv.URL)
	})
	mux.HandleFunc("/song", func(w http.ResponseWriter, r *http.Request) {
		// Lyrics container with a nested section-header div
		fmt.Fprint(w, `<div data-lyrics-container="true"><div class="section">Verse 1</div>Line 1<br/>Line 2</div>`)
	})
	defer srv.Close()

	c := genius.New("token", srv.URL)
	resp, ok, err := c.Search(context.Background(), "Artist", "Song")
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("want found")
	}
	// Should contain content from both inside and outside the nested div
	if resp.PlainLyrics == "" {
		t.Fatal("want non-empty PlainLyrics")
	}
	if !strings.Contains(resp.PlainLyrics, "Line 1") {
		t.Errorf("expected Line 1 in lyrics, got %q", resp.PlainLyrics)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

```
go test ./pkg/genius/... 2>&1
```
Expected: compile error (package doesn't exist yet).

- [ ] **Step 3: Create pkg/genius/client.go**

```go
package genius

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

const defaultAPIURL = "https://api.genius.com"

// Response holds lyrics scraped from a Genius song page.
type Response struct {
	PlainLyrics  string // plain text, no timestamps
	SyncedLyrics string // always empty — Genius has no timed lyrics
}

// Client is a Genius API + scraping client.
type Client struct {
	token      string
	apiBaseURL string
	httpClient *http.Client
}

// New creates a Client. Pass "" for apiBaseURL to use the default.
func New(token, apiBaseURL string) *Client {
	if apiBaseURL == "" {
		apiBaseURL = defaultAPIURL
	}
	return &Client{
		token:      token,
		apiBaseURL: apiBaseURL,
		httpClient: &http.Client{Timeout: 15 * time.Second},
	}
}

type searchResp struct {
	Response struct {
		Hits []struct {
			Result struct {
				URL           string `json:"url"`
				PrimaryArtist struct {
					Name string `json:"name"`
				} `json:"primary_artist"`
			} `json:"result"`
		} `json:"hits"`
	} `json:"response"`
}

// Search finds lyrics by artist+title. Returns (zero, false, nil) if not found
// or if the Genius page no longer has a parseable lyrics container.
func (c *Client) Search(ctx context.Context, artist, title string) (Response, bool, error) {
	q := url.QueryEscape(artist + " " + title)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		c.apiBaseURL+"/search?q="+q, nil)
	if err != nil {
		return Response{}, false, err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("User-Agent", "navilyrics/1.0")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return Response{}, false, fmt.Errorf("genius search: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return Response{}, false, fmt.Errorf("genius search: status %d", resp.StatusCode)
	}

	var sr searchResp
	if err := json.NewDecoder(resp.Body).Decode(&sr); err != nil {
		return Response{}, false, fmt.Errorf("genius search decode: %w", err)
	}

	// Pick first hit whose primary_artist matches (case-insensitive contains).
	artistLower := strings.ToLower(artist)
	var songURL string
	for _, hit := range sr.Response.Hits {
		if strings.Contains(strings.ToLower(hit.Result.PrimaryArtist.Name), artistLower) {
			songURL = hit.Result.URL
			break
		}
	}
	if songURL == "" {
		return Response{}, false, nil
	}

	lyrics, err := c.scrapeLyrics(ctx, songURL)
	if err != nil {
		return Response{}, false, err
	}
	if lyrics == "" {
		return Response{}, false, nil
	}
	return Response{PlainLyrics: lyrics}, true, nil
}

var (
	brRe  = regexp.MustCompile(`(?i)<br\s*/?>`)
	tagRe = regexp.MustCompile(`<[^>]+>`)
)

func (c *Client) scrapeLyrics(ctx context.Context, pageURL string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, pageURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("genius page: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("genius page: status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("genius page read: %w", err)
	}
	return extractLyrics(string(body)), nil
}

// extractLyrics finds all data-lyrics-container divs and returns their plain
// text content. Handles nested <div> elements by tracking nesting depth.
// Returns "" if no container is found.
func extractLyrics(body string) string {
	body = brRe.ReplaceAllString(body, "\n")
	var parts []string
	for {
		attrIdx := strings.Index(body, `data-lyrics-container="true"`)
		if attrIdx < 0 {
			break
		}
		// Skip past the closing > of the opening tag.
		closeIdx := strings.Index(body[attrIdx:], ">")
		if closeIdx < 0 {
			break
		}
		content := body[attrIdx+closeIdx+1:]

		// Find the matching </div> respecting nesting depth.
		end := matchingDiv(content)
		if end < 0 {
			break
		}
		text := tagRe.ReplaceAllString(content[:end], "")
		text = html.UnescapeString(text)
		text = strings.TrimSpace(text)
		if text != "" {
			parts = append(parts, text)
		}
		body = content[end:]
	}
	return strings.Join(parts, "\n\n")
}

// matchingDiv returns the index of the </div> that closes the outermost div
// (depth 1 on entry). Returns -1 if no matching close tag is found.
func matchingDiv(s string) int {
	depth := 1
	i := 0
	for depth > 0 && i < len(s) {
		o := indexDivOpen(s[i:])
		c := strings.Index(s[i:], "</div>")
		if c < 0 {
			return -1
		}
		if o >= 0 && o < c {
			depth++
			i += o + 4
		} else {
			depth--
			if depth == 0 {
				return i + c
			}
			i += c + 6
		}
	}
	return -1
}

// indexDivOpen finds the next actual <div> open tag in s, skipping tag names
// that merely start with "div" (e.g. <divider>). Returns -1 if not found.
func indexDivOpen(s string) int {
	i := 0
	for {
		idx := strings.Index(s[i:], "<div")
		if idx < 0 {
			return -1
		}
		pos := i + idx + 4
		if pos >= len(s) {
			return -1
		}
		ch := s[pos]
		if ch == '>' || ch == ' ' || ch == '\t' || ch == '\n' || ch == '\r' || ch == '/' {
			return i + idx
		}
		i = pos
	}
}
```

- [ ] **Step 4: Run tests**

```
go test ./pkg/genius/...
```
Expected: all pass.

- [ ] **Step 5: Commit**

```
git add pkg/genius/
git commit -m "feat(genius): add Genius lyrics provider with API search and page scraping"
```

---

## Chunk 2: Provider interface + processor refactor

### Task 3: Create internal/lyrics/providers.go

**Context:** Define the `Provider` interface and `ProviderResult` type, plus three thin adapters: `lrclibProvider` (exact-Get-then-fuzzy-Search cascade), `netEaseProvider`, `geniusProvider`. The adapters bridge `pkg/` client types to the generic interface.

**Files:**
- Create: `internal/lyrics/providers.go`
- Create: `internal/lyrics/providers_test.go`

- [ ] **Step 1: Write failing tests**

Create `internal/lyrics/providers_test.go`:

```go
package lyrics_test

import (
	"context"
	"testing"

	"github.com/user/navilyrics/internal/lyrics"
)

// stubProvider implements Provider for testing.
type stubProvider struct {
	name   string
	result lyrics.ProviderResult
	ok     bool
	calls  int
}

func (s *stubProvider) Name() string { return s.name }
func (s *stubProvider) Search(_ context.Context, _, _, _ string, _ float64) (lyrics.ProviderResult, bool, error) {
	s.calls++
	return s.result, s.ok, nil
}

func TestProviderResult_fields(t *testing.T) {
	pr := lyrics.ProviderResult{
		PlainLyrics:  "plain",
		SyncedLyrics: "[00:01.00] synced",
		Instrumental: true,
	}
	if pr.PlainLyrics != "plain" {
		t.Errorf("PlainLyrics = %q", pr.PlainLyrics)
	}
	if !pr.Instrumental {
		t.Error("Instrumental should be true")
	}
}
```

- [ ] **Step 2: Run to confirm failure**

```
go test ./internal/lyrics/... -run TestProviderResult 2>&1
```
Expected: compile error (type not defined yet).

- [ ] **Step 3: Create internal/lyrics/providers.go**

```go
package lyrics

import (
	"context"

	"github.com/user/navilyrics/pkg/genius"
	"github.com/user/navilyrics/pkg/lrclib"
	"github.com/user/navilyrics/pkg/netease"
)

// ProviderResult is the common result type returned by all providers.
type ProviderResult struct {
	PlainLyrics  string
	SyncedLyrics string
	Instrumental bool
}

// Provider is the interface all lyrics sources must implement.
type Provider interface {
	// Name returns the provider's identifier (e.g. "lrclib", "netease", "genius").
	Name() string
	// Search finds lyrics. album may be empty. Returns (zero, false, nil) if not found.
	Search(ctx context.Context, artist, title, album string, duration float64) (ProviderResult, bool, error)
}

// lrclibProvider wraps lrclib.Client with exact-Get-then-fuzzy-Search cascade.
type lrclibProvider struct{ c *lrclib.Client }

// NewLRCLibProvider creates a Provider backed by lrclib.Client.
func NewLRCLibProvider(c *lrclib.Client) Provider { return &lrclibProvider{c: c} }

func (p *lrclibProvider) Name() string { return "lrclib" }

func (p *lrclibProvider) Search(ctx context.Context, artist, title, album string, duration float64) (ProviderResult, bool, error) {
	resp, ok, err := p.c.Get(ctx, artist, title, album, duration)
	if err != nil {
		return ProviderResult{}, false, err
	}
	if !ok {
		resp, ok, err = p.c.Search(ctx, artist, title, duration)
		if err != nil {
			return ProviderResult{}, false, err
		}
	}
	if !ok {
		return ProviderResult{}, false, nil
	}
	return ProviderResult{
		PlainLyrics:  resp.PlainLyrics,
		SyncedLyrics: resp.SyncedLyrics,
		Instrumental: resp.Instrumental,
	}, true, nil
}

// netEaseProvider wraps netease.Client.
type netEaseProvider struct{ c *netease.Client }

// NewNetEaseProvider creates a Provider backed by netease.Client.
func NewNetEaseProvider(c *netease.Client) Provider { return &netEaseProvider{c: c} }

func (p *netEaseProvider) Name() string { return "netease" }

func (p *netEaseProvider) Search(ctx context.Context, artist, title, _ string, duration float64) (ProviderResult, bool, error) {
	resp, ok, err := p.c.Search(ctx, artist, title, duration)
	if err != nil {
		return ProviderResult{}, false, err
	}
	if !ok {
		return ProviderResult{}, false, nil
	}
	return ProviderResult{
		PlainLyrics:  resp.PlainLyrics,
		SyncedLyrics: resp.SyncedLyrics,
	}, true, nil
}

// geniusProvider wraps genius.Client. SyncedLyrics is always empty.
type geniusProvider struct{ c *genius.Client }

// NewGeniusProvider creates a Provider backed by genius.Client.
func NewGeniusProvider(c *genius.Client) Provider { return &geniusProvider{c: c} }

func (p *geniusProvider) Name() string { return "genius" }

func (p *geniusProvider) Search(ctx context.Context, artist, title, _ string, _ float64) (ProviderResult, bool, error) {
	resp, ok, err := p.c.Search(ctx, artist, title)
	if err != nil {
		return ProviderResult{}, false, err
	}
	if !ok {
		return ProviderResult{}, false, nil
	}
	return ProviderResult{PlainLyrics: resp.PlainLyrics}, true, nil
}
```

- [ ] **Step 4: Run tests**

```
go test ./internal/lyrics/... -run TestProviderResult
```
Expected: pass.

- [ ] **Step 5: Commit**

```
git add internal/lyrics/providers.go internal/lyrics/providers_test.go
git commit -m "feat(lyrics): add Provider interface and lrclib/netease/genius adapters"
```

---

### Task 4: Refactor internal/lyrics/processor.go

**Context:** Remove `LRCFetcher` interface, `lrc`/`fallback` fields, and `SetFallback`. Replace with `providers []Provider`. Update `NewProcessor` and `FetchLyricsOnly` (gains `filter []string`). Update `ProcessSong` to pass `nil`. Update `Result.Source` doc comment.

**Files:**
- Modify: `internal/lyrics/processor.go`
- Modify: `internal/lyrics/processor_test.go` (if exists — check first)

- [ ] **Step 1: Replace processor_test.go**

`processor_test.go` uses `stubLRCLib`, `stubFetcher`, `LRCFetcher`, and `SetFallback` — all of which are being deleted. Replace the entire file with the version below (uses `stubProvider` from `providers_test.go`, same `lyrics_test` package):

```go
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
```

- [ ] **Step 2: Write new processor filtering test**

Append to `internal/lyrics/providers_test.go`. Also add `"github.com/user/navilyrics/pkg/navidrome"` to the import block at the top of that file (it is needed by `navidrome.Song` used in these tests).

```go
func TestFetchLyricsOnly_filterByProvider(t *testing.T) {
	a := &stubProvider{name: "a", result: lyrics.ProviderResult{PlainLyrics: "from a"}, ok: true}
	b := &stubProvider{name: "b", result: lyrics.ProviderResult{PlainLyrics: "from b"}, ok: true}

	proc := lyrics.NewProcessor(nil, []lyrics.Provider{a, b}, nil, false)

	// Filter to "b" only — "a" must not be called.
	song := navidrome.Song{ID: "1", Title: "T", Artist: "A"}
	result := proc.FetchLyricsOnly(context.Background(), song, []string{"b"})
	if result.Status != "found" {
		t.Fatalf("want found, got %q", result.Status)
	}
	if result.PlainLyrics != "from b" {
		t.Errorf("PlainLyrics = %q, want %q", result.PlainLyrics, "from b")
	}
	if a.calls != 0 {
		t.Errorf("provider a should not have been called, got %d calls", a.calls)
	}
}

func TestFetchLyricsOnly_unknownFilterReturnsNotFound(t *testing.T) {
	a := &stubProvider{name: "a", ok: true}
	proc := lyrics.NewProcessor(nil, []lyrics.Provider{a}, nil, false)

	song := navidrome.Song{ID: "1", Title: "T", Artist: "A"}
	result := proc.FetchLyricsOnly(context.Background(), song, []string{"nonexistent"})
	if result.Status != "not_found" {
		t.Fatalf("want not_found, got %q", result.Status)
	}
	if a.calls != 0 {
		t.Errorf("provider a should not have been called, got %d calls", a.calls)
	}
}

func TestFetchLyricsOnly_nilFilterUsesAll(t *testing.T) {
	a := &stubProvider{name: "a", ok: false}
	b := &stubProvider{name: "b", result: lyrics.ProviderResult{PlainLyrics: "from b"}, ok: true}
	proc := lyrics.NewProcessor(nil, []lyrics.Provider{a, b}, nil, false)

	song := navidrome.Song{ID: "1", Title: "T", Artist: "A"}
	result := proc.FetchLyricsOnly(context.Background(), song, nil)
	if result.Status != "found" {
		t.Fatalf("want found, got %q", result.Status)
	}
	if a.calls != 1 {
		t.Errorf("provider a should have been called once, got %d", a.calls)
	}
}
```

- [ ] **Step 3: Run to confirm tests fail**

```
go test ./internal/lyrics/... -run TestFetchLyricsOnly 2>&1
```
Expected: compile error (wrong `NewProcessor` / `FetchLyricsOnly` signature).

- [ ] **Step 4: Update processor.go**

In `internal/lyrics/processor.go`:

**a) Remove `LRCFetcher` interface** (delete the entire `type LRCFetcher interface { ... }` block) and remove the now-unused `"github.com/user/navilyrics/pkg/lrclib"` import from `processor.go`'s import block.

**b) Update `Result.Source` doc comment:**
```go
Source string // name of the winning provider ("lrclib", "netease", "genius", …), or ""
```

**c) Update `Processor` struct** (replace `lrc` and `fallback` fields):
```go
type Processor struct {
	nd        *navidrome.Client
	providers []Provider
	musicDirs []string
	dryRun    bool
}
```

**d) Delete `SetFallback` method entirely.**

**e) Update `NewProcessor`:**
```go
func NewProcessor(nd *navidrome.Client, providers []Provider, musicDirs []string, dryRun bool) *Processor {
	return &Processor{nd: nd, providers: providers, musicDirs: musicDirs, dryRun: dryRun}
}
```

**f) Replace `FetchLyricsOnly`:**
```go
// FetchLyricsOnly searches configured providers for lyrics without writing to disk.
// filter restricts which providers are tried (by Name()); nil/empty = try all.
// Returns a Result with Status "found" or "not_found".
func (p *Processor) FetchLyricsOnly(ctx context.Context, song navidrome.Song, filter []string) Result {
	r := Result{
		SongID:   song.ID,
		SongPath: song.Path,
		Title:    song.Title,
		Artist:   song.Artist,
	}

	providers := p.providers
	if len(filter) > 0 {
		active := make([]Provider, 0, len(filter))
		for _, prov := range p.providers {
			for _, f := range filter {
				if prov.Name() == f {
					active = append(active, prov)
					break
				}
			}
		}
		providers = active
	}

	for _, prov := range providers {
		res, ok, err := prov.Search(ctx, song.Artist, song.Title, song.Album, song.Duration)
		if err != nil {
			log.Printf("%s search %q: %v", prov.Name(), song.Title, err)
			continue
		}
		if !ok {
			continue
		}
		r.PlainLyrics = res.PlainLyrics
		r.SyncedLyrics = res.SyncedLyrics
		r.Instrumental = res.Instrumental
		r.Source = prov.Name()
		r.Status = "found"
		return r
	}

	log.Printf("[not_found] %s — %s", song.Artist, song.Title)
	r.Status = "not_found"
	return r
}
```

**g) Update `ProcessSong`** — change the `FetchLyricsOnly` call to pass `nil`:
```go
r := p.FetchLyricsOnly(ctx, song, nil)
```

- [ ] **Step 5: Run all internal/lyrics tests**

```
go test ./internal/lyrics/...
```
Expected: all pass.

- [ ] **Step 6: Commit**

```
git add internal/lyrics/processor.go internal/lyrics/processor_test.go internal/lyrics/providers_test.go
git commit -m "refactor(lyrics): replace LRCFetcher with Provider interface, add filter to FetchLyricsOnly"
```

---

## Chunk 3: Handler + UI + main.go wiring

### Task 5: Update internal/handlers

**Context:** `Handler` gains `availableProviders []string`. `New()` gets a new parameter. `songsData` in `songs.go` gets `AvailableProviders`. `SongFetch` in `lrc.go` adds `Providers` to the decode struct and passes it to `FetchLyricsOnly`.

**Files:**
- Modify: `internal/handlers/handler.go`
- Modify: `internal/handlers/songs.go`
- Modify: `internal/handlers/lrc.go`

- [ ] **Step 1: Add availableProviders to Handler**

In `internal/handlers/handler.go`, update the `Handler` struct:

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
}
```

Update `New()` — add `availableProviders []string` as the last parameter:

```go
func New(nd *navidrome.Client, proc *lyrics.Processor, tmpls, partials map[string]*template.Template, version string, availableProviders []string) *Handler {
	h := &Handler{
		nd: nd, proc: proc, tmpls: tmpls, partials: partials,
		version: version, runs: newRunStore(), stats: &statsCache{},
		availableProviders: availableProviders,
	}
	go func() {
		if err := h.stats.refresh(context.Background(), nd); err != nil {
			log.Printf("stats: boot refresh failed: %v", err)
		}
	}()
	return h
}
```

- [ ] **Step 2: Add AvailableProviders to songsData and Songs handler**

In `internal/handlers/songs.go`, update `songsData`:

```go
type songsData struct {
	ActiveTab          string
	Version            string
	AvailableProviders []string
	songsRowsData
}
```

Update the `Songs` handler to pass it:

```go
h.render(w, "songs.html", songsData{
	ActiveTab:          "songs",
	Version:            h.version,
	AvailableProviders: h.availableProviders,
	songsRowsData:      rows,
})
```

- [ ] **Step 3: Update SongFetch in lrc.go to accept providers filter**

In `internal/handlers/lrc.go`, replace the entire `overrides` block (from the struct declaration through the `FetchLyricsOnly` call):

```go
// Optional body may override title/artist/album for manual searches,
// and restrict which providers are queried.
var overrides struct {
	Title     string   `json:"title"`
	Artist    string   `json:"artist"`
	Album     string   `json:"album"`
	Providers []string `json:"providers"` // nil = use all configured providers
}
_ = json.NewDecoder(r.Body).Decode(&overrides)
if overrides.Title != "" {
	song.Title = overrides.Title
}
if overrides.Artist != "" {
	song.Artist = overrides.Artist
}
if overrides.Album != "" {
	song.Album = overrides.Album
}

result := h.proc.FetchLyricsOnly(r.Context(), song, overrides.Providers)
```

- [ ] **Step 4: Build to verify compilation**

```
go build ./...
```
Expected: `main.go` fails with wrong argument count for `handlers.New` — will be fixed in Task 8.
`internal/handlers/...` and `internal/lyrics/...` must compile cleanly.

- [ ] **Step 5: Commit**

```
git add internal/handlers/handler.go internal/handlers/songs.go internal/handlers/lrc.go
git commit -m "feat(handlers): add availableProviders field and pass fetch filter to processor"
```

---

### Task 6: Update web/templates/songs.html (UI)

**Context:** The search form gains provider checkboxes rendered from `{{.AvailableProviders}}`. All are checked by default. `doFetchLyrics()` reads checked boxes and includes them in the POST body.

**Files:**
- Modify: `web/templates/songs.html`

- [ ] **Step 1: Read the current songs.html search form section**

Find the `id="search-form"` div and the `doFetchLyrics` JS function. Note where title/artist/album inputs are.

- [ ] **Step 2: Add provider checkboxes to the search form**

Inside the search form div, after the existing title/artist/album inputs, add a providers row. It should only render if there are multiple providers (single provider = no choice to make):

```html
{{if gt (len .AvailableProviders) 1}}
<div class="search-row search-providers">
  <label>Providers</label>
  <div class="provider-checks">
    {{range .AvailableProviders}}
    <label class="provider-check">
      <input type="checkbox" name="provider" value="{{.}}" checked> {{.}}
    </label>
    {{end}}
  </div>
</div>
{{end}}
```

Place this after the album input row and before the search button.

- [ ] **Step 3: Update doFetchLyrics() in JS**

Find the `doFetchLyrics` function. Update the `body` object to include providers:

```js
function doFetchLyrics() {
  if (!_currentSongId) return;
  // ... existing code collecting title/artist/album ...

  const checked = [...document.querySelectorAll('#search-form input[name="provider"]:checked')]
    .map(el => el.value);

  fetch(`/songs/${_currentSongId}/fetch`, {
    method: 'POST',
    headers: {'Content-Type': 'application/json'},
    body: JSON.stringify({
      title: document.getElementById('search-title').value,
      artist: document.getElementById('search-artist').value,
      album: document.getElementById('search-album').value,
      providers: checked.length > 0 ? checked : undefined,
    })
  })
  // ... rest of existing handler unchanged ...
}
```

Note: the exact element IDs depend on the current template — read the template first (Step 1) and match them exactly.

- [ ] **Step 4: Add CSS for provider checkboxes**

In `web/static/app.css`, add:

```css
.search-providers { align-items: flex-start; }
.provider-checks { display: flex; gap: 1rem; flex-wrap: wrap; }
.provider-check { display: flex; align-items: center; gap: 0.25rem; cursor: pointer; font-size: 0.85rem; }
.provider-check input { cursor: pointer; }
```

- [ ] **Step 5: Run the build**

```
go build ./...
```
`main.go` still fails — that's the only remaining failure. Templates compile at runtime so check in Task 8.

- [ ] **Step 6: Commit**

```
git add web/templates/songs.html web/static/app.css
git commit -m "feat(ui): add provider checkboxes to lyrics search form"
```

---

### Task 7: Update cmd/navilyrics/main.go

**Context:** Replace `SetFallback` with new `[]Provider` registration. Derive `providerNames`. Pass to `handlers.New`. Both `runCLI` and `runServer` need updating.

**Files:**
- Modify: `cmd/navilyrics/main.go`

- [ ] **Step 1: Add genius import and update buildProviders helper**

Add a `buildProviders` helper so both `runCLI` and `runServer` share the same logic:

```go
import (
    // ... existing imports ...
    "github.com/user/navilyrics/pkg/genius"
)

// buildProviders constructs the ordered provider list from env config.
// lrclib and netease are always included; genius is added if GENIUS_TOKEN is set.
func buildProviders() []lyrics.Provider {
    providers := []lyrics.Provider{
        lyrics.NewLRCLibProvider(lrclib.New("")),     // "" = use default lrclib base URL
        lyrics.NewNetEaseProvider(netease.New("")),   // "" = use default netease base URL
    }
    if token := os.Getenv("GENIUS_TOKEN"); token != "" {
        providers = append(providers, lyrics.NewGeniusProvider(genius.New(token, ""))) // "" = use default genius API URL
    }
    return providers
}

func providerNames(providers []lyrics.Provider) []string {
    names := make([]string, len(providers))
    for i, p := range providers {
        names[i] = p.Name()
    }
    return names
}
```

- [ ] **Step 2: Update runCLI**

Replace:
```go
lrc := lrclib.New("https://lrclib.net")
proc := lyrics.NewProcessor(nd, lrc, strings.Split(musicDir, ":"), *dryRun)
ne := netease.New("")
proc.SetFallback(ne)
```

With:
```go
providers := buildProviders()
proc := lyrics.NewProcessor(nd, providers, strings.Split(musicDir, ":"), *dryRun)
```

- [ ] **Step 3: Update runServer**

Replace the same pattern, and update the `handlers.New` call:

```go
providers := buildProviders()
proc := lyrics.NewProcessor(nd, providers, strings.Split(musicDir, ":"), dryRun)
// ...
h := handlers.New(nd, proc, tmpls, partials, "dev", providerNames(providers))
```

- [ ] **Step 4: Verify imports are intact**

`lrclib`, `netease`, and `genius` are all referenced by `buildProviders`. Confirm all three are in the import block. The `genius` import (`github.com/user/navilyrics/pkg/genius`) is new — add it. No existing imports need removal.

- [ ] **Step 5: Build**

```
go build ./...
```
Expected: clean build, no errors.

- [ ] **Step 6: Run all tests**

```
go test -race ./...
```
Expected: all pass.

- [ ] **Step 7: Commit**

```
git add cmd/navilyrics/main.go
git commit -m "feat(main): register lrclib/netease/genius providers; wire availableProviders to handler"
```

---

## Final verification

- [ ] `go build ./...` — clean
- [ ] `go test -race ./...` — all pass
- [ ] `make fmt && make vet` — no issues
- [ ] Manual smoke test: start server (`make run-server`), open `/songs`, click a song, open "Find Lyrics" — verify provider checkboxes appear and unselecting a provider changes which source is queried
