package mcp

import (
	"fmt"
	"maps"
	"slices"
	"sync"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/nomagicln/open-bridge/pkg/config"
)

// ToolRegistry stores all tool definitions and manages loaded tool cache.
// It provides the central storage for the progressive disclosure mechanism.
type ToolRegistry struct {
	// allTools stores the full MCP tool definitions indexed by tool ID
	allTools map[string]mcp.Tool

	// metadata stores the summary metadata for each tool (for search)
	metadata []ToolMetadata

	// loadedTools caches tools that have been loaded by the client
	loadedTools map[string]mcp.Tool

	// operationMap maps tool ID to (method, path) for request building
	operationMap map[string]OperationInfo

	mu sync.RWMutex
}

// OperationInfo stores the HTTP method and path for an operation.
type OperationInfo struct {
	Method    string
	Path      string
	Operation *openapi3.Operation
}

// NewToolRegistry creates a new tool registry.
func NewToolRegistry() *ToolRegistry {
	return &ToolRegistry{
		allTools:     make(map[string]mcp.Tool),
		metadata:     make([]ToolMetadata, 0),
		loadedTools:  make(map[string]mcp.Tool),
		operationMap: make(map[string]OperationInfo),
	}
}

// BuildFromSpec populates the registry from an OpenAPI specification.
// It extracts all operations and converts them to tool definitions and metadata.
//
//nolint:funlen // This function handles the complete spec processing workflow.
func (r *ToolRegistry) BuildFromSpec(spec *openapi3.T, safetyConfig *config.SafetyConfig) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	// Clear existing data
	r.allTools = make(map[string]mcp.Tool)
	r.metadata = make([]ToolMetadata, 0)
	r.operationMap = make(map[string]OperationInfo)
	// Note: we don't clear loadedTools to preserve cache across spec reloads

	if spec == nil || spec.Paths == nil {
		return nil
	}

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

			// Generate tool ID (use smart naming to avoid redundancy)
			toolID := GenerateToolName(method, path, op)

			// Check if tool is allowed
			if !isToolAllowedByConfig(toolID, safetyConfig) {
				continue
			}

			// Build full tool definition
			tool := convertOperationToMCPTool(method, path, op)

			// Build metadata for search
			meta := ToolMetadata{
				ID:          toolID,
				Name:        tool.Name,
				Description: tool.Description,
				Method:      method,
				Path:        path,
			}
			if op.Tags != nil {
				meta.Tags = op.Tags
			}

			// Store in registry
			r.allTools[toolID] = tool
			r.metadata = append(r.metadata, meta)
			r.operationMap[toolID] = OperationInfo{
				Method:    method,
				Path:      path,
				Operation: op,
			}
		}
	}

	return nil
}

// convertOperationToMCPTool converts an OpenAPI operation to an MCP tool.
func convertOperationToMCPTool(method, path string, op *openapi3.Operation) mcp.Tool {
	// Use smart naming to generate clean, non-redundant tool names
	name := GenerateToolName(method, path, op)

	properties := make(map[string]any)
	required := []string{} // Initialize as empty slice, not nil (nil becomes null in JSON)

	// Add parameters
	required = addParametersToSchema(op.Parameters, properties, required)

	// Add request body parameters
	required = addRequestBodyToSchema(op.RequestBody, properties, required)

	inputSchema := map[string]any{
		"type":       "object",
		"properties": properties,
		"required":   required,
	}

	return mcp.Tool{
		Name:        name,
		Description: op.Summary,
		InputSchema: inputSchema,
	}
}

// addParametersToSchema extracts parameters from OpenAPI parameters and adds them to the schema.
func addParametersToSchema(params openapi3.Parameters, properties map[string]any, required []string) []string {
	for _, paramRef := range params {
		param := paramRef.Value
		propSchema := map[string]any{
			"type":        "string",
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
	return required
}

// addRequestBodyToSchema extracts properties from request body and adds them to the schema.
func addRequestBodyToSchema(reqBody *openapi3.RequestBodyRef, properties map[string]any, required []string) []string {
	if reqBody == nil || reqBody.Value == nil || reqBody.Value.Content == nil {
		return required
	}

	mediaType, ok := reqBody.Value.Content["application/json"]
	if !ok || mediaType.Schema == nil || mediaType.Schema.Value == nil {
		return required
	}

	schema := mediaType.Schema.Value
	if schema.Properties == nil {
		return required
	}

	for propName, propRef := range schema.Properties {
		propSchema := buildPropertySchema(propRef)
		properties[propName] = propSchema
	}

	// Add required fields from request body
	required = append(required, schema.Required...)
	return required
}

// buildPropertySchema creates a property schema from an OpenAPI schema reference.
func buildPropertySchema(propRef *openapi3.SchemaRef) map[string]any {
	propSchema := map[string]any{
		"type": "string",
	}

	if propRef.Value == nil {
		return propSchema
	}

	if propRef.Value.Description != "" {
		propSchema["description"] = propRef.Value.Description
	}

	if propRef.Value.Type != nil {
		types := *propRef.Value.Type
		if len(types) > 0 {
			propSchema["type"] = types[0]
		}
	}

	return propSchema
}

// isToolAllowedByConfig checks if a tool is allowed based on safety configuration.
func isToolAllowedByConfig(toolName string, safetyConfig *config.SafetyConfig) bool {
	if safetyConfig == nil {
		return true
	}

	// Check denied list first
	if slices.Contains(safetyConfig.DeniedOperations, toolName) {
		return false
	}

	// If allowed list is specified, only include those
	if len(safetyConfig.AllowedOperations) > 0 {
		return slices.Contains(safetyConfig.AllowedOperations, toolName)
	}

	return true
}

// GetMetadata returns all tool metadata for indexing in search engines.
func (r *ToolRegistry) GetMetadata() []ToolMetadata {
	r.mu.RLock()
	defer r.mu.RUnlock()

	result := make([]ToolMetadata, len(r.metadata))
	copy(result, r.metadata)
	return result
}

// Load retrieves the full tool definition by ID and caches it.
// Returns the tool, whether it was from cache, and any error.
func (r *ToolRegistry) Load(toolID string) (mcp.Tool, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	// Check cache first
	if tool, ok := r.loadedTools[toolID]; ok {
		return tool, true, nil
	}

	// Get from all tools
	tool, ok := r.allTools[toolID]
	if !ok {
		return mcp.Tool{}, false, fmt.Errorf("tool not found: %s", toolID)
	}

	// Cache the loaded tool
	r.loadedTools[toolID] = tool

	return tool, false, nil
}

// IsLoaded checks if a tool has been loaded (cached).
func (r *ToolRegistry) IsLoaded(toolID string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()

	_, ok := r.loadedTools[toolID]
	return ok
}

// GetLoadedTools returns all currently loaded (cached) tools.
func (r *ToolRegistry) GetLoadedTools() map[string]mcp.Tool {
	r.mu.RLock()
	defer r.mu.RUnlock()

	result := make(map[string]mcp.Tool, len(r.loadedTools))
	maps.Copy(result, r.loadedTools)
	return result
}

// GetOperationInfo returns the operation info for a tool ID.
func (r *ToolRegistry) GetOperationInfo(toolID string) (OperationInfo, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	info, ok := r.operationMap[toolID]
	return info, ok
}

// GetTool returns a tool definition by ID without caching.
func (r *ToolRegistry) GetTool(toolID string) (mcp.Tool, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	tool, ok := r.allTools[toolID]
	return tool, ok
}

// ClearCache clears the loaded tools cache.
func (r *ToolRegistry) ClearCache() {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.loadedTools = make(map[string]mcp.Tool)
}

// ToolCount returns the total number of tools in the registry.
func (r *ToolRegistry) ToolCount() int {
	r.mu.RLock()
	defer r.mu.RUnlock()

	return len(r.allTools)
}

// LoadedCount returns the number of loaded (cached) tools.
func (r *ToolRegistry) LoadedCount() int {
	r.mu.RLock()
	defer r.mu.RUnlock()

	return len(r.loadedTools)
}
