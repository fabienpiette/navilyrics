package goscribe

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type Client struct {
	baseURL string
	http    *http.Client
	// results holds job outcomes pushed via webhook before PollJob picks them up.
	results sync.Map // jobID → *pollResponse
}

func New(baseURL string) *Client {
	return &Client{
		baseURL: baseURL,
		http:    &http.Client{Timeout: 30 * time.Second},
	}
}

// JobOptions controls optional goscribe job parameters.
type JobOptions struct {
	// Song enables song mode: demucs vocal extraction + lyrics validation.
	// Requires goscribe to have demucs available.
	Song bool
	// CallbackURL is an optional webhook URL. Goscribe will POST the JobResult
	// to this URL on each status change (vocals_extracting, transcribing,
	// validating, completed, failed).
	CallbackURL string
}

type submitResponse struct {
	JobID  string `json:"job_id"`
	Status string `json:"status"`
}

type lyricsValidation struct {
	CleanedLyrics string  `json:"cleaned_lyrics"`
	Confidence    float64 `json:"confidence"`
}

type pollResponse struct {
	JobID            string            `json:"job_id"`
	Status           string            `json:"status"`
	Step             string            `json:"step"`
	Transcript       string            `json:"transcript"`
	Error            string            `json:"error"`
	LyricsValidation *lyricsValidation `json:"lyrics_validation"`
}

func (c *Client) SubmitJob(ctx context.Context, audioPath string, opts JobOptions) (string, error) {
	log.Printf("goscribe: submitting %s (song_mode=%v)", filepath.Base(audioPath), opts.Song)
	f, err := os.Open(audioPath)
	if err != nil {
		return "", fmt.Errorf("goscribe: open audio: %w", err)
	}
	defer f.Close()

	pr, pw := io.Pipe()
	mw := multipart.NewWriter(pw)
	go func() {
		if opts.Song {
			if err := mw.WriteField("song", "true"); err != nil {
				pw.CloseWithError(err)
				return
			}
		}
		if opts.CallbackURL != "" {
			if err := mw.WriteField("webhook_url", opts.CallbackURL); err != nil {
				pw.CloseWithError(err)
				return
			}
		}
		part, err := mw.CreateFormFile("file", filepath.Base(audioPath))
		if err != nil {
			pw.CloseWithError(err)
			return
		}
		if _, err := io.Copy(part, f); err != nil {
			pw.CloseWithError(err)
			return
		}
		pw.CloseWithError(mw.Close())
	}()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/jobs", pr)
	if err != nil {
		return "", fmt.Errorf("goscribe: build request: %w", err)
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())

	start := time.Now()
	resp, err := c.http.Do(req)
	elapsed := time.Since(start).Round(time.Millisecond)
	if err != nil {
		log.Printf("goscribe: submit %s: %v (%s)", filepath.Base(audioPath), err, elapsed)
		return "", fmt.Errorf("goscribe: submit job: %w", err)
	}
	defer resp.Body.Close()
	log.Printf("goscribe: submit %s → %d (%s)", filepath.Base(audioPath), resp.StatusCode, elapsed)

	if resp.StatusCode != http.StatusAccepted {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return "", fmt.Errorf("goscribe: submit job: status %d: %s", resp.StatusCode, body)
	}

	var result submitResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("goscribe: decode submit response: %w", err)
	}
	log.Printf("goscribe: job %s queued", result.JobID)
	return result.JobID, nil
}

// NotifyResult is called by the webhook handler when goscribe pushes a job
// result. It pre-populates the results map so PollJob can return immediately.
func (c *Client) NotifyResult(jobID string, body []byte) error {
	var r pollResponse
	if err := json.Unmarshal(body, &r); err != nil {
		return fmt.Errorf("goscribe: decode webhook payload: %w", err)
	}
	c.results.Store(jobID, &r)
	return nil
}

func (c *Client) PollJob(ctx context.Context, jobID string, interval time.Duration) (string, error) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	lastStatus := ""
	for {
		select {
		case <-ctx.Done():
			return "", fmt.Errorf("goscribe: poll job %s: %w", jobID, ctx.Err())
		case <-ticker.C:
			// Check if a webhook already delivered the result.
			if v, ok := c.results.LoadAndDelete(jobID); ok {
				r := v.(*pollResponse)
				return c.extractTranscript(r), nil
			}
			transcript, status, done, err := c.checkJob(ctx, jobID)
			if err != nil {
				return "", err
			}
			if status != lastStatus {
				log.Printf("goscribe: job %s → %s", jobID, status)
				lastStatus = status
			}
			if done {
				return transcript, nil
			}
		}
	}
}

// checkJob polls a single job status. Returns (transcript, status, done, err).
func (c *Client) checkJob(ctx context.Context, jobID string) (string, string, bool, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/jobs/"+jobID, nil)
	if err != nil {
		return "", "", false, fmt.Errorf("goscribe: build poll request: %w", err)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return "", "", false, fmt.Errorf("goscribe: poll job: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", "", false, fmt.Errorf("goscribe: poll job: status %d", resp.StatusCode)
	}

	var result pollResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", "", false, fmt.Errorf("goscribe: decode poll response: %w", err)
	}

	switch result.Status {
	case "completed":
		return c.extractTranscript(&result), result.Status, true, nil
	case "failed":
		return "", result.Status, false, fmt.Errorf("goscribe: job %s failed: %s", jobID, result.Error)
	default:
		if result.Step != "" {
			log.Printf("goscribe: job %s → %s (%s)", jobID, result.Status, result.Step)
		}
		return "", result.Status, false, nil
	}
}

// extractTranscript picks the best available text from a completed job result:
// validated cleaned lyrics when confidence is high enough, raw transcript otherwise.
func (c *Client) extractTranscript(r *pollResponse) string {
	if v := r.LyricsValidation; v != nil && v.CleanedLyrics != "" && v.Confidence >= 60 {
		log.Printf("goscribe: job %s using validated lyrics (confidence=%.0f)", r.JobID, v.Confidence)
		return v.CleanedLyrics
	}
	log.Printf("goscribe: job %s using raw transcript", r.JobID)
	return r.Transcript
}
