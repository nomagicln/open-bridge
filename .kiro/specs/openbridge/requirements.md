# Requirements Document: OpenBridge

## Introduction

OpenBridge is a universal API runtime engine that reads OpenAPI specification files and dynamically generates two interfaces: a kubectl-style semantic CLI for humans and an MCP Server for AI agents. The system eliminates the need for code generation by driving all behavior through OpenAPI specs and configuration files, providing a unified "One Spec, Dual Interface" approach to API consumption.

## Glossary

- **OpenBridge**: The universal API runtime engine system (command: `ob`)
- **App**: A configured instance of OpenBridge bound to a specific OpenAPI specification
- **Shim**: A lightweight wrapper script or symlink that provides system-level command entry points
- **Semantic_CLI**: The command-line interface that maps REST operations to human-friendly verbs
- **MCP_Server**: The Model Context Protocol server implementation for AI agent integration
- **Profile**: An environment configuration (e.g., dev, prod) for an App
- **Resource**: A noun extracted from API paths representing an entity (e.g., users, servers)
- **Verb**: A semantic action word (e.g., create, list, delete) mapped from HTTP methods
- **OpenAPI_Spec**: The OpenAPI 2.0 (Swagger), 3.0, or 3.1 specification file defining the API
- **Keyring**: System-level secure credential storage (macOS Keychain, Windows Credential Manager)
- **Tool**: An MCP protocol entity representing a callable API operation with input schema and description

## Requirements

### Requirement 1: OpenAPI Specification Loading

**User Story:** As a developer, I want to load OpenAPI specifications from local files or remote URLs, so that I can configure OpenBridge to work with any API.

#### Acceptance Criteria

1. WHEN a user provides a local file path to an OpenAPI specification, THE OpenBridge SHALL load and parse the specification from the local filesystem
2. WHEN a user provides a remote URL to an OpenAPI specification, THE OpenBridge SHALL fetch and parse the specification from the URL
3. WHEN an OpenAPI specification is loaded, THE OpenBridge SHALL validate it against OpenAPI 2.0 (Swagger), 3.0, and 3.1 standards
4. IF an OpenAPI specification is invalid or malformed, THEN THE OpenBridge SHALL return a descriptive error message indicating the validation failure
5. WHEN an OpenAPI specification is successfully loaded, THE OpenBridge SHALL extract all path definitions, operations, and schemas for runtime use
6. WHEN an OpenAPI 2.0 specification is loaded, THE OpenBridge SHALL internally normalize it to OpenAPI 3.x structure for consistent processing

### Requirement 2: Application Installation and System Integration

**User Story:** As a user, I want to install an App as a system-level command, so that I can invoke it directly without prefixing with `ob run`.

#### Acceptance Criteria

1. WHEN a user executes `ob install <app-name> --spec <path>`, THE OpenBridge SHALL create a configuration entry for the App
2. WHEN an App is installed, THE OpenBridge SHALL create a Shim in the user's PATH that invokes the App
3. WHEN a user types the App name in the terminal, THE System SHALL execute the App through the Shim
4. WHEN an App is installed, THE OpenBridge SHALL persist the configuration to allow subsequent invocations
5. WHEN a user attempts to install an App with a name that already exists, THE OpenBridge SHALL prompt for confirmation before overwriting
6. WHEN a user installs an App, THE OpenBridge SHALL prompt for initialization configuration including base URL, authentication method, custom headers, and TLS certificates
7. WHEN a user provides authentication credentials during installation, THE OpenBridge SHALL store them securely in the system Keyring
8. WHEN a user provides custom headers during installation, THE OpenBridge SHALL persist them in the App configuration
9. WHEN a user provides TLS certificates during installation, THE OpenBridge SHALL store certificate paths and validate them for HTTPS connections

### Requirement 3: Multi-Environment Profile Support

**User Story:** As a developer, I want to configure multiple environment profiles for an App, so that I can switch between dev, staging, and production environments.

#### Acceptance Criteria

1. WHEN a user configures an App, THE OpenBridge SHALL support defining multiple Profile entries
2. WHEN a user invokes an App with `--profile <name>`, THE OpenBridge SHALL use the configuration from the specified Profile
3. WHEN no profile is specified, THE OpenBridge SHALL use a default Profile
4. WHEN a Profile is selected, THE OpenBridge SHALL apply the Profile's base URL, authentication, and other environment-specific settings
5. WHEN a user lists available profiles, THE OpenBridge SHALL display all configured Profile names for the App
6. WHEN a user exports a Profile with `<app> config export --profile <name>`, THE OpenBridge SHALL generate a shareable configuration file
7. WHEN a user imports a Profile with `<app> config import <file>`, THE OpenBridge SHALL load the configuration and prompt for any missing credentials
8. WHEN a Profile is exported, THE OpenBridge SHALL exclude sensitive credentials and include placeholders for required authentication fields
9. WHEN a Profile is imported, THE OpenBridge SHALL validate the configuration structure before persisting it

### Requirement 4: Resource Extraction from API Paths

**User Story:** As a CLI user, I want OpenBridge to automatically identify resources from API paths, so that I can use intuitive resource names in commands.

#### Acceptance Criteria

1. WHEN OpenBridge parses an API path, THE OpenBridge SHALL extract the last path segment as the Resource name
2. WHEN a path contains path parameters (e.g., `/api/v1/users/{id}`), THE OpenBridge SHALL extract the noun before the parameter as the Resource
3. WHEN multiple paths share the same Resource name, THE OpenBridge SHALL group their operations under that Resource
4. WHERE an OpenAPI specification includes the extension field `x-cli-resource`, THE OpenBridge SHALL use that value as the Resource name instead of the extracted value
5. WHEN Resource names are extracted, THE OpenBridge SHALL normalize them to lowercase and handle pluralization consistently

### Requirement 5: HTTP Method to Semantic Verb Mapping

**User Story:** As a CLI user, I want HTTP methods to be mapped to semantic verbs, so that commands feel natural and intuitive like kubectl.

#### Acceptance Criteria

1. WHEN an operation uses POST method, THE OpenBridge SHALL map it to the Verb `create` by default
2. WHEN an operation uses GET method without path parameters, THE OpenBridge SHALL map it to the Verb `list` by default
3. WHEN an operation uses GET method with path parameters, THE OpenBridge SHALL map it to the Verb `get` or `describe` by default
4. WHEN an operation uses PUT or PATCH method, THE OpenBridge SHALL map it to the Verb `apply` or `update` by default
5. WHEN an operation uses DELETE method, THE OpenBridge SHALL map it to the Verb `delete` by default
6. WHERE an OpenAPI specification includes the extension field `x-cli-verb`, THE OpenBridge SHALL use that value as the Verb instead of the default mapping
7. WHEN multiple operations map to the same Resource and Verb combination, THE OpenBridge SHALL differentiate them using additional qualifiers or flags

### Requirement 6: Command-Line Interface Execution

**User Story:** As a CLI user, I want to execute API operations using semantic commands, so that I can interact with APIs without constructing raw HTTP requests.

#### Acceptance Criteria

1. WHEN a user executes a command in the format `<app> <verb> <resource> [flags]`, THE Semantic_CLI SHALL identify the corresponding API operation
2. WHEN required parameters are provided as flags, THE Semantic_CLI SHALL construct the appropriate HTTP request
3. WHEN optional parameters are omitted, THE Semantic_CLI SHALL use default values or omit them from the request
4. WHEN a command is executed successfully, THE Semantic_CLI SHALL display the response in a formatted table view by default
5. WHEN a user specifies `--json` flag, THE Semantic_CLI SHALL output the raw JSON response
6. WHEN a user specifies `--yaml` flag, THE Semantic_CLI SHALL output the response in YAML format
7. IF an API operation fails, THEN THE Semantic_CLI SHALL display a clear error message with the HTTP status code and error details

### Requirement 7: Shell Auto-Completion

**User Story:** As a CLI user, I want tab auto-completion for commands, so that I can discover available resources and parameters efficiently.

#### Acceptance Criteria

1. WHEN a user presses Tab after typing an App name, THE System SHALL suggest available Resource names
2. WHEN a user presses Tab after typing a Resource name, THE System SHALL suggest available Verb options
3. WHEN a user presses Tab after typing a Verb, THE System SHALL suggest available flag names for that operation
4. WHEN a user presses Tab after typing a flag name, THE System SHALL suggest valid values if the parameter has enumerated options
5. WHEN auto-completion is installed, THE System SHALL integrate with the user's shell (bash, zsh, fish)

### Requirement 8: Secure Credential Storage

**User Story:** As a security-conscious user, I want API credentials stored securely in the system keyring, so that sensitive information is not exposed in plain text configuration files.

#### Acceptance Criteria

1. WHEN a user configures authentication credentials, THE OpenBridge SHALL store them in the system Keyring
2. WHEN OpenBridge needs to authenticate an API request, THE OpenBridge SHALL retrieve credentials from the Keyring
3. WHEN running on macOS, THE OpenBridge SHALL use the macOS Keychain for credential storage
4. WHEN running on Windows, THE OpenBridge SHALL use the Windows Credential Manager for credential storage
5. WHEN running on Linux, THE OpenBridge SHALL use a compatible secret service (e.g., libsecret, gnome-keyring)
6. WHEN credentials are stored, THE OpenBridge SHALL never write them to configuration files in plain text
7. WHEN a user uninstalls an App, THE OpenBridge SHALL provide an option to remove associated credentials from the Keyring

### Requirement 9: MCP Protocol Implementation

**User Story:** As an AI agent developer, I want OpenBridge to implement the Model Context Protocol, so that AI agents can discover and invoke API operations as tools.

#### Acceptance Criteria

1. WHEN OpenBridge runs in MCP mode, THE MCP_Server SHALL implement the `list_tools` interface according to MCP specification
2. WHEN an AI agent requests `list_tools`, THE MCP_Server SHALL return a JSON array of Tool objects, each containing name, description, and input_schema fields
3. WHEN converting an operation to a Tool, THE MCP_Server SHALL use the operation's `summary` field as the Tool description
4. WHEN converting an operation to a Tool, THE MCP_Server SHALL use the operation's `operationId` as the Tool name
5. WHEN converting an operation to a Tool, THE MCP_Server SHALL transform the operation's request body schema and parameters into the Tool's `input_schema` in JSON Schema format
6. WHEN an operation has path parameters, THE MCP_Server SHALL include them as required fields in the Tool's input_schema
7. WHEN an operation has query parameters, THE MCP_Server SHALL include them as fields in the Tool's input_schema with appropriate required/optional designation
8. WHEN OpenBridge runs in MCP mode, THE MCP_Server SHALL implement the `call_tool` interface according to MCP specification
9. WHEN an AI agent invokes `call_tool` with a Tool name and arguments, THE MCP_Server SHALL map the Tool name to the corresponding API operation
10. WHEN `call_tool` is invoked with valid parameters, THE MCP_Server SHALL construct an HTTP request using the provided arguments and execute the API operation
11. WHEN `call_tool` completes successfully, THE MCP_Server SHALL return the API response in MCP result format
12. WHEN `call_tool` encounters an error, THE MCP_Server SHALL return an MCP error response with descriptive error message and error code

### Requirement 10: MCP Server Activation

**User Story:** As a user, I want to start an MCP server for an App, so that AI agents can connect and use the API.

#### Acceptance Criteria

1. WHEN a user executes `<app> --mcp`, THE OpenBridge SHALL start the MCP_Server for that App
2. WHEN the MCP_Server starts, THE OpenBridge SHALL listen for MCP protocol connections
3. WHEN the MCP_Server is running, THE OpenBridge SHALL accept connections from AI agents (e.g., Claude Desktop)
4. WHEN the MCP_Server receives a request, THE OpenBridge SHALL process it according to the MCP protocol specification
5. WHEN a user terminates the MCP_Server process, THE OpenBridge SHALL gracefully shut down and close all connections

### Requirement 11: AI Safety Controls

**User Story:** As a system administrator, I want to configure safety controls for AI access, so that AI agents cannot perform destructive operations without oversight.

#### Acceptance Criteria

1. WHERE an App is configured with "AI read-only mode", THE MCP_Server SHALL only expose GET operations as tools
2. WHERE an App is configured with "AI read-only mode", THE MCP_Server SHALL reject any tool invocations that correspond to POST, PUT, PATCH, or DELETE operations
3. WHERE an App is configured to require confirmation for dangerous operations, THE MCP_Server SHALL prompt for user confirmation before executing DELETE operations
4. WHEN a dangerous operation requires confirmation, THE MCP_Server SHALL display the operation details and wait for explicit user approval
5. WHERE an App is configured with operation allowlists, THE MCP_Server SHALL only expose operations that are explicitly listed

### Requirement 12: Performance Requirements

**User Story:** As a CLI user, I want fast command execution, so that the tool feels responsive and doesn't slow down my workflow.

#### Acceptance Criteria

1. WHEN a user invokes a CLI command, THE Semantic_CLI SHALL complete initialization (cold start) in less than 100 milliseconds
2. WHEN OpenBridge parses an OpenAPI specification, THE OpenBridge SHALL cache the parsed result to avoid re-parsing on subsequent invocations
3. WHEN a user executes a command, THE Semantic_CLI SHALL minimize latency between command input and HTTP request transmission
4. WHEN the MCP_Server starts, THE MCP_Server SHALL be ready to accept connections within 200 milliseconds
5. WHEN processing MCP requests, THE MCP_Server SHALL respond to `list_tools` requests in less than 50 milliseconds

### Requirement 13: Single Binary Distribution

**User Story:** As a user, I want OpenBridge distributed as a single binary, so that installation is simple and doesn't require managing dependencies.

#### Acceptance Criteria

1. WHEN OpenBridge is built, THE Build_System SHALL produce a single executable binary file
2. WHEN a user downloads OpenBridge, THE User SHALL only need to download one file to use all features
3. WHEN OpenBridge is executed, THE OpenBridge SHALL not require external runtime dependencies (e.g., interpreters, shared libraries)
4. WHEN OpenBridge is distributed, THE Distribution SHALL include binaries for major platforms (Linux, macOS, Windows)
5. WHEN a user installs OpenBridge, THE User SHALL be able to place the binary in their PATH and immediately use it

### Requirement 14: Configuration Persistence

**User Story:** As a user, I want my App configurations to persist across sessions, so that I don't need to reconfigure every time I use OpenBridge.

#### Acceptance Criteria

1. WHEN a user installs an App, THE OpenBridge SHALL save the configuration to a persistent storage location
2. WHEN a user invokes an installed App, THE OpenBridge SHALL load the saved configuration
3. WHEN a user modifies an App configuration, THE OpenBridge SHALL update the persistent storage
4. WHEN a user uninstalls an App, THE OpenBridge SHALL remove the configuration from persistent storage
5. WHEN OpenBridge stores configuration, THE OpenBridge SHALL use a standard configuration directory (e.g., `~/.config/openbridge` on Linux/macOS, `%APPDATA%\openbridge` on Windows)

### Requirement 15: Error Handling and User Feedback

**User Story:** As a user, I want clear error messages when something goes wrong, so that I can understand and fix issues quickly.

#### Acceptance Criteria

1. WHEN an API request fails with an HTTP error, THE Semantic_CLI SHALL display the HTTP status code and response body
2. WHEN a user provides invalid command syntax, THE Semantic_CLI SHALL display usage help for that command
3. WHEN a required parameter is missing, THE Semantic_CLI SHALL list all required parameters and their descriptions
4. WHEN OpenBridge cannot connect to an API endpoint, THE OpenBridge SHALL display a network connectivity error with the target URL
5. WHEN an OpenAPI specification cannot be parsed, THE OpenBridge SHALL display the specific parsing error with line and column information if available
6. WHEN a user attempts to use a non-existent App, THE OpenBridge SHALL suggest similar App names or prompt to install the App
