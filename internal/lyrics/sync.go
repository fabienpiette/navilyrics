package lyrics

import (
	"context"
	"log"
	"os"
	"regexp"
	"strings"
	"time"
	"sync"

	"github.com/user/navilyrics/pkg/navidrome"
	"github.com/user/navilyrics/pkg/tagger"
)

var lrcTimestampRe = regexp.MustCompile(`\[[^\]]*\]`)

// stripLRCTimestamps removes [mm:ss.xx] timestamp tags from an LRC string,
// returning plain multi-line text with blank lines collapsed.
func stripLRCTimestamps(lrc string) string {
	var lines []string
	for _, line := range strings.Split(lrc, "\n") {
		clean := strings.TrimSpace(lrcTimestampRe.ReplaceAllString(line, ""))
		lines = append(lines, clean)
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

// SyncSong fills the lyrics gap for a single song:
//   - has .lrc but no embedded tags → writes embedded tags from the .lrc content
//   - has embedded tags but no .lrc → writes .lrc sidecar from the embedded content
//   - both present or neither present → skipped
//
// Returned Result uses Status "synced_lrc", "synced_embedded", "skipped", or "error".
func (p *Processor) SyncSong(song navidrome.Song) Result {
	base := Result{SongID: song.ID, SongPath: song.Path, Title: song.Title, Artist: song.Artist}

	audioPath := p.resolveAudioPath(song.Path)
	if audioPath == "" {
		base.Status = "error"
		base.Err = "audio file not found in music dirs"
		return base
	}
	lrcPath := lrcPathFor(audioPath)

	lrcData, lrcErr := os.ReadFile(lrcPath)
	hasLRC := lrcErr == nil && len(strings.TrimSpace(string(lrcData))) > 0

	t, tErr := tagger.ForFile(audioPath)
	var embPlain, embSynced string
	if tErr == nil {
		var readErr error
		embPlain, embSynced, readErr = t.ReadLyrics(audioPath)
		if readErr != nil {
			if !tagger.IsBodyOverflow(readErr) {
				log.Printf("[sync:skip] cannot read tags %s — %s: %v", song.Artist, song.Title, readErr)
				base.Status = "skipped"
				return base
			}
			// Malformed ID3v2 frame sizes — attempt repair with mp3val then retry.
			if repErr := tagger.RepairID3(audioPath); repErr != nil {
				log.Printf("[sync:skip] cannot repair %s — %s: %v", song.Artist, song.Title, repErr)
				base.Status = "skipped"
				return base
			}
			log.Printf("[sync:repair] fixed malformed ID3v2 tags %s — %s", song.Artist, song.Title)
			embPlain, embSynced, readErr = t.ReadLyrics(audioPath)
			if readErr != nil {
				log.Printf("[sync:skip] still unreadable after repair %s — %s: %v", song.Artist, song.Title, readErr)
				base.Status = "skipped"
				return base
			}
		}
	}
	hasEmbedded := embPlain != "" || embSynced != ""

	switch {
	case hasLRC && !hasEmbedded:
		if tErr != nil {
			base.Status = "error"
			base.Err = tErr.Error()
			return base
		}
		synced := string(lrcData)
		plain := stripLRCTimestamps(synced)
		if err := t.WriteLyrics(audioPath, plain, synced); err != nil {
			base.Status = "error"
			base.Err = err.Error()
			return base
		}
		log.Printf("[sync:embedded] %s — %s", song.Artist, song.Title)
		base.Status = "synced_embedded"

	case hasEmbedded && !hasLRC:
		content := embSynced
		if content == "" {
			content = embPlain
		}
		if err := WriteRawLRC(lrcPath, content); err != nil {
			base.Status = "error"
			base.Err = err.Error()
			return base
		}
		log.Printf("[sync:lrc]      %s — %s", song.Artist, song.Title)
		base.Status = "synced_lrc"

	default:
		base.Status = "skipped"
	}

	return base
}

// SyncAll runs SyncSong for every song in the list with a worker pool.
// progress is called once per result (may be called from any goroutine).
func (p *Processor) SyncAll(ctx context.Context, songs []navidrome.Song, progress func(Result)) error {
	log.Printf("sync: starting %d songs", len(songs))
	start := time.Now()

	const workers = 4
	jobs := make(chan navidrome.Song, workers)
	var wg sync.WaitGroup

	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for song := range jobs {
				if ctx.Err() != nil {
					return
				}
				result := p.SyncSong(song)
				if progress != nil {
					progress(result)
				}
			}
		}()
	}

	for _, s := range songs {
		select {
		case jobs <- s:
		case <-ctx.Done():
			close(jobs)
			wg.Wait()
			return ctx.Err()
		}
	}
	close(jobs)
	wg.Wait()

	log.Printf("sync: finished %d songs in %s", len(songs), time.Since(start).Round(time.Millisecond))
	return ctx.Err()
}
