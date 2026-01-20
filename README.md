# OpenBridge

[![CI](https://github.com/nomagicln/open-bridge/actions/workflows/ci.yml/badge.svg)](https://github.com/nomagicln/open-bridge/actions/workflows/ci.yml)
[![Go Report Card](https://goreportcard.com/badge/github.com/nomagicln/open-bridge)](https://goreportcard.com/report/github.com/nomagicln/open-bridge)
[![License: Apache 2.0](https://img.shields.io/badge/License-Apache%202.0-blue.svg)](https://opensource.org/licenses/Apache-2.0)
[![Cat Alliance](https://img.shields.io/badge/Cat%20Alliance-Member-orange?logo=kitty-terminal&logoColor=white)](https://github.com/nomagicln/open-bridge/blob/main/NOTICE)

**OpenBridge** is a universal API runtime engine that bridges the gap between OpenAPI specifications and two distinct interfaces:

1. **For Humans**: A semantic CLI with resource-oriented commands
2. **For AI Agents**: An MCP (Model Context Protocol) server

> "One Spec, Dual Interface."

## Features

- **Zero Code Generation**: All behavior is dynamically driven by OpenAPI specifications
- **Semantic CLI**: Resource-oriented commands (`myapp user create --name "John"`)
- **Shell Auto-Completion**: Tab completion for commands, resources, flags, and enum values
- **MCP Server**: Native AI agent integration via Model Context Protocol
- **Multi-Platform**: Works on Linux, macOS, and Windows
- **Secure Credentials**: Uses system keyring (Keychain, Credential Manager, Secret Service)
- **Fast Startup**: Cold start under 100ms

## Installation

### From Source

```bash
go install github.com/nomagicln/open-bridge/cmd/ob@latest
```

### From Binary

Download the latest release from the [Releases page](https://github.com/nomagicln/open-bridge/releases).

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

```bash
# Start MCP server
myapi --mcp
```

Then configure your AI agent (Claude, etc.) to connect to the MCP server.

## Configuration

Configuration files are stored in platform-specific locations:

- **macOS**: `~/Library/Application Support/openbridge/`
- **Linux**: `~/.config/openbridge/`
- **Windows**: `%APPDATA%\openbridge\`

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
```

Switch profiles with:

```bash
myapi users list --profile prod
```

## Command Reference

### ob Commands

| Command | Description |
|---------|-------------|
| `ob install <name> --spec <path>` | Install an API as a CLI application |
| `ob uninstall <name>` | Remove an installed application |
| `ob list` | List all installed applications |
| `ob run <name> [args...]` | Run commands for an installed application |
| `ob completion [bash\|zsh\|fish]` | Generate shell completion script |
| `ob version` | Show version information |
| `ob help` | Show help |

### App Commands

Once installed, each app provides semantic commands:

| Pattern | Example |
|---------|---------|
| `<app> <resource> list` | `myapi users list` |
| `<app> <resource> get --id <id>` | `myapi user get --id 123` |
| `<app> <resource> create [flags]` | `myapi user create --name "John"` |
| `<app> <resource> update [flags]` | `myapi user update --id 123 --name "Jane"` |
| `<app> <resource> delete --id <id>` | `myapi user delete --id 123` |

### Output Formats

```bash
# Table output (default)
myapi users list

# JSON output
myapi users list --json

# YAML output
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

- Go 1.25 or later
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

```
.
├── cmd/
│   └── ob/              # Main entry point
├── pkg/
│   ├── spec/            # OpenAPI spec parsing
│   ├── config/          # Configuration management
│   ├── credential/      # Keyring integration
│   ├── semantic/        # Verb-resource mapping
│   ├── request/         # HTTP request building
│   ├── cli/             # CLI command handling
│   ├── mcp/             # MCP protocol
│   ├── completion/      # Shell completion support
│   └── router/          # Command routing
├── internal/
│   └── testutil/        # Testing utilities
└── .github/
    └── workflows/       # CI/CD configuration
```

## Contributing

Contributions are welcome! Please read our [Contributing Guide](CONTRIBUTING.md) for details.

## License

Apache License 2.0 - see [LICENSE](LICENSE) and [NOTICE](NOTICE) for details.

---

*OpenBridge is a proud member of the Cat Alliance. 🐱 🐾*
