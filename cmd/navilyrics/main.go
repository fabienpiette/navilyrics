package main

import (
	"context"
	"flag"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/user/navilyrics/internal/handlers"
	"github.com/user/navilyrics/internal/lyrics"
	"github.com/user/navilyrics/pkg/genius"
	"github.com/user/navilyrics/pkg/goscribe"
	"github.com/user/navilyrics/pkg/lrclib"
	"github.com/user/navilyrics/pkg/navidrome"
	"github.com/user/navilyrics/pkg/netease"
	"github.com/user/navilyrics/pkg/tagger"
	"github.com/user/navilyrics/web"
)

// buildProviders constructs the ordered provider list from env config.
// lrclib and netease are always included; genius is added if GENIUS_TOKEN is set.
func buildProviders() []lyrics.Provider {
	providers := []lyrics.Provider{
		lyrics.NewLRCLibProvider(lrclib.New("")),   // "" = use default lrclib base URL
		lyrics.NewNetEaseProvider(netease.New("")), // "" = use default netease base URL
	}
	if token := os.Getenv("GENIUS_TOKEN"); token != "" {
		providers = append(providers, lyrics.NewGeniusProvider(genius.New(token, ""))) // "" = use default genius API URL
	}
	return providers
}

func providerNames(providers []lyrics.Provider) []string {
	names := make([]string, len(providers))
	for i, p := range providers {
		names[i] = p.Name()
	}
	return names
}

func buildTranscribers() []lyrics.Transcriber {
	if url := os.Getenv("GOSCRIBE_URL"); url != "" {
		return []lyrics.Transcriber{lyrics.NewGoscribeTranscriber(goscribe.New(url), true)}
	}
	return nil
}

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: navilyrics <run|serve|upgrade> [flags]")
		os.Exit(1)
	}
	switch os.Args[1] {
	case "run":
		if err := runCLI(os.Args[2:]); err != nil {
			log.Fatal(err)
		}
	case "serve":
		if err := runServer(os.Args[2:]); err != nil {
			log.Fatal(err)
		}
	case "upgrade":
		if err := runUpgrade(os.Args[2:]); err != nil {
			log.Fatal(err)
		}
	case "sync":
		if err := runSync(os.Args[2:]); err != nil {
			log.Fatal(err)
		}
	default:
		fmt.Fprintf(os.Stderr, "unknown subcommand %q\n", os.Args[1])
		os.Exit(1)
	}
}

func runCLI(args []string) error {
	fs := flag.NewFlagSet("run", flag.ExitOnError)
	dryRun := fs.Bool("dry-run", envBool("DRY_RUN"), "skip writing files")
	if err := fs.Parse(args); err != nil {
		return err
	}

	ndURL := requireEnv("NAVIDROME_URL")
	ndUser := requireEnv("NAVIDROME_USER")
	ndPass := requireEnv("NAVIDROME_PASS")
	musicDir := requireEnv("MUSIC_DIR")

	nd := navidrome.New(ndURL, ndUser, ndPass)
	providers := buildProviders()
	proc := lyrics.NewProcessor(nd, providers, buildTranscribers(), strings.Split(musicDir, ":"), *dryRun)

	var found, notFound, skipped, instrumental, errCount atomic.Int64
	progress := func(r lyrics.Result) {
		switch r.Status {
		case "found":
			found.Add(1)
			log.Printf("found        %s — %s", r.Artist, r.Title)
		case "dry_run":
			found.Add(1)
			log.Printf("dry_run      %s — %s", r.Artist, r.Title)
		case "not_found":
			notFound.Add(1)
		case "skipped":
			skipped.Add(1)
		case "instrumental":
			instrumental.Add(1)
			log.Printf("instrumental %s — %s", r.Artist, r.Title)
		case "error":
			errCount.Add(1)
			log.Printf("error        %s — %s: %s", r.Artist, r.Title, r.Err)
		}
	}

	ctx := context.Background()
	if err := proc.Run(ctx, progress); err != nil {
		return fmt.Errorf("run: %w", err)
	}

	if !*dryRun {
		if err := nd.TriggerScan(ctx); err != nil {
			log.Printf("trigger scan: %v", err)
		}
	}

	log.Printf("done: found=%d not_found=%d skipped=%d instrumental=%d errors=%d",
		found.Load(), notFound.Load(), skipped.Load(), instrumental.Load(), errCount.Load())
	return nil
}

func runServer(args []string) error {
	fs2 := flag.NewFlagSet("serve", flag.ExitOnError)
	port := fs2.String("port", envOr("PORT", "8080"), "listen port")
	if err := fs2.Parse(args); err != nil {
		return err
	}

	ndURL := requireEnv("NAVIDROME_URL")
	ndUser := requireEnv("NAVIDROME_USER")
	ndPass := requireEnv("NAVIDROME_PASS")
	musicDir := requireEnv("MUSIC_DIR")
	dryRun := envBool("DRY_RUN")

	nd := navidrome.New(ndURL, ndUser, ndPass)
	providers := buildProviders()
	transcribers := buildTranscribers()
	proc := lyrics.NewProcessor(nd, providers, transcribers, strings.Split(musicDir, ":"), dryRun)

	tmpls, err := handlers.ParseTemplates(web.FS)
	if err != nil {
		return fmt.Errorf("parse templates: %w", err)
	}
	partials, err := handlers.ParsePartials(web.FS)
	if err != nil {
		return fmt.Errorf("parse partials: %w", err)
	}

	goscribeEnabled := os.Getenv("GOSCRIBE_URL") != ""
	h := handlers.New(nd, proc, tmpls, partials, "dev", providerNames(providers), goscribeEnabled)

	staticFS, err := fs.Sub(web.FS, "static")
	if err != nil {
		return fmt.Errorf("static sub FS: %w", err)
	}

	r := chi.NewRouter()
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)

	r.Get("/", h.Dashboard)
	r.Get("/dashboard/stats", h.DashboardStats)
	r.Get("/songs", h.Songs)
	r.Get("/songs/rows", h.SongsRows)
	r.Get("/songs/{id}/lrc", h.SongLRC)
	r.Get("/songs/{id}/meta", h.SongMeta)
	r.Post("/songs/{id}/fetch", h.SongFetch)
	r.Post("/songs/{id}/save", h.SongSave)
	r.Post("/songs/{id}/tags", h.SongTagsSave)
	r.Put("/songs/{id}/lrc", h.SongLRCSave)
	r.Post("/run", h.RunBatch)
	r.Post("/run/filtered", h.RunFiltered)
	r.Get("/run/{id}/events", h.RunEvents)
	r.Post("/sync", h.RunSync)
	r.Get("/sync/{id}/events", h.SyncEvents)
	r.Post("/transcribe/song", h.TranscribeSong)
	r.Get("/transcribe/song/poll", h.TranscribeSongPoll)
	r.Post("/transcribe/song/save", h.TranscribeSongSave)
	r.Post("/transcribe/batch", h.TranscribeBatch)
	r.Get("/transcribe/batch/{id}/events", h.TranscribeBatchEvents)
	r.Get("/favicon.ico", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
	r.Handle("/static/*", http.StripPrefix("/static/", http.FileServer(http.FS(staticFS))))

	log.Printf("listening on :%s", *port)
	return http.ListenAndServe(":"+*port, r)
}

// runUpgrade walks all music directories and backfills SYLT frames into MP3
// files that have TXXX:SYNCEDLYRICS but no SYLT (written by an older version).
func runUpgrade(args []string) error {
	fs3 := flag.NewFlagSet("upgrade", flag.ExitOnError)
	if err := fs3.Parse(args); err != nil {
		return err
	}

	musicDirs := strings.Split(requireEnv("MUSIC_DIR"), ":")

	var upgraded, skipped, errCount int
	for _, dir := range musicDirs {
		err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return err
			}
			if !strings.EqualFold(filepath.Ext(path), ".mp3") {
				return nil
			}
			ok, err := tagger.BackfillSYLT(path)
			if err != nil {
				log.Printf("[error] %s: %v", path, err)
				errCount++
				return nil
			}
			if ok {
				log.Printf("[upgraded] %s", path)
				upgraded++
			} else {
				skipped++
			}
			return nil
		})
		if err != nil {
			return fmt.Errorf("walk %s: %w", dir, err)
		}
	}
	log.Printf("done: upgraded=%d skipped=%d errors=%d", upgraded, skipped, errCount)
	return nil
}

// runSync walks all Navidrome songs and fills lyrics gaps:
// songs with a .lrc but no embedded tags get their tags written,
// and songs with embedded tags but no .lrc get a sidecar written.
func runSync(args []string) error {
	fs4 := flag.NewFlagSet("sync", flag.ExitOnError)
	if err := fs4.Parse(args); err != nil {
		return err
	}

	ndURL := requireEnv("NAVIDROME_URL")
	ndUser := requireEnv("NAVIDROME_USER")
	ndPass := requireEnv("NAVIDROME_PASS")
	musicDir := requireEnv("MUSIC_DIR")

	nd := navidrome.New(ndURL, ndUser, ndPass)
	proc := lyrics.NewProcessor(nd, nil, nil, strings.Split(musicDir, ":"), false)

	ctx := context.Background()
	songs, err := nd.AllSongs(ctx)
	if err != nil {
		return fmt.Errorf("list songs: %w", err)
	}

	var syncedLRC, syncedEmbedded, skipped, errCount atomic.Int64
	if err := proc.SyncAll(ctx, songs, func(r lyrics.Result) {
		switch r.Status {
		case "synced_lrc":
			syncedLRC.Add(1)
		case "synced_embedded":
			syncedEmbedded.Add(1)
		case "skipped":
			skipped.Add(1)
		case "error":
			errCount.Add(1)
			log.Printf("error  %s — %s: %s", r.Artist, r.Title, r.Err)
		}
	}); err != nil {
		return fmt.Errorf("sync: %w", err)
	}

	if syncedEmbedded.Load() > 0 {
		if err := nd.TriggerScan(ctx); err != nil {
			log.Printf("trigger scan: %v", err)
		}
	}

	log.Printf("done: synced_lrc=%d synced_embedded=%d skipped=%d errors=%d",
		syncedLRC.Load(), syncedEmbedded.Load(), skipped.Load(), errCount.Load())
	return nil
}

func requireEnv(key string) string {
	v := os.Getenv(key)
	if v == "" {
		log.Fatalf("required env var %s is not set", key)
	}
	return v
}

func envBool(key string) bool {
	return os.Getenv(key) == "true" || os.Getenv(key) == "1"
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
