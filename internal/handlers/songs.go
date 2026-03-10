package handlers

import (
	"context"
	"net/http"
	"sync"

	"github.com/user/navilyrics/internal/lyrics"
	"github.com/user/navilyrics/pkg/navidrome"
)

type songsData struct {
	ActiveTab string
	Version   string
	Songs     []navidrome.Song
	Filter    string // "all" | "missing" | "has"
}

// Songs renders the /songs page with optional filter.
func (h *Handler) Songs(w http.ResponseWriter, r *http.Request) {
	filter := r.URL.Query().Get("filter")
	if filter == "" {
		filter = "all"
	}

	ctx := context.Background()
	all, err := h.nd.AllSongs(ctx)
	if err != nil {
		http.Error(w, "navidrome: "+err.Error(), http.StatusBadGateway)
		return
	}

	filtered := make([]navidrome.Song, 0, len(all))
	for _, s := range all {
		switch filter {
		case "missing":
			if !s.HasLyrics {
				filtered = append(filtered, s)
			}
		case "has":
			if s.HasLyrics {
				filtered = append(filtered, s)
			}
		default:
			filtered = append(filtered, s)
		}
	}

	data := songsData{
		ActiveTab: "songs",
		Version:   h.version,
		Songs:     filtered,
		Filter:    filter,
	}
	h.render(w, "songs.html", data)
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

	ctx := context.Background()
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
