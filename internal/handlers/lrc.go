package handlers

import (
	"errors"
	"html"
	"net/http"
	"os"

	"github.com/go-chi/chi/v5"
	"github.com/user/navilyrics/pkg/navidrome"
)

// SongLRC serves the raw .lrc sidecar for a song as plain text.
// GET /songs/{id}/lrc
func (h *Handler) SongLRC(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	song, err := h.nd.GetSong(r.Context(), id)
	if err != nil {
		if errors.Is(err, navidrome.ErrNotFound) {
			http.NotFound(w, r)
			return
		}
		http.Error(w, html.EscapeString(err.Error()), http.StatusBadGateway)
		return
	}

	lrcPath := h.proc.ResolveLRCPath(song.Path)
	if lrcPath == "" {
		http.NotFound(w, r)
		return
	}

	data, err := os.ReadFile(lrcPath)
	if err != nil {
		if os.IsNotExist(err) {
			http.NotFound(w, r)
			return
		}
		http.Error(w, "read lrc: "+html.EscapeString(err.Error()), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = w.Write(data)
}
