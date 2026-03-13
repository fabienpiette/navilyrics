package netease

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

const defaultBaseURL = "https://music.163.com"

// Response holds lyrics fetched from NetEase Cloud Music.
type Response struct {
	SyncedLyrics string // LRC format with timestamps
	PlainLyrics  string // timestamps stripped
}

var lrcTagRe = regexp.MustCompile(`\[[^\]]*\]`)

// stripTimestamps removes [mm:ss.xx] and metadata tags from LRC lines.
func stripTimestamps(lrc string) string {
	lines := strings.Split(lrc, "\n")
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		stripped := lrcTagRe.ReplaceAllString(line, "")
		stripped = strings.TrimSpace(stripped)
		if stripped != "" {
			out = append(out, stripped)
		}
	}
	return strings.Join(out, "\n")
}

// Client is a NetEase Cloud Music API client.
type Client struct {
	baseURL    string
	httpClient *http.Client
}

// New creates a Client. Pass "" to use the default base URL.
func New(baseURL string) *Client {
	if baseURL == "" {
		baseURL = defaultBaseURL
	}
	return &Client{
		baseURL:    baseURL,
		httpClient: &http.Client{Timeout: 10 * time.Second},
	}
}

// Search searches for a track and returns the lyrics if a duration match is found.
// Uses ±5 s tolerance (same as lrclib). Returns (zero, false, nil) if not found.
func (c *Client) Search(ctx context.Context, artist, title string, targetDuration float64) (Response, bool, error) {
	id, _, ok, err := c.searchSong(ctx, artist, title, targetDuration)
	if err != nil || !ok {
		return Response{}, false, err
	}
	synced, err := c.fetchLyrics(ctx, id)
	if err != nil {
		return Response{}, false, err
	}
	if synced == "" {
		return Response{}, false, nil
	}
	return Response{
		SyncedLyrics: synced,
		PlainLyrics:  stripTimestamps(synced),
	}, true, nil
}

type neSearchResp struct {
	Result struct {
		Songs []struct {
			ID       int64 `json:"id"`
			Duration int64 `json:"duration"` // milliseconds
		} `json:"songs"`
	} `json:"result"`
}

func (c *Client) searchSong(ctx context.Context, artist, title string, targetDuration float64) (id int64, dur float64, ok bool, err error) {
	body := url.Values{
		"s":      {artist + " " + title},
		"type":   {"1"},
		"limit":  {"10"},
		"offset": {"0"},
	}.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		c.baseURL+"/api/search/get", strings.NewReader(body))
	if err != nil {
		return 0, 0, false, err
	}
	c.setHeaders(req)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return 0, 0, false, fmt.Errorf("netease search: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return 0, 0, false, fmt.Errorf("netease search: status %d", resp.StatusCode)
	}

	var sr neSearchResp
	if err := json.NewDecoder(resp.Body).Decode(&sr); err != nil {
		return 0, 0, false, fmt.Errorf("netease search decode: %w", err)
	}

	const maxDelta = 5.0
	var bestID int64
	bestDur, bestDelta, found := 0.0, math.MaxFloat64, false
	for _, s := range sr.Result.Songs {
		secs := float64(s.Duration) / 1000.0
		if d := math.Abs(secs - targetDuration); d < bestDelta && d <= maxDelta {
			bestDelta, bestID, bestDur, found = d, s.ID, secs, true
		}
	}
	if !found {
		return 0, 0, false, nil
	}
	return bestID, bestDur, true, nil
}

type neLyricResp struct {
	Lrc struct {
		Lyric string `json:"lyric"`
	} `json:"lrc"`
}

func (c *Client) fetchLyrics(ctx context.Context, id int64) (synced string, err error) {
	q := url.Values{"id": {fmt.Sprintf("%d", id)}, "lv": {"-1"}, "kv": {"-1"}, "tv": {"-1"}}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		c.baseURL+"/api/song/lyric?"+q.Encode(), nil)
	if err != nil {
		return "", err
	}
	c.setHeaders(req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("netease lyrics: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("netease lyrics: status %d", resp.StatusCode)
	}

	var lr neLyricResp
	if err := json.NewDecoder(resp.Body).Decode(&lr); err != nil {
		return "", fmt.Errorf("netease lyrics decode: %w", err)
	}
	return strings.TrimSpace(lr.Lrc.Lyric), nil
}

func (c *Client) setHeaders(req *http.Request) {
	req.Header.Set("Referer", "https://music.163.com/")
	req.Header.Set("Cookie", "os=pc; appver=2.0.2")
	req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36")
}
