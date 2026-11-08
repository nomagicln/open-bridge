# Changelog

This file distinguishes published releases from changes on `main`. The latest published tag is **v0.1.3**; `@latest` currently resolves to that release. The entries below do not announce a new release. See the [upgrade guide](docs/guide/upgrading.md) for consumer-specific migration instructions and the [architecture audit](docs/maintenance/architecture-audit.md) for verification status.

## [Unreleased]

### Go source compatibility since v0.1.3

The following changes are verified against `git diff v0.1.3..6378155 -- pkg/config` (`6378155ed1e7c67733534c35ee257d961e9446a3`):

- Installation/shim and TLS path-validation APIs were removed from `pkg/config`. Their repository implementation lives in `internal/install`, which downstream Go modules cannot import. CLI installation automation can call `ob install`, `ob info`, `ob list --details`, and `ob uninstall`; no public Go installation replacement is provided.
- `ProfileManager` and its convenience APIs were removed. Supported model operations use `AppConfig` and `Manager.SaveAppConfig`; profile mutation requires `GetProfile` → edit → `AddProfile(*profile)` → save. The upgrade guide identifies convenience operations with no equivalent public replacement.
- Historical profile import/export formats and APIs, the app-oriented spec cache manager, and background watcher APIs were removed. Parser cache options cover spec loading, not the removed management/watcher contracts. Consumers that require these APIs must retain an older compatible tag or implement their own explicitly scoped behavior.
- Optional `spec_title` and `spec_version` app YAML fields preserve API metadata. Existing YAML files remain readable; these additions do not restore the deleted profile export formats.

### Runtime corrections in this iteration

These Unreleased contracts are implemented and verified; the check results belong in the [architecture audit](docs/maintenance/architecture-audit.md).

- Standard and progressive MCP modes share an authorized tool registry. Filtering happens before collision checks; remaining duplicate tool names fail with operation diagnostics. Existing unambiguous names are preserved.
- PathItem parameters are merged with operation parameters by `(in, name)`, with operation declarations taking precedence, across CLI, completion, derived indexes, and MCP.
- Validate the actual raw JSON request body, and validate MCP parameters before sending requests. MCP applies the same profile header/query overrides as CLI.
- Derived index schema **4 → 5** rebuilds old indexes automatically for the effective-parameter change. App configuration and credentials do not need deletion.
- Unify `ob <app>`, `ob run <app>`, and shim dispatch. Explicit app flags override explicit root defaults; repeated options within the same scope use the last value. The first `--` ends runtime option parsing. Preserve `--output` priority over `--json`/`--yaml`.
- Propagate request cancellation through MCP, make retry backoff cancellable, and shut down SSE with the server context while preserving existing retry and safety rules.

### Behavior changes that downstream code can observe

- `ob --profile <name> <app> …` and `ob --output <format> <app> …` now reach the app handler. Previously the root command consumed those flags and dropped them, so the app silently used its default profile and output format.
- `ob --mcp <app>` now starts the MCP server. Previously only `ob <app> --mcp` did.
- The MCP server reports the build version of the binary instead of the fixed string `1.0`.
- An MCP tool call with arguments that violate the operation schema now fails with an `Invalid parameters` error before any request is sent. Previously the call was sent unchanged.
- The MCP surface applies the profile's `query_params` in addition to its headers, matching the CLI.
- A cancelled MCP tool call or a cancelled retry backoff stops the request instead of continuing in the background. `SIGINT`/`SIGTERM` now stop the running CLI or MCP server through the same context.
- The SSE transport shuts down when its context ends instead of listening forever. The listen address and the transport protocol are unchanged.
- The progressive-mode error hint names the registered load meta-tool (`Load<App>Tool`) instead of the unscoped `LoadTool`, so an agent is pointed at a tool name that exists.

### Additions to the public Go API

- `cli.Options` and `cli.Handler.ExecuteCommandWithOptions` let a caller supply output format and profile as defaults, with command arguments taking precedence.
- `router.AppOptions`, `router.Router.ExecuteContext`, `router.Router.ExecuteAppContext`, `router.Router.SetServerVersion` and `router.Router.ServerVersion` make the dispatch policy explicit and injectable.
- `app.RunContext` and `app.Deps.RunContext` accept a context and execute the given argv.
- `internal/openapi.EffectiveOperation` exposes the merged path-item/operation parameter view used by every adapter.


### Documentation and engineering

- Add current architecture and upgrade references in English and Chinese, a downstream AI upgrade prompt, corrected command/installation examples, and immutable-release rollback guidance.
- Restore the boundary between production dependencies and test helpers, with reproducible checks documented by the engineering gate changes. Consult the audit for checks actually run.
- Correct the progressive-disclosure meta-tool names in the documentation. The registered names are `Search<App>Tools`, `Load<App>Tool` and `Invoke<App>Tool`, built from the PascalCase app name; the unscoped `SearchTools`/`LoadTool`/`InvokeTool` names were never registered. The registered names are unchanged, and the exported `MetaToolSearchTools`/`MetaToolLoad`/`MetaToolInvoke` constants keep their values for source compatibility.

## [v0.1.3]

Published release. See its [release notes](https://github.com/nomagicln/open-bridge/releases/tag/v0.1.3) and tagged source for that version's behavior. Unreleased changes above are not included when installing this tag.

[v0.1.3]: https://github.com/nomagicln/open-bridge/releases/tag/v0.1.3
[Unreleased]: https://github.com/nomagicln/open-bridge/compare/v0.1.3...main
