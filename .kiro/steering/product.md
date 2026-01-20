# Product Overview

OpenBridge is a universal API runtime engine that bridges OpenAPI specifications to two distinct interfaces:

1. **Semantic CLI**: kubectl-style commands for human users (e.g., `myapp create user --name "John"`)
2. **MCP Server**: Model Context Protocol server for AI agent integration

## Core Concept

"One Spec, Dual Interface" - All behavior is dynamically driven by OpenAPI specifications with zero code generation.

## Key Features

- Dynamic runtime engine that parses OpenAPI specs (2.0, 3.0, 3.1)
- Automatic conversion of API operations to semantic CLI commands
- Multi-profile configuration for different environments (dev, prod, etc.)
- Secure credential management via system keyring
- Fast startup (cold start under 100ms)
- Cross-platform support (Linux, macOS, Windows)

## Installation Model

Users install APIs as CLI applications using `ob install <name> --spec <path>`. Each installed app becomes a standalone CLI command with semantic verb-resource syntax.

## Target Use Cases

- Human developers interacting with APIs via CLI
- AI agents accessing APIs through MCP protocol
- Multi-environment API testing and development
- API exploration and documentation
