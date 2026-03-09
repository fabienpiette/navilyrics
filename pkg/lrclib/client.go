package lrclib

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

const defaultBaseURL = "https://lrclib.net"

// Response is a lrclib.net track result.
type Response struct {
	ID           int     `json:"id"`
	TrackName    string  `json:"trackName"`
	ArtistName   string  `json:"artistName"`
	AlbumName    string  `json:"albumName"`
	Duration     float64 `json:"duration"`
	Instrumental bool    `json:"instrumental"`
	PlainLyrics  string  `json:"plainLyrics"`
	SyncedLyrics string  `json:"syncedLyrics"`
}

// Client is a lrclib.net API client.
type Client struct {
	baseURL    string
	httpClient *http.Client
}

// New creates a Client. Pass "" for baseURL to use the default (https://lrclib.net).
func New(baseURL string) *Client {
	if baseURL == "" {
		baseURL = defaultBaseURL
	}
	return &Client{
		baseURL:    baseURL,
		httpClient: &http.Client{Timeout: 10 * time.Second},
	}
}

// Get performs an exact-match lookup by artist, title, album, and duration (seconds).
// Returns (result, true, nil) if found, (zero, false, nil) if 404, or (zero, false, err) on error.
func (c *Client) Get(ctx context.Context, artist, title, album string, duration float64) (Response, bool, error) {
	q := url.Values{
		"artist_name": []string{artist},
		"track_name":  []string{title},
		"album_name":  []string{album},
		"duration":    []string{strconv.FormatFloat(duration, 'f', 0, 64)},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/api/get?"+q.Encode(), nil)
	if err != nil {
		return Response{}, false, err
	}
	req.Header.Set("User-Agent", "navilyrics/1.0 (https://github.com/user/navilyrics)")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return Response{}, false, fmt.Errorf("lrclib get: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return Response{}, false, nil
	}
	if resp.StatusCode != http.StatusOK {
		return Response{}, false, fmt.Errorf("lrclib get: status %d", resp.StatusCode)
	}

	var r Response
	if err := json.NewDecoder(resp.Body).Decode(&r); err != nil {
		return Response{}, false, fmt.Errorf("lrclib get decode: %w", err)
	}
	return r, true, nil
}

// Search performs a fuzzy search and picks the result whose duration is closest
// to targetDuration (within ±5 seconds). Returns (zero, false, nil) if no match.
func (c *Client) Search(ctx context.Context, artist, title string, targetDuration float64) (Response, bool, error) {
	q := url.Values{
		"artist_name": []string{artist},
		"track_name":  []string{title},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/api/search?"+q.Encode(), nil)
	if err != nil {
		return Response{}, false, err
	}
	req.Header.Set("User-Agent", "navilyrics/1.0 (https://github.com/user/navilyrics)")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return Response{}, false, fmt.Errorf("lrclib search: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return Response{}, false, fmt.Errorf("lrclib search: status %d", resp.StatusCode)
	}

	var results []Response
	if err := json.NewDecoder(resp.Body).Decode(&results); err != nil {
		return Response{}, false, fmt.Errorf("lrclib search decode: %w", err)
	}

	const maxDelta = 5.0
	best := Response{}
	bestDelta := math.MaxFloat64
	for _, r := range results {
		delta := math.Abs(r.Duration - targetDuration)
		if delta < bestDelta && delta <= maxDelta {
			bestDelta = delta
			best = r
		}
	}
	if bestDelta == math.MaxFloat64 {
		return Response{}, false, nil
	}
	return best, true, nil
}
