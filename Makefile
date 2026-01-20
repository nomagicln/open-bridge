.PHONY: build test lint clean install

# Build variables
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
COMMIT ?= $(shell git rev-parse HEAD 2>/dev/null || echo "unknown")
DATE ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS := -s -w -X main.version=$(VERSION) -X main.commit=$(COMMIT) -X main.date=$(DATE)

# Default target
all: build

# Build the binary
build:
	CGO_ENABLED=0 go build -ldflags="$(LDFLAGS)" -o bin/ob ./cmd/ob

# Run tests
test:
	go test -v -race ./...

# Run tests with coverage
test-coverage:
	go test -v -race -coverprofile=coverage.out ./...
	go tool cover -html=coverage.out -o coverage.html

# Run linter (golangci-lint v2)
lint:
	@which golangci-lint > /dev/null || (echo "golangci-lint not found. Install with: make install-lint" && exit 1)
	golangci-lint run --timeout=5m ./...

# Run linter with auto-fix
lint-fix:
	@which golangci-lint > /dev/null || (echo "golangci-lint not found. Install with: make install-lint" && exit 1)
	golangci-lint run --timeout=5m --fix ./...

# Install golangci-lint v2
install-lint:
	@echo "Installing golangci-lint v2..."
	go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@latest
	@echo "✓ golangci-lint installed"

# Advanced formatting and modernization
fmt-fix:
	@echo "Advanced code modernization..."
	@echo "1. Converting interface{} to any..."
	gofmt -w -r 'interface{} -> any' .
	@echo "2. Simplifying slice expressions..."
	gofmt -w -r 'α[β:len(α)] -> α[β:]' .
	@echo "3. Running standard formatting..."
	go fmt ./...
	goimports -w .
	@echo "✓ Advanced formatting complete"

# Clean build artifacts
clean:
	rm -rf bin/
	rm -f coverage.out coverage.html

# Install the binary
install: build
	cp bin/ob $(GOPATH)/bin/ob

# Download dependencies
deps:
	go mod download
	go mod verify

# Update dependencies
update-deps:
	go get -u ./...
	go mod tidy

# Run the development binary
run: build
	./bin/ob

# Cross-compile for all platforms
build-all:
	GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -ldflags="$(LDFLAGS)" -o bin/ob-linux-amd64 ./cmd/ob
	GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -ldflags="$(LDFLAGS)" -o bin/ob-linux-arm64 ./cmd/ob
	GOOS=darwin GOARCH=amd64 CGO_ENABLED=0 go build -ldflags="$(LDFLAGS)" -o bin/ob-darwin-amd64 ./cmd/ob
	GOOS=darwin GOARCH=arm64 CGO_ENABLED=0 go build -ldflags="$(LDFLAGS)" -o bin/ob-darwin-arm64 ./cmd/ob
	GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -ldflags="$(LDFLAGS)" -o bin/ob-windows-amd64.exe ./cmd/ob

# Generate documentation
docs:
	godoc -http=:6060

# Property tests (with increased iterations)
test-property:
	go test -v -count=1000 ./... -run "Property"
