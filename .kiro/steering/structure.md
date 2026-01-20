# Project Structure

## Directory Layout

```
.
├── cmd/ob/              # Main entry point and CLI commands
├── pkg/                 # Public packages (importable)
│   ├── spec/           # OpenAPI spec parsing and validation
│   ├── config/         # Configuration management
│   ├── credential/     # Keyring integration for secure storage
│   ├── semantic/       # Verb-resource mapping from OpenAPI
│   ├── request/        # HTTP request building
│   ├── cli/            # CLI command handling and execution
│   ├── mcp/            # MCP protocol implementation
│   └── router/         # Command routing
├── internal/           # Private packages (not importable)
│   └── testutil/       # Testing utilities
└── .github/workflows/  # CI/CD configuration
```

## Package Organization

### cmd/ob
Entry point for the OpenBridge CLI. Contains the root command and subcommands (install, uninstall, list).

### pkg/spec
Handles OpenAPI specification loading, parsing, and validation. Supports OpenAPI 2.0 (Swagger), 3.0, and 3.1. Includes caching and version detection.

### pkg/config
Configuration management for installed apps and profiles. Handles YAML serialization, platform-specific config directories, and profile management.

### pkg/credential
Secure credential storage using system keyring (Keychain on macOS, Credential Manager on Windows, Secret Service on Linux).

### pkg/semantic
Core semantic mapping logic. Converts OpenAPI operations to CLI commands using verb-resource patterns (e.g., GET /users → list users).

### pkg/request
HTTP request building from CLI parameters. Handles parameter validation, path substitution, query strings, and request bodies.

### pkg/cli
CLI command execution and output formatting. Includes error formatting, help generation, and response formatting (table, JSON, YAML).

### pkg/mcp
Model Context Protocol server implementation for AI agent integration.

### pkg/router
Command routing and dispatch logic.

### internal/testutil
Shared testing utilities and helpers. Not exposed as public API.

## Code Conventions

### Package Documentation
Every package has a package-level comment describing its purpose:
```go
// Package semantic provides semantic mapping from OpenAPI operations to CLI commands.
package semantic
```

### Error Handling
- Custom error types for domain-specific errors (e.g., `AppNotFoundError`, `ProfileNotFoundError`)
- Wrapped errors with context using `fmt.Errorf("context: %w", err)`
- User-friendly error formatting in CLI layer

### Testing
- Test files co-located with source: `file.go` → `file_test.go`
- Integration tests in dedicated files: `integration_test.go`
- Property-based tests for core logic validation
- Test utilities in `internal/testutil/`

### Configuration
- Functional options pattern for constructors (e.g., `WithCacheTTL`, `WithHTTPClient`)
- YAML for configuration files
- Platform-specific paths handled in `config` package

### Naming Conventions
- Exported types and functions use PascalCase
- Unexported types and functions use camelCase
- Interfaces named with -er suffix when appropriate (e.g., `Formatter`)
- Test functions: `TestFunctionName`, `TestType_Method`

## Architecture Patterns

### Dependency Injection
Components accept dependencies via constructor parameters rather than creating them internally.

### Caching
Spec parser includes built-in caching with TTL to avoid repeated parsing.

### Validation
Multi-layer validation: config validation, spec validation, parameter validation.

### Extensibility
OpenAPI extensions supported (e.g., `x-cli-verb`, `x-cli-resource`) for customizing CLI behavior.
