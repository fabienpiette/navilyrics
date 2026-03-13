package handlers

import (
	"context"
	"fmt"
	"html"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/user/navilyrics/internal/lyrics"
)

type runProgressData struct {
	ActiveTab string
	Version   string
	RunID     string
}

// RunBatch starts a background run for all songs and returns a live-progress page.
func (h *Handler) RunBatch(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	id := fmt.Sprintf("%x", time.Now().UnixNano())
	ch := h.runs.create(id)
	go func() {
		defer close(ch)
		_ = h.proc.Run(context.Background(), func(res lyrics.Result) { ch <- res })
	}()
	h.render(w, "run_progress.html", runProgressData{ActiveTab: "songs", Version: h.version, RunID: id})
}

// RunFiltered starts a background run for matching songs and returns a live-progress page.
func (h *Handler) RunFiltered(w http.ResponseWriter, r *http.Request) {
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
		_ = h.proc.RunSongs(context.Background(), songs, func(res lyrics.Result) { ch <- res })
	}()
	h.render(w, "run_progress.html", runProgressData{ActiveTab: "songs", Version: h.version, RunID: id})
}

// RunEvents streams per-song results for a run as Server-Sent Events.
// GET /run/{id}/events
func (h *Handler) RunEvents(w http.ResponseWriter, r *http.Request) {
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

	var found, notFound, skipped, instrumental, errors int
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
			case "found", "dry_run":
				found++
			case "not_found":
				notFound++
			case "skipped":
				skipped++
			case "instrumental":
				instrumental++
			case "error":
				errors++
			}
		case <-r.Context().Done():
			// Client disconnected — drain the channel so the producer goroutine
			// can finish and close it rather than blocking on a full buffer.
			go func() {
				for range ch {
				}
			}()
			return
		}
	}

	summary := fmt.Sprintf(`{"found":%d,"not_found":%d,"skipped":%d,"instrumental":%d,"errors":%d}`,
		found, notFound, skipped, instrumental, errors)
	fmt.Fprintf(w, "event: done\ndata: %s\n\n", summary)
	flusher.Flush()
}

// resultLineHTML builds an HTML fragment for one run result line.
// All user-supplied strings are escaped to prevent XSS.
func resultLineHTML(r lyrics.Result) string {
	errPart := ""
	if r.Err != "" {
		errPart = ` <span class="muted">(` + html.EscapeString(r.Err) + `)</span>`
	}
	return fmt.Sprintf(
		`<div class="run-log-line"><span class="badge badge-%s">%s</span> <span class="muted">%s</span> — %s%s</div>`,
		html.EscapeString(r.Status), html.EscapeString(r.Status),
		html.EscapeString(r.Artist), html.EscapeString(r.Title), errPart,
	)
}
