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

Defined in `internal/lyrics/providers.go`:

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
    // Search finds lyrics by artist, title, album, and duration (seconds).
    // album may be empty. Returns (zero, false, nil) if not found.
    Search(ctx context.Context, artist, title, album string, duration float64) (ProviderResult, bool, error)
}
```

The `album` parameter is included in the interface so the lrclib adapter can pass it to `lrclib.Client.Get`. Other providers that don't use album may ignore it.

### Processor changes

**Constructor** — updated signature replacing `lrc`/`fallback` with a provider slice:

```go
func NewProcessor(nd *navidrome.Client, providers []Provider, musicDirs []string, dryRun bool) *Processor
```

**`FetchLyricsOnly`** — gains an optional filter parameter:

```go
func (p *Processor) FetchLyricsOnly(ctx context.Context, song navidrome.Song, filter []string) Result
```

- If `filter` is nil/empty, all configured providers are tried in registration order.
- If `filter` contains provider names, only those whose `Name()` matches are tried (in registration order). Unknown names are silently ignored — if the filter matches no registered provider, `Status = "not_found"` is returned.
- First provider to return `ok == true` wins; remaining providers are skipped.
- `Result.Source` is set to `winner.Name()` for whichever provider finds lyrics.

**`ProcessSong`** (batch) must be updated to call `FetchLyricsOnly(ctx, song, nil)`. This is a compile-time breaking change — every call site in the package must be updated.

**`SetFallback`** method must be deleted along with the `lrc` and `fallback` fields and the `LRCFetcher` interface. Any call to `SetFallback` in `main.go` must also be removed.

The existing `LRCFetcher` interface is removed. The lrclib client is wrapped by a `lrclibProvider` adapter in `internal/lyrics/providers.go` that:
- Implements the exact-Get-then-fuzzy-Search cascade internally, passing `album` to `lrclib.Client.Get`
- Maps `lrclib.Response.Instrumental` → `ProviderResult.Instrumental` (preserving the instrumental flag)

### New packages

#### `pkg/netease/`

Unofficial NetEase Cloud Music API. No API key required.

**Search flow:**
1. `POST https://music.163.com/api/search/get` (form-encoded: `s=artist title`, `type=1`, `limit=10`) with spoofed `Referer` and `User-Agent` headers → returns a list of songs with IDs and durations
2. Pick the result whose duration is closest to `targetDuration` (±5 s tolerance, same as lrclib)
3. `GET https://music.163.com/api/song/lyric?id={id}&lv=1&kv=1` → returns `lrc.lyric` (LRC-formatted timestamped lyrics)

Returns synced lyrics (LRC) and plain lyrics (strip timestamps from synced). If the lyric body is empty after fetching, return `(zero, false, nil)` — treat as not-found rather than returning a result with empty lyrics. `Instrumental` is always `false` (NetEase has no instrumental flag).

#### `pkg/genius/`

Official Genius API. Requires `GENIUS_TOKEN` env var.

**Search flow:**
1. `GET https://api.genius.com/search?q=artist+title` with `Authorization: Bearer {token}` header → returns hits with song metadata and page URLs
2. Pick the best hit by comparing `primary_artist.name` to the requested artist (case-insensitive contains match)
3. `GET {song_url}` → scrape HTML for `<div data-lyrics-container="true">` elements → strip HTML tags → plain text lyrics

Returns plain lyrics only (`SyncedLyrics` is always empty — Genius has no timed lyrics). `Instrumental` is always `false`.

**Known fragility:** Genius regularly updates their page HTML. If the `data-lyrics-container` selector stops matching, `Search` returns `(zero, false, nil)` — treated as not-found, no error surfaced to the user.

---

## Handler changes

### `POST /songs/{id}/fetch`

The existing decode struct is **extended** (not replaced) to add `Providers`:

```go
var body struct {
    Title     string   `json:"title"`
    Artist    string   `json:"artist"`
    Album     string   `json:"album"`
    Providers []string `json:"providers"` // new; nil = use all
}
```

`body.Providers` is passed as the `filter` argument to `FetchLyricsOnly`. If absent or empty, all providers are used. Unknown provider names are silently ignored by the processor.

### Available providers in templates

`internal/handlers/handler.go` gains an `availableProviders []string` field. The `New()` constructor is updated to accept it — the full updated signature is:

```go
func New(
    nd       *navidrome.Client,
    proc     *lyrics.Processor,
    tmpls    map[string]*template.Template,
    partials map[string]*template.Template,
    version  string,
    availableProviders []string,
) *Handler
```

The slice is stored on `Handler` and threaded into any template data struct that needs to render provider controls.

---

## UI changes

The search form in `web/templates/songs.html` gains a provider selector rendered from `availableProviders`. All providers are checked by default. Genius only appears when configured (i.e. when it is in `availableProviders`).

Example layout (rendered server-side):
```
[✓] lrclib  [✓] netease  [✓] genius
```

`doFetchLyrics()` reads the checked checkboxes and includes `providers: [...]` in the POST body.

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
    lyrics.NewLRCLibProvider(lrclib.New("")), // "" = use default base URL
    lyrics.NewNetEaseProvider(netease.New()),
}
if token := os.Getenv("GENIUS_TOKEN"); token != "" {
    providers = append(providers, lyrics.NewGeniusProvider(genius.New(token)))
}

providerNames := make([]string, len(providers))
for i, p := range providers {
    providerNames[i] = p.Name()
}

proc := lyrics.NewProcessor(nd, providers, musicDirs, dryRun)
h := handlers.New(nd, proc, tmpls, partials, version, providerNames)
```

---

## File map

| File | Change |
|---|---|
| `pkg/netease/client.go` | New — NetEase search + lyric fetch |
| `pkg/genius/client.go` | New — Genius API search + HTML scrape |
| `internal/lyrics/providers.go` | New — `Provider` interface, `ProviderResult`, `lrclibProvider`/`netEaseProvider`/`geniusProvider` adapters |
| `internal/lyrics/processor.go` | Remove `LRCFetcher`, `lrc`, `fallback`, `SetFallback`; replace with `[]Provider`; update `NewProcessor` and `FetchLyricsOnly` signatures; update `ProcessSong` to pass `nil` filter; update `Result.Source` doc comment |
| `internal/handlers/handler.go` | Add `availableProviders []string`; update `New()` to accept it |
| `internal/handlers/lrc.go` | Extend fetch body struct with `Providers`; pass to `FetchLyricsOnly` |
| `web/templates/songs.html` | Add provider checkboxes to search form; include checked names in POST body |
| `cmd/navilyrics/main.go` | Register providers at boot; derive `providerNames`; pass to `handlers.New`; remove `SetFallback` call |

---

## Error handling

- Provider errors are logged and skipped; the next provider in the list is tried.
- If all providers fail or return not-found, `Result.Status = "not_found"` as today.
- NetEase: empty lyric body after a successful HTTP fetch → return `(zero, false, nil)`.
- Genius HTML scraping failure (no matching div) → return `(zero, false, nil)`.
- Unknown provider names in the `filter` list are silently ignored.
- `Result.Source` is now set dynamically to the winning provider's `Name()`. The doc comment on `Result.Source` must be updated from the hardcoded `"lrclib" | "netease" | ""` to `// name of the winning provider, or ""`.

## Testing

- `pkg/netease/` and `pkg/genius/` get unit tests with `httptest.Server` stubs for the real APIs.
- `internal/lyrics/processor_test.go` gets a test for provider filtering: a filter containing one valid name should only call that provider; a filter with only unknown names should return `not_found`.
