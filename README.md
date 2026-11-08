# OpenBridge

![OpenBridge Logo](docs/public/logo.jpeg)

[![CI](https://github.com/nomagicln/open-bridge/actions/workflows/ci.yml/badge.svg)](https://github.com/nomagicln/open-bridge/actions/workflows/ci.yml)
[![codecov](https://codecov.io/gh/nomagicln/open-bridge/graph/badge.svg)](https://codecov.io/gh/nomagicln/open-bridge)
[![Go Report Card](https://goreportcard.com/badge/github.com/nomagicln/open-bridge)](https://goreportcard.com/report/github.com/nomagicln/open-bridge)
[![License: Apache 2.0](https://img.shields.io/badge/License-Apache%202.0-blue.svg)](https://opensource.org/licenses/Apache-2.0)
[![Cat Alliance](https://catalliance.org/badge.svg)](https://catalliance.org)
[![Ask DeepWiki](https://deepwiki.com/badge.svg)](https://deepwiki.com/nomagicln/open-bridge)

**OpenBridge** is a universal API runtime engine that bridges the gap between OpenAPI specifications and two distinct interfaces:

1. **For Humans**: A semantic CLI with resource-oriented commands
2. **For AI Agents**: An MCP (Model Context Protocol) server

> "One Spec, Dual Interface."

## Features

- **No SDK Code Generation**: CLI and MCP behavior is driven dynamically by OpenAPI specifications — no generated clients to maintain; optional client snippets are available via `--generate`
- **Semantic CLI**: Resource-oriented commands (`myapp user create --name "John"`)
- **Shell Auto-Completion**: Tab completion for commands, resources, flags, and enum values
- **MCP Server**: Native AI agent integration via Model Context Protocol
- **Multi-Platform**: Works on Linux, macOS, and Windows
- **Secure Credentials**: Uses system keyring (Keychain, Credential Manager, Secret Service); on headless systems, set `OPENBRIDGE_KEYRING_PASSWORD` to use an encrypted file backend
- **Resilient Requests**: Per-profile timeout, retry policy and TLS options (custom CA, client certificates, insecure-skip-verify, minimum version)
- **Offline-Friendly Remote Specs**: remote OpenAPI specs are cached locally and revalidated with `ETag` / `Last-Modified`
- **Cached Help & Completion**: app help and shell completion reuse a versioned derived index
- **Conservative Execution Fallbacks**: supported schemas can reuse indexed command data; other operations use the full parser, with latency depending on spec size and environment

## Installation

### From Source

```bash
go install github.com/nomagicln/open-bridge/cmd/ob@latest
```

### From Binary

Download the latest release from the [Releases page](https://github.com/nomagicln/open-bridge/releases).

`@latest` currently resolves to **v0.1.3**. This README follows `main`, whose changes are **Unreleased**; installing a release does not install those changes. Pin a release or a specific commit when reproducibility matters:

```bash
go install github.com/nomagicln/open-bridge/cmd/ob@v0.1.3
```

## Upgrading with an AI Agent

Start with the [upgrade guide](docs/guide/upgrading.md) and [CHANGELOG](CHANGELOG.md) before changing an existing integration. The upgrade guide covers CLI scripts, MCP clients, YAML configuration, and Go imports separately, including public Go APIs removed after v0.1.3.

For work in this repository, read [AGENTS.md](AGENTS.md), [CONTRIBUTING.md](CONTRIBUTING.md), the [architecture reference](docs/reference/architecture.md), and the [CLI reference](docs/reference/cli.md). The [architecture audit](docs/maintenance/architecture-audit.md) records the current iteration's evidence and verification status. Historical proposals and diagnosis reports explain past decisions; they do not override current source and tests.

Copy this prompt into your downstream project's agent session and replace the placeholders:

```text
Upgrade this project's OpenBridge integration from <OLD_TAG_OR_COMMIT> to
<NEW_TAG_OR_COMMIT>. Resolve both refs first; do not substitute main or @latest
without recording the resolved commit. Identify every consumption mode we use:
CLI/subprocess, MCP, configuration files, and/or Go package imports.

Read OpenBridge's README, CHANGELOG, docs/guide/upgrading.md, and
docs/reference/architecture.md at the target ref. Inspect the actual upstream
git diff <OLD_REF>..<NEW_REF> for the packages and commands we consume, then
find and read our local imports, command invocations, MCP tool names, config
readers/writers, and tests. Base each edit on those callsites and the diff.
Do not assume that a removed symbol has a one-line replacement.

Use supported public pkg APIs or the ob CLI. Never import OpenBridge internal
packages. If a required API has no public replacement, report the blocker and
retain/pin the older compatible version rather than inventing a replacement.
Preserve credentials, config files, profile selection, TLS verification,
read-only/allow/deny rules, confirmations, and rate limits. Do not print secrets,
delete configuration/credentials, or relax safety settings to make tests pass.
Treat derived caches as disposable only when their source can be recovered;
allow versioned indexes to rebuild automatically.

Make the smallest necessary local changes. Run this project's own build and
tests plus representative CLI/MCP/configuration calls in an isolated test
configuration. For profile edits, reload the saved app and verify the value.
Report resolved old/new refs, changed callsites, compatibility decisions,
commands and results, and every blocker or unverified path. Do not claim an
upgrade is complete when a required consumer remains incompatible.
```

## Quick Start

### 1. Install an API

```bash
ob install myapi --spec ./openapi.yaml
```

### 2. Run Commands

```bash
# List resources
myapi users list

# Create a resource
myapi user create --name "John"
```

> **Note**: For a detailed step-by-step guide, see [QUICKSTART.md](./QUICKSTART.md).

### 3. Use with AI (MCP Mode)

OpenBridge implements the Model Context Protocol (MCP) to serve as a bridge between your API and AI agents (like Claude).

#### Progressive Disclosure

Enable `safety.progressive_disclosure: true` in the selected profile to use **Progressive Disclosure** for large APIs. This mode exposes three meta-tools. Their names are scoped by the PascalCase app name, so an agent must read them from the MCP `tools/list` response:

1. **`Search<App>Tools`** (for example `SearchPetstoreTools`): Find relevant tools using a powerful query language.
2. **`Load<App>Tool`** (for example `LoadPetstoreTool`): Load the full schema for a specific tool.
3. **`Invoke<App>Tool`** (for example `InvokePetstoreTool`): Execute the tool.

The app name is converted to PascalCase, so `my-api-v2` becomes `SearchMyApiV2Tools`, `LoadMyApiV2Tool`, and `InvokeMyApiV2Tool`.

#### Search Syntax

With `safety.search_engine: predicate`, the search meta-tool supports a predicate-based query language for precise filtering:

- **Methods**: `MethodIs("GET")`, `MethodIs("POST")`
- **Paths**: `PathContains("/users")`, `PathStartsWith("/api/v1")`
- **Tags**: `HasTag("admin")`
- **Logical Operators**: `&&` (AND), `||` (OR), `!` (NOT)

**Examples:**

- Find read-only user operations:

  ```predicate
  MethodIs("GET") && PathContains("/users")
  ```

- Find operations tagged with 'pet' or 'store':

  ```predicate
  HasTag("pet") || HasTag("store")
  ```

#### Starting the Server

```bash
# Start MCP server
myapi --mcp
```

Then configure your AI agent to connect to this server (stdio transport). SSE is also supported with `ob myapi --mcp --transport sse --port 8080`. See the [CLI reference](docs/reference/cli.md) for installation flags, snippet generation (`curl`, `nodejs`, `go`, `python`), and Unreleased runtime option precedence.

## Configuration

Configuration files are stored in platform-specific locations:

- **macOS**: `~/Library/Application Support/openbridge/`
- **Linux/Unix**: `$XDG_CONFIG_HOME/openbridge/`, or `~/.config/openbridge/` when unset
- **Windows**: `%APPDATA%\openbridge\`

Set `OPENBRIDGE_CONFIG_DIR` to override the complete configuration directory on any platform.

### Profiles

Each app can have multiple profiles for different environments:

```yaml
name: myapi
spec_source: ./openapi.yaml
default_profile: dev
profiles:
  dev:
    base_url: http://localhost:8080
    auth:
      type: bearer
  prod:
    base_url: https://api.example.com
    auth:
      type: api_key
      location: header
      key_name: X-API-Key
    timeout: 30s
    tls:
      ca_file: /etc/ssl/private-ca.pem
      min_version: "1.2"
    retry:
      max_retries: 3
      initial_delay: 200ms
      max_delay: 2s
      retryable_status_codes: [429, 503]
```

Derived help/completion indexes and remote spec copies are cached under the config directory. Profile-level `timeout` / `tls` / `retry` settings are applied to CLI requests. MCP safety controls
(`read_only_mode`, `require_confirm`, `max_requests_per_minute`, `dangerous_operation_patterns`, …)
are documented in the CLI reference.

Switch profiles with:

```bash
myapi users list --profile prod
```

## Command Reference

### ob Commands

| Command | Description |
| --- | --- |
| `ob install <name> --spec <path>` | Install an API as a CLI application |
| `ob uninstall <name>` | Remove an installed application |
| `ob list [--details]` | List installed applications, optionally with details |
| `ob info <name> [-o text\|json\|yaml]` | Inspect an installed application's configuration |
| `ob run <name> [args...]` | Run commands for an installed application |
| `ob <name> [args...]` | Run an installed application directly |
| `ob completion [bash\|zsh\|fish]` | Generate shell completion script |
| `ob --version` | Show version information |
| `ob help` | Show help |

### App Commands

Once installed, each app provides semantic commands:

| Pattern | Example |
| --- | --- |
| `<app> <resource> list` | `myapi users list` |
| `<app> <resource> get --id <id>` | `myapi user get --id 123` |
| `<app> <resource> create [flags]` | `myapi user create --name "John"` |
| `<app> <resource> update [flags]` | `myapi user update --id 123 --name "Jane"` |
| `<app> <resource> delete --id <id>` | `myapi user delete --id 123` |

### Output Formats

```bash
# YAML output (default)
myapi users list

# JSON output
myapi users list --json

# Explicit format selection
myapi users list --output json
myapi users list --yaml
```

## Shell Auto-Completion

OpenBridge supports shell auto-completion for bash, zsh, and fish shells. Completion provides suggestions for:

- App names
- Verbs (list, get, create, update, delete, etc.)
- Resources (users, servers, etc.)
- Flag names (based on operation parameters)
- Flag values (for enum parameters like status, output format, profiles)

### Installing Completion

**Bash:**

```bash
# Load in current session
source <(ob completion bash)

# Install permanently (Linux)
ob completion bash | sudo tee /etc/bash_completion.d/ob

# Install permanently (macOS with Homebrew)
ob completion bash > /usr/local/etc/bash_completion.d/ob
```

**Zsh:**

```bash
# Load in current session
source <(ob completion zsh)

# Install permanently
ob completion zsh > "${fpath[1]}/_ob"
```

**Fish:**

```bash
# Load in current session
ob completion fish | source

# Install permanently
ob completion fish > ~/.config/fish/completions/ob.fish
```

### Using Completion

Once installed, press Tab to get completion suggestions:

```bash
# Complete app names
ob run <TAB>

# Complete resources for an app
ob run myapi <TAB>

# Complete verbs
ob run myapi users <TAB>

# Complete flags
ob run myapi user get --<TAB>

# Complete flag values (for enums)
ob run myapi users list --output <TAB>
```

## OpenAPI Extensions

You can customize CLI behavior using OpenAPI extensions:

```yaml
paths:
  /server/reboot:
    post:
      x-cli-verb: trigger      # Override default verb mapping
      x-cli-resource: server   # Override resource name
```

## Development

### Prerequisites

- Go 1.26 or later
- Make (optional)

### Building

```bash
# Build
go build -o ob ./cmd/ob

# Run tests
go test -v ./...

# Run with race detector
go test -race ./...
```

### Project Structure

```text
.
├── cmd/ob/              # Process entry point and build metadata
├── internal/
│   ├── app/             # Composition root and management commands
│   ├── install/         # Installation, shims, and TLS asset validation
│   ├── integration/     # Cross-package integration tests
│   └── proptest/        # Shared property-test support
├── pkg/
│   ├── spec/            # OpenAPI parsing, remote cache, derived indexes
│   ├── config/          # Public config models and persistence
│   ├── credential/      # Credential backends
│   ├── semantic/        # Resource and verb mapping
│   ├── request/         # Shared HTTP construction and client policies
│   ├── cli/             # App commands, help, response formatting
│   ├── mcp/             # Standard and progressive MCP tools
│   ├── completion/      # Shell completion
│   ├── router/          # App mode and profile dispatch
│   ├── codegen/         # Optional request snippets
│   └── tui/             # Interactive installation UI
├── docs/                # English and Chinese VitePress guides
└── .github/workflows/   # CI/CD configuration
```

`pkg` paths are importable Go packages; their compatibility still depends on the selected ref. `internal` packages are repository implementation details. See the [architecture reference](docs/reference/architecture.md) for boundaries and cache lifecycle.

## Contributing

Contributions are welcome! Please read our [Contributing Guide](CONTRIBUTING.md) and [Code of Conduct](CODE_OF_CONDUCT.md) for details.

## License

Apache License 2.0 - see [LICENSE](LICENSE) and [NOTICE](NOTICE) for details.

---

## OpenBridge is a proud member of the Cat Alliance. 🐱 🐾