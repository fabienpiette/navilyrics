package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"sync/atomic"

	"github.com/user/navilyrics/internal/lyrics"
	"github.com/user/navilyrics/pkg/lrclib"
	"github.com/user/navilyrics/pkg/navidrome"
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

func runServer(_ []string) error {
	return fmt.Errorf("serve subcommand not yet implemented")
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
