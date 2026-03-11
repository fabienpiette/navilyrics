package handlers

import (
	"encoding/json"
	"errors"
	"html"
	"io"
	"net/http"
	"os"

	"github.com/go-chi/chi/v5"
	"github.com/user/navilyrics/internal/lyrics"
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

// SongMeta returns selected song metadata as JSON for the preview panel.
// GET /songs/{id}/meta
func (h *Handler) SongMeta(w http.ResponseWriter, r *http.Request) {
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
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"year":        song.Year,
		"genre":       song.Genre,
		"albumArtist": song.AlbumArtist,
		"trackNumber": song.TrackNumber,
		"duration":    song.Duration,
		"bitRate":     song.BitRate,
		"suffix":      song.Suffix,
		"path":        song.Path,
	})
}

// SongFetch force-fetches lyrics for a single song via the processor and returns
// a JSON result. HasLyrics is cleared so already-tagged songs are re-processed.
// POST /songs/{id}/fetch
func (h *Handler) SongFetch(w http.ResponseWriter, r *http.Request) {
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

	// Optional body may override title/artist/album for manual searches.
	var overrides struct {
		Title  string `json:"title"`
		Artist string `json:"artist"`
		Album  string `json:"album"`
	}
	_ = json.NewDecoder(r.Body).Decode(&overrides)
	if overrides.Title != "" {
		song.Title = overrides.Title
	}
	if overrides.Artist != "" {
		song.Artist = overrides.Artist
	}
	if overrides.Album != "" {
		song.Album = overrides.Album
	}

	song.HasLyrics = false // bypass "skipped" guard — always attempt fetch
	result := h.proc.ProcessSong(r.Context(), song)

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{
		"status": result.Status,
		"err":    result.Err,
	})
}

// SongLRCSave writes a manually-edited LRC body to the sidecar file.
// PUT /songs/{id}/lrc
func (h *Handler) SongLRCSave(w http.ResponseWriter, r *http.Request) {
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
		http.Error(w, "audio file not found in music dirs", http.StatusUnprocessableEntity)
		return
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20)) // 1 MiB cap
	if err != nil {
		http.Error(w, "read body: "+err.Error(), http.StatusBadRequest)
		return
	}

	if err := lyrics.WriteRawLRC(lrcPath, string(body)); err != nil {
		http.Error(w, "write lrc: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
