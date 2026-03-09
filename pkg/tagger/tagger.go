package tagger

import (
	"fmt"
	"path/filepath"
	"strings"
)

// Tagger reads and writes LYRICS and SYNCEDLYRICS tags in an audio file.
type Tagger interface {
	// ReadLyrics returns the plain and synced lyrics stored in the file.
	// Returns ("", "", nil) if no lyrics are present.
	ReadLyrics(path string) (plain, synced string, err error)

	// WriteLyrics writes the plain and synced lyrics to the file.
	// An empty string for either value leaves that tag unchanged.
	WriteLyrics(path, plain, synced string) error
}

// ForFile returns the appropriate Tagger for the given file based on extension.
func ForFile(path string) (Tagger, error) {
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".mp3":
		return &mp3Tagger{}, nil
	case ".flac":
		return &flacTagger{}, nil
	default:
		return nil, fmt.Errorf("unsupported audio format: %q", ext)
	}
}
