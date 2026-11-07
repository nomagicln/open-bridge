// Package mcp provides MCP (Model Context Protocol) server functionality.
package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"slices"
	"time"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/nomagicln/open-bridge/pkg/config"
	"github.com/nomagicln/open-bridge/pkg/request"
	"github.com/nomagicln/open-bridge/pkg/semantic"
)

// Handler implements the MCP protocol for AI agent integration.
type Handler struct {
	mapper         *semantic.Mapper
	requestBuilder *request.Builder
	httpClient     *http.Client
	spec           *openapi3.T
	appConfig      *config.AppConfig
	profileName    string
	safety         *safetyGuard
	safetyErr      error
}

// defaultHTTPTimeout bounds every MCP tool call so a slow or unreachable API
// cannot hang the server indefinitely. It matches the parser/cache/CLI clients.
const defaultHTTPTimeout = 30 * time.Second

// NewHandler creates a new MCP handler.
func NewHandler(mapper *semantic.Mapper, requestBuilder *request.Builder, httpClient *http.Client) *Handler {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: defaultHTTPTimeout}
	}
	return &Handler{
		mapper:         mapper,
		requestBuilder: requestBuilder,
		httpClient:     httpClient,
	}
}

// SetSpec sets the OpenAPI specification for the handler.
func (h *Handler) SetSpec(spec *openapi3.T) {
	h.spec = spec
}

// SetAppConfig sets the app configuration for the handler.
func (h *Handler) SetAppConfig(appCfg *config.AppConfig, profileName string) {
	h.appConfig = appCfg
	h.profileName = profileName
}

// SetSafetyConfig validates and installs the MCP safety configuration enforced
// at tool invocation time. It returns an error when the configuration is invalid
// (for example an unparsable dangerous_operation_patterns regex). When validation
// fails the handler fails closed: every tool call is rejected.
func (h *Handler) SetSafetyConfig(cfg *config.SafetyConfig) error {
	guard, err := newSafetyGuard(cfg)
	if err != nil {
		h.safety = nil
		h.safetyErr = fmt.Errorf("invalid safety configuration: %w", err)
		return h.safetyErr
	}
	h.safety = guard
	h.safetyErr = nil
	return nil
}

// GetRequestBuilder returns the request builder used by the handler.
func (h *Handler) GetRequestBuilder() *request.Builder {
	return h.requestBuilder
}

// Register registers the tools with the MCP server.
func (h *Handler) Register(s *mcp.Server, safetyConfig *config.SafetyConfig) {
	// Register cannot return an error: invalid safety configuration is surfaced as
	// a startup warning and then every tool call fails closed with an isError
	// result. Callers that need a hard startup failure can call SetSafetyConfig or
	// ValidateSafetyConfig directly.
	if err := h.SetSafetyConfig(safetyConfig); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: MCP safety configuration invalid: %v\n", err)
	}

	tools := h.GetTools(safetyConfig)
	for _, t := range tools {
		tool := t // capture loop variable
		s.AddTool(&tool, h.HandleCallTool)
	}
}

// GetTools returns the list of available tools.
func (h *Handler) GetTools(safetyConfig *config.SafetyConfig) []mcp.Tool {
	if h.spec == nil {
		return []mcp.Tool{}
	}
	tools := h.BuildMCPTools(h.spec, safetyConfig)
	return tools
}

// errorResult creates an MCP error result with the given message.
func errorResult(format string, args ...any) *mcp.CallToolResult {
	return &mcp.CallToolResult{
		Content: []mcp.Content{
			&mcp.TextContent{Text: fmt.Sprintf(format, args...)},
		},
		IsError: true,
	}
}

// parseToolArguments unmarshals the tool arguments from the request.
func parseToolArguments(req *mcp.CallToolRequest) (map[string]any, error) {
	var arguments map[string]any
	if len(req.Params.Arguments) > 0 {
		if err := json.Unmarshal(req.Params.Arguments, &arguments); err != nil {
			return nil, err
		}
	}
	return arguments, nil
}

// getActiveProfile returns the active profile name and configuration.
func (h *Handler) getActiveProfile() (string, *config.Profile, error) {
	return getActiveProfile(h.appConfig, h.profileName)
}

// buildAndExecuteRequest builds and executes the HTTP request.
func (h *Handler) buildAndExecuteRequest(operation *openapi3.Operation, method, path string, arguments map[string]any, profileName string, profile *config.Profile) (*mcp.CallToolResult, error) {
	httpReq, err := h.buildRequest(method, path, arguments, operation, profile)
	if err != nil {
		return errorResult("Failed to build request: %v", err), nil
	}

	if err := h.injectAuthAndHeaders(httpReq, profileName, profile); err != nil {
		return nil, fmt.Errorf("failed to inject authentication: %w", err)
	}

	httpResp, err := h.executeRequest(httpReq)
	if err != nil {
		return errorResult("Failed to execute API call: %v", err), nil
	}
	defer func() { _ = httpResp.Body.Close() }()

	bodyBytes, err := io.ReadAll(httpResp.Body)
	if err != nil {
		return errorResult("Failed to read response body: %v", err), nil
	}

	return h.FormatMCPResult(httpResp.StatusCode, bodyBytes), nil
}

// buildRequest builds the HTTP request with parameters.
func (h *Handler) buildRequest(method, path string, arguments map[string]any, operation *openapi3.Operation, profile *config.Profile) (*http.Request, error) {
	var requestBody *openapi3.RequestBody
	if operation.RequestBody != nil {
		requestBody = operation.RequestBody.Value
	}

	return h.requestBuilder.BuildRequest(method, path, profile.BaseURL, arguments, operation.Parameters, requestBody)
}

// injectAuthAndHeaders injects authentication and custom headers into the request.
func (h *Handler) injectAuthAndHeaders(httpReq *http.Request, profileName string, profile *config.Profile) error {
	if err := h.requestBuilder.InjectAuth(httpReq, h.appConfig.Name, profileName, &profile.Auth); err != nil {
		return err
	}

	for key, value := range profile.Headers {
		httpReq.Header.Set(key, value)
	}

	return nil
}

// executeRequest performs the HTTP request and returns the response.
func (h *Handler) executeRequest(httpReq *http.Request) (*http.Response, error) {
	return h.httpClient.Do(httpReq)
}

// HandleCallTool handles tool execution requests.
func (h *Handler) HandleCallTool(_ context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if h.safetyErr != nil {
		return errorResult("%v", h.safetyErr), nil
	}

	arguments, err := parseToolArguments(req)
	if err != nil {
		return errorResult("Error unmarshaling arguments: %v", err), nil
	}

	// _confirm is an invocation-only flag: record it, then strip it so it is
	// never forwarded to the API.
	confirmed := confirmRequested(arguments)
	stripConfirmArgument(arguments)

	operation, method, path, err := h.MapToolToOperation(req.Params.Name)
	if err != nil {
		return errorResult("Error mapping tool: %v", err), nil
	}

	if safetyResult := h.safety.checkInvocation(req.Params.Name, method, path, confirmed); safetyResult != nil {
		return safetyResult, nil
	}

	profileName, profile, err := h.getActiveProfile()
	if err != nil {
		return nil, err
	}

	return h.buildAndExecuteRequest(operation, method, path, arguments, profileName, profile)
}

// mapToolToOperation maps a tool name to its corresponding OpenAPI operation.
func (h *Handler) MapToolToOperation(toolName string) (*openapi3.Operation, string, string, error) {
	if h.spec == nil {
		return nil, "", "", fmt.Errorf("OpenAPI spec not loaded")
	}

	// Search for operation by operationId or generated tool name
	for path, pathItem := range h.spec.Paths.Map() {
		if op, method, found := h.findOperationInPath(path, pathItem, toolName); found {
			return op, method, path, nil
		}
	}

	return nil, "", "", fmt.Errorf("operation not found for tool: %s", toolName)
}

// findOperationInPath searches for a tool in a specific path item.
func (h *Handler) findOperationInPath(path string, pathItem *openapi3.PathItem, toolName string) (*openapi3.Operation, string, bool) {
	operations := map[string]*openapi3.Operation{
		"GET":    pathItem.Get,
		"POST":   pathItem.Post,
		"PUT":    pathItem.Put,
		"PATCH":  pathItem.Patch,
		"DELETE": pathItem.Delete,
	}

	for method, op := range operations {
		if op == nil {
			continue
		}

		if matchesToolName(op, method, path, toolName) {
			return op, method, true
		}
	}

	return nil, "", false
}

// matchesToolName checks if an operation matches the given tool name.
func matchesToolName(op *openapi3.Operation, method, path, toolName string) bool {
	// Match by operationId
	if op.OperationID == toolName {
		return true
	}

	// Match by generated tool name (same logic as BuildMCPTools)
	generatedName := GenerateToolName(method, path, op)
	return generatedName == toolName
}

// FormatMCPResult formats an API response into MCP result format.
func (h *Handler) FormatMCPResult(statusCode int, bodyBytes []byte) *mcp.CallToolResult {
	return formatMCPResult(statusCode, bodyBytes)
}

// buildMCPTools converts OpenAPI operations to MCP tool definitions.
func (h *Handler) BuildMCPTools(spec *openapi3.T, safetyConfig *config.SafetyConfig) []mcp.Tool {
	var tools []mcp.Tool

	for path, pathItem := range spec.Paths.Map() {
		tools = h.processPathItem(path, pathItem, safetyConfig, tools)
	}

	return tools
}

// processPathItem processes operations in a single path item.
func (h *Handler) processPathItem(path string, pathItem *openapi3.PathItem, safetyConfig *config.SafetyConfig, tools []mcp.Tool) []mcp.Tool {
	operations := map[string]*openapi3.Operation{
		"GET":    pathItem.Get,
		"POST":   pathItem.Post,
		"PUT":    pathItem.Put,
		"PATCH":  pathItem.Patch,
		"DELETE": pathItem.Delete,
	}

	for method, op := range operations {
		if op == nil {
			continue
		}

		if h.shouldSkipOperation(method, safetyConfig) {
			continue
		}

		tool := h.convertOperationToTool(method, path, op)
		if h.isToolAllowed(tool.Name, safetyConfig) {
			tools = append(tools, tool)
		}
	}

	return tools
}

// shouldSkipOperation checks if an operation should be skipped based on safety config.
func (h *Handler) shouldSkipOperation(method string, safetyConfig *config.SafetyConfig) bool {
	if safetyConfig == nil {
		return false
	}

	return safetyConfig.ReadOnlyMode && method != "GET"
}

// convertOperationToTool converts an OpenAPI operation to an MCP tool.
// Delegates to the shared convertOperationToMCPTool function.
func (h *Handler) convertOperationToTool(method, path string, op *openapi3.Operation) mcp.Tool {
	return convertOperationToMCPTool(method, path, op)
}

// isToolAllowed checks if a tool is allowed based on safety configuration.
func (h *Handler) isToolAllowed(toolName string, safetyConfig *config.SafetyConfig) bool {
	if safetyConfig == nil {
		return true
	}

	// Check denied list first
	if slices.Contains(safetyConfig.DeniedOperations, toolName) {
		return false
	}

	// If allowed list is specified, only include those
	if len(safetyConfig.AllowedOperations) > 0 {
		allowed := slices.Contains(safetyConfig.AllowedOperations, toolName)
		if !allowed {
			return false
		}
	}

	return true
}
