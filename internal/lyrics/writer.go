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

// writeLyrics writes both the .lrc sidecar and the embedded audio tags.
// On any error it rolls back (removes .lrc if just written) and returns the error.
func writeLyrics(audioPath, plain, synced string) error {
	lrcWritten := false

	if synced != "" {
		if err := WriteLRCFile(audioPath, synced); err != nil {
			return err
		}
		lrcWritten = true
	}

	tgr, err := tagger.ForFile(audioPath)
	if err != nil {
		log.Printf("tags: skip embed (unsupported format) %s", audioPath)
		return nil
	}

	if err := tgr.WriteLyrics(audioPath, plain, synced); err != nil {
		if lrcWritten {
			os.Remove(lrcPathFor(audioPath))
		}
		return fmt.Errorf("embed tags %s: %w", audioPath, err)
	}
	log.Printf("tags: embedded %s", audioPath)
	return nil
}

// lrcPathFor returns the .lrc sidecar path for a given audio file path.
func lrcPathFor(audioPath string) string {
	ext := filepath.Ext(audioPath)
	return strings.TrimSuffix(audioPath, ext) + ".lrc"
}
