# navilyrics — Suggested Commands

## Build & Run
```
make build           # build ./navilyrics binary
make run-server      # run web UI (loads .env)
make run-cli         # run CLI batch in dry-run mode
make build-all       # cross-compile linux/mac/windows
```

## Test & Quality
```
make test            # go test -v -race ./...
make test-coverage   # produces coverage.html
make fmt             # gofmt -w (go fmt ./...)
make vet             # go vet ./...
```

## Docker
```
make docker-build
make up              # docker compose up -d --build
make down
make logs
make clean
```

## Direct Go commands
```
go test -v -race ./...
go build -o navilyrics ./cmd/navilyrics
go fmt ./...
go vet ./...
```
