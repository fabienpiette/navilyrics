BINARY_NAME ?= navilyrics
BIN_DIR     ?= bin
VERSION     ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
LDFLAGS     := -ldflags="-s -w -X main.version=$(VERSION)"

.PHONY: build test test-coverage clean run-server run-cli fmt vet \
        build-all docker-build up down restart logs

## Build the binary
build:
	go build $(LDFLAGS) -o $(BINARY_NAME) ./cmd/navilyrics

## Run all tests with race detector
test:
	go test -v -race ./...

## Run tests and produce an HTML coverage report
test-coverage:
	go test -v -race -coverprofile=coverage.out ./...
	go tool cover -html=coverage.out -o coverage.html

## Remove build artifacts
clean:
	rm -f $(BINARY_NAME) coverage.out coverage.html
	rm -rf $(BIN_DIR)

## Run the web server locally (loads .env if present)
run-server:
	@set -a && [ -f .env ] && . ./.env; set +a && go run $(LDFLAGS) ./cmd/navilyrics serve

## Run the CLI batch processor in dry-run mode locally
run-cli:
	@set -a && [ -f .env ] && . ./.env; set +a && go run $(LDFLAGS) ./cmd/navilyrics run --dry-run

## Format all Go source files
fmt:
	go fmt ./...

## Run go vet
vet:
	go vet ./...

## Cross-compile for Linux amd64, macOS amd64/arm64, Windows amd64
build-all:
	mkdir -p $(BIN_DIR)
	GOOS=linux   GOARCH=amd64 CGO_ENABLED=0 go build $(LDFLAGS) -o $(BIN_DIR)/$(BINARY_NAME)-linux-amd64    ./cmd/navilyrics
	GOOS=darwin  GOARCH=amd64 CGO_ENABLED=0 go build $(LDFLAGS) -o $(BIN_DIR)/$(BINARY_NAME)-darwin-amd64   ./cmd/navilyrics
	GOOS=darwin  GOARCH=arm64 CGO_ENABLED=0 go build $(LDFLAGS) -o $(BIN_DIR)/$(BINARY_NAME)-darwin-arm64   ./cmd/navilyrics
	GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build $(LDFLAGS) -o $(BIN_DIR)/$(BINARY_NAME)-windows-amd64.exe ./cmd/navilyrics

## Build the Docker image
docker-build:
	docker build --build-arg VERSION=$(VERSION) -t $(BINARY_NAME):$(VERSION) -t $(BINARY_NAME):latest .

## Start the stack with Docker Compose
up:
	VERSION=$(VERSION) docker compose up -d --build

## Stop the stack
down:
	docker compose down

## Hard stop then rebuild and start the stack
restart:
	docker compose down && VERSION=$(VERSION) docker compose up -d --build

## Tail Docker Compose logs
logs:
	docker compose logs -f
