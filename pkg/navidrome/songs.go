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

// SongQuery parameterises a ListSongs request.
type SongQuery struct {
	Sort  string // "title" | "artist" | "album"
	Dir   string // "ASC" | "DESC"
	Start int    // zero-based offset for pagination
	Limit int
}

// ListSongs fetches up to q.Limit songs with the given sort/filter.
func (c *Client) ListSongs(ctx context.Context, q SongQuery) ([]Song, error) {
	if q.Limit <= 0 {
		return nil, fmt.Errorf("list songs: Limit must be > 0")
	}
	if q.Sort == "" {
		q.Sort = "title"
	}
	if q.Dir == "" {
		q.Dir = "ASC"
	}
	params := url.Values{
		"_start": []string{strconv.Itoa(q.Start)},
		"_end":   []string{strconv.Itoa(q.Start + q.Limit)},
		"_sort":  []string{q.Sort},
		"_order": []string{q.Dir},
	}
	resp, err := c.Do(ctx, http.MethodGet, "/api/song?"+params.Encode(), nil)
	if err != nil {
		return nil, fmt.Errorf("list songs: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("list songs: status %d", resp.StatusCode)
	}
	var songs []Song
	err = json.NewDecoder(resp.Body).Decode(&songs)
	resp.Body.Close()
	if err != nil {
		return nil, fmt.Errorf("list songs decode: %w", err)
	}
	return songs, nil
}

// GetSong fetches a single song by ID.
func (c *Client) GetSong(ctx context.Context, id string) (Song, error) {
	resp, err := c.Do(ctx, http.MethodGet, "/api/song/"+id, nil)
	if err != nil {
		return Song{}, fmt.Errorf("get song: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return Song{}, fmt.Errorf("song %s: %w", id, ErrNotFound)
	}
	if resp.StatusCode != http.StatusOK {
		return Song{}, fmt.Errorf("get song: status %d", resp.StatusCode)
	}
	var s Song
	if err := json.NewDecoder(resp.Body).Decode(&s); err != nil {
		return Song{}, fmt.Errorf("get song decode: %w", err)
	}
	return s, nil
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
