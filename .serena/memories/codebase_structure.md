# navilyrics — Codebase Structure

```
cmd/navilyrics/        — entrypoint (main package)
pkg/navidrome/         — Navidrome REST client (JWT auth, song listing, pagination)
  client.go            — Client struct, New, Authenticate, ensureToken, Do
  songs.go             — AllSongs (paginated), TriggerScan
  types.go             — Song, authRequest, authResponse
pkg/lrclib/            — lrclib.net HTTP client
  client.go            — Client, New, Get (exact), Search (fuzzy ±5s duration)
pkg/tagger/            — Audio tag read/write
  tagger.go            — Tagger interface, ForFile factory
  mp3.go               — mp3Tagger (bogem/id3v2/v2)
  flac.go              — flacTagger (mewkiz/flac)
internal/lyrics/       — Processor: fetches lyrics, writes .lrc, embeds tags, worker pool (4)
internal/handlers/     — HTTP handlers
web/templates/         — html/template embedded via //go:embed
web/static/            — Static assets
docs/plans/            — Design and implementation plan docs
```

## Key architectural rules
- `pkg/` packages NEVER import `internal/`
- `internal/lyrics/` is the ONLY package that imports multiple `pkg/` packages
- LRCFetcher is an interface in internal/lyrics for testability
- No `defer resp.Body.Close()` inside loops — close explicitly per iteration
- Navidrome JWT token guarded by mutex to prevent data race
