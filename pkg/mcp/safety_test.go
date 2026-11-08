package mcp

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/nomagicln/open-bridge/pkg/config"
	"github.com/nomagicln/open-bridge/pkg/request"
	"github.com/nomagicln/open-bridge/pkg/semantic"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// recordedRequests records API calls made by an MCP handler during a test.
type recordedRequests struct {
	mu      sync.Mutex
	bodies  []string
	queries []string
	methods []string
	paths   []string
	headers []http.Header
}

func (r *recordedRequests) record(req *http.Request) {
	body, _ := io.ReadAll(req.Body)
	r.mu.Lock()
	defer r.mu.Unlock()
	r.bodies = append(r.bodies, string(body))
	r.queries = append(r.queries, req.URL.RawQuery)
	r.methods = append(r.methods, req.Method)
	r.paths = append(r.paths, req.URL.Path)
	r.headers = append(r.headers, req.Header.Clone())
}

// Count returns the number of recorded API requests.
func (r *recordedRequests) Count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.bodies)
}

// LastBody returns the last recorded request body.
func (r *recordedRequests) LastBody() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.bodies) == 0 {
		return ""
	}
	return r.bodies[len(r.bodies)-1]
}

// LastQuery returns the raw query string of the last recorded request.
func (r *recordedRequests) LastQuery() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.queries) == 0 {
		return ""
	}
	return r.queries[len(r.queries)-1]
}

// LastHeader returns the value of a header on the last recorded request.
func (r *recordedRequests) LastHeader(name string) string {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.headers) == 0 {
		return ""
	}
	return r.headers[len(r.headers)-1].Get(name)
}

// newSafetyMockServer starts an API server that records every request.
func newSafetyMockServer(t *testing.T) (*httptest.Server, *recordedRequests) {
	t.Helper()

	recorder := &recordedRequests{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		recorder.record(r)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	t.Cleanup(server.Close)

	return server, recorder
}

// newHandlerSafetyEnv builds a Handler wired to a mock API with the given safety config.
func newHandlerSafetyEnv(t *testing.T, safety *config.SafetyConfig) (*Handler, *recordedRequests) {
	t.Helper()

	server, recorder := newSafetyMockServer(t)
	handler := NewHandler(semantic.NewMapper(), request.NewBuilder(nil), server.Client())
	handler.SetSpec(createTestOpenAPISpec())
	handler.SetAppConfig(safetyTestAppConfig(server.URL), "default")

	serverSDK := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "1.0.0"}, nil)
	handler.Register(serverSDK, safety)

	return handler, recorder
}

// newProgressiveSafetyEnv builds a ProgressiveHandler wired to a mock API.
func newProgressiveSafetyEnv(t *testing.T, safety *config.SafetyConfig) (*ProgressiveHandler, *recordedRequests) {
	t.Helper()

	server, recorder := newSafetyMockServer(t)
	handler, err := NewProgressiveHandler(request.NewBuilder(nil), server.Client(), SearchEnginePredicate)
	require.NoError(t, err)
	t.Cleanup(func() { _ = handler.Close() })

	handler.SetAppConfig(safetyTestAppConfig(server.URL), "default")
	require.NoError(t, handler.SetSpec(createTestOpenAPISpec(), safety))

	return handler, recorder
}

// safetyTestAppConfig returns an app config pointing at baseURL.
func safetyTestAppConfig(baseURL string) *config.AppConfig {
	return &config.AppConfig{
		Name:           "testapp",
		DefaultProfile: "default",
		Profiles: map[string]config.Profile{
			"default": {Name: "default", BaseURL: baseURL},
		},
	}
}

// callTool invokes a tool on a Handler and requires a handler-level nil error.
func callTool(t *testing.T, handler *Handler, toolName, arguments string) *mcp.CallToolResult {
	t.Helper()

	result, err := handler.HandleCallTool(context.Background(), createCallToolRequest(toolName, arguments))
	require.NoError(t, err)
	require.NotNil(t, result)

	return result
}

// loadProgressiveTool loads a tool through the ProgressiveHandler meta-tool.
func loadProgressiveTool(t *testing.T, handler *ProgressiveHandler, toolID string) {
	t.Helper()

	result, err := handler.handleLoadTool(context.Background(),
		createCallToolRequest(MetaToolLoad, fmt.Sprintf(`{"toolId":%q}`, toolID)))
	require.NoError(t, err)
	require.NotNil(t, result)
	require.False(t, result.IsError, "loading tool %s failed: %s", toolID, getTextContent(result))
}

// invokeProgressiveTool invokes a tool through the ProgressiveHandler meta-tool.
func invokeProgressiveTool(t *testing.T, handler *ProgressiveHandler, rawArguments string) *mcp.CallToolResult {
	t.Helper()

	result, err := handler.handleInvokeTool(context.Background(), createCallToolRequest(MetaToolInvoke, rawArguments))
	require.NoError(t, err)
	require.NotNil(t, result)

	return result
}

func TestSafetyGuardChecksVisibilityAtInvocation(t *testing.T) {
	for _, test := range []struct {
		name   string
		config config.SafetyConfig
		method string
		tool   string
	}{
		{"read-only", config.SafetyConfig{ReadOnlyMode: true}, "DELETE", "users_posts"},
		{"denied", config.SafetyConfig{DeniedOperations: []string{"deleteUser"}}, "DELETE", "deleteUser"},
		{"not-allowed", config.SafetyConfig{AllowedOperations: []string{"getUser"}}, "GET", "listUsers"},
	} {
		t.Run(test.name, func(t *testing.T) {
			guard, err := newSafetyGuard(&test.config)
			require.NoError(t, err)
			result := guard.checkInvocation(test.tool, test.method, "/users", true)
			require.NotNil(t, result)
			assert.True(t, result.IsError)
		})
	}
}

func TestNewSafetyGuard_NilConfig(t *testing.T) {
	guard, err := newSafetyGuard(nil)
	require.NoError(t, err)
	require.NotNil(t, guard)

	assert.False(t, guard.confirmationRequired("POST"))

	_, matched := guard.dangerousMatch("DELETE", "/pets", "deletePet")
	assert.False(t, matched)

	remaining, allowed := guard.allowRequest()
	assert.True(t, allowed)
	assert.Zero(t, remaining)

	assert.NoError(t, ValidateSafetyConfig(nil))
}

func TestNewSafetyGuard_InvalidDangerousPattern(t *testing.T) {
	cfg := &config.SafetyConfig{
		DangerousOperationPatterns: []string{`^DELETE /pets`, `(["`},
	}

	guard, err := newSafetyGuard(cfg)
	require.Error(t, err)
	assert.Nil(t, guard)
	assert.Contains(t, err.Error(), "dangerous_operation_patterns")

	require.Error(t, ValidateSafetyConfig(cfg))
}

func TestSafetyGuard_ConfirmationRequired(t *testing.T) {
	guard, err := newSafetyGuard(&config.SafetyConfig{
		RequireConfirm: []string{"POST", "delete", " PUT "},
	})
	require.NoError(t, err)

	tests := []struct {
		method   string
		expected bool
	}{
		{method: "POST", expected: true},
		{method: "post", expected: true},
		{method: "DELETE", expected: true},
		{method: "PUT", expected: true},
		{method: "GET", expected: false},
		{method: "PATCH", expected: false},
	}

	for _, tt := range tests {
		t.Run(tt.method, func(t *testing.T) {
			assert.Equal(t, tt.expected, guard.confirmationRequired(tt.method))
		})
	}
}

func TestSafetyGuard_DangerousMatch(t *testing.T) {
	guard, err := newSafetyGuard(&config.SafetyConfig{
		DangerousOperationPatterns: []string{`^DELETE /pets`, `^deletePet$`, `admin`},
	})
	require.NoError(t, err)

	tests := []struct {
		name      string
		method    string
		path      string
		toolID    string
		wantMatch bool
		wantPat   string
	}{
		{
			name:      "matches METHOD path",
			method:    "DELETE",
			path:      "/pets/{petId}",
			toolID:    "deletePet",
			wantMatch: true,
			wantPat:   `^DELETE /pets`,
		},
		{
			name:      "matches tool id",
			method:    "GET",
			path:      "/pets",
			toolID:    "deletePet",
			wantMatch: true,
			wantPat:   `^deletePet$`,
		},
		{
			name:      "matches substring of METHOD path",
			method:    "GET",
			path:      "/admin/users",
			toolID:    "listUsers",
			wantMatch: true,
			wantPat:   "admin",
		},
		{
			name:      "no match",
			method:    "GET",
			path:      "/pets",
			toolID:    "listPets",
			wantMatch: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pattern, matched := guard.dangerousMatch(tt.method, tt.path, tt.toolID)
			assert.Equal(t, tt.wantMatch, matched)
			if tt.wantMatch {
				assert.Equal(t, tt.wantPat, pattern)
			}
		})
	}
}

func TestRequestRateLimiter_FixedWindow(t *testing.T) {
	limiter := newRequestRateLimiter(2)
	require.NotNil(t, limiter)

	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	limiter.now = func() time.Time { return now }

	remaining, allowed := limiter.Allow()
	assert.True(t, allowed)
	assert.Zero(t, remaining)

	_, allowed = limiter.Allow()
	assert.True(t, allowed)

	remaining, allowed = limiter.Allow()
	assert.False(t, allowed)
	assert.Equal(t, time.Minute, remaining)

	now = now.Add(30 * time.Second)
	remaining, allowed = limiter.Allow()
	assert.False(t, allowed)
	assert.Equal(t, 30*time.Second, remaining)

	now = now.Add(30 * time.Second)
	_, allowed = limiter.Allow()
	assert.True(t, allowed)
}

func TestRequestRateLimiter_ConcurrentSafety(t *testing.T) {
	limiter := newRequestRateLimiter(10)

	var allowed atomic.Int64
	var wg sync.WaitGroup
	for range 100 {
		wg.Go(func() {
			if _, ok := limiter.Allow(); ok {
				allowed.Add(1)
			}
		})
	}
	wg.Wait()

	assert.Equal(t, int64(10), allowed.Load())
}

func TestRetryAfterSeconds(t *testing.T) {
	tests := []struct {
		name     string
		wait     time.Duration
		expected int
	}{
		{name: "zero", wait: 0, expected: 1},
		{name: "sub-second rounds up", wait: 500 * time.Millisecond, expected: 1},
		{name: "one second", wait: time.Second, expected: 1},
		{name: "rounds up", wait: 1500 * time.Millisecond, expected: 2},
		{name: "one minute", wait: time.Minute, expected: 60},
		{name: "negative clamps to one", wait: -5 * time.Second, expected: 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, retryAfterSeconds(tt.wait))
		})
	}
}

func TestConfirmRequested(t *testing.T) {
	tests := []struct {
		name      string
		arguments map[string]any
		expected  bool
	}{
		{name: "nil arguments", arguments: nil, expected: false},
		{name: "absent", arguments: map[string]any{"limit": 5}, expected: false},
		{name: "bool true", arguments: map[string]any{confirmArgumentName: true}, expected: true},
		{name: "bool false", arguments: map[string]any{confirmArgumentName: false}, expected: false},
		{name: "string true", arguments: map[string]any{confirmArgumentName: "true"}, expected: true},
		{name: "string false", arguments: map[string]any{confirmArgumentName: "false"}, expected: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, confirmRequested(tt.arguments))
		})
	}
}

func TestSafetyGuard_CheckInvocation(t *testing.T) {
	guard, err := newSafetyGuard(&config.SafetyConfig{
		RequireConfirm:       []string{"POST"},
		MaxRequestsPerMinute: 1,
	})
	require.NoError(t, err)

	// Unconfirmed confirmation-required invocation is rejected.
	result := guard.checkInvocation("createPet", "POST", "/pets", false)
	require.NotNil(t, result)
	assert.True(t, result.IsError)
	assert.Contains(t, getTextContent(result), "operation requires confirmation; re-invoke with _confirm=true")

	// Confirmed invocation passes and consumes the only rate-limit slot.
	assert.Nil(t, guard.checkInvocation("createPet", "POST", "/pets", true))

	// The next invocation is rate limited.
	result = guard.checkInvocation("createPet", "POST", "/pets", true)
	require.NotNil(t, result)
	assert.True(t, result.IsError)
	assert.Contains(t, getTextContent(result), "rate limit")
	assert.Contains(t, getTextContent(result), "retry after")
}

func TestSafetyGuard_CheckInvocation_DangerousNotBypassedByConfirm(t *testing.T) {
	guard, err := newSafetyGuard(&config.SafetyConfig{
		RequireConfirm:             []string{"POST"},
		DangerousOperationPatterns: []string{`^POST /pets$`},
	})
	require.NoError(t, err)

	result := guard.checkInvocation("createPet", "POST", "/pets", true)
	require.NotNil(t, result)
	assert.True(t, result.IsError)
	assert.Contains(t, getTextContent(result), "dangerous_operation_patterns")
}

func TestSafetyGuard_CheckInvocation_NilGuard(t *testing.T) {
	var guard *safetyGuard
	assert.Nil(t, guard.checkInvocation("listPets", "GET", "/pets", false))
}

func TestConvertOperationToMCPTool_ConfirmSchema(t *testing.T) {
	tool := convertOperationToMCPTool("POST", "/pets", &openapi3.Operation{OperationID: "createPet"})

	schema, ok := tool.InputSchema.(map[string]any)
	require.True(t, ok)
	properties, ok := schema["properties"].(map[string]any)
	require.True(t, ok)

	confirm, ok := properties[confirmArgumentName].(map[string]any)
	require.True(t, ok, "tool input schema must contain the optional _confirm property")
	assert.Equal(t, "boolean", confirm["type"])
	assert.NotNil(t, confirm["description"])

	required, ok := schema["required"].([]string)
	require.True(t, ok)
	assert.NotContains(t, required, confirmArgumentName)
}

// TestSafetyConfigBehaviorMatrix locks the configuration -> behavior contract for
// the visibility rules (read-only/allowed/denied keep their previous behavior).
func TestSafetyConfigBehaviorMatrix(t *testing.T) {
	tests := []struct {
		name    string
		config  *config.SafetyConfig
		wantIDs []string
	}{
		{
			name:    "no safety config exposes all tools",
			config:  nil,
			wantIDs: []string{"listPets", "createPet", "getPetById", "deletePet"},
		},
		{
			name:    "read-only exposes GET only",
			config:  &config.SafetyConfig{ReadOnlyMode: true},
			wantIDs: []string{"listPets", "getPetById"},
		},
		{
			name:    "denied operations are hidden",
			config:  &config.SafetyConfig{DeniedOperations: []string{"deletePet"}},
			wantIDs: []string{"listPets", "createPet", "getPetById"},
		},
		{
			name:    "allowed operations restrict visibility",
			config:  &config.SafetyConfig{AllowedOperations: []string{"listPets", "getPetById"}},
			wantIDs: []string{"listPets", "getPetById"},
		},
		{
			name: "denied wins over allowed",
			config: &config.SafetyConfig{
				AllowedOperations: []string{"listPets", "getPetById", "deletePet"},
				DeniedOperations:  []string{"deletePet"},
			},
			wantIDs: []string{"listPets", "getPetById"},
		},
		{
			name:    "require_confirm does not hide tools",
			config:  &config.SafetyConfig{RequireConfirm: []string{"POST"}},
			wantIDs: []string{"listPets", "createPet", "getPetById", "deletePet"},
		},
		{
			name:    "dangerous patterns do not hide tools",
			config:  &config.SafetyConfig{DangerousOperationPatterns: []string{`^DELETE`}},
			wantIDs: []string{"listPets", "createPet", "getPetById", "deletePet"},
		},
	}

	spec := createTestOpenAPISpec()
	handler := NewHandler(semantic.NewMapper(), request.NewBuilder(nil), nil)

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			registry := NewToolRegistry()
			require.NoError(t, registry.BuildFromSpec(spec, tt.config))

			registryIDs := make([]string, 0, len(registry.GetMetadata()))
			for _, meta := range registry.GetMetadata() {
				registryIDs = append(registryIDs, meta.ID)
			}

			handlerIDs := make([]string, 0, len(tt.wantIDs))
			for _, tool := range handler.BuildMCPTools(spec, tt.config) {
				handlerIDs = append(handlerIDs, tool.Name)
			}

			assert.ElementsMatch(t, tt.wantIDs, registryIDs, "registry visibility")
			assert.ElementsMatch(t, tt.wantIDs, handlerIDs, "handler visibility")
		})
	}
}

// --- Handler invocation-time tests ---

func TestHandler_SetSafetyConfig_InvalidPattern(t *testing.T) {
	handler := NewHandler(nil, nil, nil)

	err := handler.SetSafetyConfig(&config.SafetyConfig{
		DangerousOperationPatterns: []string{"(["},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid safety configuration")
}

func TestHandler_Register_InvalidSafetyConfigFailsClosed(t *testing.T) {
	handler, recorder := newHandlerSafetyEnv(t, &config.SafetyConfig{
		DangerousOperationPatterns: []string{"(["},
	})

	result := callTool(t, handler, "listPets", `{}`)
	require.True(t, result.IsError, "invalid safety configuration must fail closed")
	assert.Contains(t, getTextContent(result), "invalid safety configuration")
	assert.Zero(t, recorder.Count())
}

func TestHandleCallTool_RequireConfirm(t *testing.T) {
	handler, recorder := newHandlerSafetyEnv(t, &config.SafetyConfig{
		RequireConfirm: []string{"POST"},
	})

	// First call without confirmation is rejected and never reaches the API.
	result := callTool(t, handler, "createPet", `{"name":"Rex"}`)
	require.True(t, result.IsError)
	assert.Contains(t, getTextContent(result), "operation requires confirmation; re-invoke with _confirm=true")
	assert.Zero(t, recorder.Count())

	// Confirmed call executes, and _confirm is not forwarded as request data.
	result = callTool(t, handler, "createPet", `{"_confirm":true,"name":"Rex"}`)
	require.False(t, result.IsError, getTextContent(result))
	assert.Equal(t, 1, recorder.Count())
	assert.NotContains(t, recorder.LastBody(), confirmArgumentName)
	assert.Contains(t, recorder.LastBody(), `"name":"Rex"`)

	// Confirmations are not remembered: the next call needs _confirm again.
	result = callTool(t, handler, "createPet", `{"name":"Rex"}`)
	require.True(t, result.IsError)
	assert.Contains(t, getTextContent(result), "_confirm=true")
	assert.Equal(t, 1, recorder.Count())
}

func TestHandleCallTool_DangerousPatternMethodPath(t *testing.T) {
	handler, recorder := newHandlerSafetyEnv(t, &config.SafetyConfig{
		DangerousOperationPatterns: []string{`^DELETE /pets`},
	})

	result := callTool(t, handler, "deletePet", `{"petId":"42"}`)
	require.True(t, result.IsError)
	assert.Contains(t, getTextContent(result), "dangerous_operation_patterns")
	assert.Zero(t, recorder.Count())

	// Confirmation cannot override a dangerous-pattern rejection.
	result = callTool(t, handler, "deletePet", `{"_confirm":true,"petId":"42"}`)
	require.True(t, result.IsError)
	assert.Zero(t, recorder.Count())
}

func TestHandleCallTool_DangerousPatternMatchesToolID(t *testing.T) {
	handler, recorder := newHandlerSafetyEnv(t, &config.SafetyConfig{
		DangerousOperationPatterns: []string{`^deletePet$`},
	})

	result := callTool(t, handler, "deletePet", `{"petId":"42"}`)
	require.True(t, result.IsError)
	assert.Contains(t, getTextContent(result), "dangerous_operation_patterns")
	assert.Zero(t, recorder.Count())
}

func TestHandleCallTool_RateLimit(t *testing.T) {
	handler, recorder := newHandlerSafetyEnv(t, &config.SafetyConfig{
		MaxRequestsPerMinute: 2,
	})

	for range 2 {
		result := callTool(t, handler, "listPets", `{}`)
		require.False(t, result.IsError, getTextContent(result))
	}
	require.Equal(t, 2, recorder.Count())

	result := callTool(t, handler, "listPets", `{}`)
	require.True(t, result.IsError)
	text := getTextContent(result)
	assert.Contains(t, text, "rate limit")
	assert.Regexp(t, `retry after \d+ seconds`, text)
	assert.Equal(t, 2, recorder.Count())
}

// --- ProgressiveHandler invocation-time tests ---

func TestProgressiveHandler_SetSpec_InvalidDangerousPattern(t *testing.T) {
	handler, err := NewProgressiveHandler(request.NewBuilder(nil), nil, SearchEnginePredicate)
	require.NoError(t, err)
	t.Cleanup(func() { _ = handler.Close() })

	err = handler.SetSpec(createTestOpenAPISpec(), &config.SafetyConfig{
		DangerousOperationPatterns: []string{"(["},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "dangerous_operation_patterns")
}

func TestProgressiveHandler_RequireConfirm(t *testing.T) {
	handler, recorder := newProgressiveSafetyEnv(t, &config.SafetyConfig{
		RequireConfirm: []string{"GET"},
	})
	loadProgressiveTool(t, handler, "listPets")

	// Unconfirmed invocation is rejected.
	result := invokeProgressiveTool(t, handler, `{"toolId":"listPets","arguments":{"limit":"5"}}`)
	require.True(t, result.IsError)
	assert.Contains(t, getTextContent(result), "operation requires confirmation; re-invoke with _confirm=true")
	assert.Zero(t, recorder.Count())

	// Top-level _confirm executes the operation and is not forwarded.
	result = invokeProgressiveTool(t, handler, `{"toolId":"listPets","arguments":{"limit":"5"},"_confirm":true}`)
	require.False(t, result.IsError, getTextContent(result))
	assert.Equal(t, 1, recorder.Count())

	// Confirmations are not remembered.
	result = invokeProgressiveTool(t, handler, `{"toolId":"listPets","arguments":{"limit":"5"}}`)
	require.True(t, result.IsError)
	assert.Equal(t, 1, recorder.Count())
}

func TestProgressiveHandler_RequireConfirm_NestedArgument(t *testing.T) {
	handler, recorder := newProgressiveSafetyEnv(t, &config.SafetyConfig{
		RequireConfirm: []string{"GET"},
	})
	loadProgressiveTool(t, handler, "listPets")

	result := invokeProgressiveTool(t, handler, `{"toolId":"listPets","arguments":{"_confirm":true}}`)
	require.False(t, result.IsError, getTextContent(result))
	assert.Equal(t, 1, recorder.Count())
}

func TestProgressiveHandler_DangerousPattern(t *testing.T) {
	handler, recorder := newProgressiveSafetyEnv(t, &config.SafetyConfig{
		DangerousOperationPatterns: []string{`^GET /pets$`},
	})
	loadProgressiveTool(t, handler, "listPets")

	result := invokeProgressiveTool(t, handler, `{"toolId":"listPets","arguments":{},"_confirm":true}`)
	require.True(t, result.IsError)
	assert.Contains(t, getTextContent(result), "dangerous_operation_patterns")
	assert.Zero(t, recorder.Count())
}

func TestProgressiveHandler_RateLimit(t *testing.T) {
	handler, recorder := newProgressiveSafetyEnv(t, &config.SafetyConfig{
		MaxRequestsPerMinute: 1,
	})
	loadProgressiveTool(t, handler, "listPets")

	result := invokeProgressiveTool(t, handler, `{"toolId":"listPets","arguments":{}}`)
	require.False(t, result.IsError, getTextContent(result))

	result = invokeProgressiveTool(t, handler, `{"toolId":"listPets","arguments":{}}`)
	require.True(t, result.IsError)
	assert.Contains(t, getTextContent(result), "rate limit")
	assert.Equal(t, 1, recorder.Count())
}

func TestProgressiveHandler_MetaToolsNotRateLimited(t *testing.T) {
	handler, recorder := newProgressiveSafetyEnv(t, &config.SafetyConfig{
		MaxRequestsPerMinute: 1,
	})

	for range 3 {
		result, err := handler.handleSearchTools(context.Background(),
			createCallToolRequest(MetaToolSearchTools, `{}`))
		require.NoError(t, err)
		require.False(t, result.IsError)
	}
	loadProgressiveTool(t, handler, "listPets")
	assert.Zero(t, recorder.Count(), "meta-tools must not consume the API request quota")

	// The quota is still available for the first API invocation.
	result := invokeProgressiveTool(t, handler, `{"toolId":"listPets","arguments":{}}`)
	require.False(t, result.IsError, getTextContent(result))
	assert.Equal(t, 1, recorder.Count())
}

func TestProgressiveHandler_InvokeToolSchemaHasConfirm(t *testing.T) {
	handler, _ := newProgressiveSafetyEnv(t, nil)

	tool := handler.buildInvokeToolDefinition()
	schema, ok := tool.InputSchema.(map[string]any)
	require.True(t, ok)
	properties, ok := schema["properties"].(map[string]any)
	require.True(t, ok)

	confirm, ok := properties[confirmArgumentName].(map[string]any)
	require.True(t, ok, "InvokeTool schema must contain the optional _confirm property")
	assert.Equal(t, "boolean", confirm["type"])

	required, ok := schema["required"].([]string)
	require.True(t, ok)
	assert.NotContains(t, required, confirmArgumentName)
}

// TestHandlerGetTools_ConfirmSchema verifies every exposed tool advertises the
// optional _confirm argument, including in read-only mode.
func TestHandlerGetTools_ConfirmSchema(t *testing.T) {
	handler := NewHandler(semantic.NewMapper(), request.NewBuilder(nil), nil)
	handler.SetSpec(createTestOpenAPISpec())

	for _, tool := range handler.GetTools(&config.SafetyConfig{RequireConfirm: []string{"DELETE"}}) {
		schema, ok := tool.InputSchema.(map[string]any)
		require.True(t, ok, tool.Name)
		properties, ok := schema["properties"].(map[string]any)
		require.True(t, ok, tool.Name)
		_, hasConfirm := properties[confirmArgumentName]
		assert.True(t, hasConfirm, "tool %s missing %s", tool.Name, confirmArgumentName)
	}
}

// TestSafetyGuard_WaitSecondsText locks the human-readable rate-limit message.
func TestSafetyGuard_WaitSecondsText(t *testing.T) {
	guard, err := newSafetyGuard(&config.SafetyConfig{MaxRequestsPerMinute: 1})
	require.NoError(t, err)

	require.Nil(t, guard.checkInvocation("listPets", "GET", "/pets", false))
	result := guard.checkInvocation("listPets", "GET", "/pets", false)
	require.NotNil(t, result)
	assert.True(t, result.IsError)

	text := getTextContent(result)
	assert.Contains(t, text, "max_requests_per_minute=1")
	assert.Contains(t, text, "retry after")
	assert.Contains(t, text, "seconds")
	assert.True(t, strings.HasSuffix(text, "seconds") || strings.Contains(text, " seconds"))
}
