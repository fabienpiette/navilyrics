# navilyrics

Fetch and embed lyrics for your Navidrome music library.

- Fetches plain + synced lyrics from [lrclib.net](https://lrclib.net)
- Writes `.lrc` sidecar files alongside audio files
- Embeds `LYRICS` / `SYNCEDLYRICS` tags directly into MP3 (ID3v2) and FLAC (Vorbis comments)
- Web UI and CLI batch mode

## Quick start

```bash
cp .env.example .env   # fill in your values
make run-server        # web UI at http://localhost:8080
make run-cli           # batch CLI (dry-run by default)
```

## Environment variables

| Variable | Required | Description |
|---|---|---|
| `NAVIDROME_URL` | yes | e.g. `http://localhost:4533` |
| `NAVIDROME_USER` | yes | Navidrome username |
| `NAVIDROME_PASS` | yes | Navidrome password |
| `MUSIC_DIR` | yes | Path to music files on disk |
| `PORT` | no | HTTP listen port (default `8080`) |
| `DRY_RUN` | no | `true` to skip writing files |

## Docker

```bash
docker compose up -d --build
```

## Usage

### Web UI

```
navilyrics serve [--port 8080]
```

- **Dashboard** — library coverage overview
- **Songs** — browse all songs, filter by lyrics status
- **Run batch** — process all missing songs via the web

### CLI batch

```
navilyrics run [--dry-run]
```

Processes all songs without lyrics, prints a summary, then triggers a Navidrome rescan.

## Architecture

```
cmd/navilyrics/     — binary entry point (serve + run subcommands)
pkg/navidrome/      — Navidrome REST API client (JWT auth, song listing)
pkg/lrclib/         — lrclib.net client (exact get + fuzzy search)
pkg/tagger/         — audio tag read/write (MP3 ID3v2, FLAC Vorbis)
internal/lyrics/    — Processor with worker pool, LRC writer
internal/handlers/  — HTTP handlers (chi, html/template)
web/                — embedded templates and static assets
```

`pkg/` packages never import `internal/`. `internal/lyrics/` is the only package that imports multiple `pkg/` packages.
