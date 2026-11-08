# Product Overview

OpenBridge interprets OpenAPI specifications at runtime to provide two interfaces:

1. A semantic CLI: `<app> <resource> <verb>`, for example `myapp users create --name "John"`.
2. An MCP server for API discovery and invocation by AI clients.

"One Spec, Dual Interface" describes the runtime model. Generated SDKs are not required; optional `--generate` request snippets support `curl`, `nodejs`, `go`, and `python`.

## Current product boundaries

- Install an app with `ob install <name> --spec <path-or-url>`. Interactive mode requires `-i`; it is off by default.
- Invoke the app with `ob <app> ...`, `ob run <app> ...`, or its installed shim. Resources are derived from the spec and may be flattened names such as `todos-batch`.
- Profiles select environments, auth configuration, headers/query values, TLS, timeouts, retries, and MCP safety. Secrets belong in the credential backend.
- API responses support YAML (default) and JSON. `ob list` has its own management table; general response table formatting is not a current feature.
- Standard MCP exposes API tools; `safety.progressive_disclosure: true` selects the meta-tools `Search<App>Tools`, `Load<App>Tool`, and `Invoke<App>Tool`, built from the PascalCase app name. Preserve read-only, allow/deny, confirmation, rate-limit, and dangerous-operation controls.
- Remote content and derived indexes improve repeated use; unsupported indexed schemas retain the full-parser fallback. Performance depends on the spec and environment, so historical timing figures are not guarantees.

## Version and reading contract

The latest published tag is v0.1.3; `@latest` currently selects it. `main` contains Unreleased behavior and public API removals. For downstream upgrades, identify old/new refs and whether the consumer uses CLI, MCP, config, or Go imports. Never substitute an `internal` import for a removed public API.

Read [current architecture](../../docs/reference/architecture.md), [CLI reference](../../docs/reference/cli.md), [upgrading](../../docs/guide/upgrading.md), and [CHANGELOG](../../CHANGELOG.md). The [architecture audit](../../docs/maintenance/architecture-audit.md) records this iteration's evidence and verification status. The original PRD and `.kiro/specs/openbridge` preserve historical intent; missing profile import/export and table-output goals are not implicit implementation requirements.
