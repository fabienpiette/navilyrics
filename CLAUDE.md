# CLAUDE.md

## Git Commit Style
- Conventional Commits: `<type>[scope]: <description>`
- Lowercase imperative, no trailing period, max 50 chars
- One-line only; no body unless necessary
- No Claude/AI attribution in commit messages

## Commands

make build          # build binary
make run-server     # run web UI locally (reads env from .env)
make run-cli        # run CLI batch in dry-run mode
make test           # go test -v -race ./...
make test-coverage  # produces coverage.html
make fmt            # gofmt -w
make vet            # go vet ./...
make build-all      # cross-compile
make docker-build
make up             # docker compose up -d --build

Required env vars: NAVIDROME_URL, NAVIDROME_USER, NAVIDROME_PASS, MUSIC_DIR.
Optional: PORT (default 8080), DRY_RUN (default false).

## Architecture

Single binary with two subcommands: `navilyrics serve` (web UI) and `navilyrics run [--dry-run]` (CLI batch).

- pkg/navidrome/ — Navidrome REST API client (JWT auth, song listing)
- pkg/lrclib/    — lrclib.net HTTP client (exact get + fuzzy search)
- pkg/tagger/    — Audio tag reader/writer (MP3 ID3v2, FLAC Vorbis comments)
- internal/lyrics/   — Processor: fetches lyrics, writes .lrc, embeds tags, worker pool
- internal/handlers/ — HTTP handlers (chi, html/template)
- web/               — Embedded templates and static assets

pkg/ packages never import internal/. internal/lyrics/ is the only package that imports multiple pkg/ packages.
