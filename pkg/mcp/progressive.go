package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/nomagicln/open-bridge/pkg/config"
	"github.com/nomagicln/open-bridge/pkg/request"
)

// MetaToolName constants for the three meta-tools.
const (
	MetaToolSearchTools = "SearchTools"
	MetaToolLoad        = "LoadTool"
	MetaToolInvoke      = "InvokeTool"
)

// ProgressiveHandler implements the progressive disclosure mechanism.
// Instead of exposing all tools at once, it provides three meta-tools:
// - SearchTools: Find tools by query
// - LoadTool: Load full tool definition (cached)
// - InvokeTool: Execute a tool by ID
type ProgressiveHandler struct {
	registry       *ToolRegistry
	searchEngine   ToolSearchEngine
	requestBuilder *request.Builder
	httpClient     *http.Client
	spec           *openapi3.T
	appConfig      *config.AppConfig
	profileName    string
}

// NewProgressiveHandler creates a new progressive disclosure handler.
func NewProgressiveHandler(
	requestBuilder *request.Builder,
	httpClient *http.Client,
	engineType SearchEngineType,
) (*ProgressiveHandler, error) {
	if httpClient == nil {
		httpClient = &http.Client{}
	}

	engine, err := NewSearchEngine(engineType)
	if err != nil {
		return nil, fmt.Errorf("failed to create search engine: %w", err)
	}

	return &ProgressiveHandler{
		registry:       NewToolRegistry(),
		searchEngine:   engine,
		requestBuilder: requestBuilder,
		httpClient:     httpClient,
	}, nil
}

// SetSpec sets the OpenAPI specification and indexes tools.
func (h *ProgressiveHandler) SetSpec(spec *openapi3.T, safetyConfig *config.SafetyConfig) error {
	h.spec = spec

	// Build registry from spec
	if err := h.registry.BuildFromSpec(spec, safetyConfig); err != nil {
		return fmt.Errorf("failed to build registry: %w", err)
	}

	// Index metadata in search engine
	metadata := h.registry.GetMetadata()
	if err := h.searchEngine.Index(metadata); err != nil {
		return fmt.Errorf("failed to index tools: %w", err)
	}

	return nil
}

// SetAppConfig sets the app configuration.
func (h *ProgressiveHandler) SetAppConfig(appCfg *config.AppConfig, profileName string) {
	h.appConfig = appCfg
	h.profileName = profileName
}

// Register registers the three meta-tools with the MCP server.
func (h *ProgressiveHandler) Register(s *mcp.Server) {
	// Register SearchTools
	searchTool := h.buildSearchToolDefinition()
	s.AddTool(&searchTool, h.handleSearchTools)

	// Register LoadTool
	loadTool := h.buildLoadToolDefinition()
	s.AddTool(&loadTool, h.handleLoadTool)

	// Register InvokeTool
	invokeTool := h.buildInvokeToolDefinition()
	s.AddTool(&invokeTool, h.handleInvokeTool)
}

// buildSearchToolDefinition creates the SearchTools tool definition.
func (h *ProgressiveHandler) buildSearchToolDefinition() mcp.Tool {
	description := fmt.Sprintf(`Search for available API tools. %s

Example: %s

Returns a list of matching tools with their ID, name, description, HTTP method, and path.
Use LoadTool to get the full definition of a specific tool before invoking it.`,
		h.searchEngine.GetDescription(),
		h.searchEngine.GetQueryExample(),
	)

	return mcp.Tool{
		Name:        MetaToolSearchTools,
		Description: description,
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"query": map[string]any{
					"type":        "string",
					"description": "Search query to find matching tools. Leave empty to list all available tools.",
				},
			},
			"required": []string{},
		},
	}
}

// buildLoadToolDefinition creates the LoadTool tool definition.
func (h *ProgressiveHandler) buildLoadToolDefinition() mcp.Tool {
	return mcp.Tool{
		Name: MetaToolLoad,
		Description: `Load the full definition of a tool by its ID.
This retrieves the complete tool schema including all parameters and their types.
Loaded tools are cached for faster subsequent access.
You must load a tool before invoking it with InvokeTool.`,
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"toolId": map[string]any{
					"type":        "string",
					"description": "The unique ID of the tool to load (from SearchTools results).",
				},
			},
			"required": []string{"toolId"},
		},
	}
}

// buildInvokeToolDefinition creates the InvokeTool tool definition.
func (h *ProgressiveHandler) buildInvokeToolDefinition() mcp.Tool {
	return mcp.Tool{
		Name: MetaToolInvoke,
		Description: `Invoke a loaded tool with the specified arguments.
The tool must be loaded first using LoadTool.
Arguments should match the tool's input schema.`,
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"toolId": map[string]any{
					"type":        "string",
					"description": "The unique ID of the tool to invoke.",
				},
				"arguments": map[string]any{
					"type":        "object",
					"description": "The arguments to pass to the tool, matching its input schema.",
				},
			},
			"required": []string{"toolId"},
		},
	}
}

// handleSearchTools handles SearchTools requests.
func (h *ProgressiveHandler) handleSearchTools(_ context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	var args struct {
		Query string `json:"query"`
	}

	if len(req.Params.Arguments) > 0 {
		if err := json.Unmarshal(req.Params.Arguments, &args); err != nil {
			return errorResultProg("Invalid arguments: %v", err), nil
		}
	}

	results, err := h.searchEngine.Search(args.Query)
	if err != nil {
		return errorResultProg("Search failed: %v", err), nil
	}

	// Format results
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Found %d tool(s):\n\n", len(results)))

	for _, tool := range results {
		sb.WriteString(fmt.Sprintf("**%s** (`%s`)\n", tool.Name, tool.ID))
		sb.WriteString(fmt.Sprintf("  %s %s\n", tool.Method, tool.Path))
		if tool.Description != "" {
			sb.WriteString(fmt.Sprintf("  %s\n", tool.Description))
		}
		if len(tool.Tags) > 0 {
			sb.WriteString(fmt.Sprintf("  Tags: %s\n", strings.Join(tool.Tags, ", ")))
		}
		sb.WriteString("\n")
	}

	if len(results) == 0 {
		sb.WriteString("No tools found matching your query.\n")
	}

	return &mcp.CallToolResult{
		Content: []mcp.Content{
			&mcp.TextContent{Text: sb.String()},
		},
	}, nil
}

// handleLoadTool handles LoadTool requests.
//
//nolint:funlen // This function handles the complete load workflow including validation, caching, and response formatting.
func (h *ProgressiveHandler) handleLoadTool(_ context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	var args struct {
		ToolID string `json:"toolId"`
	}

	if err := json.Unmarshal(req.Params.Arguments, &args); err != nil {
		return errorResultProg("Invalid arguments: %v", err), nil
	}

	if args.ToolID == "" {
		return errorResultProg("toolId is required"), nil
	}

	tool, fromCache, err := h.registry.Load(args.ToolID)
	if err != nil {
		return errorResultProg("Failed to load tool: %v", err), nil
	}

	// Format tool definition
	var sb strings.Builder
	if fromCache {
		sb.WriteString(fmt.Sprintf("Tool **%s** (from cache):\n\n", tool.Name))
	} else {
		sb.WriteString(fmt.Sprintf("Tool **%s** (newly loaded):\n\n", tool.Name))
	}

	sb.WriteString(fmt.Sprintf("**Description:** %s\n\n", tool.Description))
	sb.WriteString("**Parameters:**\n")

	// Extract and format input schema
	inputSchema, ok := tool.InputSchema.(map[string]any)
	if !ok {
		sb.WriteString("(No parameters defined)\n")
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: sb.String()},
			},
		}, nil
	}

	if props, ok := inputSchema["properties"].(map[string]any); ok {
		required := []string{}
		if req, ok := inputSchema["required"].([]string); ok {
			required = req
		}

		for name, propAny := range props {
			prop, ok := propAny.(map[string]any)
			if !ok {
				continue
			}

			typeStr := "any"
			if t, ok := prop["type"].(string); ok {
				typeStr = t
			}

			isRequired := slices.Contains(required, name)

			reqMarker := ""
			if isRequired {
				reqMarker = " (required)"
			}

			desc := ""
			if d, ok := prop["description"].(string); ok {
				desc = d
			}

			sb.WriteString(fmt.Sprintf("- `%s` (%s)%s: %s\n", name, typeStr, reqMarker, desc))
		}
	}

	return &mcp.CallToolResult{
		Content: []mcp.Content{
			&mcp.TextContent{Text: sb.String()},
		},
	}, nil
}

// handleInvokeTool handles InvokeTool requests.
func (h *ProgressiveHandler) handleInvokeTool(_ context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	var args struct {
		ToolID    string         `json:"toolId"`
		Arguments map[string]any `json:"arguments"`
	}

	if err := json.Unmarshal(req.Params.Arguments, &args); err != nil {
		return errorResultProg("Invalid arguments: %v", err), nil
	}

	if args.ToolID == "" {
		return errorResultProg("toolId is required"), nil
	}

	// Check if tool is loaded
	if !h.registry.IsLoaded(args.ToolID) {
		return errorResultProg("Tool '%s' is not loaded. Use LoadTool first.", args.ToolID), nil
	}

	// Get operation info
	opInfo, ok := h.registry.GetOperationInfo(args.ToolID)
	if !ok {
		return errorResultProg("Operation info not found for tool: %s", args.ToolID), nil
	}

	// Get active profile
	profileName, profile, err := h.getActiveProfile()
	if err != nil {
		return nil, err
	}

	// Build and execute request
	return h.buildAndExecuteRequest(opInfo.Operation, opInfo.Method, opInfo.Path, args.Arguments, profileName, profile)
}

// getActiveProfile returns the active profile name and configuration.
func (h *ProgressiveHandler) getActiveProfile() (string, *config.Profile, error) {
	return getActiveProfile(h.appConfig, h.profileName)
}

// buildAndExecuteRequest builds and executes the HTTP request.
func (h *ProgressiveHandler) buildAndExecuteRequest(
	operation *openapi3.Operation,
	method, path string,
	arguments map[string]any,
	profileName string,
	profile *config.Profile,
) (*mcp.CallToolResult, error) {
	var requestBody *openapi3.RequestBody
	if operation.RequestBody != nil {
		requestBody = operation.RequestBody.Value
	}

	httpReq, err := h.requestBuilder.BuildRequest(method, path, profile.BaseURL, arguments, operation.Parameters, requestBody)
	if err != nil {
		return errorResultProg("Failed to build request: %v", err), nil
	}

	if err := h.requestBuilder.InjectAuth(httpReq, h.appConfig.Name, profileName, &profile.Auth); err != nil {
		return nil, fmt.Errorf("failed to inject authentication: %w", err)
	}

	for key, value := range profile.Headers {
		httpReq.Header.Set(key, value)
	}

	httpResp, err := h.httpClient.Do(httpReq)
	if err != nil {
		return errorResultProg("Failed to execute API call: %v", err), nil
	}
	defer func() { _ = httpResp.Body.Close() }()

	bodyBytes, err := io.ReadAll(httpResp.Body)
	if err != nil {
		return errorResultProg("Failed to read response body: %v", err), nil
	}

	return formatMCPResult(httpResp.StatusCode, bodyBytes), nil
}

// errorResultProg creates an MCP error result.
func errorResultProg(format string, args ...any) *mcp.CallToolResult {
	return &mcp.CallToolResult{
		Content: []mcp.Content{
			&mcp.TextContent{Text: fmt.Sprintf(format, args...)},
		},
		IsError: true,
	}
}

// Close releases resources held by the handler.
func (h *ProgressiveHandler) Close() error {
	if h.searchEngine != nil {
		return h.searchEngine.Close()
	}
	return nil
}

// GetRegistry returns the tool registry for testing and inspection.
func (h *ProgressiveHandler) GetRegistry() *ToolRegistry {
	return h.registry
}

// GetSearchEngine returns the search engine for testing and inspection.
func (h *ProgressiveHandler) GetSearchEngine() ToolSearchEngine {
	return h.searchEngine
}
