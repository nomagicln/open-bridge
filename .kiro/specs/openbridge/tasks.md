# Implementation Plan: OpenBridge

## Overview

This implementation plan breaks down the OpenBridge project into discrete, incremental tasks. The approach follows a bottom-up strategy, building core components first, then integrating them into higher-level features. Each task builds on previous work, ensuring continuous integration and testability.

## Tasks

- [x] 1. Project setup and core infrastructure
  - Initialize Go module with proper structure
  - Set up testing framework and CI configuration
  - Create directory structure for packages
  - Add dependencies: kin-openapi, cobra, pflag, 99designs/keyring
  - _Requirements: 13.1, 13.3, 13.4_

- [x] 2. Implement OpenAPI Spec Parser
  - [x] 2.1 Create spec parser with loading and validation
    - Implement LoadSpec for local files and remote URLs
    - Add validation for OpenAPI 2.0, 3.0, and 3.1
    - Implement OpenAPI 2.0 to 3.x normalization
    - Add in-memory caching with TTL
    - _Requirements: 1.1, 1.2, 1.3, 1.4, 1.5, 1.6_
  
  - [ ]* 2.2 Write property test for spec parsing round-trip
    - **Property 1: OpenAPI Specification Round-Trip Parsing**
    - **Validates: Requirements 1.1, 1.2, 1.3, 1.5, 1.6**
  
  - [ ]* 2.3 Write property test for invalid spec rejection
    - **Property 2: Invalid Specification Rejection**
    - **Validates: Requirements 1.4, 15.5**
  
  - [ ]* 2.4 Write property test for spec caching
    - **Property 19: Specification Caching**
    - **Validates: Requirements 12.2**

- [x] 3. Implement Configuration Manager
  - [x] 3.1 Create configuration data structures and persistence
    - Define AppConfig, Profile, and related structs
    - Implement config file I/O (YAML format)
    - Use platform-specific config directories
    - _Requirements: 14.1, 14.2, 14.5_
  
  - [x] 3.2 Implement app installation and management
    - Implement InstallApp with interactive prompts
    - Implement UninstallApp with cleanup
    - Implement GetAppConfig and ListApps
    - _Requirements: 2.1, 2.4, 2.6, 14.4_
  
  - [x] 3.3 Implement profile management
    - Support multiple profiles per app
    - Implement profile selection logic
    - Implement default profile handling
    - _Requirements: 3.1, 3.2, 3.3, 3.4, 3.5_
  
  - [x] 3.4 Implement profile import/export
    - Implement ExportProfile with credential exclusion
    - Implement ImportProfile with validation
    - _Requirements: 3.6, 3.8, 3.9_
  
  - [x]* 3.5 Write property test for configuration persistence round-trip
    - **Property 3: Configuration Persistence Round-Trip**
    - **Validates: Requirements 2.1, 2.4, 14.1, 14.2, 14.3**
  
  - [x]* 3.6 Write property test for profile export credential exclusion
    - **Property 5: Profile Export Excludes Credentials**
    - **Validates: Requirements 3.8**
  
  - [x]* 3.7 Write property test for profile import validation
    - **Property 6: Profile Import Validation**
    - **Validates: Requirements 3.9**
  
  - [ ]* 3.8 Write property test for config directory standard
    - **Property 22: Configuration Directory Standard**
    - **Validates: Requirements 14.5**

- [x] 4. Implement Credential Manager
  - [x] 4.1 Create keyring integration
    - Integrate 99designs/keyring library
    - Implement StoreCredential, GetCredential, DeleteCredential
    - Handle platform-specific keyring backends
    - _Requirements: 2.7, 8.1, 8.2, 8.7_
  
  - [x] 4.2 Add credential security measures
    - Ensure credentials never appear in config files
    - Implement secure credential prompting
    - Add keyring error handling
    - _Requirements: 8.6_
  
  - [x]* 4.3 Write property test for credential keyring round-trip
    - **Property 4: Credential Keyring Round-Trip**
    - **Validates: Requirements 2.7, 8.1, 8.2, 8.6**
  
  - [x]* 4.4 Write unit tests for platform-specific keyring backends
    - Test macOS Keychain integration
    - Test Windows Credential Manager integration
    - Test Linux Secret Service integration
    - _Requirements: 8.3, 8.4, 8.5_

- [x] 5. Implement Semantic Mapper
  - [x] 5.1 Create resource extraction logic
    - Implement ExtractResource from API paths
    - Handle path parameters correctly
    - Implement resource name normalization
    - Support x-cli-resource override
    - _Requirements: 4.1, 4.2, 4.4, 4.5_
  
  - [x] 5.2 Create HTTP method to verb mapping
    - Implement default mapping rules (POST→create, GET→list/get, etc.)
    - Support x-cli-verb override
    - Handle verb conflicts with qualifiers
    - _Requirements: 5.1, 5.2, 5.3, 5.4, 5.5, 5.6, 5.7_
  
  - [x] 5.3 Implement command structure builder
    - Build CommandTree from OpenAPI spec
    - Group operations by resource
    - _Requirements: 4.3_
  
  - [ ]* 5.4 Write property test for resource extraction consistency
    - **Property 7: Resource Extraction Consistency**
    - **Validates: Requirements 4.1, 4.2, 4.4, 4.5**
  
  - [ ]* 5.5 Write property test for HTTP method to verb mapping
    - **Property 8: HTTP Method to Verb Mapping**
    - **Validates: Requirements 5.1, 5.2, 5.3, 5.4, 5.5, 5.6**

- [x] 6. Checkpoint - Core components complete
  - Ensure all tests pass, ask the user if questions arise.

- [ ] 7. Implement Request Builder
  - [x] 7.1 Create parameter resolution logic
    - Implement parameter location resolution (path, query, header, cookie)
    - Implement path parameter substitution
    - Implement query string construction
    - _Requirements: 6.2_
  
  - [x] 7.2 Implement request body construction enhancements
    - Support schema-based body construction from flags with nested objects
    - Support arrays via repeated flags or comma-separated values
    - Support file input via --body @file
    - _Requirements: 6.2_
  
  - [x] 7.3 Implement authentication injection
    - Support Bearer token authentication
    - Support API key authentication (header/query)
    - Support Basic authentication
    - Integrate with Credential Manager
    - _Requirements: 2.7, 8.2_
  
  - [x] 7.4 Add parameter validation
    - Validate required parameters are present
    - Validate parameter types against schema
    - _Requirements: 6.2_
  
  - [ ]* 7.5 Write property test for request construction from flags
    - **Property 10: Request Construction from Flags**
    - **Validates: Requirements 6.2, 6.3**

- [ ] 8. Implement CLI Handler
  - [x] 8.1 Create command parsing logic
    - Parse command format: <app> <verb> <resource> [flags]
    - Implement flag parsing with support for nested objects and arrays
    - Map command to operation via Semantic Mapper
    - _Requirements: 6.1_
  
  - [x] 8.2 Implement command execution
    - Build HTTP request via Request Builder
    - Execute HTTP request
    - Handle HTTP errors
    - _Requirements: 6.2, 6.7_
  
  - [x] 8.3 Implement output formatting
    - Default table output for list operations
    - Support --json flag for JSON output
    - Support --yaml flag for YAML output
    - _Requirements: 6.4, 6.5, 6.6_
  
  - [x] 8.4 Add error handling and user feedback
    - Display HTTP status codes and error details
    - Show usage help for invalid syntax
    - List required parameters when missing
    - Display network connectivity errors
    - Suggest similar app names for non-existent apps
    - _Requirements: 6.7, 15.1, 15.2, 15.3, 15.4, 15.6_
  
  - [ ]* 8.5 Write property test for command to operation mapping
    - **Property 9: Command to Operation Mapping**
    - **Validates: Requirements 6.1**
  
  - [ ]* 8.6 Write property test for output format consistency
    - **Property 11: Output Format Consistency**
    - **Validates: Requirements 6.4, 6.5, 6.6**
  
  - [ ]* 8.7 Write property test for error message completeness
    - **Property 12: Error Message Completeness**
    - **Validates: Requirements 6.7, 15.1**
  
  - [ ]* 8.8 Write property test for missing parameter error
    - **Property 23: Missing Parameter Error**
    - **Validates: Requirements 15.3**
  
  - [ ]* 8.9 Write property test for network error reporting
    - **Property 24: Network Error Reporting**
    - **Validates: Requirements 15.4**

- [ ] 9. Implement MCP Handler
  - [x] 9.1 Create MCP tool conversion logic
    - Convert OpenAPI operations to MCP tools
    - Map operationId to tool name
    - Map summary to tool description
    - Convert request schema to input_schema (JSON Schema format)
    - Handle path parameters as required fields
    - Handle query parameters with required/optional designation
    - _Requirements: 9.3, 9.4, 9.5, 9.6, 9.7_
  
  - [x] 9.2 Implement JSON-RPC protocol handler
    - Implement tools/list endpoint
    - Implement tools/call endpoint
    - Handle JSON-RPC request/response format
    - _Requirements: 9.1, 9.2, 9.8_
  
  - [x] 9.3 Implement tool invocation logic
    - Map tool name to API operation
    - Build HTTP request from tool arguments
    - Execute API call
    - Format response in MCP result format
    - Handle errors with MCP error format
    - _Requirements: 9.9, 9.10, 9.11, 9.12_
  
  - [x] 9.4 Implement AI safety controls
    - Implement read-only mode (GET operations only)
    - Implement operation allowlist filtering
    - Add confirmation prompts for dangerous operations
    - _Requirements: 11.1, 11.2, 11.5_
  
  - [ ]* 9.5 Write property test for MCP tool list structure
    - **Property 13: MCP Tool List Structure**
    - **Validates: Requirements 9.1, 9.2, 9.3, 9.4, 9.5, 9.6, 9.7**
  
  - [ ]* 9.6 Write property test for MCP tool invocation round-trip
    - **Property 14: MCP Tool Invocation Round-Trip**
    - **Validates: Requirements 9.8, 9.9, 9.10, 9.11**
  
  - [ ]* 9.7 Write property test for MCP error handling
    - **Property 15: MCP Error Handling**
    - **Validates: Requirements 9.12**
  
  - [ ]* 9.8 Write property test for AI read-only mode enforcement
    - **Property 16: AI Read-Only Mode Enforcement**
    - **Validates: Requirements 11.1, 11.2**
  
  - [ ]* 9.9 Write property test for operation allowlist filtering
    - **Property 17: Operation Allowlist Filtering**
    - **Validates: Requirements 11.5**

- [ ] 10. Implement Command Router
  - [x] 10.1 Create main entry point and routing logic
    - Implement app name detection (ob vs shim mode)
    - Implement mode detection (CLI vs MCP)
    - Route to appropriate handler
    - Handle ob-level commands (install, uninstall, list, version, help)
    - _Requirements: 2.1, 2.2, 2.3_
  
  - [x] 10.2 Implement shim creation
    - Create shim scripts/symlinks in user's PATH
    - Handle platform-specific PATH locations
    - _Requirements: 2.2, 2.3_
  
  - [x] 10.3 Implement MCP server startup
    - Start MCP server when --mcp flag is present
    - Listen for MCP protocol connections (stdio or TCP)
    - Handle graceful shutdown
    - _Requirements: 10.1, 10.2, 10.3, 10.5_

- [ ] 11. Integration and end-to-end testing
  - [x] 11.1 Wire up all components in main.go
    - Initialize all handlers with proper dependencies
    - Connect router to CLI and MCP handlers
    - Implement ob install command with full workflow
    - Implement ob uninstall command with cleanup
    - Implement ob list command
    - _Requirements: 2.1, 2.2, 2.4, 2.6_
  
  - [x] 11.2 Create end-to-end integration tests
    - Test full CLI workflow (install, execute commands, uninstall)
    - Test MCP server workflow (start, list tools, call tool)
    - Test profile switching
    - Test error scenarios
    - _Requirements: All_
  
  - [x] 11.3 Test with real OpenAPI specs
    - Test with Petstore API
    - Test with GitHub API
    - Test with Stripe API
    - Verify all operations work correctly
    - _Requirements: All_

- [x] 12. Implement shell auto-completion
  - [ ] 12.1 Generate completion scripts
    - Use cobra's built-in completion support
    - Generate bash completion script
    - Generate zsh completion script
    - Generate fish completion script
    - _Requirements: 7.5_
  
  - [ ] 12.2 Implement dynamic completion
    - Complete resource names from OpenAPI spec
    - Complete verb names for selected resource
    - Complete flag names for selected operation
    - Complete flag values for enum parameters
    - _Requirements: 7.1, 7.2, 7.3, 7.4_

- [ ] 13. Performance optimization
  - [x] 13.1 Optimize cold start performance
    - Profile startup time
    - Optimize spec parsing and caching
    - Lazy load components where possible
    - Target < 100ms cold start
    - _Requirements: 12.1_
  
  - [x] 13.2 Optimize MCP server performance
    - Profile MCP server startup
    - Optimize list_tools response time
    - Target < 200ms server startup, < 50ms list_tools response
    - _Requirements: 12.4, 12.5_
  
  - [ ]* 13.3 Write property test for cold start performance
    - **Property 18: Cold Start Performance**
    - **Validates: Requirements 12.1**
  
  - [ ]* 13.4 Write property test for MCP server startup performance
    - **Property 20: MCP Server Startup Performance**
    - **Validates: Requirements 12.4**
  
  - [ ]* 13.5 Write property test for MCP list tools performance
    - **Property 21: MCP List Tools Performance**
    - **Validates: Requirements 12.5**

- [ ] 14. Build and distribution
  - [x] 14.1 Set up cross-platform builds
    - Configure Go build for Linux (amd64, arm64)
    - Configure Go build for macOS (amd64, arm64)
    - Configure Go build for Windows (amd64)
    - _Requirements: 13.4_
  
  - [ ] 14.2 Create release artifacts
    - Package binaries for each platform
    - Create installation scripts
    - Write installation documentation
    - _Requirements: 13.1, 13.2_

- [ ] 15. Documentation and examples
  - [ ] 15.1 Write user documentation
    - Installation guide
    - Quick start guide
    - Command reference
    - Configuration guide
    - MCP integration guide
  
  - [ ] 15.2 Create example configurations
    - Example OpenAPI specs with x-cli-* extensions
    - Example app configurations
    - Example profile configurations

- [ ] 16. Property-based testing implementation
  - [x] 16.1 Add gopter dependency and setup
    - Add gopter to go.mod
    - Create test generators package
    - Set up property test infrastructure
    - _Requirements: All_
  
  - [x] 16.2 Implement property test generators
    - Create OpenAPI spec generator
    - Create config generator
    - Create parameter generator
    - Create request/response generators
    - _Requirements: All_
  
  - [x] 16.3 Run all property tests
    - Execute all property tests with 100 iterations
    - Fix any failures
    - Document property test results
    - _Requirements: All_

- [ ] 17. Final checkpoint - Complete system test
  - Run full test suite with all unit tests
  - Run property tests with 1000 iterations
  - Test on all platforms (Linux, macOS, Windows)
  - Verify performance requirements
  - Test with real-world OpenAPI specs
  - Ensure all tests pass, ask the user if questions arise.

## Notes

- Tasks marked with `*` are optional property-based tests and can be skipped for faster MVP
- Each task references specific requirements for traceability
- Checkpoints ensure incremental validation
- Property tests validate universal correctness properties
- Unit tests validate specific examples and edge cases
- Performance tests ensure < 100ms cold start and MCP responsiveness
- The implementation is approximately 60% complete with core components done
- Remaining work focuses on integration, MCP server implementation, and testing
