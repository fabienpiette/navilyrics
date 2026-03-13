FROM golang:1.23-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o navilyrics ./cmd/navilyrics

# mp3val is not in Alpine 3.20 repos — build from source.
FROM alpine:3.20 AS mp3val-builder
RUN apk add --no-cache g++ make curl
RUN curl -sL "https://downloads.sourceforge.net/project/mp3val/mp3val/mp3val-0.1.8/mp3val-0.1.8.tar.gz" \
        -o mp3val.tar.gz \
    && tar xzf mp3val.tar.gz \
    && make -C mp3val-0.1.8 -f Makefile.linux \
    && strip mp3val-0.1.8/mp3val

FROM alpine:3.20
RUN apk add --no-cache ca-certificates
WORKDIR /app
COPY --from=builder /app/navilyrics .
COPY --from=mp3val-builder /mp3val-0.1.8/mp3val /usr/local/bin/mp3val
EXPOSE 8080
CMD ["./navilyrics", "serve"]
