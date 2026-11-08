# Architecture and contract audit

Status: remediation implemented and verified. Evidence baseline: `6378155ed1e7c67733534c35ee257d961e9446a3` (2026-11-08). The findings below were remediated in the working tree; the ledger at the end of this document records the checks that were actually run.

This is the current audit record. The root `ENGINEERING-DIAGNOSIS.md`, `FIX-PLAN*.md`, `FIXES-BATCH*.md` and `.diagnosis/` are historical investigations, not evidence that today's checks pass. Source references below identify the baseline; line numbers change during remediation.

## Product boundary and archaeological findings

OpenBridge is an OpenAPI runtime: install an API configuration, derive resource-oriented CLI commands or MCP tools, resolve a profile and credentials, and issue HTTP requests. Runtime behavior does not require a generated SDK. Optional generated request snippets are a separate output feature.

The original PRD (`proposals/01_init.md`) establishes “One Spec, Dual Interface.” Its broad direction remains appropriate. Some original requirements were never completed or were superseded: verb-first examples, table output by default, profile import/export commands, and unconditional cold-start promises are not the current contract. They must not be restored merely to make old prose true.

Subsequent Git history established a useful explicit composition root (`03c58e2`), moved installation out of configuration (`1211854`, `4c144fd`), and introduced/versioned a derived index (`6ca6601`, `c312976`, `9902e66`). These changes substantially improved separation and startup cost. They also left duplicated orchestration, incomplete cross-interface tests, and undocumented public API removals.

| Component | Current responsibility | Boundary to retain |
| --- | --- | --- |
| `cmd/ob`, `internal/app` | Binary metadata, dependency composition, management commands | Construct dependencies explicitly; do not reintroduce init globals |
| `pkg/router` | Installed application and MCP dispatch | One dispatch policy for shim, direct app, and run forms |
| `pkg/spec`, `pkg/semantic` | Parse/normalize OpenAPI; derive command meaning and indexes | Semantic mapping remains authoritative; derived data is disposable |
| `pkg/config`, `internal/install` | Persistent domain configuration; installation/shim/TLS assets | Configuration remains a leaf; installation is application orchestration |
| `pkg/request`, `pkg/credential` | Request construction, validation, TLS/retry, credential access | Share behavior across CLI and MCP |
| `pkg/cli`, `pkg/mcp`, `pkg/completion` | User/protocol adapters and discovery | Adapter presentation must preserve the same operation identity and effective parameters |

The derived index is an optimization within CLI resolution, not a second HTTP executor. Its fingerprint, corruption checks, atomic writes, depth bounds and unsupported-schema fallback are deliberate safeguards. Remote HTTP caching and parsed in-process caching have different lifetimes. `LoadSpecWithPersistentCache` forwards to the existing loading path; different wrapper names do not prove different caching behavior.

## Findings, root causes, and remediation

Statuses describe evidence, not estimated effort. “Source-confirmed” means a complete source/SDK path was traced; it does not imply a dynamic reproduction was already run.

| ID | Priority / baseline evidence | Root cause | Remediation and status |
| --- | --- | --- | --- |
| A01 | P1, source-confirmed. `pkg/mcp/tool_naming.go:202`, `handler.go:143,264,282`, `safety.go:120`. GET and DELETE `/users/{userId}/posts` with FastAPI-style operation IDs both become `users_posts`. Registration hides DELETE in read-only mode, but invocation rescans the unfiltered spec and can select DELETE. The SDK's raw AddTool handler does not prevent this. | Visibility and execution independently resolve a non-unique name. | Fixed. The authorized catalog is built once and published as a state transition; tool calls resolve only published IDs through `ToolRegistry.GetOperationInfo`, so a hidden DELETE can no longer be selected by name. `RegisterChecked` reports construction failures instead of serving a partial catalog. Independent confirmation and dangerous-pattern rules still apply. |
| A02 | P2, source-confirmed. `pkg/mcp/registry.go:94,121,129` silently overwrites duplicate IDs and `clearRegistry` retains loaded tools. | Catalog construction is not a validated state transition. | Fixed. `BuildFromSpec` constructs the catalog in a fresh registry and publishes it only on success, in deterministic path order; duplicates inside the authorized set are rejected with a diagnostic naming method, path and operationId instead of overwriting. A failed rebuild clears the loaded state and blocks stale search/load/invoke results. |
| A03 | P2, source-confirmed. CLI `handler.go:142,447`, index `derived_index.go:858`, MCP `registry.go:158` use only operation parameters. | No common effective-parameter boundary for OpenAPI path inheritance. | Fixed. `internal/openapi.EffectiveOperation` merges path-item and operation parameters by `(in, name)` with the operation definition winning, without mutating the parsed specification; the CLI, the derived index and both MCP modes use it. `DerivedIndexSchemaVersion` was raised to 5, so older index caches are rebuilt. |
| A04 | P2, source-confirmed. `pkg/request/builder.go:593,619` accepts raw body presence but checks separate flags for required body fields. | Validation and serialization inspect different representations. | Fixed. `ValidateParams` now inspects the representation that is actually sent: an explicit raw JSON body is parsed and checked against the schema, a field-based body is checked field by field, and the resolved bytes are frozen for the build. Construction precedence (explicit body over separate flags) is unchanged and covered by a property test. |
| A05 | P2, source-confirmed. CLI `handler.go:159` applies profile query parameters; both MCP pipelines omit them and omit existing `ValidateParams`. | Shared request primitives are wrapped by inconsistent policy code. | Fixed. `internal/requestpolicy.ApplyProfile` is the single definition of the profile override semantics and is used by the CLI and by both MCP modes. Both MCP modes execute through one shared `executeOperation` path that validates the arguments with the existing `request.Builder.ValidateParams` before sending. `internal/integration/equivalence_integration_test.go` compares the recorded outgoing requests of CLI, standard MCP and progressive MCP for one fixture. |
| A06 | P2, source-confirmed. `internal/app/app.go:114,164,181` ignores supplied argv for Cobra and drops root application flags. `commands_run.go` and `pkg/router/router.go` parse/start MCP independently, with different duplicate-profile and version behavior. | Multiple live application composition paths. | Fixed. Every entry form (shim, `ob <app>`, `ob run <app>`) now executes through `Router.ExecuteAppContext`; the duplicated MCP startup in `internal/app/commands_run.go` was removed. `Deps.RunContext` hands `argv[1:]` to Cobra and runs it with the caller's context. Explicitly set root flags are forwarded as defaults, an app-level flag wins over them, and repeated options use the last value. `Router.SetServerVersion` injects the build version. The four-argument `NewRouter` and the previous `Execute`/`ExecuteApp`/`Deps.Run` signatures remain available. |
| A07 | P2, source-confirmed. MCP ignores invocation context; `pkg/request/client.go:214,223` sleeps unconditionally; SSE ignores its context. | Cancellation stops at adapter boundaries. | Fixed. The MCP invocation context is bound to the outgoing HTTP request, so a cancelled tool call stops the request. The retry backoff waits on a cancellable timer and returns the context error instead of sleeping through the delay. The SSE transport runs on an explicit `http.Server` with a header timeout and shuts down gracefully when the context ends; the bind address and the transport protocol are unchanged. `cmd/ob` turns SIGINT/SIGTERM into a cancelled context. |
| A08 | P2, dynamically confirmed by `go list -deps ./cmd/ob`: testing and testify/assert/require are production dependencies. Private `test_helpers.go` import test libraries. | Agent instructions prescribe a production filename for test helpers. | Fixed. Package-local helpers moved to `test_helpers_test.go`; shared test-only packages keep ordinary files, and AGENTS.md states that exception. `make production-deps` rejects `testing`, `testify` and `gopter` in the production import graph and runs in CI. Test dependencies remain in the module graph for tests. |
| A09 | P2, Git-confirmed. Since `v0.1.3`, public config installation/profile/export/cache/watcher APIs were removed or internalized without a changelog/migration contract. | In-repository call counts were treated as sufficient evidence to remove importable APIs. | Fixed. `CHANGELOG.md` records the released history and the Unreleased corrections; `docs/guide/upgrading.md` (and its Chinese counterpart) lists the migration families per consumption form, including the operations without a replacement; the README carries the downstream AI adaptation recipe. No further public removals were made in this iteration. |
| A10 | P2 engineering gap. Declared Go 1.26 is not exercised by the Go 1.27 CI matrix. Local lint binaries cannot process the required config/toolchain. | Local and CI tool contracts are insufficiently explicit. | Fixed. CI keeps the cross-platform matrix and adds a Linux job on the declared minimum Go version; `make lint` selects an explicit `GOLANGCI_LINT` executable, checks the pinned version and its Go compatibility, and `make install-lint` installs it into a separate directory. `.golangci.yml` was not weakened. |
| A11 | P2, dynamically confirmed by `pkg/mcp/progressive_test.go`. The progressive handler registers `Search<App>Tools`, `Load<App>Tool` and `Invoke<App>Tool`, while the documentation and the exported `MetaToolSearchTools`/`MetaToolLoad`/`MetaToolInvoke` constants use the unscoped `SearchTools`/`LoadTool`/`InvokeTool`. An agent that follows the documentation calls a tool name that does not exist. | The tool identity was scoped by the app name without the names and the documentation being corrected together. | Fixed in the documentation; the registered names are unchanged. Renaming them would break every recorded MCP client, so the app scope stays. The constants keep their values for source compatibility and are now documented as legacy aliases. The README, CLI reference, quick start, architecture reference and the Chinese counterparts state the PascalCase rule and point at the MCP `tools/list` response; `TestProgressiveHandler_RegistersAppScopedMetaToolNames` locks the names a client observes and was mutation-checked. |

## Assessed items that do not justify an immediate rewrite

- Public `pkg/tui/install` exposes an internal install options type. It is a real public-boundary limitation; moving the public package or deleting it would add another breaking change. A public DTO migration needs a separate compatibility design.
- `CachedSpec.AccessedAt` was written on concurrent cache hits without synchronization. This is now fixed inside the cache: `Parser` guards the access-time update with a mutex, so the public `CachedSpec` type and the cache architecture are unchanged. `TestGetCachedSpecConcurrentAccess` reproduces the race under `-race` before the fix and passes afterwards.
- Name-keyed request maps cannot fully express all path/query/body namespace combinations. Repeated flags and older parsing contracts need precise regression requirements; blindly connecting every parser is not a safe simplification.
- Public helpers with no repository callers are not proven dead: downstream imports are unknown. Keep them unless removal is explicitly designed and versioned.
- The exported `MetaToolSearchTools`, `MetaToolLoad` and `MetaToolInvoke` constants cannot express the app-scoped registered names, and no production code uses them. Their values stay unchanged so downstream Go code keeps compiling; they are documented as legacy aliases. Replacing the registered names would break recorded MCP clients, so that change needs its own compatibility design.
- Unsupported derived-index schemas already fall back. Expanding schema coverage, replacing predicate parsing/test frameworks, mass-moving historical documents, and deleting ignored local artifacts have no demonstrated priority over the correctness fixes.
- SSE currently binds on all interfaces without adding authentication. This audit does not silently change deployment policy; lifecycle improvements retain the existing bind behavior. Deployment boundaries belong in the transport documentation.

## Compatibility and design decisions

1. Preserve public constructor/method signatures and non-ambiguous tool names. Add checked/context adapters where needed; keep old adapters usable.
2. Filter operations before detecting duplicate published IDs. A single authorized GET can keep its old name under read-only mode; multiple authorized candidates must fail rather than pick a map-order winner.
3. Merge inherited parameters by location plus name. Operation definitions override the same pair. Do not modify shared parsed specifications.
4. App-level explicit flags override explicit root defaults; configuration is the final fallback. Same-scope repeated values use the last value. The first `--` ends runtime option parsing; application `--output` retains priority over `--json/--yaml`.
5. Preserve CLI's existing profile header/query override order and use it consistently in MCP. Keep idempotent-only retries, credential handling, confirmation and dangerous-operation controls.
6. Derived caches are disposable; configuration and credentials are not. Index semantic changes cause automatic index rebuilds, not deletion of user state.
7. New product functionality retains the AGENTS interface → red tests → human confirmation → implementation checkpoint. Existing defects receive failing regressions before fixes.

## Validation ledger

Baseline checks obtained during read-only investigation:

| Check | Baseline result | Result of this iteration |
| --- | --- | --- |
| `go build ./...` | Passed | Passed |
| `go vet ./...`, `go vet -tags=integration ./...` | Passed | Passed |
| `go test -short ./...` | Passed | Passed (1544 tests, 18 packages) |
| `go test -tags=integration ./...` | Passed | Passed (1648 tests, 19 packages) |
| `go test -race -short ./...` | Not run | Passed (1544 tests). The run covers the new concurrent cache test, which reproduced the `CachedSpec.AccessedAt` race before the fix. |
| `go mod tidy -diff` | No differences | No differences |
| `make production-deps` | Not available | Passed: the production import graph contains no test packages |
| Full lint (`golangci-lint` v2.15.0) | Not verified: available binaries incompatible with the v2 config / target Go version | Passed with 0 issues, using the pinned version built with the current Go toolchain (`make install-lint`, then `make lint GOLANGCI_LINT=<path>`) |
| `actionlint` (CI workflow) | Not verified | Passed (exit 0) on the current workflow file. The workflow was not modified afterwards; a repeat run in the final sweep was not possible because the binary is not installed locally and downloading it was denied. |
| `make test-coverage` | Not run | Passed; coverage report generated |
| Minimum-Go version job | Not run locally | CI runs it on Linux; not executed in this environment |
| Performance regression comparison | Not run | Not run: no benchmark was measured before and after the change, so no performance claim is made |

Failing regressions observed before the corresponding fixes: MCP profile query parameters and header policy, MCP argument validation, the MCP cancelled-context call, the retry backoff cancellation, the SSE shutdown on context cancellation, the root profile/output forwarding, the empty-argv handling, and the concurrent spec cache access. Each has a standing regression test in the repository. The progressive meta-tool naming test was mutation-checked: breaking the PascalCase conversion makes it fail.

Remaining limitations are listed in the section above and in `CHANGELOG.md`. Historical reports that depend on `/tmp/lead_verify*.py` are not reproducible regression gates. New fixtures and checks belong in the repository.

## Reading and maintenance

Use the current architecture reference and upgrade guide for current contracts. Preserve original proposals as evidence of intent and older diagnosis reports as historical snapshots. For each significant future change, update the affected interface contract, its regression tests, migration notes and this ledger's status together.
