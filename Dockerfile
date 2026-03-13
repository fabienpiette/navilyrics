FROM golang:1.23-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o navilyrics ./cmd/navilyrics

FROM debian:bookworm-slim
RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates mp3val \
    && rm -rf /var/lib/apt/lists/*
WORKDIR /app
COPY --from=builder /app/navilyrics .
EXPOSE 8080
CMD ["./navilyrics", "serve"]
