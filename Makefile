.PHONY: build test lint clean install all deps update-deps run docs \
        test-short test-coverage test-integration bench vet check lint-fix fmt-fix \
        build-all install-lint check-tools lint-version production-deps help

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

# Install destination: GOBIN when set, otherwise GOPATH/bin.
# Resolved via `go env` so the bare $GOPATH environment variable is not required.
GOBIN ?= $(shell go env GOBIN)
ifeq ($(strip $(GOBIN)),)
GOBIN := $(shell go env GOPATH)/bin
endif

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
GOLANGCI_LINT_VERSION ?= v2.15.0
GOLANGCI_LINT ?= golangci-lint

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

# Run integration tests (build tag: integration; skips when no backend is available)
test-integration:
	go test -tags=integration ./...

# Run benchmarks for spec parsing/derived index and CLI hot paths (non-blocking)
bench:
	@echo "==> pkg/spec benchmarks"
	@go test -run '^$$' -bench . -benchmem ./pkg/spec/... || true
	@echo "==> CLI hot-path benchmarks"
	@go test -run '^$$' -bench . -benchmem ./pkg/cli/... || true
	@echo "✓ Benchmarks complete"

# ============================================================================
# Lint & Format Targets
# ============================================================================

# Run go vet with default and integration build tags
vet:
	go vet ./...
	go vet -tags=integration ./...

# Check the selected linter's pinned version and Go compiler compatibility.
lint-version:
	@lint_error() { \
		echo "Error: $$1" >&2; \
		echo "Rebuild $(GOLANGCI_LINT_VERSION) with the current Go toolchain in a separate directory:" >&2; \
		echo "  make install-lint GOBIN=/absolute/path/to/lint-tools" >&2; \
		echo "Select it with GOLANGCI_LINT=/absolute/path/to/lint-tools/golangci-lint (.exe on Windows)." >&2; \
		exit 1; \
	}; \
	lint_version="$$("$(GOLANGCI_LINT)" version)" || lint_error "Cannot run $(GOLANGCI_LINT) version"; \
	printf '%s\n' "$$lint_version"; \
	case "$$lint_version" in \
		*" version $(GOLANGCI_LINT_VERSION) "*|*" version $(patsubst v%,%,$(GOLANGCI_LINT_VERSION)) "*) ;; \
		*) lint_error "Expected golangci-lint $(GOLANGCI_LINT_VERSION)" ;; \
	esac; \
	lint_go=; \
	for word in $$lint_version; do \
		case "$$word" in go[0-9]*.[0-9]*) lint_go=$$word; break ;; esac; \
	done; \
	go_version="$$(go env GOVERSION)" || lint_error "Cannot determine the current Go version"; \
	lint_release=$${lint_go#go}; \
	go_release=$${go_version#go}; \
	lint_major=$${lint_release%%.*}; \
	lint_minor=$${lint_release#*.}; lint_minor=$${lint_minor%%[!0-9]*}; \
	go_major=$${go_release%%.*}; \
	go_minor=$${go_release#*.}; go_minor=$${go_minor%%[!0-9]*}; \
	for part in "$$lint_major" "$$lint_minor" "$$go_major" "$$go_minor"; do \
		case "$$part" in ''|*[!0-9]*) lint_error "Cannot compare Go versions: $$lint_go / $$go_version" ;; esac; \
	done; \
	if [ "$$lint_major" -lt "$$go_major" ] || { \
		[ "$$lint_major" -eq "$$go_major" ] && [ "$$lint_minor" -lt "$$go_minor" ]; \
	}; then \
		lint_error "golangci-lint was built with $$lint_go; the project uses $$go_version"; \
	fi

# Run linter (requires the pinned golangci-lint built with a compatible Go).
lint: lint-version
	"$(GOLANGCI_LINT)" run --timeout=5m ./...

# Run linter with auto-fix
lint-fix: lint-version
	"$(GOLANGCI_LINT)" run --timeout=5m --fix ./...

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
# Quality Gate
# ============================================================================

# Inspect the executable import graph; test dependencies may remain in go.mod.
production-deps:
	@echo "==> production dependency check"
	@deps="$$(go list -deps ./cmd/ob)" || { \
		echo "Error: go list -deps ./cmd/ob failed" >&2; \
		exit 1; \
	}; \
	invalid=0; \
	for dependency in $$deps; do \
		case "$$dependency" in \
			testing|testing/*|github.com/stretchr/testify|github.com/stretchr/testify/*|github.com/leanovate/gopter|github.com/leanovate/gopter/*) \
				echo "Error: production command depends on test package $$dependency" >&2; \
				invalid=1; \
				;; \
		esac; \
	done; \
	[ "$$invalid" -eq 0 ] || exit 1
	@echo "✓ Production dependencies contain no test packages"

# Run all local gates: production imports, formatting, vet, lint, short tests.
check:
	@$(MAKE) --no-print-directory production-deps
	@echo "==> gofmt check"
	@out="$$(gofmt -l cmd pkg internal)"; \
	if [ -n "$$out" ]; then \
		echo "gofmt required for:"; \
		echo "$$out"; \
		exit 1; \
	fi
	$(call check_tool,goimports,Install with: go install golang.org/x/tools/cmd/goimports@latest)
	@echo "==> goimports check"
	@out="$$(goimports -l cmd pkg internal)"; \
	if [ -n "$$out" ]; then \
		echo "goimports required for:"; \
		echo "$$out"; \
		exit 1; \
	fi
	@echo "==> go vet (default + integration build tags)"
	@$(MAKE) --no-print-directory vet
	@echo "==> golangci-lint"
	@$(MAKE) --no-print-directory lint
	@echo "==> short tests"
	@$(MAKE) --no-print-directory test-short
	@echo "✓ All checks passed"

# ============================================================================
# Tool Installation Targets
# ============================================================================

# Install the pinned linter; use a separate GOBIN to keep existing tools intact.
install-lint:
	@echo "Installing golangci-lint $(GOLANGCI_LINT_VERSION) to $(GOBIN)..."
	GOBIN="$(GOBIN)" go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION)
	@echo "✓ golangci-lint installed; select the installed executable with GOLANGCI_LINT"

# Check all required tools
check-tools:
	@echo "Checking required tools..."
	@printf "  %-16s" "go:"; go version 2>/dev/null || echo "NOT FOUND"
	@printf "  %-16s" "gofmt:"; gofmt -h >/dev/null 2>&1 && echo "OK" || echo "NOT FOUND"
	@printf "  %-16s" "goimports:"; goimports -h >/dev/null 2>&1 && echo "OK" || echo "NOT FOUND (optional)"
	@printf "  %-16s" "golangci-lint:"; "$(GOLANGCI_LINT)" version 2>/dev/null || echo "NOT FOUND (optional; select with GOLANGCI_LINT)"
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

# Install the binary to GOBIN (or GOPATH/bin); GOPATH is resolved via `go env`
install: build
	@mkdir -p $(GOBIN)
	cp $(OUTPUT_DIR)/$(BINARY_NAME) $(GOBIN)/$(BINARY_NAME)
	@echo "✓ Installed to $(GOBIN)/$(BINARY_NAME)"

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
	@echo "  install         Install binary to GOBIN (or GOPATH/bin)"
	@echo "  clean           Remove build artifacts"
	@echo ""
	@echo "Test Targets:"
	@echo "  test            Run all tests (unit + property)"
	@echo "  test-short      Run tests quickly (skip property tests)"
	@echo "  test-coverage   Run tests with coverage report"
	@echo "  test-integration Run integration tests (build tag: integration)"
	@echo "  bench           Run benchmarks for spec + CLI hot paths (non-blocking)"
	@echo ""
	@echo "Lint & Format:"
	@echo "  lint            Run golangci-lint"
	@echo "  lint-fix        Run golangci-lint with auto-fix"
	@echo "  lint-version    Check pinned linter version and Go compatibility"
	@echo "  vet             Run go vet (default + integration build tags)"
	@echo "  fmt-fix         Advanced code formatting"
	@echo ""
	@echo "Quality Gate:"
	@echo "  check           Check production imports, formatting, vet, lint and short tests"
	@echo "  production-deps Reject test packages in the production command's imports"
	@echo ""
	@echo "Tools:"
	@echo "  check-tools     Check availability of required tools"
	@echo "  install-lint    Install pinned golangci-lint (choose a separate GOBIN)"
	@echo ""
	@echo "Configuration Variables:"
	@echo "  PLATFORMS       Target platforms for build-all (default: $(PLATFORMS))"
	@echo "  BINARY_NAME     Output binary name (default: $(BINARY_NAME))"
	@echo "  OUTPUT_DIR      Output directory (default: $(OUTPUT_DIR))"
	@echo "  GOLANGCI_LINT   Linter executable (default: $(GOLANGCI_LINT))"
	@echo "  GOLANGCI_LINT_VERSION Pinned linter version (default: $(GOLANGCI_LINT_VERSION))"
	@echo "  GOBIN           Install destination (default: $(GOBIN))"
	@echo "  CGO_ENABLED     Enable CGO (default: $(CGO_ENABLED))"
	@echo "  VERSION         Version string (default: auto-detected from git)"
	@echo ""
	@echo "Examples:"
	@echo "  make build-all PLATFORMS=\"linux/amd64 darwin/arm64\""
	@echo "  make build BINARY_NAME=myapp OUTPUT_DIR=dist"
	@echo "  make install-lint GOBIN=/absolute/path/to/lint-tools"
	@echo "  make lint GOLANGCI_LINT=/absolute/path/to/lint-tools/golangci-lint"
	@echo "  make production-deps"
	@echo "  make test-integration"
