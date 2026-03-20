package lyrics

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/user/navilyrics/pkg/navidrome"
)

func (p *Processor) TranscribeSong(ctx context.Context, song navidrome.Song) (audioPath, transcript string, err error) {
	if len(p.transcribers) == 0 {
		return "", "", fmt.Errorf("no transcribers configured")
	}
	audioPath = p.resolveAudioPath(song.Path)
	if audioPath == "" {
		return "", "", fmt.Errorf("audio file not found in any music dir: %s", song.Path)
	}
	log.Printf("transcribe: %s — %s via %s", song.Artist, song.Title, p.transcribers[0].Name())
	transcript, err = p.transcribers[0].Transcribe(ctx, audioPath)
	if err != nil {
		return audioPath, "", fmt.Errorf("transcribe %q: %w", song.Title, err)
	}
	log.Printf("transcribe: %s — %s done (%d chars)", song.Artist, song.Title, len(transcript))
	return audioPath, transcript, nil
}

func (p *Processor) TranscribeAll(ctx context.Context, songs []navidrome.Song, progress func(Result)) error {
	log.Printf("transcribe: starting %d songs", len(songs))
	start := time.Now()

	const workers = 2
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
				result := p.transcribeOne(ctx, song)
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

	log.Printf("transcribe: finished %d songs in %s", len(songs), time.Since(start).Round(time.Millisecond))
	return ctx.Err()
}

func (p *Processor) transcribeOne(ctx context.Context, song navidrome.Song) Result {
	base := Result{SongID: song.ID, SongPath: song.Path, Title: song.Title, Artist: song.Artist}

	tCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()

	audioPath, transcript, err := p.TranscribeSong(tCtx, song)
	if err != nil {
		log.Printf("[transcribe:error] %s — %s: %v", song.Artist, song.Title, err)
		base.Status = "error"
		base.Err = err.Error()
		return base
	}

	if err := writeLyrics(audioPath, transcript, ""); err != nil {
		log.Printf("[transcribe:error] save %s — %s: %v", song.Artist, song.Title, err)
		base.Status = "error"
		base.Err = err.Error()
		return base
	}

	log.Printf("[transcribed]  %s — %s", song.Artist, song.Title)
	base.PlainLyrics = transcript
	base.Source = "goscribe"
	base.Status = "transcribed"
	return base
}
