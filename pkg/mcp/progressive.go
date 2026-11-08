package mcp

import (
	"context"
	"encoding/json"
	"fmt"
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
//
// These are legacy aliases. The names registered with the MCP server are scoped
// by the PascalCase app name — Search<App>Tools, Load<App>Tool and
// Invoke<App>Tool — so none of the constants below matches a registered tool.
// They are kept so existing Go callers keep compiling; agents must read the
// names from the MCP tools/list response.
const (
	MetaToolSearchTools = "SearchTools"
	MetaToolLoad        = "LoadTool"
	MetaToolInvoke      = "InvokeTool"
)

// searchToolDescTemplate is the template for building the search meta-tool description.
const searchToolDescTemplate = `Search for available API tools in {{.AppName}} using intelligent tool discovery.

⚠️ IMPORTANT: Always use specific search queries to filter results efficiently. Avoid listing all tools - use the search strategy below to find exactly what you need.{{if .AppDescription}}

{{.AppName}} Description: {{.AppDescription}}{{end}}

Searching Strategy:
{{.SearchStrategy}}

Best Practices:
{{.BestPractices}}

Examples:
{{- range .Examples}}
- {{.}}
{{- end}}

Returns a list of matching tools with their ID, name, and description.
Use Load{{.AppName}}Tool to get the full definition of a specific {{.AppName}} tool before invoking it with Invoke{{.AppName}}Tool.`

// loadToolDescTemplate is the template for building the load meta-tool description.
const loadToolDescTemplate = `Load the full definition of a {{.AppName}} API tool by its ID from Search{{.AppName}}Tools results.
This retrieves the complete tool schema including all parameters and their types.
Loaded tools are cached for faster subsequent access.
You must load a {{.AppName}} tool before invoking it with Invoke{{.AppName}}Tool.`

// invokeToolDescTemplate is the template for building the invoke meta-tool description.
const invokeToolDescTemplate = `Invoke a {{.AppName}} API tool with the specified arguments.
The tool must be loaded first using Load{{.AppName}}Tool.
Arguments should match the tool's input schema as returned by Load{{.AppName}}Tool.`

// ProgressiveHandler implements the progressive disclosure mechanism.
// Instead of exposing all tools at once, it provides three meta-tools, whose
// names are scoped by the PascalCase app name:
// - Search<App>Tools: Find tools by query
// - Load<App>Tool: Load full tool definition (cached)
// - Invoke<App>Tool: Execute a tool by ID
type ProgressiveHandler struct {
	apiExecutor

	registry     *ToolRegistry
	searchEngine ToolSearchEngine
	spec         *openapi3.T
	safety       *safetyGuard
	catalogErr   error
}

// NewProgressiveHandler creates a new progressive disclosure handler.
func NewProgressiveHandler(
	requestBuilder *request.Builder,
	httpClient *http.Client,
	engineType SearchEngineType,
) (*ProgressiveHandler, error) {
	engine, err := NewSearchEngine(engineType)
	if err != nil {
		return nil, fmt.Errorf("failed to create search engine: %w", err)
	}

	handler := &ProgressiveHandler{
		apiExecutor:  newAPIExecutor(requestBuilder, httpClient),
		registry:     NewToolRegistry(),
		searchEngine: engine,
	}

	return handler, nil
}

// SetSpec sets the OpenAPI specification and indexes tools.
func (h *ProgressiveHandler) SetSpec(spec *openapi3.T, safetyConfig *config.SafetyConfig) (err error) {
	defer func() {
		h.catalogErr = err
		if err != nil {
			_ = h.registry.BuildFromSpec(nil, nil)
			_ = h.searchEngine.Index(nil)
		}
	}()
	guard, err := newSafetyGuard(safetyConfig)
	if err != nil {
		return fmt.Errorf("invalid safety configuration: %w", err)
	}
	h.safety = guard
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
	// Register the search meta-tool.
	searchTool := h.buildSearchToolDefinition()
	s.AddTool(&searchTool, h.handleSearchTools)

	// Register the load meta-tool.
	loadTool := h.buildLoadToolDefinition()
	s.AddTool(&loadTool, h.handleLoadTool)

	// Register the invoke meta-tool.
	invokeTool := h.buildInvokeToolDefinition()
	s.AddTool(&invokeTool, h.handleInvokeTool)
}

// buildSearchToolDefinition creates the search meta-tool definition.
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
		"BestPractices":  h.searchEngine.GetBestPractices(),
		"Examples":       h.searchEngine.GetExamples(),
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
					"description": "Search query to filter tools. Refer to the search strategy documented above for the appropriate query syntax and best practices. Strongly prefer using a focused query over listing all tools.",
				},
			},
			"required": []string{},
		},
	}
}

// buildLoadToolDefinition creates the load meta-tool definition.
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

// buildInvokeToolDefinition creates the invoke meta-tool definition.
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
				confirmArgumentName: map[string]any{
					"type":        "boolean",
					"description": "Optional. Set to true to confirm the invoked operation when the MCP safety policy requires confirmation (require_confirm). It is never forwarded to the API.",
				},
			},
			"required": []string{"toolId"},
		},
	}
}

// handleSearchTools handles search meta-tool requests.
// formatSearchResults formats search results into a string.
func formatSearchResults(results []ToolMetadata) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "Found %d tool(s):\n\n", len(results))

	for _, tool := range results {
		fmt.Fprintf(&sb, "**%s** (`%s`)\n", tool.Name, tool.ID)
		if tool.Description != "" {
			fmt.Fprintf(&sb, "  %s\n", tool.Description)
		}
		sb.WriteString("\n")
	}

	if len(results) == 0 {
		sb.WriteString("No tools found matching your query.\n")
	}

	return sb.String()
}

func (h *ProgressiveHandler) handleSearchTools(_ context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if h.catalogErr != nil {
		return errorResult("MCP tool catalog unavailable: %v", h.catalogErr), nil
	}
	var args struct {
		Query string `json:"query"`
	}

	if len(req.Params.Arguments) > 0 {
		if err := json.Unmarshal(req.Params.Arguments, &args); err != nil {
			return errorResult("Invalid arguments: %v", err), nil
		}
	}

	results, err := h.searchEngine.Search(args.Query)
	if err != nil {
		return errorResult("Search failed: %v", err), nil
	}

	return &mcp.CallToolResult{
		Content: []mcp.Content{
			&mcp.TextContent{Text: formatSearchResults(results)},
		},
	}, nil
}

// formatToolParameters extracts and formats parameters from input schema.
func formatToolParameters(inputSchema map[string]any, sb *strings.Builder) {
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

			fmt.Fprintf(sb, "- `%s` (%s)%s: %s\n", name, typeStr, reqMarker, desc)
		}
	}
}

// formatToolDefinition formats tool definition into a string.
func formatToolDefinition(tool mcp.Tool, fromCache bool) string {
	var sb strings.Builder
	if fromCache {
		fmt.Fprintf(&sb, "Tool **%s** (from cache):\n\n", tool.Name)
	} else {
		fmt.Fprintf(&sb, "Tool **%s** (newly loaded):\n\n", tool.Name)
	}

	fmt.Fprintf(&sb, "**Description:** %s\n\n", tool.Description)
	sb.WriteString("**Parameters:**\n")

	inputSchema, ok := tool.InputSchema.(map[string]any)
	if !ok {
		sb.WriteString("(No parameters defined)\n")
		return sb.String()
	}

	formatToolParameters(inputSchema, &sb)
	return sb.String()
}

// handleLoadTool handles load meta-tool requests.
func (h *ProgressiveHandler) handleLoadTool(_ context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if h.catalogErr != nil {
		return errorResult("MCP tool catalog unavailable: %v", h.catalogErr), nil
	}
	var args struct {
		ToolID string `json:"toolId"`
	}

	if err := json.Unmarshal(req.Params.Arguments, &args); err != nil {
		return errorResult("Invalid arguments: %v", err), nil
	}

	if args.ToolID == "" {
		return errorResult("toolId is required"), nil
	}

	tool, fromCache, err := h.registry.Load(args.ToolID)
	if err != nil {
		return errorResult("Failed to load tool: %v", err), nil
	}

	return &mcp.CallToolResult{
		Content: []mcp.Content{
			&mcp.TextContent{Text: formatToolDefinition(tool, fromCache)},
		},
	}, nil
}

// handleInvokeTool handles invoke meta-tool requests.
func (h *ProgressiveHandler) handleInvokeTool(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if h.catalogErr != nil {
		return errorResult("MCP tool catalog unavailable: %v", h.catalogErr), nil
	}
	var args struct {
		ToolID    string         `json:"toolId"`
		Arguments map[string]any `json:"arguments"`

		// Confirm acknowledges an operation listed in SafetyConfig.RequireConfirm.
		Confirm bool `json:"_confirm"`
	}

	if err := json.Unmarshal(req.Params.Arguments, &args); err != nil {
		return errorResult("Invalid arguments: %v", err), nil
	}

	if args.ToolID == "" {
		return errorResult("toolId is required"), nil
	}

	if args.Arguments == nil {
		args.Arguments = map[string]any{}
	}

	// _confirm may be passed at the top level or inside arguments; it is
	// invocation-only and never forwarded to the API.
	confirmed := args.Confirm || confirmRequested(args.Arguments)
	stripConfirmArgument(args.Arguments)

	// Check if tool is loaded
	if !h.registry.IsLoaded(args.ToolID) {
		return errorResult("Tool '%s' is not loaded. Use Load%sTool first.", args.ToolID, h.formatAppName()), nil
	}

	// Get operation info
	opInfo, ok := h.registry.GetOperationInfo(args.ToolID)
	if !ok {
		return errorResult("Operation info not found for tool: %s", args.ToolID), nil
	}

	if safetyResult := h.safety.checkInvocation(args.ToolID, opInfo.Method, opInfo.Path, confirmed); safetyResult != nil {
		return safetyResult, nil
	}

	// Get active profile
	profileName, profile, err := h.getActiveProfile()
	if err != nil {
		return nil, err
	}

	// Build and execute request
	return h.buildAndExecuteRequest(ctx, opInfo.Operation, opInfo.Method, opInfo.Path, args.Arguments, profileName, profile)
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
