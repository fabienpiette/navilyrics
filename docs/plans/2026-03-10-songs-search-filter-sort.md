# Songs Page Search/Filter/Sort Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Replace the all-songs full-page load with a 100-row initial view plus HTMX-powered search (title/artist/album), filter (all/missing/has), and sort (title/artist/album) that swap only the table body.

**Architecture:** Add `ListSongs(ctx, SongQuery)` to `pkg/navidrome` for bounded, sorted fetches. The `/songs/rows` HTMX endpoint fetches up to 500 rows, applies Go-side case-insensitive multi-field search, slices to 100, and returns a `<tbody>` partial. The full `/songs` page renders the chrome + initial rows. All state (query, sort, dir, filter) travels as query params.

**Tech Stack:** Go `html/template`, HTMX 2.x (served locally at `/static/htmx.min.js`), chi v5 router, `pkg/navidrome` Navidrome REST client.

---

### Task 1: `pkg/navidrome` — add `SongQuery` + `ListSongs`

**Files:**
- Modify: `pkg/navidrome/songs.go`
- Modify: `pkg/navidrome/client_test.go`

**Step 1: Write the failing test** (add to `client_test.go`)

```go
func TestListSongs(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/auth/login":
			json.NewEncoder(w).Encode(map[string]string{"token": "tok"})
		case "/api/song":
			q := r.URL.Query()
			if q.Get("_sort") != "artist" {
				t.Errorf("want _sort=artist, got %q", q.Get("_sort"))
			}
			if q.Get("_order") != "DESC" {
				t.Errorf("want _order=DESC, got %q", q.Get("_order"))
			}
			if q.Get("_start") != "0" {
				t.Errorf("want _start=0, got %q", q.Get("_start"))
			}
			if q.Get("_end") != "50" {
				t.Errorf("want _end=50, got %q", q.Get("_end"))
			}
			if q.Get("has_lyrics") != "true" {
				t.Errorf("want has_lyrics=true, got %q", q.Get("has_lyrics"))
			}
			json.NewEncoder(w).Encode([]navidrome.Song{{ID: "1", Title: "T", HasLyrics: true}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	c := navidrome.New(srv.URL, "u", "p")
	_ = c.Authenticate()
	hasLyrics := true
	songs, err := c.ListSongs(context.Background(), navidrome.SongQuery{
		Sort:      "artist",
		Dir:       "DESC",
		HasLyrics: &hasLyrics,
		Limit:     50,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(songs) != 1 {
		t.Fatalf("want 1 song, got %d", len(songs))
	}
}
```

**Step 2: Run test to verify it fails**

```bash
go test ./pkg/navidrome/ -run TestListSongs -v
```
Expected: FAIL — `c.ListSongs undefined`

**Step 3: Implement in `pkg/navidrome/songs.go`** (add after `AllSongs`)

```go
// SongQuery parameterises a ListSongs request.
type SongQuery struct {
	Sort      string // "title" | "artist" | "album"
	Dir       string // "ASC" | "DESC"
	HasLyrics *bool  // nil = all, true = has lyrics, false = missing
	Limit     int
}

// ListSongs fetches up to q.Limit songs with the given sort/filter.
func (c *Client) ListSongs(ctx context.Context, q SongQuery) ([]Song, error) {
	params := url.Values{
		"_start": []string{"0"},
		"_end":   []string{strconv.Itoa(q.Limit)},
		"_sort":  []string{q.Sort},
		"_order": []string{q.Dir},
	}
	if q.HasLyrics != nil {
		params.Set("has_lyrics", strconv.FormatBool(*q.HasLyrics))
	}
	resp, err := c.Do(ctx, http.MethodGet, "/api/song?"+params.Encode(), nil)
	if err != nil {
		return nil, fmt.Errorf("list songs: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("list songs: status %d", resp.StatusCode)
	}
	var songs []Song
	err = json.NewDecoder(resp.Body).Decode(&songs)
	resp.Body.Close()
	if err != nil {
		return nil, fmt.Errorf("list songs decode: %w", err)
	}
	return songs, nil
}
```

**Step 4: Run tests**

```bash
go test -race ./pkg/navidrome/ -v
```
Expected: all PASS including `TestListSongs`.

**Step 5: Commit**

```bash
git add pkg/navidrome/songs.go pkg/navidrome/client_test.go
git commit -m "feat(navidrome): add ListSongs with SongQuery params"
```

---

### Task 2: `internal/handlers` — partial support + `SongsRows` handler

**Files:**
- Modify: `internal/handlers/handler.go`
- Modify: `internal/handlers/songs.go`

**Step 1: Update `handler.go`**

Replace the entire file:

```go
package handlers

import (
	"html/template"
	"io/fs"
	"net/http"
	"path"
	"strings"

	"github.com/user/navilyrics/internal/lyrics"
	"github.com/user/navilyrics/pkg/navidrome"
)

// Handler holds shared dependencies for all HTTP handlers.
type Handler struct {
	nd      *navidrome.Client
	proc    *lyrics.Processor
	tmpls   map[string]*template.Template
	partials map[string]*template.Template
	version string
}

// New creates a Handler.
func New(nd *navidrome.Client, proc *lyrics.Processor, tmpls, partials map[string]*template.Template, version string) *Handler {
	return &Handler{nd: nd, proc: proc, tmpls: tmpls, partials: partials, version: version}
}

// render executes a full page template (enters via base.html).
func (h *Handler) render(w http.ResponseWriter, name string, data any) {
	t, ok := h.tmpls[name]
	if !ok {
		http.Error(w, "template not found: "+name, http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := t.ExecuteTemplate(w, "base.html", data); err != nil {
		http.Error(w, "template error: "+err.Error(), http.StatusInternalServerError)
	}
}

// renderPartial executes a standalone partial template (no base.html wrapper).
func (h *Handler) renderPartial(w http.ResponseWriter, name string, data any) {
	t, ok := h.partials[name]
	if !ok {
		http.Error(w, "partial not found: "+name, http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := t.Execute(w, data); err != nil {
		http.Error(w, "partial error: "+err.Error(), http.StatusInternalServerError)
	}
}

// ParseTemplates builds a per-page template map (each page clones base.html).
func ParseTemplates(fsys fs.FS) (map[string]*template.Template, error) {
	base, err := template.ParseFS(fsys, "templates/base.html")
	if err != nil {
		return nil, err
	}
	pages, err := fs.Glob(fsys, "templates/*.html")
	if err != nil {
		return nil, err
	}
	tmpls := make(map[string]*template.Template, len(pages))
	for _, p := range pages {
		name := path.Base(p)
		if name == "base.html" {
			continue
		}
		t, err := template.Must(base.Clone()).ParseFS(fsys, p)
		if err != nil {
			return nil, err
		}
		tmpls[name] = t
	}
	return tmpls, nil
}

// ParsePartials builds a map of standalone partial templates.
func ParsePartials(fsys fs.FS) (map[string]*template.Template, error) {
	files, err := fs.Glob(fsys, "templates/partials/*.html")
	if err != nil {
		return nil, err
	}
	partials := make(map[string]*template.Template, len(files))
	for _, f := range files {
		name := path.Base(f)
		t, err := template.New(name).Funcs(template.FuncMap{
			"lower": strings.ToLower,
		}).ParseFS(fsys, f)
		if err != nil {
			return nil, err
		}
		partials[name] = t
	}
	return partials, nil
}
```

**Step 2: Update `songs.go`** — replace the `Songs` handler and add `SongsRows`

```go
package handlers

import (
	"context"
	"net/http"
	"strings"
	"sync"

	"github.com/user/navilyrics/internal/lyrics"
	"github.com/user/navilyrics/pkg/navidrome"
)

const (
	maxFetch   = 500
	maxDisplay = 100
)

type songsRowsData struct {
	Songs     []navidrome.Song
	Query     string
	Sort      string
	Dir       string
	Filter    string
	Total     int  // count after search filter, before slice
	Truncated bool // true when Total > maxDisplay
}

type songsData struct {
	ActiveTab string
	Version   string
	songsRowsData
}

// Songs renders the /songs full page with the first 100 songs.
func (h *Handler) Songs(w http.ResponseWriter, r *http.Request) {
	sort, dir, filter := songParams(r)
	rows, err := h.fetchRows(r.Context(), "", sort, dir, filter)
	if err != nil {
		http.Error(w, "navidrome: "+err.Error(), http.StatusBadGateway)
		return
	}
	h.render(w, "songs.html", songsData{
		ActiveTab:     "songs",
		Version:       h.version,
		songsRowsData: rows,
	})
}

// SongsRows is the HTMX partial endpoint — returns only <tbody> rows.
func (h *Handler) SongsRows(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query().Get("q")
	sort, dir, filter := songParams(r)
	rows, err := h.fetchRows(r.Context(), q, sort, dir, filter)
	if err != nil {
		http.Error(w, "navidrome: "+err.Error(), http.StatusBadGateway)
		return
	}
	h.renderPartial(w, "songs_rows.html", rows)
}

// songParams extracts and validates sort/dir/filter from request query params.
func songParams(r *http.Request) (sort, dir, filter string) {
	sort = r.URL.Query().Get("sort")
	switch sort {
	case "artist", "album":
	default:
		sort = "title"
	}
	dir = r.URL.Query().Get("dir")
	if dir != "DESC" {
		dir = "ASC"
	}
	filter = r.URL.Query().Get("filter")
	switch filter {
	case "missing", "has":
	default:
		filter = "all"
	}
	return
}

// fetchRows calls Navidrome and applies Go-side search + truncation.
func (h *Handler) fetchRows(ctx context.Context, query, sort, dir, filter string) (songsRowsData, error) {
	var hasLyrics *bool
	switch filter {
	case "has":
		v := true
		hasLyrics = &v
	case "missing":
		v := false
		hasLyrics = &v
	}

	songs, err := h.nd.ListSongs(ctx, navidrome.SongQuery{
		Sort:      sort,
		Dir:       dir,
		HasLyrics: hasLyrics,
		Limit:     maxFetch,
	})
	if err != nil {
		return songsRowsData{}, err
	}

	// Go-side multi-field search.
	if query != "" {
		q := strings.ToLower(query)
		filtered := songs[:0]
		for _, s := range songs {
			if strings.Contains(strings.ToLower(s.Title), q) ||
				strings.Contains(strings.ToLower(s.Artist), q) ||
				strings.Contains(strings.ToLower(s.Album), q) {
				filtered = append(filtered, s)
			}
		}
		songs = filtered
	}

	total := len(songs)
	truncated := total > maxDisplay
	if truncated {
		songs = songs[:maxDisplay]
	}

	return songsRowsData{
		Songs:     songs,
		Query:     query,
		Sort:      sort,
		Dir:       dir,
		Filter:    filter,
		Total:     total,
		Truncated: truncated,
	}, nil
}

type runResult struct {
	ActiveTab string
	Version   string
	Results   []lyrics.Result
	Found     int
	NotFound  int
	Skipped   int
	Errors    int
}

// RunBatch processes all missing songs and renders a result page.
func (h *Handler) RunBatch(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var mu sync.Mutex
	var results []lyrics.Result

	ctx := r.Context()
	err := h.proc.Run(ctx, func(res lyrics.Result) {
		mu.Lock()
		results = append(results, res)
		mu.Unlock()
	})

	found, notFound, skipped, errs := 0, 0, 0, 0
	for _, res := range results {
		switch res.Status {
		case "found", "dry_run":
			found++
		case "not_found":
			notFound++
		case "skipped":
			skipped++
		case "error":
			errs++
		}
	}

	data := runResult{
		ActiveTab: "songs",
		Version:   h.version,
		Results:   results,
		Found:     found,
		NotFound:  notFound,
		Skipped:   skipped,
		Errors:    errs,
	}
	if err != nil {
		data.Errors++
	}
	h.render(w, "run_result.html", data)
}
```

**Step 3: Build to verify no compile errors**

```bash
go build ./...
```
Expected: compile errors in `cmd/navilyrics/main.go` because `handlers.New` signature changed — fix in Task 3.

**Step 4: Commit (after Task 3 fixes compile)**

Deferred — commit together with Task 3.

---

### Task 3: `cmd/navilyrics/main.go` — wire new route + ParsePartials

**Files:**
- Modify: `cmd/navilyrics/main.go`

**Step 1: Update `runServer` in `main.go`**

Replace the `tmpls` + `h` construction and add the new route:

```go
	tmpls, err := handlers.ParseTemplates(web.FS)
	if err != nil {
		return fmt.Errorf("parse templates: %w", err)
	}
	partials, err := handlers.ParsePartials(web.FS)
	if err != nil {
		return fmt.Errorf("parse partials: %w", err)
	}

	h := handlers.New(nd, proc, tmpls, partials, "dev")
```

And add the route:
```go
	r.Get("/songs/rows", h.SongsRows)
```

Full updated route block:
```go
	r.Get("/", h.Dashboard)
	r.Get("/songs", h.Songs)
	r.Get("/songs/rows", h.SongsRows)
	r.Post("/run", h.RunBatch)
	r.Get("/favicon.ico", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
	r.Handle("/static/*", http.StripPrefix("/static/", http.FileServer(http.FS(staticFS))))
```

**Step 2: Build and test**

```bash
go build ./... && make test
```
Expected: all tests pass, binary builds.

**Step 3: Commit**

```bash
git add internal/handlers/handler.go internal/handlers/songs.go cmd/navilyrics/main.go
git commit -m "feat(handlers): add SongsRows partial handler with search and sort"
```

---

### Task 4: Templates — update `songs.html` + add `songs_rows.html` partial

**Files:**
- Modify: `web/templates/songs.html`
- Create: `web/templates/partials/songs_rows.html`

**Step 1: Replace `web/templates/songs.html`**

```html
{{define "title"}}navilyrics — songs{{end}}

{{define "content"}}
<div id="songs-controls">
  <input type="hidden" id="sort-input" name="sort" value="{{.Sort}}">
  <input type="hidden" id="dir-input"  name="dir"  value="{{.Dir}}">
  <input type="hidden" id="filter-input" name="filter" value="{{.Filter}}">

  <div class="toolbar">
    <input id="search-input" type="search" name="q" value="{{.Query}}"
           placeholder="Search title, artist, album…"
           class="toolbar-search" style="width:260px"
           hx-get="/songs/rows"
           hx-trigger="input delay:300ms, search"
           hx-target="#songs-tbody"
           hx-include="#songs-controls">

    <div class="filter-pills">
      <a class="filter-pill {{if eq .Filter "all"}}active{{end}}"
         href="#" onclick="setFilter('all');return false">All</a>
      <a class="filter-pill {{if eq .Filter "missing"}}active{{end}}"
         href="#" onclick="setFilter('missing');return false">Missing</a>
      <a class="filter-pill {{if eq .Filter "has"}}active{{end}}"
         href="#" onclick="setFilter('has');return false">Has lyrics</a>
    </div>

    <form method="post" action="/run" style="margin-left:auto">
      <button type="submit" class="btn small">Run batch</button>
    </form>
  </div>
</div>

<div class="table-wrap">
  <table>
    <thead>
      <tr>
        <th class="sortable" onclick="sortBy('title')">
          Title{{if eq .Sort "title"}} {{if eq .Dir "ASC"}}▲{{else}}▼{{end}}{{end}}
        </th>
        <th class="sortable" onclick="sortBy('artist')">
          Artist{{if eq .Sort "artist"}} {{if eq .Dir "ASC"}}▲{{else}}▼{{end}}{{end}}
        </th>
        <th class="sortable" onclick="sortBy('album')">
          Album{{if eq .Sort "album"}} {{if eq .Dir "ASC"}}▲{{else}}▼{{end}}{{end}}
        </th>
        <th>Lyrics</th>
      </tr>
    </thead>
    <tbody id="songs-tbody">
      {{template "songs_rows_body" .}}
    </tbody>
  </table>
</div>

{{if .Truncated}}
<p class="muted" style="font-size:.75rem;margin-top:.5rem">
  Showing first {{len .Songs}} of {{.Total}} results — refine your search.
</p>
{{else if .Songs}}
<p class="muted" style="font-size:.75rem;margin-top:.5rem">{{len .Songs}} songs</p>
{{end}}

<script>
function sortBy(col) {
  var s = document.getElementById('sort-input');
  var d = document.getElementById('dir-input');
  d.value = (s.value === col && d.value === 'ASC') ? 'DESC' : 'ASC';
  s.value = col;
  htmx.trigger(document.getElementById('search-input'), 'search');
}
function setFilter(val) {
  document.getElementById('filter-input').value = val;
  htmx.trigger(document.getElementById('search-input'), 'search');
}
</script>
{{end}}
```

Note: `{{template "songs_rows_body" .}}` reuses the body block defined in the partial so the initial render and HTMX swaps share the same row markup.

**Step 2: Create `web/templates/partials/songs_rows.html`**

This file defines the `songs_rows_body` named template AND wraps it in `<tbody>` for the HTMX swap target:

```html
{{define "songs_rows_body"}}
{{range .Songs}}
<tr>
  <td>{{.Title}}</td>
  <td class="muted">{{.Artist}}</td>
  <td class="muted">{{.Album}}</td>
  <td>
    {{if .HasLyrics}}
    <span class="badge badge-found">yes</span>
    {{else}}
    <span class="badge badge-not_found">missing</span>
    {{end}}
  </td>
</tr>
{{else}}
<tr><td colspan="4" class="empty">No songs found.</td></tr>
{{end}}
{{end}}

<tbody id="songs-tbody">
  {{template "songs_rows_body" .}}
</tbody>
{{if .Truncated}}
<tr><td colspan="4" class="muted" style="font-size:.75rem;text-align:center;padding:.5rem">
  Showing first {{len .Songs}} of {{.Total}} — refine your search.
</td></tr>
{{end}}
```

Wait — HTMX swaps `#songs-tbody` (the `<tbody>` element's *inner HTML* by default with `hx-swap="innerHTML"`, or the element itself with `hx-swap="outerHTML"`). Since we're targeting `#songs-tbody` (the tbody tag itself), use `hx-swap="outerHTML"` so the returned `<tbody id="songs-tbody">` replaces it cleanly.

**Revised `songs.html` HTMX attribute** — add `hx-swap="outerHTML"`:

```html
    hx-get="/songs/rows"
    hx-trigger="input delay:300ms, search"
    hx-target="#songs-tbody"
    hx-swap="outerHTML"
    hx-include="#songs-controls">
```

**Step 3: The `songs_rows.html` partial needs to be registered in the full page template set too** so `{{template "songs_rows_body" .}}` resolves in `songs.html`. Update `ParseTemplates` in `handler.go` to also parse the partial into each page template that needs it:

In `ParseTemplates`, after cloning base and parsing the page, also parse the partial for `songs.html`:

```go
	for _, p := range pages {
		name := path.Base(p)
		if name == "base.html" {
			continue
		}
		t, err := template.Must(base.Clone()).ParseFS(fsys, p)
		if err != nil {
			return nil, err
		}
		// songs.html uses {{template "songs_rows_body"}} defined in the partial.
		if name == "songs.html" {
			t, err = t.ParseFS(fsys, "templates/partials/songs_rows.html")
			if err != nil {
				return nil, err
			}
		}
		tmpls[name] = t
	}
```

**Step 4: Build and smoke-test**

```bash
go build ./... && make test
```
Expected: all tests pass.

Manual smoke test:
```bash
NAVIDROME_URL=http://... NAVIDROME_USER=... NAVIDROME_PASS=... MUSIC_DIR=/music ./navilyrics serve
```
- Open `http://localhost:8080/songs` — see first 100 songs
- Type in search box — table updates without full reload
- Click column headers — rows re-sort
- Click filter pills — rows re-filter

**Step 5: Commit**

```bash
git add web/templates/songs.html web/templates/partials/songs_rows.html internal/handlers/handler.go
git commit -m "feat(web): add search, sort and filter to songs page"
```
