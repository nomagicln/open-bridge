package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"
	"text/template"

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

// searchToolDescTemplate is the template for building the SearchTools description.
const searchToolDescTemplate = `Search for available API tools in {{.AppName}} using intelligent tool discovery.{{if .AppDescription}}

{{.AppName}} Description: {{.AppDescription}}{{end}}

Searching Strategy: {{.SearchStrategy}}

Example: {{.Example}}

Returns a list of matching tools from {{.AppName}} with their ID, name, and description.
Use Load{{.AppName}}Tool to get the full definition of a specific {{.AppName}} tool before invoking it with Invoke{{.AppName}}Tool.`

// loadToolDescTemplate is the template for building the LoadTool description.
const loadToolDescTemplate = `Load the full definition of a {{.AppName}} API tool by its ID from Search{{.AppName}}Tools results.
This retrieves the complete tool schema including all parameters and their types.
Loaded tools are cached for faster subsequent access.
You must load a {{.AppName}} tool before invoking it with Invoke{{.AppName}}Tool.`

// invokeToolDescTemplate is the template for building the InvokeTool description.
const invokeToolDescTemplate = `Invoke a {{.AppName}} API tool with the specified arguments.
The tool must be loaded first using Load{{.AppName}}Tool.
Arguments should match the tool's input schema as returned by Load{{.AppName}}Tool.`

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
// Panics if appCfg is nil or appCfg.Name is empty.
func (h *ProgressiveHandler) SetAppConfig(appCfg *config.AppConfig, profileName string) {
	if appCfg == nil {
		panic("appCfg cannot be nil")
	}
	if appCfg.Name == "" {
		panic("appCfg.Name cannot be empty")
	}
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
	appName := h.formatAppName()

	// Parse and execute template
	tpl, err := template.New("searchToolDesc").Parse(searchToolDescTemplate)
	if err != nil {
		// Fallback if template parsing fails
		return mcp.Tool{
			Name: "Search" + appName + "Tools",
			Description: fmt.Sprintf("Search for available API tools in %s using intelligent tool discovery.\n\nSearching Strategy: %s\n\nExample: %s",
				appName,
				h.searchEngine.GetDescription(),
				h.searchEngine.GetQueryExample()),
		}
	}

	data := map[string]any{
		"AppName":        appName,
		"AppDescription": "",
		"SearchStrategy": h.searchEngine.GetDescription(),
		"Example":        h.searchEngine.GetQueryExample(),
	}

	if h.appConfig != nil && h.appConfig.Description != "" {
		data["AppDescription"] = h.appConfig.Description
	}

	var buf strings.Builder
	if err := tpl.Execute(&buf, data); err != nil {
		// Fallback if template execution fails
		return mcp.Tool{
			Name: "Search" + appName + "Tools",
			Description: fmt.Sprintf("Search for available API tools in %s using intelligent tool discovery.\n\nSearching Strategy: %s\n\nExample: %s",
				appName,
				h.searchEngine.GetDescription(),
				h.searchEngine.GetQueryExample()),
		}
	}

	return mcp.Tool{
		Name:        "Search" + appName + "Tools",
		Description: buf.String(),
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
	appName := h.formatAppName()

	// Parse and execute template
	tpl, err := template.New("loadToolDesc").Parse(loadToolDescTemplate)
	if err != nil {
		// Fallback if template parsing fails
		return mcp.Tool{
			Name:        "Load" + appName + "Tool",
			Description: "Load the full definition of an API tool",
		}
	}

	data := map[string]any{
		"AppName": appName,
	}

	var buf strings.Builder
	if err := tpl.Execute(&buf, data); err != nil {
		// Fallback if template execution fails
		return mcp.Tool{
			Name:        "Load" + appName + "Tool",
			Description: "Load the full definition of an API tool",
		}
	}

	return mcp.Tool{
		Name:        "Load" + appName + "Tool",
		Description: buf.String(),
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"toolId": map[string]any{
					"type":        "string",
					"description": fmt.Sprintf("The unique ID of the tool to load (from Search%sTools results).", appName),
				},
			},
			"required": []string{"toolId"},
		},
	}
}

// buildInvokeToolDefinition creates the InvokeTool tool definition.
func (h *ProgressiveHandler) buildInvokeToolDefinition() mcp.Tool {
	appName := h.formatAppName()

	// Parse and execute template
	tpl, err := template.New("invokeToolDesc").Parse(invokeToolDescTemplate)
	if err != nil {
		// Fallback if template parsing fails
		return mcp.Tool{
			Name:        "Invoke" + appName + "Tool",
			Description: "Invoke an API tool",
		}
	}

	data := map[string]any{
		"AppName": appName,
	}

	var buf strings.Builder
	if err := tpl.Execute(&buf, data); err != nil {
		// Fallback if template execution fails
		return mcp.Tool{
			Name:        "Invoke" + appName + "Tool",
			Description: "Invoke an API tool",
		}
	}

	return mcp.Tool{
		Name:        "Invoke" + appName + "Tool",
		Description: buf.String(),
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
	fmt.Fprintf(&sb, "Found %d tool(s):\n\n", len(results))

	for _, tool := range results {
		sb.WriteString(fmt.Sprintf("**%s** (`%s`)\n", tool.Name, tool.ID))
		if tool.Description != "" {
			fmt.Fprintf(&sb, "  %s\n", tool.Description)
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
		fmt.Fprintf(&sb, "Tool **%s** (from cache):\n\n", tool.Name)
	} else {
		fmt.Fprintf(&sb, "Tool **%s** (newly loaded):\n\n", tool.Name)
	}

	fmt.Fprintf(&sb, "**Description:** %s\n\n", tool.Description)
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

// formatAppName converts the app name to PascalCase format.
// Handles special characters like hyphens, underscores, and dots.
// Examples: "petstore" -> "Petstore", "my-api-v2" -> "MyApiV2", "my_api" -> "MyApi".
func (h *ProgressiveHandler) formatAppName() string {
	if h.appConfig == nil || h.appConfig.Name == "" {
		return ""
	}

	name := h.appConfig.Name
	var result strings.Builder
	capitalizeNext := true

	for _, r := range name {
		switch {
		case r == '-' || r == '_' || r == '.':
			// Skip special characters and mark next character for capitalization
			capitalizeNext = true
		case capitalizeNext:
			upper := strings.ToUpper(string(r))
			result.WriteString(upper)
			capitalizeNext = false
		default:
			result.WriteRune(r)
		}
	}

	return result.String()
}
