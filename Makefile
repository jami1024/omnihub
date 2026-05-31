.PHONY: help build run dev test lint tidy clean web-install web-build web-dev web-typecheck

BIN_DIR := bin
BINARY  := omnihub
MAIN    := ./cmd/omnihub
WEB_DIR := web

# Set WEB_BUILD=0 to skip the frontend stage during a Go-only build.
# Useful for fast backend iteration; the embedded UI then falls back to
# whatever placeholder is checked into internal/web/dist.
WEB_BUILD ?= 1

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
COMMIT  := $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
DATE    := $(shell date -u +%Y-%m-%dT%H:%M:%SZ)

LDFLAGS := -ldflags "-s -w \
	-X main.version=$(VERSION) \
	-X main.commit=$(COMMIT) \
	-X main.date=$(DATE)"

help: ## Show this help
	@awk 'BEGIN {FS = ":.*##"; printf "Usage:\n  make \033[36m<target>\033[0m\n\nTargets:\n"} /^[a-zA-Z_-]+:.*?##/ { printf "  \033[36m%-16s\033[0m %s\n", $$1, $$2 }' $(MAKEFILE_LIST)

build: ## Build the omnihub binary into ./bin (set WEB_BUILD=0 to skip frontend)
	@if [ "$(WEB_BUILD)" = "1" ]; then $(MAKE) web-build; fi
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

clean: ## Remove build artifacts (binary, frontend dist, node_modules)
	rm -rf $(BIN_DIR) dist reports
	rm -rf $(WEB_DIR)/node_modules $(WEB_DIR)/dist

web-install: ## Install frontend dependencies (npm)
	cd $(WEB_DIR) && npm install

web-build: ## Build the React admin UI into internal/web/dist
	@if [ ! -d $(WEB_DIR)/node_modules ]; then $(MAKE) web-install; fi
	cd $(WEB_DIR) && npm run build
	@echo "✓ Frontend bundle in internal/web/dist"

web-dev: ## Run the Vite dev server (requires the gateway running on :8080)
	@if [ ! -d $(WEB_DIR)/node_modules ]; then $(MAKE) web-install; fi
	cd $(WEB_DIR) && npm run dev

web-typecheck: ## Run the TypeScript type checker without emitting
	cd $(WEB_DIR) && npm run typecheck

.DEFAULT_GOAL := help
