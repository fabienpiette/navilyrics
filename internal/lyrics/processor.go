package lyrics

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/user/navilyrics/pkg/lrclib"
	"github.com/user/navilyrics/pkg/navidrome"
)

// LRCFetcher abstracts lrclib lookups (enables test stubs without network).
type LRCFetcher interface {
	Get(ctx context.Context, artist, title, album string, duration float64) (lrclib.Response, bool, error)
	Search(ctx context.Context, artist, title string, duration float64) (lrclib.Response, bool, error)
}

// Result holds the outcome of processing a single song.
type Result struct {
	SongID       string
	SongPath     string
	Title        string
	Artist       string
	PlainLyrics  string
	SyncedLyrics string
	Instrumental bool   // true when the provider confirmed no lyrics (instrumental track)
	Source       string // "lrclib" | "netease" | ""
	Status       string // "found" | "not_found" | "skipped" | "error" | "dry_run"
	Err          string
}

// Processor fetches and writes lyrics for Navidrome songs.
type Processor struct {
	nd        *navidrome.Client // may be nil in tests
	lrc       LRCFetcher
	fallback  LRCFetcher // optional; nil = disabled
	musicDirs []string
	dryRun    bool
}

// SetFallback sets an optional secondary lyrics source tried after lrc fails.
func (p *Processor) SetFallback(f LRCFetcher) { p.fallback = f }

// NewProcessor creates a Processor. nd may be nil when using ProcessSong directly.
// musicDirs is a list of base directories to search for audio files; the first
// directory containing the relative song path is used.
func NewProcessor(nd *navidrome.Client, lrc LRCFetcher, musicDirs []string, dryRun bool) *Processor {
	return &Processor{nd: nd, lrc: lrc, musicDirs: musicDirs, dryRun: dryRun}
}

// resolveAudioPath finds the first musicDir where song.Path exists on disk.
// Returns empty string if not found in any directory.
func (p *Processor) resolveAudioPath(relPath string) string {
	for _, dir := range p.musicDirs {
		full := filepath.Join(dir, relPath)
		if _, err := os.Stat(full); err == nil {
			return full
		}
	}
	return ""
}

// ResolveLRCPath returns the full .lrc sidecar path for a relative song path,
// or "" if the audio file is not found in any music dir.
func (p *Processor) ResolveLRCPath(relPath string) string {
	audioPath := p.resolveAudioPath(relPath)
	if audioPath == "" {
		return ""
	}
	return lrcPathFor(audioPath)
}

// FetchLyricsOnly searches external providers for lyrics without writing anything.
// Returns a Result with status "found" or "not_found" and the lyrics if found.
func (p *Processor) FetchLyricsOnly(ctx context.Context, song navidrome.Song) Result {
	r := Result{
		SongID:   song.ID,
		SongPath: song.Path,
		Title:    song.Title,
		Artist:   song.Artist,
	}

	// Strategy 1: exact get
	resp, ok, err := p.lrc.Get(ctx, song.Artist, song.Title, song.Album, song.Duration)
	if err != nil {
		log.Printf("lrclib get %q: %v", song.Title, err)
	}
	// Strategy 2: fuzzy search if exact failed
	if !ok {
		resp, ok, err = p.lrc.Search(ctx, song.Artist, song.Title, song.Duration)
		if err != nil {
			log.Printf("lrclib search %q: %v", song.Title, err)
		}
	}
	// Strategy 3: fallback provider
	if !ok && p.fallback != nil {
		resp, ok, err = p.fallback.Search(ctx, song.Artist, song.Title, song.Duration)
		if err != nil {
			log.Printf("netease search %q: %v", song.Title, err)
		}
		if ok {
			r.Source = "netease"
		}
	}

	if !ok {
		log.Printf("[not_found] %s — %s", song.Artist, song.Title)
		r.Status = "not_found"
		return r
	}

	r.PlainLyrics = resp.PlainLyrics
	r.SyncedLyrics = resp.SyncedLyrics
	r.Instrumental = resp.Instrumental
	if r.Source == "" {
		r.Source = "lrclib"
	}
	r.Status = "found"
	return r
}

// SaveLyrics writes lyrics to disk for a song (lrc sidecar + tag embedding).
func (p *Processor) SaveLyrics(song navidrome.Song, plain, synced string) error {
	audioPath := p.resolveAudioPath(song.Path)
	if audioPath == "" {
		return fmt.Errorf("file not found in any music dir")
	}
	return writeLyrics(audioPath, plain, synced)
}

// ProcessSong fetches and (unless dry-run) writes lyrics for a single song.
func (p *Processor) ProcessSong(ctx context.Context, song navidrome.Song) Result {
	base := Result{SongID: song.ID, SongPath: song.Path, Title: song.Title, Artist: song.Artist}

	if song.HasLyrics {
		return Result{SongID: song.ID, SongPath: song.Path, Title: song.Title, Artist: song.Artist, Status: "skipped"}
	}

	// Skip tracks already marked instrumental from a previous run.
	audioPath := p.resolveAudioPath(song.Path)
	if audioPath != "" && isInstrumental(audioPath) {
		log.Printf("[skipped]   %s — %s (instrumental)", song.Artist, song.Title)
		base.Status = "skipped"
		base.Instrumental = true
		return base
	}

	r := p.FetchLyricsOnly(ctx, song)
	if r.Status != "found" {
		return r
	}

	// Persist instrumental status so future batch runs skip this track.
	if r.Instrumental {
		if !p.dryRun && audioPath != "" {
			_ = markInstrumental(audioPath)
		}
		log.Printf("[instrumental] %s — %s", song.Artist, song.Title)
		r.Status = "instrumental"
		return r
	}

	if p.dryRun {
		log.Printf("[dry_run]   %s — %s", song.Artist, song.Title)
		r.Status = "dry_run"
		return r
	}

	if err := p.SaveLyrics(song, r.PlainLyrics, r.SyncedLyrics); err != nil {
		log.Printf("[error]     %s — %s: %v", song.Artist, song.Title, err)
		r.Status = "error"
		r.Err = err.Error()
		return r
	}

	log.Printf("[found]     %s — %s", song.Artist, song.Title)
	return r
}

// Run processes all songs from Navidrome with a worker pool of 4.
// progress is called once per result (may be called from any goroutine).
func (p *Processor) Run(ctx context.Context, progress func(Result)) error {
	songs, err := p.nd.AllSongs(ctx)
	if err != nil {
		return err
	}
	return p.RunSongs(ctx, songs, progress)
}

// RunSongs processes a given list of songs with a worker pool of 4.
// progress is called once per result (may be called from any goroutine).
func (p *Processor) RunSongs(ctx context.Context, songs []navidrome.Song, progress func(Result)) error {
	log.Printf("run: starting %d songs (dry_run=%v)", len(songs), p.dryRun)
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
				result := p.ProcessSong(ctx, song)
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

	log.Printf("run: finished %d songs in %s", len(songs), time.Since(start).Round(time.Millisecond))
	return ctx.Err()
}
