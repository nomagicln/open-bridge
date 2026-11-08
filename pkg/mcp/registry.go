package mcp

import (
	"fmt"
	"maps"
	"slices"
	"sync"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/nomagicln/open-bridge/internal/openapi"
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
func (r *ToolRegistry) BuildFromSpec(spec *openapi3.T, safetyConfig *config.SafetyConfig) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	next := NewToolRegistry()
	if spec != nil && spec.Paths != nil {
		if err := next.processSpecPaths(spec.Paths.Map(), safetyConfig); err != nil {
			r.clearRegistry()
			return fmt.Errorf("build tool catalog: %w", err)
		}
	}

	r.allTools = next.allTools
	r.metadata = next.metadata
	r.operationMap = next.operationMap
	r.loadedTools = next.loadedTools
	return nil
}

// clearRegistry clears the registry data.
func (r *ToolRegistry) clearRegistry() {
	r.allTools = make(map[string]mcp.Tool)
	r.metadata = make([]ToolMetadata, 0)
	r.operationMap = make(map[string]OperationInfo)
	r.loadedTools = make(map[string]mcp.Tool)
}

// processSpecPaths uses a stable order so collisions have reproducible diagnostics.
func (r *ToolRegistry) processSpecPaths(paths map[string]*openapi3.PathItem, safetyConfig *config.SafetyConfig) error {
	for _, path := range slices.Sorted(maps.Keys(paths)) {
		if err := r.processPathItem(path, paths[path], safetyConfig); err != nil {
			return err
		}
	}
	return nil
}

// processPathItem processes a single path item and its operations.
func (r *ToolRegistry) processPathItem(path string, pathItem *openapi3.PathItem, safetyConfig *config.SafetyConfig) error {
	if pathItem == nil {
		return nil
	}
	operations := pathItem.Operations()
	for _, method := range []string{"GET", "POST", "PUT", "PATCH", "DELETE"} {
		if err := r.processOperation(path, method, openapi.EffectiveOperation(pathItem, operations[method]), safetyConfig); err != nil {
			return err
		}
	}
	return nil
}

// processOperation filters before checking identity, so hidden operations cannot
// invalidate a unique authorized tool or become a dispatch target.
func (r *ToolRegistry) processOperation(path, method string, op *openapi3.Operation, safetyConfig *config.SafetyConfig) error {
	if op == nil || !r.isOperationAllowed(method, safetyConfig) {
		return nil
	}
	toolID := GenerateToolName(method, path, op)
	if !isToolAllowedByConfig(toolID, safetyConfig) {
		return nil
	}
	if previous, exists := r.operationMap[toolID]; exists {
		return fmt.Errorf("ambiguous tool %q: %s %s (operationId %q) conflicts with %s %s (operationId %q); use unique operationIds",
			toolID, previous.Method, previous.Path, previous.Operation.OperationID, method, path, op.OperationID)
	}
	r.registerTool(path, method, op, toolID)
	return nil
}

// isOperationAllowed checks if an operation is allowed by safety config.
func (r *ToolRegistry) isOperationAllowed(method string, safetyConfig *config.SafetyConfig) bool {
	if safetyConfig != nil && safetyConfig.ReadOnlyMode && method != "GET" {
		return false
	}
	return true
}

// registerTool registers a tool in the registry.
func (r *ToolRegistry) registerTool(path, method string, op *openapi3.Operation, toolID string) {
	tool := convertOperationToMCPTool(method, path, op)
	meta := r.buildToolMetadata(toolID, tool, method, path)

	if op != nil && op.Tags != nil {
		meta.Tags = op.Tags
	}

	r.allTools[toolID] = tool
	r.metadata = append(r.metadata, meta)
	r.operationMap[toolID] = OperationInfo{
		Method:    method,
		Path:      path,
		Operation: op,
	}
}

// buildToolMetadata creates metadata for a tool.
func (r *ToolRegistry) buildToolMetadata(toolID string, tool mcp.Tool, method, path string) ToolMetadata {
	return ToolMetadata{
		ID:          toolID,
		Name:        tool.Name,
		Description: tool.Description,
		Method:      method,
		Path:        path,
	}
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

	// Optional confirmation flag for operations protected by
	// SafetyConfig.RequireConfirm. It is stripped before the request is built.
	properties[confirmArgumentName] = map[string]any{
		"type":        "boolean",
		"description": "Optional. Set to true to confirm this operation when the MCP safety policy requires confirmation (require_confirm). It is never forwarded to the API.",
	}

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
