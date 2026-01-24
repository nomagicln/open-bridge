# Introduction

**OpenBridge** is a universal API runtime engine that bridges the gap between OpenAPI specifications and two distinct interfaces:

1. **For Humans**: A semantic CLI with resource-oriented commands
2. **For AI Agents**: An MCP (Model Context Protocol) server

> "One Spec, Dual Interface."

## Why OpenBridge?

Traditional API tools often require generating code or manually crafting CLI wrappers. OpenBridge takes a different approach by interpreting your OpenAPI specifications at runtime. This means:

- **No Code Generation**: You don't need to rebuild your CLI when the API spec changes.
- **Unified Interface**: One tool provides both a human-friendly CLI and a machine-friendly MCP server.
- **Context Aware**: Designed for the era of AI agents, providing tools in a way that LLMs can understand and use effectively.

## Key Features

- **Zero Code Generation**: All behavior is dynamically driven by OpenAPI specifications
- **Semantic CLI**: Resource-oriented commands (`myapp user create --name "John"`)
- **Shell Auto-Completion**: Tab completion for commands, resources, flags, and enum values
- **MCP Server**: Native AI agent integration via Model Context Protocol
- **Multi-Platform**: Works on Linux, macOS, and Windows
- **Secure Credentials**: Uses system keyring (Keychain, Credential Manager, Secret Service)
- **Fast Startup**: Cold start under 100ms
