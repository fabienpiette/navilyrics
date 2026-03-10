package navidrome

import "encoding/json"

// Song represents a Navidrome track.
type Song struct {
	ID        string  `json:"id"`
	Title     string  `json:"title"`
	Artist    string  `json:"artist"`
	Album     string  `json:"album"`
	Duration  float64 `json:"duration"`
	Path      string  `json:"path"` // relative to library root
	HasLyrics bool    `json:"-"`    // derived from "lyrics" array via UnmarshalJSON
}

// UnmarshalJSON decodes a Navidrome song object. HasLyrics is derived from
// the "lyrics" JSON array: a non-empty array means the file has embedded lyrics.
func (s *Song) UnmarshalJSON(data []byte) error {
	var raw struct {
		ID       string            `json:"id"`
		Title    string            `json:"title"`
		Artist   string            `json:"artist"`
		Album    string            `json:"album"`
		Duration float64           `json:"duration"`
		Path     string            `json:"path"`
		Lyrics   []json.RawMessage `json:"lyrics"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	s.ID = raw.ID
	s.Title = raw.Title
	s.Artist = raw.Artist
	s.Album = raw.Album
	s.Duration = raw.Duration
	s.Path = raw.Path
	s.HasLyrics = len(raw.Lyrics) > 0
	return nil
}

type authRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type authResponse struct {
	Token string `json:"token"`
}
