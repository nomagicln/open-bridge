// Package mcp provides MCP (Model Context Protocol) server functionality.
package mcp

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/nomagicln/open-bridge/pkg/config"
	"github.com/nomagicln/open-bridge/pkg/request"
	"github.com/nomagicln/open-bridge/pkg/semantic"
)

// Tool represents an MCP tool definition.
type Tool struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"inputSchema"`
}

// Request represents a JSON-RPC request.
type Request struct {
	JSONRPC string         `json:"jsonrpc"`
	ID      any            `json:"id"`
	Method  string         `json:"method"`
	Params  map[string]any `json:"params,omitempty"`
}

// Response represents a JSON-RPC response.
type Response struct {
	JSONRPC string `json:"jsonrpc"`
	ID      any    `json:"id"`
	Result  any    `json:"result,omitempty"`
	Error   *Error `json:"error,omitempty"`
}

// Error represents a JSON-RPC error.
type Error struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

// Standard JSON-RPC error codes
const (
	ErrCodeParseError     = -32700
	ErrCodeInvalidRequest = -32600
	ErrCodeMethodNotFound = -32601
	ErrCodeInvalidParams  = -32602
	ErrCodeInternalError  = -32603
)

// Handler implements the MCP protocol for AI agent integration.
type Handler struct {
	mapper         *semantic.Mapper
	requestBuilder *request.Builder
	httpClient     *http.Client
	spec           *openapi3.T
	appConfig      *config.AppConfig
	profileName    string
}

// NewHandler creates a new MCP handler.
func NewHandler(mapper *semantic.Mapper, requestBuilder *request.Builder, httpClient *http.Client) *Handler {
	if httpClient == nil {
		httpClient = &http.Client{}
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
func (h *Handler) SetAppConfig(config *config.AppConfig, profileName string) {
	h.appConfig = config
	h.profileName = profileName
}

// HandleRequest processes a JSON-RPC request and returns a response.
func (h *Handler) HandleRequest(data []byte) (*Response, error) {
	var req Request
	if err := json.Unmarshal(data, &req); err != nil {
		return &Response{
			JSONRPC: "2.0",
			ID:      nil,
			Error: &Error{
				Code:    ErrCodeParseError,
				Message: "Parse error",
			},
		}, nil
	}

	if req.JSONRPC != "2.0" {
		return &Response{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error: &Error{
				Code:    ErrCodeInvalidRequest,
				Message: "Invalid Request: jsonrpc must be '2.0'",
			},
		}, nil
	}

	switch req.Method {
	case "tools/list":
		return h.handleListTools(&req), nil
	case "tools/call":
		return h.handleCallTool(&req), nil
	default:
		return &Response{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error: &Error{
				Code:    ErrCodeMethodNotFound,
				Message: fmt.Sprintf("Method not found: %s", req.Method),
			},
		}, nil
	}
}

// handleListTools handles the tools/list method.
func (h *Handler) handleListTools(req *Request) *Response {
	// This would be populated from the loaded OpenAPI spec
	// Placeholder implementation
	return &Response{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result: map[string]any{
			"tools": []Tool{},
		},
	}
}

// handleCallTool handles the tools/call method.
func (h *Handler) handleCallTool(req *Request) *Response {
	// Extract tool name
	toolName, ok := req.Params["name"].(string)
	if !ok || toolName == "" {
		return &Response{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error: &Error{
				Code:    ErrCodeInvalidParams,
				Message: "Invalid params: 'name' is required",
			},
		}
	}

	// Extract arguments
	arguments, ok := req.Params["arguments"].(map[string]any)
	if !ok {
		arguments = make(map[string]any)
	}

	// Map tool name to operation
	operation, method, path, err := h.mapToolToOperation(toolName)
	if err != nil {
		return &Response{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error: &Error{
				Code:    ErrCodeInvalidParams,
				Message: fmt.Sprintf("Tool not found: %s", toolName),
				Data:    err.Error(),
			},
		}
	}

	// Get profile configuration
	if h.appConfig == nil {
		return &Response{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error: &Error{
				Code:    ErrCodeInternalError,
				Message: "App configuration not set",
			},
		}
	}

	profileName := h.profileName
	if profileName == "" {
		profileName = h.appConfig.DefaultProfile
	}

	profile, ok := h.appConfig.GetProfile(profileName)
	if !ok {
		return &Response{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error: &Error{
				Code:    ErrCodeInternalError,
				Message: fmt.Sprintf("Profile '%s' not found", profileName),
			},
		}
	}

	// Build HTTP request
	var requestBody *openapi3.RequestBody
	if operation.RequestBody != nil {
		requestBody = operation.RequestBody.Value
	}
	
	httpReq, err := h.requestBuilder.BuildRequest(
		method,
		path,
		profile.BaseURL,
		arguments,
		operation.Parameters,
		requestBody,
	)
	if err != nil {
		return &Response{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error: &Error{
				Code:    ErrCodeInvalidParams,
				Message: "Failed to build request",
				Data:    err.Error(),
			},
		}
	}

	// Inject authentication
	if err := h.requestBuilder.InjectAuth(httpReq, h.appConfig.Name, profileName, &profile.Auth); err != nil {
		return &Response{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error: &Error{
				Code:    ErrCodeInternalError,
				Message: "Failed to inject authentication",
				Data:    err.Error(),
			},
		}
	}

	// Add custom headers from profile
	for key, value := range profile.Headers {
		httpReq.Header.Set(key, value)
	}

	// Execute API call
	httpResp, err := h.httpClient.Do(httpReq)
	if err != nil {
		return &Response{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error: &Error{
				Code:    ErrCodeInternalError,
				Message: "Failed to execute API call",
				Data:    err.Error(),
			},
		}
	}
	defer httpResp.Body.Close()

	// Read response body
	bodyBytes, err := io.ReadAll(httpResp.Body)
	if err != nil {
		return &Response{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error: &Error{
				Code:    ErrCodeInternalError,
				Message: "Failed to read response body",
				Data:    err.Error(),
			},
		}
	}

	// Check for HTTP errors
	if httpResp.StatusCode >= 400 {
		return &Response{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error: &Error{
				Code:    ErrCodeInternalError,
				Message: fmt.Sprintf("API request failed with status %d", httpResp.StatusCode),
				Data: map[string]any{
					"status_code": httpResp.StatusCode,
					"status":      httpResp.Status,
					"body":        string(bodyBytes),
				},
			},
		}
	}

	// Format response in MCP result format
	result := h.formatMCPResult(httpResp.StatusCode, bodyBytes)

	return &Response{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result:  result,
	}
}

// mapToolToOperation maps a tool name to its corresponding OpenAPI operation.
func (h *Handler) mapToolToOperation(toolName string) (*openapi3.Operation, string, string, error) {
	if h.spec == nil {
		return nil, "", "", fmt.Errorf("OpenAPI spec not loaded")
	}

	// Search for operation by operationId
	for path, pathItem := range h.spec.Paths.Map() {
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

			// Match by operationId
			if op.OperationID == toolName {
				return op, method, path, nil
			}

			// Fallback: match by generated name (method_path)
			generatedName := fmt.Sprintf("%s_%s", method, path)
			if generatedName == toolName {
				return op, method, path, nil
			}
		}
	}

	return nil, "", "", fmt.Errorf("operation not found for tool: %s", toolName)
}

// formatMCPResult formats an API response into MCP result format.
func (h *Handler) formatMCPResult(statusCode int, bodyBytes []byte) map[string]any {
	// Try to parse as JSON
	var bodyJSON any
	if err := json.Unmarshal(bodyBytes, &bodyJSON); err != nil {
		// Not JSON, return as text
		return map[string]any{
			"content": []map[string]any{
				{
					"type": "text",
					"text": string(bodyBytes),
				},
			},
			"isError": false,
		}
	}

	// Format JSON response
	prettyJSON, err := json.MarshalIndent(bodyJSON, "", "  ")
	if err != nil {
		prettyJSON = bodyBytes
	}

	return map[string]any{
		"content": []map[string]any{
			{
				"type": "text",
				"text": string(prettyJSON),
			},
		},
		"isError": false,
	}
}

// BuildMCPTools converts OpenAPI operations to MCP tool definitions.
func (h *Handler) BuildMCPTools(spec *openapi3.T, safetyConfig *config.SafetyConfig) []Tool {
	var tools []Tool

	for path, pathItem := range spec.Paths.Map() {
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

			// Apply safety controls
			if safetyConfig != nil && safetyConfig.ReadOnlyMode && method != "GET" {
				continue
			}

			tool := h.convertOperationToTool(method, path, op)
			tools = append(tools, tool)
		}
	}

	return tools
}

// convertOperationToTool converts an OpenAPI operation to an MCP tool.
func (h *Handler) convertOperationToTool(method, path string, op *openapi3.Operation) Tool {
	// Use operationId as tool name, fallback to method+path
	name := op.OperationID
	if name == "" {
		name = fmt.Sprintf("%s_%s", method, path)
	}

	// Build input schema from parameters and request body
	inputSchema := map[string]any{
		"type":       "object",
		"properties": map[string]any{},
		"required":   []string{},
	}

	properties := inputSchema["properties"].(map[string]any)
	required := inputSchema["required"].([]string)

	// Add parameters
	for _, paramRef := range op.Parameters {
		param := paramRef.Value
		propSchema := map[string]any{
			"type":        "string", // Default type
			"description": param.Description,
		}

		if param.Schema != nil && param.Schema.Value != nil {
			if param.Schema.Value.Type != nil {
				types := *param.Schema.Value.Type
				if len(types) > 0 {
					propSchema["type"] = types[0]
				}
			}
		}

		properties[param.Name] = propSchema

		if param.Required {
			required = append(required, param.Name)
		}
	}

	inputSchema["required"] = required

	return Tool{
		Name:        name,
		Description: op.Summary,
		InputSchema: inputSchema,
	}
}

// ApplySafetyControls filters tools based on safety configuration.
func (h *Handler) ApplySafetyControls(tools []Tool, safetyConfig *config.SafetyConfig) []Tool {
	if safetyConfig == nil {
		return tools
	}

	var filtered []Tool

	allowedSet := make(map[string]bool)
	for _, op := range safetyConfig.AllowedOperations {
		allowedSet[op] = true
	}

	deniedSet := make(map[string]bool)
	for _, op := range safetyConfig.DeniedOperations {
		deniedSet[op] = true
	}

	for _, tool := range tools {
		// Check denied list first
		if deniedSet[tool.Name] {
			continue
		}

		// If allowed list is specified, only include those
		if len(allowedSet) > 0 {
			if !allowedSet[tool.Name] {
				continue
			}
		}

		filtered = append(filtered, tool)
	}

	return filtered
}
