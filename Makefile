.PHONY: build clean test lint run help

# Build variables
BINARY_NAME=benchmarkoor
VERSION?=$(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
COMMIT?=$(shell git rev-parse --short HEAD 2>/dev/null || echo "none")
DATE?=$(shell date -u +"%Y-%m-%dT%H:%M:%SZ")
LDFLAGS=-ldflags "-X main.version=$(VERSION) -X main.commit=$(COMMIT) -X main.date=$(DATE)"

# Go variables
GOBIN?=$(shell go env GOPATH)/bin

## help: Show this help message
help:
	@echo "Usage: make [target]"
	@echo ""
	@echo "Targets:"
	@sed -n 's/^##//p' $(MAKEFILE_LIST) | column -t -s ':' | sed 's/^/ /'

## build: Build the binary
build:
	go build $(LDFLAGS) -o bin/$(BINARY_NAME) ./cmd/benchmarkoor

## install: Install the binary to GOPATH/bin
install:
	go install $(LDFLAGS) ./cmd/benchmarkoor

## clean: Remove build artifacts
clean:
	rm -rf bin/
	rm -rf results/

## test: Run tests
test:
	go test -race -v ./...

## test-coverage: Run tests with coverage
test-coverage:
	go test -race -coverprofile=coverage.out -covermode=atomic ./...
	go tool cover -html=coverage.out -o coverage.html

## lint: Run linter
lint:
	golangci-lint run --new-from-rev="origin/master"

## lint-all: Run linter on all files
lint-all:
	golangci-lint run

## fmt: Format code
fmt:
	go fmt ./...
	gofumpt -l -w .

## tidy: Tidy go modules
tidy:
	go mod tidy

## run: Run with example config
run: build
	./bin/$(BINARY_NAME) run --config config.example.yaml

## version: Show version
version: build
	./bin/$(BINARY_NAME) version
