package handlers

import (
	"context"
	"net/http"
	goSort "sort"
	"strings"
	"sync"

	"github.com/user/navilyrics/internal/lyrics"
	"github.com/user/navilyrics/pkg/navidrome"
)

const maxDisplay = 100

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
//
// No query: uses ListSongs (Navidrome-native sort/filter, limit=maxDisplay) — fast.
// With query: uses AllSongs (full paginated fetch) then filters/sorts in Go so
// all songs in the library are searched, not just the first page.
func (h *Handler) fetchRows(ctx context.Context, query, sort, dir, filter string) (songsRowsData, error) {
	var songs []navidrome.Song
	var err error

	if query != "" {
		// Fetch the entire library so search covers all songs.
		songs, err = h.nd.AllSongs(ctx)
		if err != nil {
			return songsRowsData{}, err
		}
		// Apply hasLyrics filter in Go (AllSongs doesn't filter).
		songs = filterByLyrics(songs, filter)
		// Sort in Go (AllSongs always returns title ASC).
		sortSongs(songs, sort, dir)
		// Text search across title, artist, album.
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
	} else {
		// No search: let Navidrome sort/filter and return only what we display.
		var hasLyrics *bool
		switch filter {
		case "has":
			v := true
			hasLyrics = &v
		case "missing":
			v := false
			hasLyrics = &v
		}
		songs, err = h.nd.ListSongs(ctx, navidrome.SongQuery{
			Sort:      sort,
			Dir:       dir,
			HasLyrics: hasLyrics,
			Limit:     maxDisplay,
		})
		if err != nil {
			return songsRowsData{}, err
		}
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

func filterByLyrics(songs []navidrome.Song, filter string) []navidrome.Song {
	if filter != "has" && filter != "missing" {
		return songs
	}
	want := filter == "has"
	out := songs[:0]
	for _, s := range songs {
		if s.HasLyrics == want {
			out = append(out, s)
		}
	}
	return out
}

func sortSongs(songs []navidrome.Song, field, dir string) {
	goSort.Slice(songs, func(i, j int) bool {
		var a, b string
		switch field {
		case "artist":
			a, b = songs[i].Artist, songs[j].Artist
		case "album":
			a, b = songs[i].Album, songs[j].Album
		default:
			a, b = songs[i].Title, songs[j].Title
		}
		a, b = strings.ToLower(a), strings.ToLower(b)
		if dir == "DESC" {
			return a > b
		}
		return a < b
	})
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
