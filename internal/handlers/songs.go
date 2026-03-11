package handlers

import (
	"context"
	"net/http"
	goSort "sort"
	"strconv"
	"strings"

	"github.com/user/navilyrics/pkg/navidrome"
)

const pageSize = 50

type songsRowsData struct {
	Songs     []navidrome.Song
	Query     string
	Sort      string
	Dir       string
	Filter    string
	HasMore   bool
	NextStart int
}

type songsData struct {
	ActiveTab string
	Version   string
	songsRowsData
}

// Songs renders the /songs full page with the first page of songs.
func (h *Handler) Songs(w http.ResponseWriter, r *http.Request) {
	sort, dir, filter := songParams(r)
	rows, err := h.fetchRows(r.Context(), "", 0, sort, dir, filter)
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
	start, _ := strconv.Atoi(r.URL.Query().Get("start"))
	sort, dir, filter := songParams(r)
	rows, err := h.fetchRows(r.Context(), q, start, sort, dir, filter)
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

// fetchRows calls Navidrome and returns one page of songs starting at start.
//
// No query, no filter: uses ListSongs (Navidrome-native pagination) — fast.
// With query or filter: uses AllSongs then filters/sorts in Go so all songs
// are searched, then slices the requested page.
func (h *Handler) fetchRows(ctx context.Context, query string, start int, sort, dir, filter string) (songsRowsData, error) {
	var songs []navidrome.Song
	var hasMore bool

	if query != "" || filter != "all" {
		// Need the full library for client-side search/filter.
		all, err := h.nd.AllSongs(ctx)
		if err != nil {
			return songsRowsData{}, err
		}
		songs = filterByLyrics(all, filter)
		sortSongs(songs, sort, dir)
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
		// Paginate Go-side.
		total := len(songs)
		if start >= total {
			songs = nil
		} else {
			end := start + pageSize
			hasMore = end < total
			if end > total {
				end = total
			}
			songs = songs[start:end]
		}
	} else {
		// No search, no filter: delegate pagination to Navidrome.
		var err error
		songs, err = h.nd.ListSongs(ctx, navidrome.SongQuery{
			Sort:  sort,
			Dir:   dir,
			Start: start,
			Limit: pageSize + 1, // fetch one extra to detect more pages
		})
		if err != nil {
			return songsRowsData{}, err
		}
		hasMore = len(songs) > pageSize
		if hasMore {
			songs = songs[:pageSize]
		}
	}

	return songsRowsData{
		Songs:     songs,
		Query:     query,
		Sort:      sort,
		Dir:       dir,
		Filter:    filter,
		HasMore:   hasMore,
		NextStart: start + pageSize,
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

