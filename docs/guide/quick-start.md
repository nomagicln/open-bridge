# Quick Start

Get up and running with OpenBridge in minutes.

> **Note**: For a detailed walkthrough, see our [QUICKSTART.md](https://github.com/nomagicln/open-bridge/blob/main/QUICKSTART.md) in the repository.

## 1. Install an API

Install an API by providing its name and the path or URL to its OpenAPI specification:

```bash
ob install myapi --spec ./openapi.yaml
```

### Interactive Installation Wizard

When you run the install command, `ob` will launch an interactive wizard to configure your application:

1.  **OpenAPI Specification Source**: If not provided via flag, you'll be asked to enter the URL or local path to your OpenAPI spec.
2.  **Base URL**: The wizard will try to auto-detect this from the spec. You can override it to point to different environments (e.g., `http://localhost:8080` vs `https://api.example.com`).
3.  **Authentication**: Select your auth strategy:
    *   `None`: Public APIs.
    *   `Bearer`: Standard Bearer token (JWT).
    *   `API Key`: Key passed in Header or Query param.
    *   `Basic`: Username and Password.
4.  **Security**: Configure TLS settings (like Client Certificates) if needed.
5.  **MCP Options**:
    *   **Progressive Disclosure**: Enable this for large APIs to help AI agents discover tools efficiently. 
    *   **Read-Only Mode**: Restrict the AI to only safe `GET` operations.
    *   **Sensitive Info**: Mask API keys in code generated for the AI context.

## 2. Run Commands

Once installed, use the CLI to interact with the API resources:

```bash
# List resources
myapi users list

# Create a resource
myapi user create --name "John"
```

## 3. Use with AI (MCP Mode)

OpenBridge implements the Model Context Protocol (MCP) to serve as a bridge between your API and AI agents (like Claude).

### Configuration Example (Claude Desktop)

To use your installed API with Claude Desktop, add the following to your configuration file:

*   **macOS**: `~/Library/Application Support/Claude/claude_desktop_config.json`
*   **Windows**: `%APPDATA%\Claude\claude_desktop_config.json`

```json
{
  "mcpServers": {
    "myapi": {
      "command": "/path/to/ob",
      "args": ["run", "myapi", "--mcp"]
      // Or if you created a shim:
      // "command": "myapi",
      // "args": ["--mcp"]
    }
  }
}
```

> **Note**: Replace `/path/to/ob` with the absolute path to your `ob` binary (e.g., `/usr/local/bin/ob` or `/${HOME}/go/bin/ob`).

### Starting the Server Manually

You can also test the server manually in your terminal:

```bash
# Start MCP server
myapi --mcp
```

### Progressive Disclosure

To handle large APIs efficiently, OpenBridge uses a **Progressive Disclosure** strategy. It exposes three meta-tools instead of dumping all endpoints at once:

1. **`SearchTools`**: Find relevant tools using a powerful query language.
2. **`LoadTool`**: Load the full schema for a specific tool.
3. **`InvokeTool`**: Execute the tool.

#### Search Syntax

- **Methods**: `MethodIs("GET")`
- **Paths**: `PathContains("/users")`
- **Tags**: `HasTag("admin")`
- **Operations**: `&&` (AND), `||` (OR), `!` (NOT)

## Configuration

Configuration files are stored in:

- **macOS**: `~/Library/Application Support/openbridge/`
- **Linux**: `~/.config/openbridge/`
- **Windows**: `%APPDATA%\openbridge\`

### Profiles

Manage different environments (dev, prod) using profiles:

```bash
myapi users list --profile prod
```
