# Project Structure

## Directory layout

```text
cmd/ob/                 Process entry point and build metadata
internal/app/           Composition root and management command wiring
internal/install/       Installation, shims, and TLS asset validation
internal/integration/   Cross-package integration tests
internal/proptest/      Property-test support
pkg/config/             Public config models, validation, YAML persistence
pkg/spec/               OpenAPI parsing, remote content cache, derived indexes
pkg/semantic/           Resource/verb inference and mapping
pkg/request/            HTTP construction, validation, auth, client policies
pkg/credential/         Credential backends
pkg/cli/                App execution, help, YAML/JSON response formatting
pkg/mcp/                Tool registry, standard/progressive exposure, safety
pkg/router/             App mode/profile dispatch
pkg/completion/         Shell completion
pkg/codegen/            Optional request snippets
pkg/tui/                Interactive installation UI
docs/                   English/Chinese VitePress documentation
```

## Dependency boundaries

`cmd/ob` delegates to `internal/app.New` and `Deps.Run`. The composition root explicitly constructs config, parser/cache options, credentials, semantic mapper, request builder, CLI/MCP handlers, router, installer, and completion provider.

`pkg/config` owns models and persistence, not spec parsing or installation. `pkg/spec` receives cache paths through options rather than importing config to discover them. CLI/MCP reuse request construction and credentials. Keep transport logic outside the request/spec models.

`pkg` paths are importable Go packages, but compare selected refs for source compatibility. `internal` packages are private repository implementation and cannot be used as replacement APIs by downstream modules. `pkg/tui` remains in its current location; its installation DTO coupling does not make `internal/install` public.

The Unreleased runtime contract routes direct, `run`, and shim entrypoints through the same runtime path and binds MCP calls to the authorized tool registry. The derived index accelerates supported interpretation paths and falls back to full parsing; it is not another HTTP executor.

## Working conventions

Use [AGENTS.md](../../AGENTS.md) and [CONTRIBUTING.md](../../CONTRIBUTING.md) for current testing, naming, and gate rules. Production dependencies must exclude testing/assertion/property-test code. Shared test-only support and integration cases must follow those instructions rather than introducing fictitious production files.

See the [architecture reference](../../docs/reference/architecture.md) for flows/cache versions and the [upgrade guide](../../docs/guide/upgrading.md) for the public API removals since v0.1.3. Historical `.kiro/specs`, proposals, and diagnosis reports describe their own baselines; the [current audit](../../docs/maintenance/architecture-audit.md) owns this iteration's validation status.
