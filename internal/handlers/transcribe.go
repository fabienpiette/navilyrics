package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/user/navilyrics/internal/lyrics"
	"github.com/user/navilyrics/pkg/goscribe"
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
	SongID  string
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
		h.renderPartial(w, "transcribe_result.html", transcribeResultData{SongID: songID, Error: "song not found: " + err.Error()})
		return
	}
	if h.proc.ResolveAudioPath(song.Path) == "" {
		h.renderPartial(w, "transcribe_result.html", transcribeResultData{SongID: songID, Error: "audio file not found on disk"})
		return
	}
	h.renderPartial(w, "transcribe_poll.html", transcribePollData{SongID: songID, Attempt: 0, JobID: ""})
}

func (h *Handler) TranscribeSongPoll(w http.ResponseWriter, r *http.Request) {
	songID := r.URL.Query().Get("song_id")
	jobID := r.URL.Query().Get("job_id")
	attempt, _ := strconv.Atoi(r.URL.Query().Get("attempt"))

	if attempt >= 100 {
		h.renderPartial(w, "transcribe_result.html", transcribeResultData{SongID: songID, Error: "transcription timed out after 5 minutes"})
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
		h.renderPartial(w, "transcribe_result.html", transcribeResultData{SongID: songID, Error: result.err.Error()})
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
		h.renderPartial(w, "transcribe_result.html", transcribeResultData{SongID: songID, Error: "save failed: " + err.Error()})
		return
	}
	h.renderPartial(w, "transcribe_result.html", transcribeResultData{SongID: songID, Message: "Lyrics saved."})
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
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
		defer cancel()
		song, err := h.nd.GetSong(ctx, songID)
		if err != nil {
			transcribeJobs.Store(songID, &transcribeJobResult{err: err})
			return
		}

		// When webhook delivery is configured, submit the job and let goscribe
		// push the result back via POST /webhook/goscribe/:songID. PollJob still
		// runs as a fallback in case the webhook never arrives.
		if h.goscribeClient != nil && h.selfURL != "" {
			audioPath := h.proc.ResolveAudioPath(song.Path)
			if audioPath == "" {
				transcribeJobs.Store(songID, &transcribeJobResult{err: fmt.Errorf("audio file not found: %s", song.Path)})
				return
			}
			callbackURL := h.selfURL + "/webhook/goscribe/" + songID
			jobID, err := h.goscribeClient.SubmitJob(ctx, audioPath, goscribe.JobOptions{Song: true, CallbackURL: callbackURL})
			if err != nil {
				transcribeJobs.Store(songID, &transcribeJobResult{err: err})
				return
			}
			// Poll as fallback at a longer interval — webhook will usually win.
			transcript, err := h.goscribeClient.PollJob(ctx, jobID, 10*time.Second)
			transcribeJobs.Store(songID, &transcribeJobResult{transcript: transcript, err: err})
			return
		}

		_, transcript, err := h.proc.TranscribeSong(ctx, song)
		transcribeJobs.Store(songID, &transcribeJobResult{transcript: transcript, err: err})
	}()
}

// GoscribeWebhook receives job progress/completion pushes from goscribe.
// Route: POST /webhook/goscribe/:songID
func (h *Handler) GoscribeWebhook(w http.ResponseWriter, r *http.Request) {
	if h.goscribeClient == nil {
		http.NotFound(w, r)
		return
	}
	songID := chi.URLParam(r, "songID")
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		http.Error(w, "read body", http.StatusBadRequest)
		return
	}

	var payload struct {
		JobID  string `json:"job_id"`
		Status string `json:"status"`
		Step   string `json:"step"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		http.Error(w, "bad payload", http.StatusBadRequest)
		return
	}

	step := payload.Status
	if payload.Step != "" {
		step = payload.Step
	}
	log.Printf("webhook: goscribe job %s → %s (song %s)", payload.JobID, step, songID)

	// Store in client results map so PollJob returns on its next tick.
	_ = h.goscribeClient.NotifyResult(payload.JobID, body)

	w.WriteHeader(http.StatusNoContent)
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
