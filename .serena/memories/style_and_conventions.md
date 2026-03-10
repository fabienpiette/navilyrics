# navilyrics — Style & Conventions

## Go style
- Standard Go formatting via `gofmt` / `go fmt ./...`
- Error wrapping: `fmt.Errorf("context: %w", err)`
- Interfaces defined in the consumer package (e.g. LRCFetcher in internal/lyrics)
- No `defer resp.Body.Close()` inside loops — close explicitly each iteration
- Mutexes for shared state (e.g. JWT token in navidrome client)

## Git commits (strict)
- Conventional Commits: `<type>[scope]: <description>`
- One line only — no body, no Co-Authored-By, no AI attribution
- Lowercase imperative, no trailing period, max 50 chars
- Types: feat, fix, docs, refactor, test, chore

## Web / UI (when implemented)
- CSS: pure black/white, CSS vars, 1px borders, no border-radius, system font
- HTMX 2.x via CDN, chi v5 router
- Templates embedded via `//go:embed`

## Key dependencies
- github.com/bogem/id3v2/v2 — MP3 ID3v2 tags
- github.com/mewkiz/flac — FLAC Vorbis comments
- chi v5 — HTTP router (planned)
- HTMX 2.x — web UI (planned)
