# Features: NetEase Fallback, LRC Preview, SSE Live-Progress

Date: 2026-03-10

## Goals

Three independent features added to navilyrics:

1. **NetEase fallback** — second lyrics source tried after lrclib fails
2. **LRC sidecar preview** — raw-text endpoint serving the existing `.lrc` file for a song
3. **SSE live-progress** — real-time per-song log while a batch run is in flight

---

## Feature 1: NetEase Fallback

### Package

New `pkg/netease/` satisfying the existing `LRCFetcher` interface.

### API

Uses old `music.163.com/api/` endpoints — no signing required, only specific headers:

```
Referer: https://music.163.com/
Cookie: os=pc; appver=2.0.2
User-Agent: Mozilla/5.0 ...
```

- **Search**: `POST /api/search/get` (form body: `s`, `type=1`, `limit=10`)
  → returns candidate tracks with IDs and duration (ms)
- **Lyrics**: `GET /api/song/lyric?id={id}&lv=-1&kv=-1&tv=-1`
  → returns `lrc.lyric` (LRC string); translation field ignored

### Duration matching

Same ±5 s tolerance used by lrclib `Search`. Pick candidate with closest duration.

### Interface

```go
func New(baseURL string) *Client          // default: https://music.163.com
func (c *Client) Get(...)  (lrclib.Response, bool, error)   // delegates to Search
func (c *Client) Search(...) (lrclib.Response, bool, error)
```

`Get` delegates to `Search` (NetEase has no exact-match endpoint).

### Processor changes

- `Processor` gains an optional `fallback LRCFetcher` field (nil = disabled)
- `NewProcessor` adds a `fallback LRCFetcher` parameter
- Try order: lrclib `Get` → lrclib `Search` → fallback `Search`
- `Result.Source` set to `"lrclib"` or `"netease"` accordingly

### main.go

Construct `netease.New("https://music.163.com")` and pass as fallback to `NewProcessor`.

---

## Feature 2: LRC Sidecar Preview

### Route

```
GET /songs/{id}/lrc
```

Response: `text/plain; charset=utf-8` — raw `.lrc` file contents.
404 if audio path not resolved or `.lrc` file does not exist.

### Flow

1. `nd.GetSong(ctx, id) (Song, error)` — new method; hits `GET /api/song/{id}` on Navidrome
2. `proc.ResolveLRCPath(song.Path) string` — new public method wrapping `resolveAudioPath` + `lrcPathFor`; returns `""` if not found
3. `os.ReadFile(lrcPath)` → write to response

### Navidrome client addition

```go
func (c *Client) GetSong(ctx context.Context, id string) (Song, error)
// GET /api/song/{id} — returns single song or error
```

---

## Feature 3: SSE Live-Progress

### New route

```
GET /run/{id}/events    SSE stream for a run in progress
```

### Run lifecycle

1. `POST /run` or `POST /run/filtered`
   → generate UUID run ID
   → start goroutine: `proc.Run` / `proc.RunSongs` sending to `chan Result`
   → return `run_progress.html` page (HTTP 200, not a redirect)

2. Page opens SSE connection:
   ```html
   <div hx-ext="sse" sse-connect="/run/{id}/events">
     <div id="run-log" sse-swap="result" hx-swap="beforeend"></div>
     <div id="run-summary" sse-swap="done" hx-swap="outerHTML"></div>
   </div>
   ```

3. SSE handler reads `chan Result` until closed:
   - `event: result` — HTML fragment (badge + "Artist — Title") appended to log
   - `event: done` — HTML fragment with final counts swaps `#run-summary`

4. Goroutine closes channel when done; SSE handler sends `done` then closes connection.

### RunStore

```go
type RunStore struct { runs sync.Map }  // key: runID → chan Result
```

Lives on the `Handler` struct. Entry deleted after `done` event is sent.
Concurrent runs supported; each has its own channel.

### Templates

- `run_progress.html` — new template; shows live log div + summary placeholder + SSE wiring
- `run_result.html` — kept for the `navilyrics run` CLI summary (non-SSE)

---

## What does not change

- `LRCFetcher` interface signature (unchanged)
- Worker pool size (4)
- `.lrc` atomic write + tag embed logic
- All existing routes and templates
