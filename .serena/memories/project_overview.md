# navilyrics — Project Overview

## Purpose
navilyrics is a Go tool that fetches lyrics for songs in a Navidrome music server, writes them as `.lrc` sidecar files, and embeds them in audio tags (ID3v2 for MP3, Vorbis comments for FLAC). Primary lyrics source: lrclib.net.

## Module
`github.com/user/navilyrics` — Go 1.23.2

## Two subcommands
- `navilyrics serve` — web UI (chi router, HTMX 2.x, html/template)
- `navilyrics run [--dry-run]` — CLI batch processor with worker pool

## Required env vars
NAVIDROME_URL, NAVIDROME_USER, NAVIDROME_PASS, MUSIC_DIR
Optional: PORT (default 8080), DRY_RUN (default false)
