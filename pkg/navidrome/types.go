package navidrome

import (
	"encoding/json"
	"errors"
	"strings"
)

// ErrNotFound is returned by GetSong when the requested song does not exist.
var ErrNotFound = errors.New("not found")

// Song represents a Navidrome track.
type Song struct {
	ID          string  `json:"id"`
	Title       string  `json:"title"`
	Artist      string  `json:"artist"`
	AlbumArtist string  `json:"albumArtist"`
	Album       string  `json:"album"`
	Genre       string  `json:"genre"`
	Year        int     `json:"year"`
	TrackNumber int     `json:"trackNumber"`
	Duration    float64 `json:"duration"`
	BitRate     int     `json:"bitRate"`
	Suffix      string  `json:"suffix"` // file format: mp3, flac, ogg…
	Path        string  `json:"path"`   // relative to library root
	HasLyrics   bool    `json:"-"`      // derived from "lyrics" array via UnmarshalJSON
}

// UnmarshalJSON decodes a Navidrome song object. HasLyrics is derived from the
// "lyrics" field, which Navidrome returns as a JSON-encoded string (e.g. "[]"
// or "[{...}]"). A non-empty, non-"[]" value means the file has embedded lyrics.
func (s *Song) UnmarshalJSON(data []byte) error {
	var raw struct {
		ID          string          `json:"id"`
		Title       string          `json:"title"`
		Artist      string          `json:"artist"`
		AlbumArtist string          `json:"albumArtist"`
		Album       string          `json:"album"`
		Genre       string          `json:"genre"`
		Year        int             `json:"year"`
		TrackNumber int             `json:"trackNumber"`
		Duration    float64         `json:"duration"`
		BitRate     int             `json:"bitRate"`
		Suffix      string          `json:"suffix"`
		Path        string          `json:"path"`
		Lyrics      json.RawMessage `json:"lyrics"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	s.ID = raw.ID
	s.Title = raw.Title
	s.Artist = raw.Artist
	s.AlbumArtist = raw.AlbumArtist
	s.Album = raw.Album
	s.Genre = raw.Genre
	s.Year = raw.Year
	s.TrackNumber = raw.TrackNumber
	s.Duration = raw.Duration
	s.BitRate = raw.BitRate
	s.Suffix = raw.Suffix
	s.Path = raw.Path
	// lyrics is a JSON string like "[]" or "[{...}]"; strip the outer quotes
	// to get the inner array literal, then check for empty.
	lyr := strings.Trim(strings.TrimSpace(string(raw.Lyrics)), `"`)
	s.HasLyrics = lyr != "" && lyr != "null" && lyr != "[]"
	return nil
}

type authRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type authResponse struct {
	Token string `json:"token"`
}
