.PHONY: build build-core build-ui clean test-core test-coverage-core lint-core lint-core-all lint-ui fmt-core tidy-core install-core deps-ui run-core version-core run-ui help docker-build docker-build-core docker-build-ui docker-down docker-run docker-run-benchmark

# Build variables
BINARY_NAME=benchmarkoor
VERSION?=$(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
COMMIT?=$(shell git rev-parse --short HEAD 2>/dev/null || echo "none")
DATE?=$(shell date -u +"%Y-%m-%dT%H:%M:%SZ")
LDFLAGS=-ldflags "-X main.version=$(VERSION) -X main.commit=$(COMMIT) -X main.date=$(DATE)"

# Go variables
GOBIN?=$(shell go env GOPATH)/bin
GO_BUILD_TAGS=exclude_graphdriver_btrfs,exclude_graphdriver_devicemapper,containers_image_openpgp

# Directories
UI_DIR := ui

## help: Show this help message
help:
	@echo "Usage: make [target]"
	@echo ""
	@echo "Targets:"
	@sed -n 's/^##//p' $(MAKEFILE_LIST) | column -t -s ':' | sed 's/^/ /'

## build: Build all components (core + ui)
build: build-core build-ui

## build-core: Build the Go binary
build-core:
	go build -tags "$(GO_BUILD_TAGS)" $(LDFLAGS) -o bin/$(BINARY_NAME) ./cmd/benchmarkoor

## build-ui: Build the UI
build-ui: deps-ui
	npm run --prefix $(UI_DIR) build

## install-core: Install the binary to GOPATH/bin
install-core:
	go install -tags "$(GO_BUILD_TAGS)" $(LDFLAGS) ./cmd/benchmarkoor

## deps-ui: Install UI dependencies
deps-ui:
	npm install --prefix $(UI_DIR)

## clean: Remove build artifacts
clean:
	@echo "Cleaning..."
	rm -rf bin/
	rm -rf $(UI_DIR)/dist
	rm -rf $(UI_DIR)/node_modules

## test-core: Run Go tests
test-core:
	go test -tags "$(GO_BUILD_TAGS)" -race -v ./...

## test-coverage-core: Run Go tests with coverage
test-coverage-core:
	go test -tags "$(GO_BUILD_TAGS)" -race -coverprofile=coverage.out -covermode=atomic ./...
	go tool cover -html=coverage.out -o coverage.html

## lint-core: Run Go linter
lint-core:
	golangci-lint run --build-tags "$(GO_BUILD_TAGS)" --new-from-rev="origin/master"

## lint-core-all: Run Go linter on all files
lint-core-all:
	golangci-lint run --build-tags "$(GO_BUILD_TAGS)"

## lint-ui: Run UI linter
lint-ui: deps-ui
	npm run --prefix $(UI_DIR) lint

## fmt-core: Format Go code
fmt-core:
	go fmt ./...
	gofumpt -l -w .

## tidy-core: Tidy go modules
tidy-core:
	go mod tidy

## run-core: Run with example config
run-core: build-core
	./bin/$(BINARY_NAME) run --config config.example.yaml

## version-core: Show version
version-core: build-core
	./bin/$(BINARY_NAME) version

## run-ui: Run the UI dev server
run-ui: deps-ui
	npm run --prefix $(UI_DIR) dev

# Docker variables
DOCKER_REGISTRY?=ethpandaops
DOCKER_IMAGE_CORE?=$(DOCKER_REGISTRY)/benchmarkoor
DOCKER_IMAGE_UI?=$(DOCKER_REGISTRY)/benchmarkoor-ui
DOCKER_TAG?=$(VERSION)

## docker-build: Build all Docker images
docker-build: docker-build-core docker-build-ui

## docker-build-core: Build the core Docker image
docker-build-core:
	docker build \
		--build-arg VERSION=$(VERSION) \
		--build-arg COMMIT=$(COMMIT) \
		--build-arg DATE=$(DATE) \
		-t $(DOCKER_IMAGE_CORE):$(DOCKER_TAG) \
		-t $(DOCKER_IMAGE_CORE):latest \
		-f Dockerfile .

## docker-build-ui: Build the UI Docker image
docker-build-ui:
	docker build \
		--build-arg APP_VERSION=$(VERSION) \
		-t $(DOCKER_IMAGE_UI):$(DOCKER_TAG) \
		-t $(DOCKER_IMAGE_UI):latest \
		-f Dockerfile.ui .

## docker-down: Stop services with docker-compose
docker-down:
	docker compose down

## docker-run: Start the UI and API services with docker-compose (UI_PORT=number to override UI port, API_PORT=number to override API port)
UI_PORT?=8080
API_PORT?=9090
docker-run:
	UI_PORT=$(UI_PORT) API_PORT=$(API_PORT) docker compose up -d --build ui api

## docker-run-benchmark: Start the benchmarkoor service with docker-compose (CLIENT=name to limit, CONFIG=file to override config)
CONFIG?=config.example.docker.yaml
docker-run-benchmark:
	USER_UID=$(shell id -u) USER_GID=$(shell id -g) BENCHMARKOOR_CONFIG=$(CONFIG) docker compose run --rm --build benchmarkoor run --config /app/config.yaml $(if $(CLIENT),--limit-instance-client=$(CLIENT))
