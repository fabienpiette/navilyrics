package handlers

import (
	"encoding/json"
	"errors"
	"html"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/user/navilyrics/internal/lyrics"
	"github.com/user/navilyrics/pkg/navidrome"
	"github.com/user/navilyrics/pkg/tagger"
)

// SongLRC serves lyrics for a song as plain text.
// It first looks for a .lrc sidecar file; if absent, falls back to lyrics
// embedded in the audio file's tags. The response header X-Lyrics-Source is
// set to "lrc" or "embedded" so the client can display the provenance.
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

	audioPath := h.proc.ResolveAudioPath(song.Path)
	if audioPath == "" {
		http.NotFound(w, r)
		return
	}
	lrcPath := strings.TrimSuffix(audioPath, filepath.Ext(audioPath)) + ".lrc"

	// Try .lrc sidecar first.
	if data, err := os.ReadFile(lrcPath); err == nil {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Header().Set("X-Lyrics-Source", "lrc")
		_, _ = w.Write(data)
		return
	} else if !os.IsNotExist(err) {
		http.Error(w, "read lrc: "+html.EscapeString(err.Error()), http.StatusInternalServerError)
		return
	}

	// Fallback: read lyrics embedded in the audio file's tags.
	t, err := tagger.ForFile(audioPath)
	if err == nil {
		plain, synced, err := t.ReadLyrics(audioPath)
		if err == nil && (plain != "" || synced != "") {
			text := synced
			if text == "" {
				text = plain
			}
			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
			w.Header().Set("X-Lyrics-Source", "embedded")
			_, _ = w.Write([]byte(text))
			return
		}
	}

	http.NotFound(w, r)
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

// SongFetch searches external providers for lyrics and returns what was found
// without writing anything to disk. The client previews the result and may
// confirm by calling SongSave.
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

	// Optional body may override title/artist/album for manual searches,
	// and restrict which providers are queried.
	var overrides struct {
		Title     string   `json:"title"`
		Artist    string   `json:"artist"`
		Album     string   `json:"album"`
		Providers []string `json:"providers"` // nil = use all configured providers
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

	result := h.proc.FetchLyricsOnly(r.Context(), song, overrides.Providers)

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"status":       result.Status,
		"syncedLyrics": result.SyncedLyrics,
		"plainLyrics":  result.PlainLyrics,
		"instrumental": result.Instrumental,
		"source":       result.Source,
		"err":          result.Err,
	})
}

// SongSave writes confirmed lyrics to disk (lrc sidecar + tag embedding).
// POST /songs/{id}/save
func (h *Handler) SongSave(w http.ResponseWriter, r *http.Request) {
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

	var body struct {
		Plain  string `json:"plain"`
		Synced string `json:"synced"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "bad request: "+err.Error(), http.StatusBadRequest)
		return
	}

	if err := h.proc.SaveLyrics(song, body.Plain, body.Synced); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
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
