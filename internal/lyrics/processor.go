package lyrics

import (
	"context"
	"log"
	"sync"

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
	Source       string // "lrclib" | ""
	Status       string // "found" | "not_found" | "skipped" | "error" | "dry_run"
	Err          string
}

// Processor fetches and writes lyrics for Navidrome songs.
type Processor struct {
	nd       *navidrome.Client // may be nil in tests
	lrc      LRCFetcher
	musicDir string
	dryRun   bool
}

// NewProcessor creates a Processor. nd may be nil when using ProcessSong directly.
func NewProcessor(nd *navidrome.Client, lrc LRCFetcher, musicDir string, dryRun bool) *Processor {
	return &Processor{nd: nd, lrc: lrc, musicDir: musicDir, dryRun: dryRun}
}

// ProcessSong fetches and (unless dry-run) writes lyrics for a single song.
func (p *Processor) ProcessSong(ctx context.Context, song navidrome.Song) Result {
	r := Result{
		SongID:   song.ID,
		SongPath: song.Path,
		Title:    song.Title,
		Artist:   song.Artist,
	}

	if song.HasLyrics {
		r.Status = "skipped"
		return r
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

	if !ok {
		r.Status = "not_found"
		return r
	}

	r.PlainLyrics = resp.PlainLyrics
	r.SyncedLyrics = resp.SyncedLyrics
	r.Source = "lrclib"

	if p.dryRun {
		r.Status = "dry_run"
		return r
	}

	if err := writeLyrics(song.Path, r.PlainLyrics, r.SyncedLyrics); err != nil {
		r.Status = "error"
		r.Err = err.Error()
		return r
	}

	r.Status = "found"
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
	return ctx.Err()
}
