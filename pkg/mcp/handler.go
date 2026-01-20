// Package mcp provides MCP (Model Context Protocol) server functionality.
package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

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

// Register registers the tools with the MCP server.
func (h *Handler) Register(s *mcp.Server, safetyConfig *config.SafetyConfig) {
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

// HandleCallTool handles tool execution requests.
func (h *Handler) HandleCallTool(ctx context.Context, request *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	toolName := request.Params.Name

	// Unmarshal arguments from json.RawMessage
	var arguments map[string]any
	if len(request.Params.Arguments) > 0 {
		if err := json.Unmarshal(request.Params.Arguments, &arguments); err != nil {
			return &mcp.CallToolResult{
				Content: []mcp.Content{
					&mcp.TextContent{
						Text: fmt.Sprintf("Error unmarshaling arguments: %v", err),
					},
				},
				IsError: true,
			}, nil
		}
	}

	// Map tool name to operation
	operation, method, path, err := h.MapToolToOperation(toolName)
	if err != nil {
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{
					Text: fmt.Sprintf("Error mapping tool: %v", err),
				},
			},
			IsError: true,
		}, nil
	}

	// Get profile configuration
	if h.appConfig == nil {
		return nil, fmt.Errorf("app configuration not set")
	}

	profileName := h.profileName
	if profileName == "" {
		profileName = h.appConfig.DefaultProfile
	}

	profile, ok := h.appConfig.GetProfile(profileName)
	if !ok {
		return nil, fmt.Errorf("profile '%s' not found", profileName)
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
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{
					Text: fmt.Sprintf("Failed to build request: %v", err),
				},
			},
			IsError: true,
		}, nil
	}

	// Inject authentication
	if err := h.requestBuilder.InjectAuth(httpReq, h.appConfig.Name, profileName, &profile.Auth); err != nil {
		return nil, fmt.Errorf("failed to inject authentication: %w", err)
	}

	// Add custom headers from profile
	for key, value := range profile.Headers {
		httpReq.Header.Set(key, value)
	}

	// Execute API call
	httpResp, err := h.httpClient.Do(httpReq)
	if err != nil {
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{
					Text: fmt.Sprintf("Failed to execute API call: %v", err),
				},
			},
			IsError: true,
		}, nil
	}
	defer func() { _ = httpResp.Body.Close() }()

	// Read response body
	bodyBytes, err := io.ReadAll(httpResp.Body)
	if err != nil {
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{
					Text: fmt.Sprintf("Failed to read response body: %v", err),
				},
			},
			IsError: true,
		}, nil
	}

	// Format response in MCP result format
	return h.FormatMCPResult(httpResp.StatusCode, bodyBytes), nil
}

// mapToolToOperation maps a tool name to its corresponding OpenAPI operation.
func (h *Handler) MapToolToOperation(toolName string) (*openapi3.Operation, string, string, error) {
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
func (h *Handler) FormatMCPResult(statusCode int, bodyBytes []byte) *mcp.CallToolResult {
	// Check for error status codes
	isError := statusCode >= 400

	// Try to parse as JSON
	var bodyJSON any
	if err := json.Unmarshal(bodyBytes, &bodyJSON); err != nil {
		// Not JSON, return as text
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{
					Text: string(bodyBytes),
				},
			},
			IsError: isError,
		}
	}

	// Format JSON response
	prettyJSON, err := json.MarshalIndent(bodyJSON, "", "  ")
	if err != nil {
		prettyJSON = bodyBytes
	}

	return &mcp.CallToolResult{
		Content: []mcp.Content{
			&mcp.TextContent{
				Text: string(prettyJSON),
			},
		},
		IsError: isError,
	}
}

// buildMCPTools converts OpenAPI operations to MCP tool definitions.
func (h *Handler) BuildMCPTools(spec *openapi3.T, safetyConfig *config.SafetyConfig) []mcp.Tool {
	var tools []mcp.Tool

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
			if h.isToolAllowed(tool.Name, safetyConfig) {
				tools = append(tools, tool)
			}
		}
	}

	return tools
}

// convertOperationToTool converts an OpenAPI operation to an MCP tool.
func (h *Handler) convertOperationToTool(method, path string, op *openapi3.Operation) mcp.Tool {
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

	return mcp.Tool{
		Name:        name,
		Description: op.Summary,
		InputSchema: inputSchema,
	}
}

// isToolAllowed checks if a tool is allowed based on safety configuration.
func (h *Handler) isToolAllowed(toolName string, safetyConfig *config.SafetyConfig) bool {
	if safetyConfig == nil {
		return true
	}

	// Check denied list first
	for _, op := range safetyConfig.DeniedOperations {
		if op == toolName {
			return false
		}
	}

	// If allowed list is specified, only include those
	if len(safetyConfig.AllowedOperations) > 0 {
		allowed := false
		for _, op := range safetyConfig.AllowedOperations {
			if op == toolName {
				allowed = true
				break
			}
		}
		if !allowed {
			return false
		}
	}

	return true
}
