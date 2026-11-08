// Package mcp provides MCP (Model Context Protocol) server functionality.
package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/nomagicln/open-bridge/pkg/config"
	"github.com/nomagicln/open-bridge/pkg/request"
	"github.com/nomagicln/open-bridge/pkg/semantic"
)

// Handler implements the MCP protocol for AI agent integration.
type Handler struct {
	apiExecutor

	mapper       *semantic.Mapper
	spec         *openapi3.T
	safety       *safetyGuard
	safetyErr    error
	safetyConfig *config.SafetyConfig
	registry     *ToolRegistry
	catalogErr   error
}

// defaultHTTPTimeout bounds every MCP tool call so a slow or unreachable API
// cannot hang the server indefinitely. It matches the parser/cache/CLI clients.
const defaultHTTPTimeout = 30 * time.Second

// NewHandler creates a new MCP handler.
func NewHandler(mapper *semantic.Mapper, requestBuilder *request.Builder, httpClient *http.Client) *Handler {
	return &Handler{
		apiExecutor: newAPIExecutor(requestBuilder, httpClient),
		mapper:      mapper,
		registry:    NewToolRegistry(),
	}
}

// profileClientCache caches HTTP clients built from profile timeout/TLS settings.
type profileClientCache struct {
	mu      sync.Mutex
	clients map[string]*http.Client
}

// newProfileClientCache creates an empty per-profile client cache.
func newProfileClientCache() *profileClientCache {
	return &profileClientCache{clients: make(map[string]*http.Client)}
}

// get returns the cached client for profileName, building it on first use.
// Construction failures (e.g. invalid TLS configuration) are returned to the
// caller and are not cached.
func (c *profileClientCache) get(profileName string, profile *config.Profile) (*http.Client, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if client, ok := c.clients[profileName]; ok {
		return client, nil
	}

	client, err := request.NewHTTPClient(profile)
	if err != nil {
		return nil, err
	}
	c.clients[profileName] = client

	return client, nil
}

// SetSpec sets the OpenAPI specification for the handler.
func (h *Handler) SetSpec(spec *openapi3.T) {
	h.spec = spec
	_ = h.rebuildCatalog()
}

// rebuildCatalog configures the binding before serving; listing tools never
// replaces this binding. As with SetSpec, configuration precedes concurrent calls.
func (h *Handler) rebuildCatalog() error {
	if h.registry == nil {
		h.registry = NewToolRegistry()
	}
	h.catalogErr = h.registry.BuildFromSpec(h.spec, h.safetyConfig)
	return h.catalogErr
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
	h.safetyConfig = cfg
	if err := h.rebuildCatalog(); err != nil {
		return fmt.Errorf("invalid MCP tool catalog: %w", err)
	}
	return nil
}

// GetRequestBuilder returns the request builder used by the handler.
func (h *Handler) GetRequestBuilder() *request.Builder {
	return h.requestBuilder
}

// Register preserves the original registration API. Failed configuration is
// reported and all subsequent calls fail closed. Use RegisterChecked at startup
// when the caller can propagate errors.
func (h *Handler) Register(s *mcp.Server, safetyConfig *config.SafetyConfig) {
	if err := h.RegisterChecked(s, safetyConfig); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: MCP registration failed: %v\n", err)
	}
}

// RegisterChecked registers the authorized catalog, or returns its validation
// error before any tools are added to the server.
func (h *Handler) RegisterChecked(s *mcp.Server, safetyConfig *config.SafetyConfig) error {
	if err := h.SetSafetyConfig(safetyConfig); err != nil {
		return fmt.Errorf("register MCP tools: %w", err)
	}
	for _, meta := range h.registry.GetMetadata() {
		tool, ok := h.registry.GetTool(meta.ID)
		if ok {
			s.AddTool(&tool, h.HandleCallTool)
		}
	}
	return nil
}

// GetTools returns the list of available tools.
func (h *Handler) GetTools(safetyConfig *config.SafetyConfig) []mcp.Tool {
	if h.spec == nil {
		return []mcp.Tool{}
	}
	tools := h.BuildMCPTools(h.spec, safetyConfig)
	return tools
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

// HandleCallTool handles tool execution requests.
func (h *Handler) HandleCallTool(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
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

	if err := h.catalogError(); err != nil {
		return errorResult("Error mapping tool: %v", err), nil
	}
	info, ok := h.registry.GetOperationInfo(req.Params.Name)
	if !ok {
		return errorResult("operation not found for published tool: %s", req.Params.Name), nil
	}

	if safetyResult := h.safety.checkInvocation(req.Params.Name, info.Method, info.Path, confirmed); safetyResult != nil {
		return safetyResult, nil
	}

	profileName, profile, err := h.getActiveProfile()
	if err != nil {
		return nil, err
	}

	return h.buildAndExecuteRequest(ctx, info.Operation, info.Method, info.Path, arguments, profileName, profile)
}

// MapToolToOperation resolves an authorized tool, retaining unique operationId
// aliases for existing library callers. Protocol calls accept published IDs only.
func (h *Handler) MapToolToOperation(toolName string) (*openapi3.Operation, string, string, error) {
	if err := h.catalogError(); err != nil {
		return nil, "", "", err
	}
	if info, ok := h.registry.GetOperationInfo(toolName); ok {
		return info.Operation, info.Method, info.Path, nil
	}
	var matched *OperationInfo
	for _, meta := range h.registry.GetMetadata() {
		info, ok := h.registry.GetOperationInfo(meta.ID)
		if !ok || info.Operation.OperationID != toolName {
			continue
		}
		if matched != nil {
			return nil, "", "", fmt.Errorf("ambiguous operationId alias: %s", toolName)
		}
		matched = &info
	}
	if matched != nil {
		return matched.Operation, matched.Method, matched.Path, nil
	}
	return nil, "", "", fmt.Errorf("operation not found for tool: %s", toolName)
}

func (h *Handler) catalogError() error {
	if h.safetyErr != nil {
		return h.safetyErr
	}
	if h.catalogErr != nil {
		return fmt.Errorf("MCP tool catalog unavailable: %w", h.catalogErr)
	}
	if h.spec == nil || h.registry == nil {
		return fmt.Errorf("OpenAPI spec not loaded")
	}
	return nil
}

// FormatMCPResult formats an API response into MCP result format.
func (h *Handler) FormatMCPResult(statusCode int, bodyBytes []byte) *mcp.CallToolResult {
	return formatMCPResult(statusCode, bodyBytes)
}

// buildMCPTools converts OpenAPI operations to MCP tool definitions.
func (h *Handler) BuildMCPTools(spec *openapi3.T, safetyConfig *config.SafetyConfig) []mcp.Tool {
	registry := NewToolRegistry()
	if err := registry.BuildFromSpec(spec, safetyConfig); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: MCP tools unavailable: %v\n", err)
		return []mcp.Tool{}
	}
	tools := make([]mcp.Tool, 0, registry.ToolCount())
	for _, meta := range registry.GetMetadata() {
		if tool, ok := registry.GetTool(meta.ID); ok {
			tools = append(tools, tool)
		}
	}
	return tools
}
