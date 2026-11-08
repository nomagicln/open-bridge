# Architecture Reference

OpenBridge interprets an OpenAPI specification at runtime to provide a resource-oriented CLI and MCP tools. Both interfaces share configuration models, credentials, request construction, and HTTP policies. Optional `--generate` request snippets do not change this runtime model.

This reference follows `main`, including the **Unreleased** contracts described below. The latest published tag is **v0.1.3**. Use the [upgrade guide](/guide/upgrading) for version boundaries and the [architecture audit](/maintenance/architecture-audit) for this iteration's evidence, outstanding items, and checks actually run.

## Reading order and authority

For repository changes, read these in order:

1. [AGENTS.md](https://github.com/nomagicln/open-bridge/blob/main/AGENTS.md) and [CONTRIBUTING.md](https://github.com/nomagicln/open-bridge/blob/main/CONTRIBUTING.md) for development rules; `.golangci.yml` remains the lint authority.
2. [README](https://github.com/nomagicln/open-bridge/blob/main/README.md), this reference, and the [CLI reference](/reference/cli) for current boundaries and commands.
3. [Upgrade guide](/guide/upgrading) and [CHANGELOG](https://github.com/nomagicln/open-bridge/blob/main/CHANGELOG.md) for consumer compatibility.
4. Source and tests **at the selected ref** to establish actual behavior; the audit records which claims have been exercised in the current iteration.

The installed OpenAPI document defines API operations, parameter locations, and schemas. Profiles define environment and safety policy. Derived indexes are disposable projections of the specification, never an independent API definition.

Historical material has a different role:

| Material | How to use it |
| --- | --- |
| [Original PRD](https://github.com/nomagicln/open-bridge/blob/main/proposals/01_init.md) and [.kiro specifications](https://github.com/nomagicln/open-bridge/tree/main/.kiro/specs/openbridge) | Product intent and original requirements/tasks. The PRD is not a dependency-injection ADR or proof that every requested feature exists. |
| [Persistent-index proposal](https://github.com/nomagicln/open-bridge/blob/main/proposals/02_persistent_cache.md) | Rationale for caching derived DTOs and retaining conservative fallbacks. Its snapshot timings and `/tmp` scripts are historical evidence, not current acceptance commands. |
| [Config-split proposal](https://github.com/nomagicln/open-bridge/blob/main/proposals/03_config_split.md) | Rationale and historical migration plan. Check current code and the upgrade table for what actually moved or was removed. |
| [Engineering diagnosis](https://github.com/nomagicln/open-bridge/blob/main/ENGINEERING-DIAGNOSIS.md), `FIX*` reports, `.diagnosis/` | Findings about their stated baselines. They do not supersede the current audit or imply that an old defect remains. |

The original table-output and profile import/export goals do not create current features. API responses currently support YAML and JSON; the management command `ob list` has its own tabular display. Historical profile import/export APIs have no equivalent current public replacement.

## Modules and dependency direction

| Module | Responsibility and boundary |
| --- | --- |
| `cmd/ob` | Process entry point and build metadata; delegates composition and execution. |
| `internal/app` | Explicit composition root (`New`, `Deps.Run`) and Cobra management commands. Constructs config, parser caches, credentials, mapper, request builder, handlers, router, installer, and completion provider. |
| `internal/install` | Repository installation use cases, shims/PATH handling, and TLS asset validation. Not importable by downstream Go modules. |
| `pkg/config` | Public app/profile/auth/TLS/safety/retry models, validation, directory resolution, and YAML persistence. It does not own spec parsing or installation. |
| `pkg/spec` | OpenAPI loading/normalization, remote content cache, in-memory parsed cache, and derived index storage. Cache paths/options are supplied by the composition root; it does not need to import `pkg/config` to choose paths. |
| `pkg/semantic` | Maps operations to resource/verb commands, including extension overrides and deterministic conflict handling. URL subresources are flattened into names such as `todos-batch`. |
| `pkg/router` | Selects the installed app, CLI versus MCP mode, and runtime profile/options. |
| `pkg/cli`, `pkg/completion` | App execution/help/output and shell completion. Reuse semantic/spec projections where supported. |
| `pkg/mcp` | Authorized tool registry, schemas and dispatch, standard/progressive exposure, search, confirmation, and rate/safety checks. |
| `pkg/request`, `pkg/credential` | Shared HTTP building/validation/auth/client policies and credential backends. Profiles contain auth configuration; secrets belong in the credential backend. |
| `pkg/codegen`, `pkg/tui` | Optional HTTP request snippets and interactive installation UI. Their presence under `pkg` does not make `internal/install` a supported downstream installation API; TUI/install DTO coupling remains a boundary to consider. |
| `internal/integration`, `internal/proptest` | Cross-package integration and property-test support. Production execution must not depend on testing/assertion/property-test packages. |

`pkg` packages are importable, but pre-1.0 source compatibility must still be checked between refs. `internal` packages are private repository implementation. Keep installation behavior out of the config model and keep CLI/MCP transport code out of request/spec models. Preserve explicit constructor wiring instead of introducing package-level service singletons.

## Request and command flow

Management commands (`install`, `uninstall`, `list`, `info`, `completion`) use `internal/app`. App commands follow this flow:

```text
cmd/ob -> internal/app -> router -> CLI handler or MCP handler
                                      |
                           config/profile + spec/semantic
                                      |
                           request builder + credentials
                                      |
                          profile overrides + HTTP client
```

The Unreleased entrypoint contract unifies `ob <app>`, `ob run <app>`, and a shim. Explicit root flags supply defaults; explicit app flags win. The first `--` ends runtime options, repeated values within one scope use the last value, and `--output` keeps priority over `--json`/`--yaml`. See the [CLI reference](/reference/cli#runtime-options).

Effective operation parameters merge `PathItem.Parameters` and `Operation.Parameters` by `(in, name)`, with operation-level declarations overriding matching path-level ones. CLI help/execution, completion, derived indexes, and both MCP modes must use the same merged information without mutating the shared spec.

Request validation applies to the actual body representation, including raw JSON. CLI and MCP build request parameters, inject credentials, then apply profile `headers`/`query_params`; profile values replace the same keys while preserving unrelated values. HTTP clients apply timeout, TLS, and retry settings; retry remains restricted to supported idempotent methods. Call context reaches HTTP execution and retry waits, and SSE follows server cancellation. These runtime corrections are Unreleased; the audit owns their verification status.

## MCP registry and safety

Standard mode exposes API tools directly. A profile with `safety.progressive_disclosure: true` exposes the three meta-tools `Search<App>Tools`, `Load<App>Tool`, and `Invoke<App>Tool`, where `<App>` is the PascalCase app name; search engine selection and the existing predicate DSL remain separate concerns from tool identity.

The Unreleased contract builds one tool registry **after** read-only/allow/deny filtering. Each published name binds its schema, metadata, and actual method/path to the same allowed operation. Calls resolve that binding, not a second scan of all specification operations. If two allowed operations still map to one name, registry construction fails with method/path/operationId diagnostics. A failed rebuild must not publish a partial catalog or retain stale loaded/search results.

Existing unambiguous tool names remain stable. To resolve a collision, use clean, unique `operationId` values supported by the existing naming rules and rediscover tools; there is no new naming extension. Invocation still enforces confirmation, rate limits, dangerous-operation patterns, and method safety. Changing naming or transport must not bypass these checks.

## Caches and versions

The application composition root opts into `spec-cache` and `index-cache` beneath the config directory. A downstream `spec.Parser` user supplies its own cache options and paths.

| Layer | Lifecycle and validity |
| --- | --- |
| In-memory parsed spec cache | Lives with the parser; TTL and invalidation control reuse within the process. It is not a serialized `openapi3.T` store. |
| `<config-dir>/spec-cache` | Remote response bytes and metadata; revalidated with `ETag`/`Last-Modified` when stale, with cached content available as an offline fallback. Removing it loses that fallback until the source can be fetched again. |
| `<config-dir>/index-cache` | Derived command/help/completion data and an execution projection for supported schemas. Local source identity uses absolute path, size, and modification time; remote identity uses URL and content hash. Parser/semantic/schema fingerprints invalidate incompatible projections. Unsupported execution schemas fall back to full parsing and request construction. |
| `<config-dir>/apps/<app>.yaml` and credential backend | User configuration and secrets, not derived caches. Preserve them across upgrades. |

The derived index is an optimization of spec interpretation, not another HTTP executor. `LoadSpecWithPersistentCache` currently forwards to the normal context-aware loader; persistent remote caching is selected by parser options. Its name does not imply that the entire parsed OpenAPI graph is serialized or that it differs from `LoadSpecWithContext`.

Keep these version concepts distinct:

- OpenBridge release/build version identifies a binary or Go ref.
- App YAML `version` describes configuration; optional `spec_title`/`spec_version` describe the installed API.
- `DerivedIndexSchemaVersion` describes the index DTO's fields **and semantics**. The Unreleased effective-parameter change advances it from **4 to 5**, so old indexes rebuild automatically.
- The on-disk container format and parser/semantic fingerprint are additional independent compatibility checks.

No cache migration requires deleting app YAML or credentials. Rebuild indexes automatically; remove remote content only when its source is recoverable. Do not copy cache directories between unrelated version/config environments as a migration strategy.

## Verification when changing boundaries

Use focused unit/property tests for pure mappings, merge rules, serialization, and validation. Use integration-tagged tests for filesystem, HTTP, process entrypoints, actual MCP dispatch, and service shutdown. Compare cold parsing with warm index behavior, verify saved profile edits after reload, and exercise the real operation bound to each tool.

The repository gates include formatting/lint, `go build ./...`, vet with and without `integration`, short tests, race tests, integration tests, `go mod tidy -diff`, production dependency isolation, minimum-Go checks, and `npm run docs:build`. Follow the current [contribution instructions](https://github.com/nomagicln/open-bridge/blob/main/CONTRIBUTING.md) for exact commands and tool versions. The audit records results and limitations; this page does not claim those gates passed for an unfinished working tree.
