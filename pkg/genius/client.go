package genius

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

const defaultAPIURL = "https://api.genius.com"

// Response holds lyrics scraped from a Genius song page.
type Response struct {
	PlainLyrics  string // plain text, no timestamps
	SyncedLyrics string // always empty — Genius has no timed lyrics
}

// Client is a Genius API + scraping client.
type Client struct {
	token      string
	apiBaseURL string
	httpClient *http.Client
}

// New creates a Client. Pass "" for apiBaseURL to use the default.
func New(token, apiBaseURL string) *Client {
	if apiBaseURL == "" {
		apiBaseURL = defaultAPIURL
	}
	return &Client{
		token:      token,
		apiBaseURL: apiBaseURL,
		httpClient: &http.Client{Timeout: 15 * time.Second},
	}
}

type searchResp struct {
	Response struct {
		Hits []struct {
			Result struct {
				URL           string `json:"url"`
				PrimaryArtist struct {
					Name string `json:"name"`
				} `json:"primary_artist"`
			} `json:"result"`
		} `json:"hits"`
	} `json:"response"`
}

// Search finds lyrics by artist+title. Returns (zero, false, nil) if not found
// or if the Genius page no longer has a parseable lyrics container.
func (c *Client) Search(ctx context.Context, artist, title string) (Response, bool, error) {
	q := url.QueryEscape(artist + " " + title)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		c.apiBaseURL+"/search?q="+q, nil)
	if err != nil {
		return Response{}, false, err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("User-Agent", "navilyrics/1.0")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return Response{}, false, fmt.Errorf("genius search: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return Response{}, false, fmt.Errorf("genius search: status %d", resp.StatusCode)
	}

	var sr searchResp
	if err := json.NewDecoder(resp.Body).Decode(&sr); err != nil {
		return Response{}, false, fmt.Errorf("genius search decode: %w", err)
	}

	// Pick first hit whose primary_artist matches (case-insensitive contains).
	artistLower := strings.ToLower(artist)
	var songURL string
	for _, hit := range sr.Response.Hits {
		if strings.Contains(strings.ToLower(hit.Result.PrimaryArtist.Name), artistLower) {
			songURL = hit.Result.URL
			break
		}
	}
	if songURL == "" {
		return Response{}, false, nil
	}

	lyrics, err := c.scrapeLyrics(ctx, songURL)
	if err != nil {
		return Response{}, false, err
	}
	if lyrics == "" {
		return Response{}, false, nil
	}
	return Response{PlainLyrics: lyrics}, true, nil
}

var (
	brRe  = regexp.MustCompile(`(?i)<br\s*/?>`)
	tagRe = regexp.MustCompile(`<[^>]+>`)
)

func (c *Client) scrapeLyrics(ctx context.Context, pageURL string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, pageURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("genius page: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("genius page: status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("genius page read: %w", err)
	}
	return extractLyrics(string(body)), nil
}

// extractLyrics finds all data-lyrics-container divs and returns their plain
// text content. Handles nested <div> elements by tracking nesting depth.
// Returns "" if no container is found.
func extractLyrics(body string) string {
	body = brRe.ReplaceAllString(body, "\n")
	var parts []string
	for {
		attrIdx := strings.Index(body, `data-lyrics-container="true"`)
		if attrIdx < 0 {
			break
		}
		// Skip past the closing > of the opening tag.
		closeIdx := strings.Index(body[attrIdx:], ">")
		if closeIdx < 0 {
			break
		}
		content := body[attrIdx+closeIdx+1:]

		// Find the matching </div> respecting nesting depth.
		end := matchingDiv(content)
		if end < 0 {
			break
		}
		text := tagRe.ReplaceAllString(content[:end], "")
		text = html.UnescapeString(text)
		text = strings.TrimSpace(text)
		if text != "" {
			parts = append(parts, text)
		}
		body = content[end:]
	}
	return strings.Join(parts, "\n\n")
}

// matchingDiv returns the index of the </div> that closes the outermost div
// (depth 1 on entry). Returns -1 if no matching close tag is found.
func matchingDiv(s string) int {
	depth := 1
	i := 0
	for depth > 0 && i < len(s) {
		o := indexDivOpen(s[i:])
		c := strings.Index(s[i:], "</div>")
		if c < 0 {
			return -1
		}
		if o >= 0 && o < c {
			depth++
			i += o + 4
		} else {
			depth--
			if depth == 0 {
				return i + c
			}
			i += c + 6
		}
	}
	return -1
}

// indexDivOpen finds the next actual <div> open tag in s, skipping tag names
// that merely start with "div" (e.g. <divider>). Returns -1 if not found.
func indexDivOpen(s string) int {
	i := 0
	for {
		idx := strings.Index(s[i:], "<div")
		if idx < 0 {
			return -1
		}
		pos := i + idx + 4
		if pos >= len(s) {
			return -1
		}
		ch := s[pos]
		if ch == '>' || ch == ' ' || ch == '\t' || ch == '\n' || ch == '\r' || ch == '/' {
			return i + idx
		}
		i = pos
	}
}
