package handlers

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/user/navilyrics/internal/lyrics"
)

// RunSync starts a background sync (gap-fill) run for songs matching the
// current search query and filter, then returns a live-progress page.
// POST /sync
func (h *Handler) RunSync(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	query := r.FormValue("q")
	filter := r.FormValue("filter")
	switch filter {
	case "missing", "has":
	default:
		filter = "all"
	}

	songs, err := h.allMatchingSongs(r.Context(), query, filter)
	if err != nil {
		http.Error(w, "navidrome: "+err.Error(), http.StatusBadGateway)
		return
	}

	id := fmt.Sprintf("%x", time.Now().UnixNano())
	ch := h.runs.create(id)
	go func() {
		defer close(ch)
		_ = h.proc.SyncAll(context.Background(), songs, func(res lyrics.Result) { ch <- res })
	}()
	h.render(w, "sync_progress.html", runProgressData{ActiveTab: "songs", Version: h.version, RunID: id})
}

// SyncEvents streams per-song results for a sync run as Server-Sent Events.
// GET /sync/{id}/events
func (h *Handler) SyncEvents(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	ch, ok := h.runs.get(id)
	if !ok {
		http.NotFound(w, r)
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	defer h.runs.delete(id)

	var syncedLRC, syncedEmbedded, skipped, errors int
loop:
	for {
		select {
		case res, ok := <-ch:
			if !ok {
				break loop
			}
			line := resultLineHTML(res)
			fmt.Fprintf(w, "event: result\ndata: %s\n\n", line)
			flusher.Flush()
			switch res.Status {
			case "synced_lrc":
				syncedLRC++
			case "synced_embedded":
				syncedEmbedded++
			case "skipped":
				skipped++
			case "error":
				errors++
			}
		case <-r.Context().Done():
			go func() {
				for range ch {
				}
			}()
			return
		}
	}

	summary := fmt.Sprintf(`{"synced_lrc":%d,"synced_embedded":%d,"skipped":%d,"errors":%d}`,
		syncedLRC, syncedEmbedded, skipped, errors)
	fmt.Fprintf(w, "event: done\ndata: %s\n\n", summary)
	flusher.Flush()

	if syncedLRC+syncedEmbedded > 0 {
		go func() {
			if err := h.nd.TriggerScan(context.Background()); err != nil {
				_ = err
			}
		}()
	}
}
