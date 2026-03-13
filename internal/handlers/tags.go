package handlers

import (
	"encoding/json"
	"errors"
	"html"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/user/navilyrics/pkg/navidrome"
	"github.com/user/navilyrics/pkg/tagger"
)

// SongTagsSave writes editable metadata fields (title, artist, album, year,
// track, genre) directly to the audio file's tags.
// POST /songs/{id}/tags
//
// If the file has corrupt ID3v2 frame data (ErrBodyOverflow) that repair
// cannot fix, the handler automatically rebuilds the ID3v2 tag from scratch
// (Parse:false) so the file becomes readable again.
func (h *Handler) SongTagsSave(w http.ResponseWriter, r *http.Request) {
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
		Title       string `json:"title"`
		Artist      string `json:"artist"`
		Album       string `json:"album"`
		Year        string `json:"year"`
		TrackNumber string `json:"trackNumber"`
		Genre       string `json:"genre"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "bad request: "+err.Error(), http.StatusBadRequest)
		return
	}

	audioPath := h.proc.ResolveAudioPath(song.Path)
	if audioPath == "" {
		http.Error(w, "audio file not found in music dirs", http.StatusUnprocessableEntity)
		return
	}

	meta := tagger.SongMeta{
		Title:       body.Title,
		Artist:      body.Artist,
		Album:       body.Album,
		Year:        body.Year,
		TrackNumber: body.TrackNumber,
		Genre:       body.Genre,
	}

	if err := tagger.WriteMeta(audioPath, meta, false); err != nil {
		if !tagger.IsBodyOverflow(err) {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		// Corrupt ID3v2 frame data — rebuild the tag from scratch (Parse:false).
		if err2 := tagger.WriteMeta(audioPath, meta, true); err2 != nil {
			http.Error(w, err2.Error(), http.StatusInternalServerError)
			return
		}
	}
	w.WriteHeader(http.StatusNoContent)
}
