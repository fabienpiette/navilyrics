package navidrome

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
)

const songPageSize = 500

// AllSongs fetches every song in the Navidrome library, paginating automatically.
func (c *Client) AllSongs(ctx context.Context) ([]Song, error) {
	var all []Song
	for start := 0; ; start += songPageSize {
		q := url.Values{
			"_start": []string{strconv.Itoa(start)},
			"_end":   []string{strconv.Itoa(start + songPageSize)},
			"_sort":  []string{"title"},
			"_order": []string{"ASC"},
		}
		resp, err := c.Do(ctx, http.MethodGet, "/api/song?"+q.Encode(), nil)
		if err != nil {
			return nil, fmt.Errorf("list songs (start=%d): %w", start, err)
		}
		if resp.StatusCode != http.StatusOK {
			resp.Body.Close()
			return nil, fmt.Errorf("list songs: status %d", resp.StatusCode)
		}
		var page []Song
		err = json.NewDecoder(resp.Body).Decode(&page)
		resp.Body.Close()
		if err != nil {
			return nil, fmt.Errorf("decode songs: %w", err)
		}
		all = append(all, page...)
		if len(page) < songPageSize {
			break
		}
	}
	return all, nil
}

// TriggerScan requests a Navidrome library rescan.
func (c *Client) TriggerScan(ctx context.Context) error {
	resp, err := c.Do(ctx, http.MethodGet, "/api/scanner/trigger", nil)
	if err != nil {
		return fmt.Errorf("trigger scan: %w", err)
	}
	defer resp.Body.Close()
	return nil
}
