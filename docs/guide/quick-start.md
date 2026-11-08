# Quick Start

This guide follows `main` (**Unreleased**). The latest published tag is **v0.1.3**, currently selected by `@latest`. For an existing installation, start with [upgrading](/guide/upgrading). A local Todo walkthrough is available in [QUICKSTART.md](https://github.com/nomagicln/open-bridge/blob/main/QUICKSTART.md).

## 1. Install an API

Provide the app name and a local path or remote URL:

```bash
ob install myapi --spec ./openapi.yaml
# Override the server URL when needed:
ob install myapi --spec ./openapi.yaml --base-url http://localhost:8080
```

These are alternative installation commands. To replace existing configuration, `--force` is required; use it only when the overwrite is intended.

Interactive installation is **off by default**. Without `-i`, `--spec` is required. To use the wizard instead:

```bash
ob install myapi -i
```

The wizard configures the specification source, base URL, authentication, TLS, MCP safety/progressive disclosure, and sensitive-information protection for generated snippets. Keep credentials in the credential backend and preserve TLS/safety controls when adapting an existing profile. See all [installation flags](/reference/cli#installation-options), including `--auth`, `--description`, and `--shim`.

## 2. Explore and run commands

Use app help to discover resources, verbs, and parameter names from the installed spec:

```bash
ob run myapi --help
ob myapi --help
```

For a spec exposing user operations, commands might be:

```bash
ob myapi users list
ob run myapi users create --name "John"
```

If installation created a shim, add its reported directory to `PATH`, then use `myapi users list`. Nested URL paths become flattened resources; for the repository Todo service, `/todos/batch` maps to `todos-batch` and `/todos/stats` to `todos-stats`.

Select a profile or output format, or generate a snippet without sending the API request:

```bash
ob myapi users list --profile prod --output json
ob myapi users list --generate curl
ob myapi users list --generate python -O request.py
```

Runtime options must appear before the first `--`. After it, request arguments use explicit prefixes such as `--path:user_id=123` or `--query:page=1`. See the [CLI reference](/reference/cli#runtime-options) for precedence and the supported generation formats.

## 3. Connect an MCP client

Start the selected app/profile over stdio:

```bash
ob run myapi --mcp --transport stdio --profile prod
```

For an MCP client that accepts `mcpServers` configuration, use an absolute binary path:

```json
{
  "mcpServers": {
    "myapi": {
      "command": "/absolute/path/to/ob",
      "args": ["run", "myapi", "--mcp", "--transport", "stdio", "--profile", "prod"]
    }
  }
}
```

The profile must already exist. A shim can also start MCP with `myapi --mcp`. SSE is supported with `ob myapi --mcp --transport sse --port 8080`; stdio is the default transport.

Standard mode exposes API tools directly. To enable progressive disclosure, set the selected profile's configuration:

```yaml
safety:
  progressive_disclosure: true
  search_engine: predicate
```

Progressive mode exposes three meta-tools named `Search<App>Tools`, `Load<App>Tool`, and `Invoke<App>Tool`, where `<App>` is the PascalCase app name — for example `SearchPetstoreTools` for the app `petstore`. Read the exact names from the MCP `tools/list` response. The predicate engine supports expressions such as `MethodIs("GET") && PathContains("/users")`, `HasTag("admin")`, and logical `&&`, `||`, `!`. Read-only, allow/deny lists, confirmations, rate limits, and dangerous-operation patterns apply independently; see [MCP safety](/reference/cli#mcp-transports-and-safety).

## Configuration and next steps

`OPENBRIDGE_CONFIG_DIR` overrides the full configuration directory. Defaults are:

- macOS: `~/Library/Application Support/openbridge/`
- Linux/Unix: `$XDG_CONFIG_HOME/openbridge/`, or `~/.config/openbridge/` when unset
- Windows: `%APPDATA%\openbridge\`

Inspect installed apps with `ob list --details` and `ob info myapi -o yaml`. App files live in `apps/<app>.yaml`; credentials use their configured backend. Remote `spec-cache` provides revalidation/offline fallback, and versioned `index-cache` data rebuilds automatically. Preserve app configuration and credentials during upgrades.

Continue with the [CLI reference](/reference/cli), [architecture reference](/reference/architecture), and [upgrade guide](/guide/upgrading). Unreleased implementation and verification status is recorded in the [architecture audit](/maintenance/architecture-audit).
