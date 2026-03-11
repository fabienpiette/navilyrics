package handlers

import (
	"context"
	"net/http"
	goSort "sort"
	"strings"

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
	} else if filter != "all" {
		// has/missing filter: Navidrome doesn't support server-side lyrics filter,
		// so fetch all songs and filter in Go.
		songs, err = h.nd.AllSongs(ctx)
		if err != nil {
			return songsRowsData{}, err
		}
		songs = filterByLyrics(songs, filter)
		sortSongs(songs, sort, dir)
	} else {
		// No search, no filter: let Navidrome sort and return only what we display.
		songs, err = h.nd.ListSongs(ctx, navidrome.SongQuery{
			Sort:  sort,
			Dir:   dir,
			Limit: maxDisplay,
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

// allMatchingSongs fetches every song matching query+filter with no display cap.
// Used by RunFiltered so the batch covers all results, not just the 100 shown.
func (h *Handler) allMatchingSongs(ctx context.Context, query, filter string) ([]navidrome.Song, error) {
	songs, err := h.nd.AllSongs(ctx)
	if err != nil {
		return nil, err
	}
	songs = filterByLyrics(songs, filter)
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
	return songs, nil
}

