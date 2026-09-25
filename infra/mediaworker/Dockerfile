# Media worker for local development (docs/media §6, T-LOCAL-04): Go plus a
# pinned Alpine ffmpeg (with libwebp and libx264). The source is mounted, so
# `go run` picks up changes; build and module caches live in volumes.
FROM golang:1.26-alpine
RUN apk add --no-cache ffmpeg=~8.1 && ffmpeg -hide_banner -encoders | grep -q libwebp
WORKDIR /src
ENV CGO_ENABLED=0 GOFLAGS=-buildvcs=false MEDIA_TMP_DIR=/tmp/media
RUN mkdir -p /tmp/media
CMD ["go", "run", "./cmd/mediaworker"]
