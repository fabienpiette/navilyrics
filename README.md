# navilyrics

Fetch and embed lyrics for your [Navidrome](https://navidrome.org) music library — synced and plain, sidecar files and embedded tags, web UI and CLI.

---

## Quick start

```bash
cp .env.example .env   # fill in NAVIDROME_URL, NAVIDROME_USER, NAVIDROME_PASS, MUSIC_DIR
docker compose up -d --build
# Web UI at http://localhost:8080
```

Or run locally:

```bash
make run-server   # web UI at http://localhost:8080
make run-cli      # batch CLI (dry-run by default)
```

## Features

- **Synced + plain lyrics** — fetches `[mm:ss.xx]` timestamped LRC and plain text
- **Two write targets** — `.lrc` sidecar alongside the audio *and* embedded `LYRICS`/`SYNCEDLYRICS` tags (ID3v2 for MP3, Vorbis comments for FLAC)
- **Provider chain** — [lrclib.net](https://lrclib.net) → NetEase → [Genius](https://genius.com) (optional, requires token)
- **Web UI** — browse library, filter by lyrics status, edit LRC inline, run batch jobs, stream live progress
- **CLI batch** — `navilyrics run` processes every missing song and triggers a Navidrome rescan when done
- **Gap sync** — fills one-sided gaps: has `.lrc` but no tags? Tags get written. Has tags but no `.lrc`? Sidecar gets written.
- **Audio transcription** — when no provider finds lyrics, transcribe the audio via [goscribe](https://github.com/fabienpiette/goscribe) with vocal extraction and AI validation; single-song preview-before-save or batch auto-save with live progress. Webhook delivery reduces latency when `NAVILYRICS_URL` is set (requires `GOSCRIBE_URL`)
- **Tag editor** — edit all standard metadata fields (title, artist, album artist, year, track, disc, genre, composer, comment) directly from the song panel; force-rebuilds corrupt ID3v2 tags transparently

## Install

**Prerequisites:** Go 1.23+ or Docker

### Docker (recommended)

```bash
docker compose up -d --build
```

### From source

```bash
git clone https://github.com/fabienpiette/navilyrics.git
cd navilyrics
make build
```

## Usage

### Web UI

```bash
navilyrics serve [--port 8080]
```

- **Dashboard** — library coverage stats with percentage bar
- **Songs** — browse, search, filter by lyrics status; edit LRC inline; fetch per-song from any provider; edit audio metadata tags
- **Run** — batch-fetch missing lyrics for all songs or filtered results, with live SSE log and filter pills
- **Transcribe** — per-song transcription with editable preview; or "Transcribe missing" batch with live SSE progress (only shown when `GOSCRIBE_URL` is set)

### CLI batch

```bash
navilyrics run [--dry-run]
```

Processes all songs without lyrics, prints a summary, and triggers a Navidrome rescan.

### Sync gaps

```bash
navilyrics sync
```

Walks all songs and fills one-sided lyrics gaps without re-fetching from providers.

## Environment variables

| Variable | Required | Default | Description |
|---|---|---|---|
| `NAVIDROME_URL` | yes | — | e.g. `http://localhost:4533` |
| `NAVIDROME_USER` | yes | — | Navidrome username |
| `NAVIDROME_PASS` | yes | — | Navidrome password |
| `MUSIC_DIR` | yes | — | Path to music files on disk (colon-separated for multiple) |
| `PORT` | no | `8080` | HTTP listen port |
| `DRY_RUN` | no | `false` | Skip writing files |
| `GENIUS_TOKEN` | no | — | Adds Genius as a third provider |
| `GOSCRIBE_URL` | no | — | Enables audio transcription via [goscribe](https://github.com/fabienpiette/goscribe) |
| `NAVILYRICS_URL` | no | — | Public base URL of navilyrics (e.g. `http://host.docker.internal:8081`); enables webhook delivery from goscribe to reduce transcription latency |

## Architecture

```
cmd/navilyrics/     — binary entry point (serve / run / sync subcommands)
pkg/navidrome/      — Navidrome REST client (JWT auth, song listing)
pkg/lrclib/         — lrclib.net client (exact get + fuzzy search)
pkg/goscribe/       — goscribe HTTP client (job submit, poll, webhook receipt)
pkg/tagger/         — audio tag read/write (MP3 ID3v2, FLAC Vorbis)
internal/lyrics/    — Processor with worker pool, LRC writer, transcription
internal/handlers/  — HTTP handlers (chi v5, html/template, HTMX)
web/                — embedded templates and static assets
```

`pkg/` packages never import `internal/`. `internal/lyrics/` is the only package that imports multiple `pkg/` packages.

## License

[AGPL-3.0](LICENSE)
