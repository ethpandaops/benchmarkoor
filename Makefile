.PHONY: build build-core build-ui clean test-core test-coverage-core lint-core lint-core-all lint-ui fmt-core tidy-core install-core deps-ui run-core version-core run-ui help docker-build docker-build-core docker-build-ui docker-up docker-down

# Build variables
BINARY_NAME=benchmarkoor
VERSION?=$(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
COMMIT?=$(shell git rev-parse --short HEAD 2>/dev/null || echo "none")
DATE?=$(shell date -u +"%Y-%m-%dT%H:%M:%SZ")
LDFLAGS=-ldflags "-X main.version=$(VERSION) -X main.commit=$(COMMIT) -X main.date=$(DATE)"

# Go variables
GOBIN?=$(shell go env GOPATH)/bin

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
	go build $(LDFLAGS) -o bin/$(BINARY_NAME) ./cmd/benchmarkoor

## build-ui: Build the UI
build-ui: deps-ui
	npm run --prefix $(UI_DIR) build

## install-core: Install the binary to GOPATH/bin
install-core:
	go install $(LDFLAGS) ./cmd/benchmarkoor

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
	go test -race -v ./...

## test-coverage-core: Run Go tests with coverage
test-coverage-core:
	go test -race -coverprofile=coverage.out -covermode=atomic ./...
	go tool cover -html=coverage.out -o coverage.html

## lint-core: Run Go linter
lint-core:
	golangci-lint run --new-from-rev="origin/master"

## lint-core-all: Run Go linter on all files
lint-core-all:
	golangci-lint run

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
		-t $(DOCKER_IMAGE_UI):$(DOCKER_TAG) \
		-t $(DOCKER_IMAGE_UI):latest \
		-f Dockerfile.ui .

## docker-up: Start services with docker-compose
docker-up:
	USER_UID=$(shell id -u) USER_GID=$(shell id -g) docker compose up -d --build

## docker-down: Stop services with docker-compose
docker-down:
	docker compose down
