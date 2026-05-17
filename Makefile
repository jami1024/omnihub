.PHONY: help build run dev test lint tidy clean

BIN_DIR := bin
BINARY  := omnihub
MAIN    := ./cmd/omnihub

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
COMMIT  := $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
DATE    := $(shell date -u +%Y-%m-%dT%H:%M:%SZ)

LDFLAGS := -ldflags "-s -w \
	-X main.version=$(VERSION) \
	-X main.commit=$(COMMIT) \
	-X main.date=$(DATE)"

help: ## Show this help
	@awk 'BEGIN {FS = ":.*##"; printf "Usage:\n  make \033[36m<target>\033[0m\n\nTargets:\n"} /^[a-zA-Z_-]+:.*?##/ { printf "  \033[36m%-12s\033[0m %s\n", $$1, $$2 }' $(MAKEFILE_LIST)

build: ## Build the omnihub binary into ./bin
	@mkdir -p $(BIN_DIR)
	CGO_ENABLED=0 go build $(LDFLAGS) -o $(BIN_DIR)/$(BINARY) $(MAIN)
	@echo "✓ Built $(BIN_DIR)/$(BINARY) ($(VERSION))"

run: ## Run the omnihub binary directly via go run
	go run $(MAIN)

dev: ## Run with hot reload (requires: go install github.com/air-verse/air@latest)
	air

test: ## Run all tests
	go test ./...

lint: ## Run linters (requires golangci-lint)
	golangci-lint run ./...

tidy: ## Tidy and verify go modules
	go mod tidy
	go mod verify

clean: ## Remove build artifacts
	rm -rf $(BIN_DIR) dist reports

.DEFAULT_GOAL := help
