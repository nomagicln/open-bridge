# Technology Stack

## Runtime and dependencies

- Go 1.26 or later for the current checkout; `go.mod` is the authority for the minimum version.
- `kin-openapi` for OpenAPI loading/normalization and validation.
- Cobra for management commands; semantic app commands are derived at runtime.
- The official MCP Go SDK for transports and protocol integration.
- `99designs/keyring` for credential backends and `yaml.v3` for configuration.
- Bubble Tea/Bubbles for the installation UI.
- Gopter and Testify are test dependencies, not production runtime dependencies.

Use the versions in `go.mod`/`go.sum`. Prefer the standard library for new dependencies. `.golangci.yml` is the lint authority; do not edit it to silence findings. Release target/build details belong to `.goreleaser.yml` and CI rather than historical platform lists.

## Build and verification

```bash
go build -o bin/ob ./cmd/ob
go test -v -short ./...
go test -race -short ./...
go test -v -tags=integration ./...
```

Before a commit, follow the current [AGENTS.md](../../AGENTS.md) and [CONTRIBUTING.md](../../CONTRIBUTING.md), including `make fmt-fix && make lint-fix`. Other gates include build/vet, production dependency isolation, minimum-Go validation, `go mod tidy -diff`, and the docs build. Read the current Makefile for target names and lint-tool configuration; engineering changes may update them. Property tests cover parser/formatter/serialization invariants; filesystem/HTTP/process checks use the integration build tag.

Docs are built with `npm run docs:build` using the repository's existing dependencies. No historical diagnosis result implies that the current working tree passes. Record actual commands, failures, environment limits, and representative CLI/MCP checks in the [architecture audit](../../docs/maintenance/architecture-audit.md).

## Configuration and cache lifecycle

`OPENBRIDGE_CONFIG_DIR` overrides the complete config directory. Defaults:

- macOS: `~/Library/Application Support/openbridge/`
- Linux/Unix: `$XDG_CONFIG_HOME/openbridge/`, or `~/.config/openbridge/` when unset
- Windows: `%APPDATA%\openbridge\`

App YAML lives in `apps/<app>.yaml`; secrets belong in the credential backend. `spec-cache` stores remote source content/revalidation metadata, and `index-cache` stores versioned derived projections. Unreleased effective-parameter semantics bump index schema 4 to 5 and trigger automatic rebuild. Do not delete app configuration or credentials to migrate caches, and retain remote cached content if needed for offline use.

The latest published tag is v0.1.3; `main` is Unreleased. Read [architecture](../../docs/reference/architecture.md) and [upgrading](../../docs/guide/upgrading.md) before changing a consumer's Go imports, command flags, tool names, profile editing, or cache assumptions.
