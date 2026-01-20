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

# Run linter
lint:
	golangci-lint run ./...

# Format code
fmt:
	go fmt ./...
	goimports -w .

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
