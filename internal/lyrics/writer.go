package lyrics

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/user/navilyrics/pkg/tagger"
)

// WriteLRCFile writes synced lyrics as a .lrc sidecar next to the audio file.
// Uses atomic write (temp→rename) to avoid partial writes on failure.
func WriteLRCFile(audioPath, synced string) error {
	if synced == "" {
		return nil
	}
	lrcPath := lrcPathFor(audioPath)
	tmp := lrcPath + ".tmp"
	if err := os.WriteFile(tmp, []byte(synced), 0644); err != nil {
		return fmt.Errorf("write lrc tmp: %w", err)
	}
	if err := os.Rename(tmp, lrcPath); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("rename lrc: %w", err)
	}
	log.Printf("lrc: wrote %s", lrcPath)
	return nil
}

// writeLyrics writes the .lrc sidecar and attempts to embed lyrics into audio tags.
// Tag embedding failure is a soft error: the .lrc sidecar is kept and a warning is
// logged, because the sidecar alone is sufficient for Navidrome to read lyrics.
func writeLyrics(audioPath, plain, synced string) error {
	if synced != "" {
		if err := WriteLRCFile(audioPath, synced); err != nil {
			return err
		}
	}

	tgr, err := tagger.ForFile(audioPath)
	if err != nil {
		log.Printf("tags: skip embed (unsupported format) %s", audioPath)
		return nil
	}

	if err := tgr.WriteLyrics(audioPath, plain, synced); err != nil {
		log.Printf("tags: skip embed (write error) %s: %v", audioPath, err)
		return nil
	}
	log.Printf("tags: embedded %s", audioPath)
	return nil
}

// WriteRawLRC atomically writes content to a known lrcPath.
// Unlike WriteLRCFile, the caller provides the final path directly.
func WriteRawLRC(lrcPath, content string) error {
	tmp := lrcPath + ".tmp"
	if err := os.WriteFile(tmp, []byte(content), 0644); err != nil {
		return fmt.Errorf("write lrc tmp: %w", err)
	}
	if err := os.Rename(tmp, lrcPath); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("rename lrc: %w", err)
	}
	log.Printf("lrc: wrote %s", lrcPath)
	return nil
}

// lrcPathFor returns the .lrc sidecar path for a given audio file path.
func lrcPathFor(audioPath string) string {
	ext := filepath.Ext(audioPath)
	return strings.TrimSuffix(audioPath, ext) + ".lrc"
}
