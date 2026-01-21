.PHONY: build test lint clean install all deps update-deps run docs \
        test-short test-coverage lint-fix fmt-fix build-all install-lint check-tools help

# ============================================================================
# Build Variables
# ============================================================================
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
COMMIT  ?= $(shell git rev-parse HEAD 2>/dev/null || echo "unknown")
DATE    ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS := -s -w -X main.version=$(VERSION) -X main.commit=$(COMMIT) -X main.date=$(DATE)

# Binary configuration
BINARY_NAME ?= ob
CMD_PATH    ?= ./cmd/ob
OUTPUT_DIR  ?= bin

# ============================================================================
# Cross-Platform Build Configuration
# ============================================================================
# Format: OS/ARCH (space-separated list)
# Override with: make build-all PLATFORMS="linux/amd64 darwin/arm64"
PLATFORMS ?= linux/amd64 linux/arm64 darwin/amd64 darwin/arm64 windows/amd64

# CGO setting for cross-compilation
CGO_ENABLED ?= 0

# ============================================================================
# Tool Versions (for installation)
# ============================================================================
GOLANGCI_LINT_VERSION ?= latest

# ============================================================================
# Tool Detection Functions
# ============================================================================
# Check if a command exists and is executable
define check_tool
	@if ! command -v $(1) >/dev/null 2>&1; then \
		echo "Error: $(1) is not installed or not in PATH"; \
		echo "$(2)"; \
		exit 1; \
	fi
endef

# Check if a command exists and can report its version
define check_tool_version
	@if ! $(1) $(2) >/dev/null 2>&1; then \
		echo "Error: $(1) is not available or not working properly"; \
		echo "$(3)"; \
		exit 1; \
	fi
endef

# ============================================================================
# Default Target
# ============================================================================
all: build

# ============================================================================
# Build Targets
# ============================================================================

# Build the binary for current platform
build:
	@mkdir -p $(OUTPUT_DIR)
	CGO_ENABLED=$(CGO_ENABLED) go build -ldflags="$(LDFLAGS)" -o $(OUTPUT_DIR)/$(BINARY_NAME) $(CMD_PATH)

# Cross-compile for configured platforms
# Usage:
#   make build-all                                    # Build for all default platforms
#   make build-all PLATFORMS="linux/amd64"            # Build for specific platform
#   make build-all PLATFORMS="linux/amd64 darwin/arm64"  # Build for multiple platforms
build-all:
	@mkdir -p $(OUTPUT_DIR)
	@for platform in $(PLATFORMS); do \
		os=$$(echo $$platform | cut -d'/' -f1); \
		arch=$$(echo $$platform | cut -d'/' -f2); \
		ext=""; \
		if [ "$$os" = "windows" ]; then ext=".exe"; fi; \
		output="$(OUTPUT_DIR)/$(BINARY_NAME)-$$os-$$arch$$ext"; \
		echo "Building $$output..."; \
		GOOS=$$os GOARCH=$$arch CGO_ENABLED=$(CGO_ENABLED) \
			go build -ldflags="$(LDFLAGS)" -o $$output $(CMD_PATH) || exit 1; \
	done
	@echo "✓ Cross-compilation complete"

# ============================================================================
# Test Targets
# ============================================================================

# Run all tests (unit + property tests)
test:
	go test -v -race ./...

# Run tests quickly (skip property tests via -short flag)
test-short:
	go test -v -race -short ./...

# Run tests with coverage report
test-coverage:
	go test -v -race -coverprofile=coverage.out ./...
	go tool cover -html=coverage.out -o coverage.html
	@echo "✓ Coverage report generated: coverage.html"

# ============================================================================
# Lint & Format Targets
# ============================================================================

# Run linter (requires golangci-lint)
lint:
	$(call check_tool_version,golangci-lint,version,Install with: make install-lint)
	golangci-lint run --timeout=5m ./...

# Run linter with auto-fix
lint-fix:
	$(call check_tool_version,golangci-lint,version,Install with: make install-lint)
	golangci-lint run --timeout=5m --fix ./...

# Advanced formatting and modernization
fmt-fix:
	$(call check_tool,gofmt,gofmt should be included with Go installation)
	$(call check_tool,goimports,Install with: go install golang.org/x/tools/cmd/goimports@latest)
	@echo "Advanced code modernization..."
	@echo "1. Converting interface{} to any..."
	@gofmt -w -r 'interface{} -> any' .
	@echo "2. Simplifying slice expressions..."
	@gofmt -w -r 'a[b:len(a)] -> a[b:]' .
	@echo "3. Running standard formatting..."
	@gofmt -s -w .
	@goimports -w .
	@echo "✓ Advanced formatting complete"

# ============================================================================
# Tool Installation Targets
# ============================================================================

# Install golangci-lint
install-lint:
	@echo "Installing golangci-lint $(GOLANGCI_LINT_VERSION)..."
	go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION)
	@echo "✓ golangci-lint installed"

# Check all required tools
check-tools:
	@echo "Checking required tools..."
	@printf "  %-16s" "go:"; go version 2>/dev/null || echo "NOT FOUND"
	@printf "  %-16s" "gofmt:"; gofmt -h >/dev/null 2>&1 && echo "OK" || echo "NOT FOUND"
	@printf "  %-16s" "goimports:"; goimports -h >/dev/null 2>&1 && echo "OK" || echo "NOT FOUND (optional)"
	@printf "  %-16s" "golangci-lint:"; golangci-lint version 2>/dev/null || echo "NOT FOUND (optional)"
	@printf "  %-16s" "godoc:"; godoc -h >/dev/null 2>&1 && echo "OK" || echo "NOT FOUND (optional)"

# ============================================================================
# Dependency Management
# ============================================================================

# Download dependencies
deps:
	go mod download
	go mod verify

# Update dependencies
update-deps:
	go get -u ./...
	go mod tidy

# ============================================================================
# Installation & Run
# ============================================================================

# Install the binary to GOPATH/bin
install: build
	@if [ -z "$(GOPATH)" ]; then \
		echo "Error: GOPATH is not set"; \
		exit 1; \
	fi
	cp $(OUTPUT_DIR)/$(BINARY_NAME) $(GOPATH)/bin/$(BINARY_NAME)
	@echo "✓ Installed to $(GOPATH)/bin/$(BINARY_NAME)"

# Run the development binary
run: build
	./$(OUTPUT_DIR)/$(BINARY_NAME)

# ============================================================================
# Documentation
# ============================================================================

# Generate documentation server
docs:
	$(call check_tool,godoc,Install with: go install golang.org/x/tools/cmd/godoc@latest)
	@echo "Starting godoc server at http://localhost:6060"
	godoc -http=:6060

# ============================================================================
# Cleanup
# ============================================================================

# Clean build artifacts
clean:
	rm -rf $(OUTPUT_DIR)/
	rm -f coverage.out coverage.html

# ============================================================================
# Help
# ============================================================================

help:
	@echo "OpenBridge Makefile"
	@echo ""
	@echo "Usage: make [target] [VARIABLE=value]"
	@echo ""
	@echo "Build Targets:"
	@echo "  build           Build binary for current platform"
	@echo "  build-all       Cross-compile for all configured platforms"
	@echo "  install         Install binary to GOPATH/bin"
	@echo "  clean           Remove build artifacts"
	@echo ""
	@echo "Test Targets:"
	@echo "  test            Run all tests (unit + property)"
	@echo "  test-short      Run tests quickly (skip property tests)"
	@echo "  test-coverage   Run tests with coverage report"
	@echo ""
	@echo "Lint & Format:"
	@echo "  lint            Run golangci-lint"
	@echo "  lint-fix        Run golangci-lint with auto-fix"
	@echo "  fmt-fix         Advanced code formatting"
	@echo ""
	@echo "Tools:"
	@echo "  check-tools     Check availability of required tools"
	@echo "  install-lint    Install golangci-lint"
	@echo ""
	@echo "Configuration Variables:"
	@echo "  PLATFORMS       Target platforms for build-all (default: $(PLATFORMS))"
	@echo "  BINARY_NAME     Output binary name (default: $(BINARY_NAME))"
	@echo "  OUTPUT_DIR      Output directory (default: $(OUTPUT_DIR))"
	@echo "  CGO_ENABLED     Enable CGO (default: $(CGO_ENABLED))"
	@echo "  VERSION         Version string (default: auto-detected from git)"
	@echo ""
	@echo "Examples:"
	@echo "  make build-all PLATFORMS=\"linux/amd64 darwin/arm64\""
	@echo "  make build BINARY_NAME=myapp OUTPUT_DIR=dist"
	@echo "  make install-lint GOLANGCI_LINT_VERSION=v2.0.0"
