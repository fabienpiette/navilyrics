FROM golang:1.23-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o navilyrics ./cmd/navilyrics

# mp3val is not in Alpine 3.20 repos but is available in Alpine edge/community.
FROM alpine:edge AS mp3val-builder
RUN apk add --no-cache mp3val

FROM alpine:3.20
RUN apk add --no-cache ca-certificates
WORKDIR /app
COPY --from=builder /app/navilyrics .
COPY --from=mp3val-builder /usr/bin/mp3val /usr/local/bin/mp3val
EXPOSE 8080
CMD ["./navilyrics", "serve"]
