# Architecture

This document describes the high-level architecture of navilyrics.
If you want to familiarize yourself with the codebase, you are in the right place.

## Bird's Eye View

navilyrics bridges a Navidrome music server and a set of lyrics providers. It
fetches lyrics for songs that don't have them, then writes the result in two
places: a `.lrc` sidecar file alongside the audio file on disk, and embedded
`LYRICS`/`SYNCEDLYRICS` tags directly inside the audio file. When done, it
triggers a Navidrome library rescan so the new lyrics become visible without
manual intervention.

The system has two runtime modes. The **CLI batch mode** (`navilyrics run`)
lists every song in Navidrome, processes all missing ones through a worker
pool of four goroutines, and exits. The **web server** (`navilyrics serve`)
exposes a UI for browsing the library, fetching lyrics for individual songs,
editing LRC files inline, and launching batch runs or gap-fill syncs with
live SSE progress. Both modes share the same `Processor` and the same write
path.

A **gap-fill sync** (`navilyrics sync`) handles the case where one side is
already present without a full re-fetch: a song with a `.lrc` but no embedded
tags gets its tags written; a song with embedded tags but no `.lrc` gets a
sidecar written. This covers files imported from other tools.

## Code Map

Modules are listed in dependency/runtime-flow order: data sources first,
orchestration in the middle, HTTP layer last.

### `pkg/navidrome/`

Navidrome REST API client. Authenticates with username/password, stores a JWT
token under a mutex, and exposes `AllSongs` (paginated song listing) and
`TriggerScan`.

Key files: `client.go` (JWT auth, `Do` helper), `songs.go` (song listing),
`types.go` (`Song` struct with all metadata fields).

**Architecture Invariant:** this package never imports any other `pkg/` or
`internal/` package in this repo.

### `pkg/lrclib/`

HTTP client for [lrclib.net](https://lrclib.net). Implements an exact `Get`
(artist + title + album + duration) and a fuzzy `Search` (artist + title
+ ±5 s duration tolerance). The exact path is tried first; fuzzy is the
fallback.

Key file: `client.go`.

### `pkg/netease/`

HTTP client for the NetEase Cloud Music lyrics API. Implements `Search`
returning plain and synced lyrics.

Key file: `client.go`.

### `pkg/genius/`

HTTP client for the Genius API. Returns plain lyrics only (no timestamped
LRC). Requires a `GENIUS_TOKEN`; absent token means the provider is omitted
from the chain at startup.

Key file: `client.go`.

### `pkg/tagger/`

Reads and writes `LYRICS`, `SYNCEDLYRICS`, and `NAVILYRICS_INSTRUMENTAL` tags
directly in audio files. Supports MP3 (ID3v2 via bogem/id3v2) and FLAC
(Vorbis comments via mewkiz/flac). Dispatches on file extension via `ForFile`.

Key files: `tagger.go` (`Tagger` interface, `ForFile` dispatch), `mp3.go`,
`flac.go`, `repair.go` (mp3val-based ID3v2 frame repair for malformed files).

**Architecture Invariant:** this package never imports `internal/`.

### `internal/lyrics/`

The core orchestration layer. `Processor` holds a `navidrome.Client`, an
ordered `[]Provider` slice, and the music directory list. It owns all
decisions about what to fetch, whether to write, and which worker pool to use.

Key files:
- `providers.go` — `Provider` interface and thin wrappers adapting each `pkg/`
  client to that interface. All three providers live here.
- `processor.go` — `Processor` struct; `ProcessSong`, `Run`/`RunSongs`, and
  the `resolveAudioPath` helper that maps Navidrome's relative paths to
  absolute on-disk paths by probing `musicDirs` in order.
- `writer.go` — `writeLyrics` (sidecar + tag embed), `WriteLRCFile` (atomic
  write via temp-file rename), instrumental tag helpers.
- `sync.go` — `SyncSong` and `SyncAll` for gap-fill runs.

**Architecture Invariant:** `internal/lyrics/` is the only package in this
repo that imports from multiple `pkg/` packages. No other `internal/` package
does so.

### `internal/handlers/`

HTTP handlers for the web UI. `Handler` holds a `navidrome.Client`, a
`*Processor`, and pre-parsed template maps. All pages render through
`base.html`; partial responses for HTMX swap targets are rendered without the
base wrapper.

Key files:
- `handler.go` — `Handler` struct, `New`, `render`/`renderPartial`, template
  parsing helpers.
- `runstore.go` — `RunStore`: a mutex-protected map from run ID to
  `chan lyrics.Result`, used to wire background goroutines to SSE streams.
- `songs.go` — songs page, song fetch, LRC save, tag editor.
- `run_progress.go`, `run_sync.go` — batch run and gap-fill sync handlers with
  SSE streaming.
- `dashboard.go` — coverage stats with a short-lived in-memory cache.
- `lrc.go`, `tags.go` — LRC editor and tag editor handlers.

**Architecture Invariant:** handlers never import `pkg/lrclib`, `pkg/netease`,
`pkg/genius`, or `pkg/tagger` directly. All lyrics operations go through
`internal/lyrics/`.

### `web/`

Embedded templates and static assets, exposed as `web.FS` (an `embed.FS`).
Templates live in `web/templates/` (full pages) and
`web/templates/partials/` (HTMX swap targets). CSS and JS live in
`web/static/`.

Key file: `web.go` (single `//go:embed` declaration).

### `cmd/navilyrics/`

Binary entry point. Reads environment variables, constructs the dependency
graph (`navidrome.Client` → `[]Provider` → `Processor`), builds the chi
router, and starts the server or runs the CLI. The `buildProviders` function
is the only place the provider order (lrclib → netease → genius) is decided.

Key file: `main.go`.

## Invariants

**`pkg/` packages never import `internal/`.** Go's module system enforces
this, but the intent is also architectural: `pkg/` packages are pure HTTP
clients with no knowledge of navilyrics' domain logic.

**`internal/lyrics/` is the only package that imports multiple `pkg/`
packages.** Handlers and the CLI reach lyrics functionality only through
`Processor`. This keeps the HTTP layer decoupled from provider details.

**`.lrc` files are written atomically.** `WriteLRCFile` and `WriteRawLRC`
write to a `.tmp` sibling first, then `os.Rename`. A crash mid-write leaves a
`.tmp` orphan rather than a corrupt `.lrc`.

**Tag embed failures are soft errors.** If tag writing fails (unsupported
format, malformed ID3v2 frames that survive repair), `writeLyrics` logs a
warning and returns `nil`. The `.lrc` sidecar alone is sufficient for
Navidrome to surface lyrics; the tag write is best-effort.

**The provider order is fixed at startup.** `buildProviders` in `main.go`
constructs the slice once. `Processor.FetchLyricsOnly` tries providers in
slice order and returns on the first hit. There is no runtime provider
switching outside of the per-song `filter` parameter used by the fetch handler.

**`navidrome.Client` token access is always under a mutex.** The token field
and its expiry are read and written only while holding `Client.mu`. This
prevents data races when the worker pool and the HTTP handler goroutines share
the same client.

## Cross-Cutting Concerns

**Error handling.** Errors wrap their context with `fmt.Errorf("context: %w",
err)`. External I/O errors (Navidrome, providers, disk) propagate up to the
caller. Tag embed errors are the deliberate exception: they are logged and
swallowed so a bad MP3 tag library doesn't block lyrics delivery.

**Logging.** `log.Printf` with a bracketed status prefix: `[found]`,
`[not_found]`, `[error]`, `[sync:embedded]`, etc. No structured logger; the
prefix convention is the only schema.

**Concurrency.** `Run`/`RunSongs`/`SyncAll` each create a fixed-size worker
pool (4 goroutines) using a buffered jobs channel and a `sync.WaitGroup`.
Background SSE goroutines communicate through `RunStore` channels. The stats
cache uses a `sync.RWMutex` for concurrent reads.

**Configuration.** All config comes from environment variables read at startup
in `main.go`. There is no config file. Required variables cause a fatal log
if absent; optional variables have explicit defaults.

**Testing.** Provider adapters and the processor accept interfaces (`Provider`,
`Tagger`) so tests can substitute fakes without HTTP or disk I/O. Integration
tests for `pkg/lrclib`, `pkg/netease`, and `pkg/tagger` use `httptest.Server`
or temp files. Handler tests use `httptest.NewRecorder`.

## A Typical Change

**Adding a new lyrics provider** (e.g. a MusicBrainz client):

1. Create `pkg/musicbrainz/client.go` — a pure HTTP client implementing
   `Search(ctx, artist, title) (plain, synced string, ok bool, err error)`.
2. Add `pkg/musicbrainz/client_test.go` with an `httptest.Server` stub.
3. In `internal/lyrics/providers.go`, add a `musicbrainzProvider` struct that
   wraps the client and implements the `Provider` interface.
4. In `cmd/navilyrics/main.go`, instantiate the client in `buildProviders` and
   append it to the slice at the desired priority position.
5. Run `make test` to verify nothing broke.

No changes to `Processor`, `Handler`, or any template are needed — the
provider chain is the only integration point.
