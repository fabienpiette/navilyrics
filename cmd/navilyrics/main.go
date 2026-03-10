package main

import (
	"context"
	"flag"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"os"
	"sync/atomic"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/user/navilyrics/internal/handlers"
	"github.com/user/navilyrics/internal/lyrics"
	"github.com/user/navilyrics/pkg/lrclib"
	"github.com/user/navilyrics/pkg/navidrome"
	"github.com/user/navilyrics/web"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: navilyrics <run|serve> [flags]")
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
	lrc := lrclib.New("https://lrclib.net")
	proc := lyrics.NewProcessor(nd, lrc, musicDir, *dryRun)

	var found, notFound, skipped, errCount atomic.Int64
	progress := func(r lyrics.Result) {
		switch r.Status {
		case "found":
			found.Add(1)
			log.Printf("found    %s — %s", r.Artist, r.Title)
		case "dry_run":
			found.Add(1)
			log.Printf("dry_run  %s — %s", r.Artist, r.Title)
		case "not_found":
			notFound.Add(1)
		case "skipped":
			skipped.Add(1)
		case "error":
			errCount.Add(1)
			log.Printf("error    %s — %s: %s", r.Artist, r.Title, r.Err)
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

	log.Printf("done: found=%d not_found=%d skipped=%d errors=%d",
		found.Load(), notFound.Load(), skipped.Load(), errCount.Load())
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
	lrc := lrclib.New("https://lrclib.net")
	proc := lyrics.NewProcessor(nd, lrc, musicDir, dryRun)

	tmpls, err := handlers.ParseTemplates(web.FS)
	if err != nil {
		return fmt.Errorf("parse templates: %w", err)
	}

	h := handlers.New(nd, proc, tmpls, "dev")

	staticFS, err := fs.Sub(web.FS, "static")
	if err != nil {
		return fmt.Errorf("static sub FS: %w", err)
	}

	r := chi.NewRouter()
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)

	r.Get("/", h.Dashboard)
	r.Get("/songs", h.Songs)
	r.Post("/run", h.RunBatch)
	r.Handle("/static/*", http.StripPrefix("/static/", http.FileServer(http.FS(staticFS))))

	log.Printf("listening on :%s", *port)
	return http.ListenAndServe(":"+*port, r)
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
