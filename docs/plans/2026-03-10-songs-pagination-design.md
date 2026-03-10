# Songs Page: Search, Filter, Sort

**Date:** 2026-03-10
**Status:** approved

## Problem

The current `/songs` handler calls `AllSongs`, fetching the entire library on every page load. This is too heavy for large libraries. We want a faster initial load with interactive search, filter, and sort without full page reloads.

## Approach: Stateless fetch + Go-side filter (Option C)

Each request fetches a bounded batch from Navidrome using its native sort/filter params. Cross-field text search is applied in Go after fetching. No server-side caching.

## Data Flow

```
GET /songs
  → navidrome.ListSongs(sort=title, dir=ASC, filter=all, limit=100)
  → full page render (songs.html)

HTMX GET /songs/rows?q=...&sort=...&dir=...&filter=...
  → navidrome.ListSongs(sort, dir, hasLyrics filter, limit=500)
  → Go: filter rows where title|artist|album contains q (case-insensitive)
  → slice to first 100
  → render songs_rows.html partial (<tbody> only)
  → HTMX swaps #songs-tbody
```

## Components

### `pkg/navidrome` — `ListSongs`

```go
type SongQuery struct {
    Sort      string // "title" | "artist" | "album"
    Dir       string // "ASC" | "DESC"
    HasLyrics *bool  // nil=all, true=has, false=missing
    Limit     int
}

func (c *Client) ListSongs(ctx context.Context, q SongQuery) ([]Song, error)
```

Passes `_sort`, `_order`, `_start=0`, `_end=Limit` to Navidrome. `HasLyrics` maps to `has_lyrics=true/false` query param if non-nil.

### `internal/handlers`

- `GET /songs` — `ListSongs(limit=100, no search)` → full `songs.html`
- `GET /songs/rows` — `ListSongs(limit=500)` → Go-side filter → slice 100 → `songs_rows.html` partial

### `web/templates`

- `songs.html` — search input, sortable headers, filter pills; all trigger `hx-get="/songs/rows"` targeting `#songs-tbody`
- `songs_rows.html` — `<tbody id="songs-tbody">` fragment only (no base template)

## UX

- **Search**: single text input, `hx-trigger="input delay:300ms"`, matches title OR artist OR album
- **Sort**: column headers toggle ASC↔DESC; current column shown with ▲/▼; state in query params
- **Filter pills**: All / Missing / Has lyrics; preserves current search + sort on click
- **Count label**: "Showing N songs" updated with each swap
- **Truncation notice**: "showing first 100 — refine your search" when result set was truncated

## What changes

| File | Change |
|---|---|
| `pkg/navidrome/songs.go` | add `SongQuery`, `ListSongs`; keep `AllSongs` (used by CLI + processor) |
| `internal/handlers/songs.go` | `Songs` uses `ListSongs`; add `SongsRows` handler |
| `cmd/navilyrics/main.go` | add `r.Get("/songs/rows", h.SongsRows)` route |
| `web/templates/songs.html` | search input, HTMX attrs, sortable headers, filter pills, count label |
| `web/templates/songs_rows.html` | new — `<tbody>` fragment |
