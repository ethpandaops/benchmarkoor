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

# Build the binary. If .build-version exists (CI), source it to override ARG defaults.
RUN if [ -f .build-version ]; then \
      . ./.build-version; \
    fi && \
    CGO_ENABLED=0 GOOS=linux go build \
    -tags "exclude_graphdriver_btrfs,exclude_graphdriver_devicemapper,containers_image_openpgp" \
    -ldflags "-X main.version=${VERSION} -X main.commit=${COMMIT} -X main.date=${DATE}" \
    -o /benchmarkoor ./cmd/benchmarkoor

# Final stage
FROM alpine:3.21

RUN apk add --no-cache ca-certificates tzdata git zfs fuse-overlayfs rsync && \
    apk add --no-cache --repository=https://dl-cdn.alpinelinux.org/alpine/edge/testing criu

WORKDIR /app

COPY --from=builder /benchmarkoor /usr/local/bin/benchmarkoor

ENTRYPOINT ["benchmarkoor"]
