package handlers

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/user/navilyrics/internal/lyrics"
)

var transcribeJobs sync.Map

type transcribeJobResult struct {
	transcript string
	err        error
}

type transcribePollData struct {
	SongID  string
	Attempt int
	JobID   string
}

type transcribePreviewData struct {
	SongID     string
	Transcript string
}

type transcribeResultData struct {
	Message string
	Error   string
}

func (h *Handler) TranscribeSong(w http.ResponseWriter, r *http.Request) {
	songID := r.FormValue("song_id")
	if songID == "" {
		http.Error(w, "song_id required", http.StatusBadRequest)
		return
	}
	song, err := h.nd.GetSong(r.Context(), songID)
	if err != nil {
		h.renderPartial(w, "transcribe_result.html", transcribeResultData{Error: "song not found: " + err.Error()})
		return
	}
	if h.proc.ResolveAudioPath(song.Path) == "" {
		h.renderPartial(w, "transcribe_result.html", transcribeResultData{Error: "audio file not found on disk"})
		return
	}
	h.renderPartial(w, "transcribe_poll.html", transcribePollData{SongID: songID, Attempt: 0, JobID: ""})
}

func (h *Handler) TranscribeSongPoll(w http.ResponseWriter, r *http.Request) {
	songID := r.URL.Query().Get("song_id")
	jobID := r.URL.Query().Get("job_id")
	attempt, _ := strconv.Atoi(r.URL.Query().Get("attempt"))

	if attempt >= 100 {
		h.renderPartial(w, "transcribe_result.html", transcribeResultData{Error: "transcription timed out after 5 minutes"})
		return
	}

	if jobID == "" {
		h.startTranscribeJob(songID)
		h.renderPartial(w, "transcribe_poll.html", transcribePollData{SongID: songID, Attempt: 1, JobID: songID})
		return
	}

	result, ready := h.checkTranscribeJob(jobID)
	if !ready {
		h.renderPartial(w, "transcribe_poll.html", transcribePollData{SongID: songID, Attempt: attempt + 1, JobID: jobID})
		return
	}
	transcribeJobs.Delete(jobID)

	if result.err != nil {
		h.renderPartial(w, "transcribe_result.html", transcribeResultData{Error: result.err.Error()})
		return
	}
	h.renderPartial(w, "transcript_preview.html", transcribePreviewData{SongID: songID, Transcript: result.transcript})
}

func (h *Handler) TranscribeSongSave(w http.ResponseWriter, r *http.Request) {
	songID := r.FormValue("song_id")
	transcript := r.FormValue("transcript")
	song, err := h.nd.GetSong(r.Context(), songID)
	if err != nil {
		h.renderPartial(w, "transcribe_result.html", transcribeResultData{Error: "song not found: " + err.Error()})
		return
	}
	if err := h.proc.SaveLyrics(song, transcript, ""); err != nil {
		h.renderPartial(w, "transcribe_result.html", transcribeResultData{Error: "save failed: " + err.Error()})
		return
	}
	h.renderPartial(w, "transcribe_result.html", transcribeResultData{Message: "Lyrics saved."})
}

func (h *Handler) TranscribeBatch(w http.ResponseWriter, r *http.Request) {
	songs, err := h.allMatchingSongs(r.Context(), "", "missing")
	if err != nil {
		http.Error(w, "navidrome: "+err.Error(), http.StatusBadGateway)
		return
	}
	id := fmt.Sprintf("%x", time.Now().UnixNano())
	ch := h.runs.create(id)
	go func() {
		defer close(ch)
		_ = h.proc.TranscribeAll(context.Background(), songs, func(res lyrics.Result) { ch <- res })
	}()
	h.render(w, "transcribe_progress.html", runProgressData{ActiveTab: "songs", Version: h.version, RunID: id})
}

func (h *Handler) TranscribeBatchEvents(w http.ResponseWriter, r *http.Request) {
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

	var transcribed, errors int
loop:
	for {
		select {
		case res, ok := <-ch:
			if !ok {
				break loop
			}
			fmt.Fprintf(w, "event: result\ndata: %s\n\n", resultLineHTML(res))
			flusher.Flush()
			switch res.Status {
			case "transcribed":
				transcribed++
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

	fmt.Fprintf(w, "event: done\ndata: {\"transcribed\":%d,\"errors\":%d}\n\n", transcribed, errors)
	flusher.Flush()

	if transcribed > 0 {
		go func() {
			if err := h.nd.TriggerScan(context.Background()); err != nil {
				_ = err
			}
		}()
	}
}

func (h *Handler) startTranscribeJob(songID string) {
	if _, loaded := transcribeJobs.LoadOrStore(songID, nil); loaded {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()
		song, err := h.nd.GetSong(ctx, songID)
		if err != nil {
			transcribeJobs.Store(songID, &transcribeJobResult{err: err})
			return
		}
		_, transcript, err := h.proc.TranscribeSong(ctx, song)
		transcribeJobs.Store(songID, &transcribeJobResult{transcript: transcript, err: err})
	}()
}

func (h *Handler) checkTranscribeJob(songID string) (*transcribeJobResult, bool) {
	v, ok := transcribeJobs.Load(songID)
	if !ok {
		return nil, false
	}
	if v == nil {
		return nil, false
	}
	r, _ := v.(*transcribeJobResult)
	return r, r != nil
}
