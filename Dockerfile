# Build stage
FROM golang:1.24-alpine AS builder

RUN apk add --no-cache git

WORKDIR /app

# Copy go mod files first for caching
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Build arguments for version info
ARG VERSION=dev
ARG COMMIT=none
ARG DATE=unknown

# Build the binary
RUN CGO_ENABLED=0 GOOS=linux go build \
    -tags "exclude_graphdriver_btrfs,exclude_graphdriver_devicemapper,containers_image_openpgp" \
    -ldflags "-X main.version=${VERSION} -X main.commit=${COMMIT} -X main.date=${DATE}" \
    -o /benchmarkoor ./cmd/benchmarkoor

# Final stage
FROM alpine:3.21

RUN apk add --no-cache ca-certificates tzdata git zfs fuse-overlayfs rsync iptables iproute2 util-linux-misc && \
    if [ "$(uname -m)" = "x86_64" ]; then \
      apk add --no-cache --repository=https://dl-cdn.alpinelinux.org/alpine/edge/testing criu; \
    fi

WORKDIR /app

COPY --from=builder /benchmarkoor /usr/local/bin/benchmarkoor

# schelk-host: thin wrapper that runs the host's `schelk` binary in the
# host's mount namespace via nsenter. Used by the schelk datadir method
# when benchmarkoor is launched inside a Docker container (e.g. by the
# GitHub Action): the container can't operate on dm-era / mounts in its
# own mount NS, so we hop into the host's. Requires the docker run to be
# launched with `--privileged --pid=host`. Point benchmarkoor at this
# via `BENCHMARKOOR_SCHELK_BIN=/usr/local/bin/schelk-host`.
RUN printf '#!/bin/sh\nexec nsenter -t 1 -m -- /usr/local/bin/schelk "$@"\n' \
      > /usr/local/bin/schelk-host && \
    chmod +x /usr/local/bin/schelk-host

ENTRYPOINT ["benchmarkoor"]
