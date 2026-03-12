# Provider Selection Design

**Date:** 2026-03-12
**Status:** Approved

## Problem

When fetching lyrics for a single song via the UI, the wrong result is sometimes returned. The user needs to choose which external provider(s) to query so they can try an alternative source when one returns incorrect lyrics.

## Goals

- Add NetEase Cloud Music and Genius as lyrics providers alongside the existing lrclib
- Let the user select which providers to use per individual fetch in the UI
- Keep batch runs using all configured providers (no change to existing behavior)
- Make adding future providers straightforward

## Non-goals

- Implementing a NetEase or Genius provider for batch runs only (individual fetch is the primary use case)
- Custom provider ordering per-batch
- Storing per-song provider preferences

---

## Architecture

### Generic `Provider` interface

Defined in `internal/lyrics/` (where the processor lives):

```go
// ProviderResult is the common result type returned by all providers.
type ProviderResult struct {
    PlainLyrics  string
    SyncedLyrics string
    Instrumental bool
}

// Provider is the interface all lyrics sources must implement.
type Provider interface {
    Name() string
    Search(ctx context.Context, artist, title, album string, duration float64) (ProviderResult, bool, error)
}
```

The `Search` method encapsulates provider-specific logic. For lrclib this means the existing exact-Get-then-fuzzy-Search cascade. For NetEase and Genius, a single search call.

### Processor changes

Replace the `lrc LRCFetcher` + `fallback LRCFetcher` fields with `providers []Provider`.

`FetchLyricsOnly` gains an optional filter parameter:

```go
func (p *Processor) FetchLyricsOnly(ctx context.Context, song navidrome.Song, filter []string) Result
```

- If `filter` is nil/empty, all configured providers are tried in registration order.
- If `filter` is non-empty, only providers whose `Name()` is in the list are tried (in registration order).
- First provider to return `ok == true` wins; remaining providers are skipped.

`ProcessSong` (batch) calls `FetchLyricsOnly` with `nil` filter — no behavior change.

The existing `LRCFetcher` interface is removed. The lrclib client is wrapped by a `lrclibProvider` adapter inside `internal/lyrics/` that implements `Provider`.

### New packages

#### `pkg/netease/`

Unofficial NetEase Cloud Music API. No API key required.

**Search flow:**
1. `POST https://music.163.com/api/search/get` (form-encoded: `s=artist title`, `type=1`, `limit=10`) with spoofed `Referer` and `User-Agent` headers → returns a list of songs with IDs and durations
2. Pick the result whose duration is closest to `targetDuration` (±5 s tolerance, same as lrclib)
3. `GET https://music.163.com/api/song/lyric?id={id}&lv=1&kv=1` → returns `lrc.lyric` (LRC-formatted timestamped lyrics)

Returns synced lyrics (LRC) and plain lyrics. No instrumental flag.

#### `pkg/genius/`

Official Genius API. Requires `GENIUS_TOKEN` env var.

**Search flow:**
1. `GET https://api.genius.com/search?q=artist+title` with `Authorization: Bearer {token}` header → returns hits with song metadata and page URLs
2. Pick the best match by artist name similarity
3. `GET {song_url}` → scrape HTML for `<div data-lyrics-container="true">` elements → strip HTML tags → plain text lyrics

Returns plain lyrics only (Genius has no timed lyrics in their API or pages). `SyncedLyrics` is always empty.

---

## Handler changes

### `POST /songs/{id}/fetch`

Request body gains an optional `providers` field:

```json
{
  "title": "...",
  "artist": "...",
  "album": "...",
  "providers": ["lrclib", "netease"]
}
```

`providers` is passed as the `filter` argument to `FetchLyricsOnly`. If absent or empty, all providers are used.

### Available providers in templates

`internal/handlers/handler.go` gains an `availableProviders []string` field, populated at boot based on which providers were registered (Genius only if `GENIUS_TOKEN` is set).

This slice is passed to any template that needs to render provider controls.

---

## UI changes

The search form in `web/templates/songs.html` gains a provider selector rendered from `availableProviders`. All providers are checked by default. Genius only appears when configured.

Example layout (rendered server-side):
```
[✓] lrclib  [✓] netease  [✓] genius
```

`doFetchLyrics()` in JS reads the checked checkboxes and includes `providers: [...]` in the POST body.

---

## Configuration

| Env var | Required | Purpose |
|---|---|---|
| `GENIUS_TOKEN` | No | Enables Genius provider. If absent, Genius is not registered. |

No new env vars for NetEase.

---

## Registration (main.go)

```go
providers := []lyrics.Provider{
    lyrics.NewLRCLibProvider(lrclib.New("")),
    lyrics.NewNetEaseProvider(netease.New()),
}
if token := os.Getenv("GENIUS_TOKEN"); token != "" {
    providers = append(providers, lyrics.NewGeniusProvider(genius.New(token)))
}
proc := lyrics.NewProcessor(nd, providers, musicDirs, dryRun)
```

---

## File map

| File | Change |
|---|---|
| `pkg/netease/client.go` | New — NetEase search + lyric fetch |
| `pkg/genius/client.go` | New — Genius API search + HTML scrape |
| `internal/lyrics/providers.go` | New — `Provider` interface, `ProviderResult`, adapter constructors |
| `internal/lyrics/processor.go` | Replace `lrc`/`fallback` with `[]Provider`, update `FetchLyricsOnly` signature |
| `internal/handlers/handler.go` | Add `availableProviders []string`, pass to templates |
| `internal/handlers/lrc.go` | Read `providers` from fetch request body, pass to `FetchLyricsOnly` |
| `web/templates/songs.html` | Add provider checkboxes to search form; include in POST body |
| `cmd/navilyrics/main.go` | Register providers at boot, pass to handler |

---

## Error handling

- Provider errors are logged and skipped; the next provider in the list is tried.
- If all providers fail or return not-found, `Result.Status = "not_found"` as today.
- NetEase uses the same soft-error pattern as lrclib: tag-embed failure is non-fatal.
- Genius HTML scraping failure returns `(zero, false, err)` — treated as not-found by the processor.

## Testing

- `pkg/netease/` and `pkg/genius/` get unit tests with an `httptest.Server` standing in for the real APIs.
- `internal/lyrics/processor_test.go` gets a test for provider filtering (stub providers).
