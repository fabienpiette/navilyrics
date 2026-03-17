# Goscribe Audio Transcription Integration

**Date:** 2026-03-17
**Status:** Approved

## Overview

Add manual audio-to-text transcription as a fallback lyrics source for songs that have no lyrics from existing providers (lrclib, netease, genius). Transcription is powered by a separately-running [goscribe](https://github.com/user/goscribe) service and is always user-triggered — never automatic.

## Goals

- Transcribe one song at a time with a preview-before-save flow (user approves or discards)
- Transcribe all missing-lyrics songs in a batch with auto-save and SSE progress
- Feature degrades gracefully when `GOSCRIBE_URL` is not configured (buttons hidden, no errors)

## Non-Goals

- goscribe is not added to the automatic provider chain (`Run` / `ProcessSong`)
- No synced/timestamped LRC output (goscribe returns plain text only)
- navilyrics does not manage or start the goscribe process

---

## Architecture

### New Package: `pkg/goscribe/`

Pure HTTP client, no imports from `internal/`. Single file `client.go`.

```go
type Client struct { baseURL string; http *http.Client }

func New(baseURL string) *Client

// SubmitJob uploads the audio file and returns the goscribe job ID.
func (c *Client) SubmitJob(ctx context.Context, audioPath string) (string, error)

// PollJob polls GET /jobs/{id} with the given interval until the job
// reaches completed or failed status, or ctx is cancelled.
func (c *Client) PollJob(ctx context.Context, jobID string, interval time.Duration) (transcript string, error)
```

`SubmitJob` sends a multipart `POST /jobs` with the audio file as the `file` field.
`PollJob` polls at `interval` (default 3s), returns `transcript` on success, error on failure or context cancellation.

### New Interface + Implementation: `internal/lyrics/transcribers.go`

```go
// Transcriber transcribes audio files into plain-text lyrics.
// Defined in the consumer package, mirroring the Provider pattern.
type Transcriber interface {
    Name() string
    Transcribe(ctx context.Context, audioPath string) (string, error)
}

type goscribeTranscriber struct{ c *goscribe.Client }

func NewGoscribeTranscriber(c *goscribe.Client) Transcriber
```

The `goscribeTranscriber.Transcribe` calls `SubmitJob` then `PollJob`.

### Updated: `internal/lyrics/processor.go`

`Processor` gains a `transcribers []Transcriber` field. `NewProcessor` signature is extended:

```go
func NewProcessor(nd *navidrome.Client, providers []Provider, transcribers []Transcriber, musicDirs []string, dryRun bool) *Processor
```

Two new methods:

```go
// TranscribeSong resolves the audio path and calls the first configured
// transcriber. Returns the resolved audioPath and transcript text.
// Returns an error if no transcribers are configured or the audio file is not found.
func (p *Processor) TranscribeSong(ctx context.Context, song navidrome.Song) (audioPath, transcript string, err error)

// TranscribeAll runs TranscribeSong for every song in the list with a
// worker pool of 2 (goscribe handles its own async processing).
// On success each song is saved automatically via SaveLyrics.
// progress is called once per result (may be called from any goroutine).
func (p *Processor) TranscribeAll(ctx context.Context, songs []navidrome.Song, progress func(Result)) error
```

`TranscribeAll` emits `Result` with status `"transcribed"` on success. Failed songs emit status `"error"`.

### New: `internal/handlers/transcribe.go`

Five HTTP handlers:

| Method | Path | Description |
|--------|------|-------------|
| `POST` | `/transcribe/song` | Submit single-song transcription job, return polling partial |
| `GET`  | `/transcribe/song/poll` | Poll goscribe job status; return preview partial when done |
| `POST` | `/transcribe/song/save` | Save approved transcript to disk |
| `POST` | `/transcribe/batch` | Start batch transcription, render SSE progress page |
| `GET`  | `/transcribe/batch/{id}/events` | SSE stream of batch results |

---

## Single-Song Flow

```
User clicks "Transcribe" on a song row
  → POST /transcribe/song  (song_id)
      Resolves song from Navidrome
      Submits job to goscribe (SubmitJob)
      Returns transcribe_poll.html partial (job_id + song_id embedded)

transcribe_poll.html polls every 3s:
  → GET /transcribe/song/poll?job_id=xxx&song_id=yyy
      If queued/processing: return same polling partial (HTMX re-triggers)
      If completed:         return transcript_preview.html partial
      If failed:            return error partial

transcript_preview.html shows:
  - Editable <textarea> with transcript text
  - "Approve" button  → POST /transcribe/song/save
  - "Discard" button  → replaces panel with nothing (hx-swap)

POST /transcribe/song/save  (song_id + transcript)
  Calls proc.SaveLyrics(song, transcript, "")
  Returns success partial (replaces preview panel)
```

## Batch Flow

```
User clicks "Transcribe all missing"
  → POST /transcribe/batch
      Fetches all songs with HasLyrics=false
      Starts background TranscribeAll goroutine
      Renders transcribe_progress.html (SSE page)

GET /transcribe/batch/{id}/events  (SSE)
  Streams one Result event per song
  On completion fires Navidrome scan (same as RunSync)
```

---

## UI Changes

### `songs_rows.html` partial

Each song row without lyrics gains a "Transcribe" button, rendered only when `GoscribeEnabled` is true in template data. The button targets an inline panel below/beside the row via HTMX swap.

### `songs.html` page

When `GoscribeEnabled`, a "Transcribe all missing" button appears in the existing run-controls area.

### New partials

```
web/templates/partials/
  transcribe_poll.html       — spinner + HTMX polling (hx-get every 3s)
  transcript_preview.html    — editable textarea, Approve/Discard buttons
  transcribe_result.html     — success or error message after save
```

### New page template

```
web/templates/
  transcribe_progress.html   — SSE progress page (mirrors sync_progress.html layout)
                               adds "transcribed" status row style
```

---

## Config & Wiring

**Environment variable:** `GOSCRIBE_URL` (e.g. `http://goscribe:8080`)

**`cmd/navilyrics/main.go`:**
- Read `GOSCRIBE_URL`
- If set: `goscribe.New(url)` → `lyrics.NewGoscribeTranscriber(client)` → pass to `NewProcessor`
- Pass `goscribeEnabled bool` to `handlers.New()`

**`internal/handlers/handler.go`:**
- Add `goscribeEnabled bool` to `Handler` struct
- Propagate to template data structs (`songsData`, etc.)

**Router** (registered in `main.go`):
```
r.Post("/transcribe/song",              h.TranscribeSong)
r.Get("/transcribe/song/poll",          h.TranscribeSongPoll)
r.Post("/transcribe/song/save",         h.TranscribeSongSave)
r.Post("/transcribe/batch",             h.TranscribeBatch)
r.Get("/transcribe/batch/{id}/events",  h.TranscribeBatchEvents)
```

---

## Error Handling

- `GOSCRIBE_URL` unset → feature absent, no error on startup
- goscribe unreachable at job submission → HTTP 502 with error partial in UI
- `PollJob` timeout (default 5 min via context deadline on the handler) → error partial
- Audio file not found on disk → error partial (song_id with no resolved path)
- `TranscribeAll` continues past per-song errors, emits `Result{Status:"error"}` per failure

---

## Testing

- `pkg/goscribe`: unit tests with `httptest.Server` stub for `/jobs` and `/jobs/{id}`
- `internal/lyrics`: `Transcriber` interface enables mock in processor tests; `TranscribeAll` tested with a mock that returns canned transcripts
- Handlers: existing handler test patterns (httptest + mock processor)

---

## Files Changed / Created

| File | Change |
|------|--------|
| `pkg/goscribe/client.go` | New |
| `pkg/goscribe/client_test.go` | New |
| `internal/lyrics/transcribers.go` | New |
| `internal/lyrics/processor.go` | Add `transcribers` field + 2 methods |
| `internal/handlers/transcribe.go` | New |
| `internal/handlers/handler.go` | Add `goscribeEnabled` field |
| `internal/handlers/songs.go` | Add `GoscribeEnabled` to template data |
| `cmd/navilyrics/main.go` | Wire `GOSCRIBE_URL` → transcriber |
| `web/templates/songs.html` | Add "Transcribe all missing" button |
| `web/templates/partials/songs_rows.html` | Add per-row "Transcribe" button |
| `web/templates/partials/transcribe_poll.html` | New |
| `web/templates/partials/transcript_preview.html` | New |
| `web/templates/partials/transcribe_result.html` | New |
| `web/templates/transcribe_progress.html` | New |
