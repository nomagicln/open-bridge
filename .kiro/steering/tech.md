# Technology Stack

## Language & Runtime

- **Go 1.25.5**: Primary language
- **CGO_ENABLED=0**: Static binaries with no C dependencies

## Core Dependencies

- `github.com/getkin/kin-openapi`: OpenAPI 2.0/3.x parsing and validation
- `github.com/spf13/cobra`: CLI framework for command structure
- `github.com/99designs/keyring`: Cross-platform secure credential storage
- `gopkg.in/yaml.v3`: YAML configuration parsing

## Build System

### Makefile Targets

```bash
# Development
make build          # Build binary to bin/ob
make test           # Run tests with race detector
make test-coverage  # Generate coverage report
make run            # Build and run development binary

# Code Quality
make lint           # Run golangci-lint
make fmt            # Format code with go fmt and goimports

# Property-Based Testing
make test-property  # Run property tests with 1000 iterations

# Distribution
make build-all      # Cross-compile for all platforms
make install        # Install to $GOPATH/bin

# Maintenance
make clean          # Remove build artifacts
make deps           # Download dependencies
make update-deps    # Update all dependencies
```

### Common Commands

```bash
# Build
go build -o bin/ob ./cmd/ob

# Test
go test -v -race ./...

# Run specific tests
go test -v ./pkg/semantic -run TestMapper

# Run with coverage
go test -v -race -coverprofile=coverage.out ./...
go tool cover -html=coverage.out

# Install locally
go install ./cmd/ob
```

## Build Configuration

- **LDFLAGS**: Version, commit, and build date injected at build time
- **Static linking**: All binaries are statically linked for portability
- **Cross-compilation**: Supports linux/darwin/windows on amd64/arm64

## Testing Strategy

- Unit tests co-located with source files (`*_test.go`)
- Integration tests in dedicated files (`integration_test.go`)
- Property-based tests for core logic validation
- Race detector enabled by default in test runs
- Test utilities in `internal/testutil/`

## Configuration Storage

Platform-specific configuration directories:
- **macOS**: `~/Library/Application Support/openbridge/`
- **Linux**: `~/.config/openbridge/`
- **Windows**: `%APPDATA%\openbridge\`

Override with `OPENBRIDGE_CONFIG_DIR` environment variable.
