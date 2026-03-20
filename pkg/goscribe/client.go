package goscribe

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

type Client struct {
	baseURL string
	http    *http.Client
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
	JobID             string            `json:"job_id"`
	Status            string            `json:"status"`
	Transcript        string            `json:"transcript"`
	Error             string            `json:"error"`
	LyricsValidation  *lyricsValidation `json:"lyrics_validation"`
}

func (c *Client) SubmitJob(ctx context.Context, audioPath string, opts JobOptions) (string, error) {
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

	resp, err := c.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("goscribe: submit job: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusAccepted {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return "", fmt.Errorf("goscribe: submit job: status %d: %s", resp.StatusCode, body)
	}

	var result submitResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("goscribe: decode submit response: %w", err)
	}
	return result.JobID, nil
}

func (c *Client) PollJob(ctx context.Context, jobID string, interval time.Duration) (string, error) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return "", fmt.Errorf("goscribe: poll job %s: %w", jobID, ctx.Err())
		case <-ticker.C:
			transcript, done, err := c.checkJob(ctx, jobID)
			if err != nil {
				return "", err
			}
			if done {
				return transcript, nil
			}
		}
	}
}

func (c *Client) checkJob(ctx context.Context, jobID string) (string, bool, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/jobs/"+jobID, nil)
	if err != nil {
		return "", false, fmt.Errorf("goscribe: build poll request: %w", err)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return "", false, fmt.Errorf("goscribe: poll job: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", false, fmt.Errorf("goscribe: poll job: status %d", resp.StatusCode)
	}

	var result pollResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", false, fmt.Errorf("goscribe: decode poll response: %w", err)
	}

	switch result.Status {
	case "completed":
		// When song mode is used, prefer the AI-validated cleaned lyrics over
		// the raw transcript, but only when confidence is high enough (≥60).
		if v := result.LyricsValidation; v != nil && v.CleanedLyrics != "" && v.Confidence >= 60 {
			return v.CleanedLyrics, true, nil
		}
		return result.Transcript, true, nil
	case "failed":
		return "", false, fmt.Errorf("goscribe: job %s failed: %s", jobID, result.Error)
	default:
		return "", false, nil
	}
}
