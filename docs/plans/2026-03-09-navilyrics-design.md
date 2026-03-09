# navilyrics — Design Document

Date: 2026-03-09

## Purpose

Self-hosted tool to complete a Navidrome music library with missing lyrics. Fetches
synced and plain lyrics from lrclib.net (with NetEase fallback), writes `.lrc` sidecar
files next to audio files, and embeds `LYRICS` / `SYNCEDLYRICS` tags into the audio
files themselves.

---

## Tech Stack

- **Language:** Go 1.23+
- **Router:** `github.com/go-chi/chi/v5`
- **Frontend:** Vanilla HTML + CSS + HTMX 2.x (CDN)
- **Templates:** Go `html/template`, embedded via `//go:embed`
- **Deployment:** Docker + Docker Compose
- **Module:** `github.com/user/navilyrics`

---

## Architecture

Single binary `navilyrics` with two subcommands:

| Command | Description |
|---|---|
| `navilyrics serve` | Starts the HTMX web UI |
| `navilyrics run [--dry-run]` | One-shot CLI batch processor |

---

## Project Layout

```
cmd/navilyrics/main.go      # entry point — subcommands, env config, bootstrap
internal/handlers/          # HTTP handlers (chi, html/template)
internal/lyrics/            # core: match logic, .lrc writer, tag embedder
pkg/navidrome/              # Navidrome REST API client (JWT, song listing)
pkg/lrclib/                 # lrclib.net HTTP API client
pkg/tagger/                 # audio tag read/write (LYRICS + SYNCEDLYRICS)
web/web.go                  # //go:embed entry point
web/templates/              # Go HTML templates
web/static/                 # app.css
docs/                       # documentation and design docs
Makefile
Dockerfile
docker-compose.yml
.env.example
go.mod
```

---

## Configuration (env vars)

| Variable | Default | Description |
|---|---|---|
| `NAVIDROME_URL` | — | Navidrome base URL (e.g. `http://navidrome:4533`) |
| `NAVIDROME_USER` | — | Navidrome username |
| `NAVIDROME_PASS` | — | Navidrome password |
| `MUSIC_DIR` | — | Absolute path to music library (must match Navidrome's `MusicFolder`) |
| `PORT` | `8080` | HTTP server port (serve mode only) |
| `DRY_RUN` | `false` | Preview without writing any files |

---

## Data Flow

### CLI mode (`navilyrics run`)

```
Navidrome API → list all songs
    ↓
For each song (worker pool, 4 workers):
  1. Check filesystem: does <song>.lrc exist?
  2. Check audio file: does it have LYRICS + SYNCEDLYRICS tags?
  3. If both present → skip
  4. If missing → query lrclib.net (artist + title + duration)
       Strategy 1: GET /api/get?artist=X&title=Y&album=Z&duration=N
       Strategy 2: GET /api/search?q=X+Y → pick best duration match (±5s)
  5. If no match → try NetEase Music API (free, no key)
  6. If found:
       a. Write <song_basename>.lrc next to audio file (temp → rename)
       b. Embed SYNCEDLYRICS (LRC) + LYRICS (plain stripped) in audio tags
       c. Log result: found | not_found | skipped | error
  7. After all songs → POST /api/scan to trigger Navidrome rescan
```

### Web app mode (`navilyrics serve`)

```
Dashboard
  ├── Stats: total / with lyrics / missing / last run timestamp
  ├── Songs table (filterable: missing | partial | complete)
  │     └── Per-song row: preview fetched lyrics → confirm → write
  └── "Fetch All Missing" button → triggers same pipeline as CLI run
                                   with live progress via SSE or polling
```

---

## Lyrics Pipeline

### Result type

```go
type Result struct {
    SongID       string
    SongPath     string  // absolute path on filesystem
    PlainLyrics  string  // for LYRICS tag
    SyncedLyrics string  // LRC format, for SYNCEDLYRICS tag + .lrc file
    Source       string  // "lrclib" | "netease" | ""
    Status       string  // "found" | "not_found" | "skipped" | "error"
}
```

### Writing (atomic per-song)

1. Write `.lrc` (temp file → rename)
2. Read audio tags → set LYRICS + SYNCEDLYRICS → write back
3. On any failure → roll back (delete `.lrc` if written), log error, continue next song

### DRY_RUN mode

Skips all writes. Logs what would be written. Safe for previewing library coverage.

### Concurrency

Worker pool of 4 goroutines. Rate-limited to ~1 req/s per worker against lrclib.net.

---

## Audio Tag Support

`pkg/tagger/` wraps a Go audio tag library to read/write tags across:
- **MP3** — ID3v2 tags: `USLT` (LYRICS), `SYLT` (SYNCEDLYRICS)
- **FLAC / OGG / OPUS** — Vorbis Comments: `LYRICS`, `SYNCEDLYRICS`
- **M4A** — MP4 atoms: `©lyr`

---

## Deployment

```yaml
# docker-compose.yml
services:
  navilyrics:
    image: navilyrics
    environment:
      NAVIDROME_URL: http://navidrome:4533
      NAVIDROME_USER: admin
      NAVIDROME_PASS: secret
      MUSIC_DIR: /music
    volumes:
      - /your/music:/music   # same path as Navidrome's MusicFolder
    ports:
      - "8080:8080"
```

**Key constraint:** `MUSIC_DIR` must match Navidrome's `MusicFolder` exactly.
`Song.Path` values from the Navidrome API are used directly to locate files on disk.

---

## Testing Strategy

| Package | Approach |
|---|---|
| `pkg/lrclib/` | Unit tests with `httptest.Server` mocking lrclib responses |
| `pkg/tagger/` | Unit tests with fixture audio files (tiny valid MP3/FLAC) |
| `internal/lyrics/` | Table-driven tests for matching logic and dry-run behavior |
| `internal/handlers/` | Handler tests with mock `Processor` |

---

## Makefile Targets

Inherits all navilist targets plus:

| Target | Description |
|---|---|
| `make run-cli` | Run `navilyrics run --dry-run` locally |
| `make run-server` | Run `navilyrics serve` locally |
