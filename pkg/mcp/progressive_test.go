//nolint:dupl // Test code duplication is acceptable for readability and isolation.
package mcp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/nomagicln/open-bridge/pkg/config"
	"github.com/nomagicln/open-bridge/pkg/request"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// createCallToolRequest creates a CallToolRequest for testing.
func createCallToolRequest(name string, args string) *mcp.CallToolRequest {
	return &mcp.CallToolRequest{
		Params: &mcp.CallToolParamsRaw{
			Name:      name,
			Arguments: json.RawMessage(args),
		},
	}
}

func TestNewProgressiveHandler(t *testing.T) {
	tests := []struct {
		name        string
		engineType  SearchEngineType
		expectError bool
	}{
		{
			name:       "predicate engine",
			engineType: SearchEnginePredicate,
		},
		{
			name:        "invalid engine",
			engineType:  SearchEngineType("invalid"),
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler, err := NewProgressiveHandler(
				request.NewBuilder(nil),
				nil,
				tt.engineType,
			)
			if tt.expectError {
				assert.Error(t, err)
				assert.Nil(t, handler)
			} else {
				require.NoError(t, err)
				require.NotNil(t, handler)
				assert.NoError(t, handler.Close())
			}
		})
	}
}

func TestProgressiveFailedRebuildBlocksCachedTools(t *testing.T) {
	handler, err := NewProgressiveHandler(request.NewBuilder(nil), nil, SearchEnginePredicate)
	require.NoError(t, err)
	t.Cleanup(func() { _ = handler.Close() })
	require.NoError(t, handler.SetSpec(createTestSpec(), nil))
	_, _, err = handler.registry.Load("listPets")
	require.NoError(t, err)

	require.Error(t, handler.SetSpec(collidingToolSpec(), nil))
	assert.Zero(t, handler.registry.LoadedCount())
	for _, call := range []struct {
		handler func(context.Context, *mcp.CallToolRequest) (*mcp.CallToolResult, error)
		args    string
	}{
		{handler.handleSearchTools, `{"query":""}`},
		{handler.handleLoadTool, `{"toolId":"listPets"}`},
		{handler.handleInvokeTool, `{"toolId":"listPets","arguments":{}}`},
	} {
		result, callErr := call.handler(context.Background(), createCallToolRequest("", call.args))
		require.NoError(t, callErr)
		assert.True(t, result.IsError, "failed rebuild must block all old catalog access")
	}
}

func TestProgressiveHandler_SetSpec(t *testing.T) {
	handler, err := NewProgressiveHandler(
		request.NewBuilder(nil),
		nil,
		SearchEnginePredicate,
	)
	require.NoError(t, err)
	defer func() { _ = handler.Close() }()

	spec := createTestOpenAPISpec()
	err = handler.SetSpec(spec, nil)
	require.NoError(t, err)

	// Verify registry was populated
	assert.Equal(t, 4, handler.GetRegistry().ToolCount())
}

func TestProgressiveHandler_SetSpec_WithSafety(t *testing.T) {
	handler, err := NewProgressiveHandler(
		request.NewBuilder(nil),
		nil,
		SearchEnginePredicate,
	)
	require.NoError(t, err)
	defer func() { _ = handler.Close() }()

	spec := createTestOpenAPISpec()
	safetyConfig := &config.SafetyConfig{
		ReadOnlyMode: true,
	}
	err = handler.SetSpec(spec, safetyConfig)
	require.NoError(t, err)

	// Only GET operations
	assert.Equal(t, 2, handler.GetRegistry().ToolCount())
}

func TestProgressiveHandler_HandleSearchTools_EmptyQuery(t *testing.T) {
	handler := setupTestHandler(t)
	defer func() { _ = handler.Close() }()

	req := createCallToolRequest(MetaToolSearchTools, `{}`)

	result, err := handler.handleSearchTools(context.Background(), req)
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.False(t, result.IsError)

	// Should list all tools
	text := getTextContent(result)
	assert.Contains(t, text, "Found 4 tool(s)")
	assert.Contains(t, text, "listPets")
	assert.Contains(t, text, "createPet")
}

func TestProgressiveHandler_HandleSearchTools_WithQuery(t *testing.T) {
	handler := setupTestHandler(t)
	defer func() { _ = handler.Close() }()

	// Use predicate expression syntax instead of SQL
	req := createCallToolRequest(MetaToolSearchTools, `{"query": "MethodIs(\"DELETE\")"}`)

	result, err := handler.handleSearchTools(context.Background(), req)
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.False(t, result.IsError)

	text := getTextContent(result)
	assert.Contains(t, text, "deletePet")
}

func TestProgressiveHandler_HandleLoadTool(t *testing.T) {
	handler := setupTestHandler(t)
	defer func() { _ = handler.Close() }()

	req := createCallToolRequest(MetaToolLoad, `{"toolId": "listPets"}`)

	result, err := handler.handleLoadTool(context.Background(), req)
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.False(t, result.IsError)

	text := getTextContent(result)
	assert.Contains(t, text, "listPets")
	assert.Contains(t, text, "newly loaded")
	assert.Contains(t, text, "Parameters")

	// Verify it's cached
	assert.True(t, handler.GetRegistry().IsLoaded("listPets"))
}

func TestProgressiveHandler_HandleLoadTool_Cached(t *testing.T) {
	handler := setupTestHandler(t)
	defer func() { _ = handler.Close() }()

	// Load first time
	req := createCallToolRequest(MetaToolLoad, `{"toolId": "listPets"}`)

	_, err := handler.handleLoadTool(context.Background(), req)
	require.NoError(t, err)

	// Load second time
	result, err := handler.handleLoadTool(context.Background(), req)
	require.NoError(t, err)

	text := getTextContent(result)
	assert.Contains(t, text, "from cache")
}

func TestProgressiveHandler_HandleLoadTool_NotFound(t *testing.T) {
	handler := setupTestHandler(t)
	defer func() { _ = handler.Close() }()

	req := createCallToolRequest(MetaToolLoad, `{"toolId": "nonExistent"}`)

	result, err := handler.handleLoadTool(context.Background(), req)
	require.NoError(t, err)
	assert.True(t, result.IsError)

	text := getTextContent(result)
	assert.Contains(t, text, "tool not found")
}

func TestProgressiveHandler_HandleLoadTool_MissingToolId(t *testing.T) {
	handler := setupTestHandler(t)
	defer func() { _ = handler.Close() }()

	req := createCallToolRequest(MetaToolLoad, `{}`)

	result, err := handler.handleLoadTool(context.Background(), req)
	require.NoError(t, err)
	assert.True(t, result.IsError)

	text := getTextContent(result)
	assert.Contains(t, text, "toolId is required")
}

func TestProgressiveHandler_HandleInvokeTool_NotLoaded(t *testing.T) {
	handler := setupTestHandler(t)
	defer func() { _ = handler.Close() }()

	req := createCallToolRequest(MetaToolInvoke, `{"toolId": "listPets"}`)

	result, err := handler.handleInvokeTool(context.Background(), req)
	require.NoError(t, err)
	assert.True(t, result.IsError)

	text := getTextContent(result)
	assert.Contains(t, text, "not loaded")
	// The hint must name the registered meta-tool, not the unscoped legacy alias.
	assert.Contains(t, text, "LoadTestTool")
}

func TestProgressiveHandler_HandleInvokeTool_Success(t *testing.T) {
	// Create mock server
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "GET", r.Method)
		assert.Equal(t, "/pets", r.URL.Path)

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`[{"id": 1, "name": "Fluffy"}]`))
	}))
	defer mockServer.Close()

	handler, err := NewProgressiveHandler(
		request.NewBuilder(nil),
		mockServer.Client(),
		SearchEnginePredicate,
	)
	require.NoError(t, err)
	defer func() { _ = handler.Close() }()

	spec := createTestOpenAPISpec()
	err = handler.SetSpec(spec, nil)
	require.NoError(t, err)

	// Set app config with mock server URL
	appConfig := &config.AppConfig{
		Name:           "test",
		DefaultProfile: "default",
		Profiles: map[string]config.Profile{
			"default": {
				Name:    "default",
				BaseURL: mockServer.URL,
			},
		},
	}
	handler.SetAppConfig(appConfig, "")

	// First load the tool
	loadReq := createCallToolRequest(MetaToolLoad, `{"toolId": "listPets"}`)
	_, err = handler.handleLoadTool(context.Background(), loadReq)
	require.NoError(t, err)

	// Then invoke
	invokeReq := createCallToolRequest(MetaToolInvoke, `{"toolId": "listPets", "arguments": {}}`)

	result, err := handler.handleInvokeTool(context.Background(), invokeReq)
	require.NoError(t, err)
	assert.False(t, result.IsError)

	text := getTextContent(result)
	assert.Contains(t, text, "Fluffy")
}

func TestProgressiveHandler_BuildToolDefinitions(t *testing.T) {
	handler := setupTestHandler(t)
	defer func() { _ = handler.Close() }()

	// Test SearchTools definition
	searchTool := handler.buildSearchToolDefinition()
	expectedSearchName := "SearchTestTools"
	assert.Equal(t, expectedSearchName, searchTool.Name)
	assert.Contains(t, searchTool.Description, "Search")
	assert.Contains(t, searchTool.Description, "Test")

	// Test LoadTool definition
	loadTool := handler.buildLoadToolDefinition()
	expectedLoadName := "LoadTestTool"
	assert.Equal(t, expectedLoadName, loadTool.Name)
	assert.Contains(t, loadTool.Description, "Load")
	assert.Contains(t, loadTool.Description, "Test")

	// Test InvokeTool definition
	invokeTool := handler.buildInvokeToolDefinition()
	expectedInvokeName := "InvokeTestTool"
	assert.Equal(t, expectedInvokeName, invokeTool.Name)
	assert.Contains(t, invokeTool.Description, "Invoke")
	assert.Contains(t, invokeTool.Description, "Test")
}

func TestProgressiveHandler_SetAppConfig_Validation(t *testing.T) {
	handler, err := NewProgressiveHandler(
		request.NewBuilder(nil),
		nil,
		SearchEnginePredicate,
	)
	require.NoError(t, err)
	defer func() { _ = handler.Close() }()

	tests := []struct {
		name       string
		appConfig  *config.AppConfig
		shoulPanic bool
	}{
		{
			name:       "valid config",
			appConfig:  &config.AppConfig{Name: "test"},
			shoulPanic: false,
		},
		{
			name:       "nil config",
			appConfig:  nil,
			shoulPanic: true,
		},
		{
			name:       "empty name",
			appConfig:  &config.AppConfig{Name: ""},
			shoulPanic: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.shoulPanic {
				assert.Panics(t, func() {
					handler.SetAppConfig(tt.appConfig, "")
				})
			} else {
				assert.NotPanics(t, func() {
					handler.SetAppConfig(tt.appConfig, "")
				})
			}
		})
	}
}

func TestProgressiveHandler_FormatAppName(t *testing.T) {
	tests := []struct {
		name     string
		appName  string
		expected string
	}{
		{name: "simple", appName: "petstore", expected: "Petstore"},
		{name: "with hyphen", appName: "my-api", expected: "MyApi"},
		{name: "with underscore", appName: "my_api", expected: "MyApi"},
		{name: "with dot", appName: "my.api", expected: "MyApi"},
		{name: "complex", appName: "my-api-v2", expected: "MyApiV2"},
		{name: "multiple separators", appName: "my_api.v-2", expected: "MyApiV2"},
		{name: "already pascal", appName: "MyAPI", expected: "MyAPI"},
		{name: "single letter", appName: "a", expected: "A"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler, err := NewProgressiveHandler(
				request.NewBuilder(nil),
				nil,
				SearchEnginePredicate,
			)
			require.NoError(t, err)
			defer func() { _ = handler.Close() }()

			handler.SetAppConfig(&config.AppConfig{Name: tt.appName}, "")
			result := handler.formatAppName()
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestProgressiveHandler_Register(t *testing.T) {
	handler := setupTestHandler(t)
	defer func() { _ = handler.Close() }()

	server := mcp.NewServer(&mcp.Implementation{
		Name:    "test",
		Version: "1.0.0",
	}, nil)

	// Should not panic
	handler.Register(server)
}

// TestProgressiveHandler_RegistersAppScopedMetaToolNames locks the tool names a
// client observes in progressive mode. The registered names are scoped by the
// PascalCase app name, so two apps served by different processes cannot collide,
// and the exported MetaTool* constants are legacy aliases that do not match the
// registered names.
func TestProgressiveHandler_RegistersAppScopedMetaToolNames(t *testing.T) {
	tests := []struct {
		appName string
		search  string
		load    string
		invoke  string
	}{
		{
			appName: "petstore",
			search:  "SearchPetstoreTools",
			load:    "LoadPetstoreTool",
			invoke:  "InvokePetstoreTool",
		},
		{
			appName: "my-api-v2",
			search:  "SearchMyApiV2Tools",
			load:    "LoadMyApiV2Tool",
			invoke:  "InvokeMyApiV2Tool",
		},
	}

	for _, tt := range tests {
		t.Run(tt.appName, func(t *testing.T) {
			handler, err := NewProgressiveHandler(
				request.NewBuilder(nil),
				nil,
				SearchEnginePredicate,
			)
			require.NoError(t, err)
			t.Cleanup(func() { _ = handler.Close() })
			require.NoError(t, handler.SetSpec(createTestOpenAPISpec(), nil))

			handler.SetAppConfig(&config.AppConfig{
				Name:           tt.appName,
				DefaultProfile: "default",
				Profiles: map[string]config.Profile{
					"default": {Name: "default"},
				},
			}, "")

			server := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "1"}, nil)
			handler.Register(server)
			session := connectToolTestClient(t, server)

			toolList, err := session.ListTools(t.Context(), nil)
			require.NoError(t, err)

			registered := make(map[string]bool, len(toolList.Tools))
			for _, tool := range toolList.Tools {
				registered[tool.Name] = true
			}

			for _, name := range []string{tt.search, tt.load, tt.invoke} {
				assert.True(t, registered[name], "meta-tool %q is not registered", name)
			}
			for _, legacy := range []string{MetaToolSearchTools, MetaToolLoad, MetaToolInvoke} {
				assert.False(t, registered[legacy], "legacy alias %q must not be a registered tool", legacy)
			}
		})
	}
}

func setupTestHandler(t *testing.T) *ProgressiveHandler {
	t.Helper()

	handler, err := NewProgressiveHandler(
		request.NewBuilder(nil),
		nil,
		SearchEnginePredicate,
	)
	require.NoError(t, err)

	spec := createTestOpenAPISpec()
	err = handler.SetSpec(spec, nil)
	require.NoError(t, err)

	// Set app config for tool naming
	appConfig := &config.AppConfig{
		Name:           "test",
		DefaultProfile: "default",
		Profiles: map[string]config.Profile{
			"default": {
				Name: "default",
			},
		},
	}
	handler.SetAppConfig(appConfig, "")

	return handler
}

func getTextContent(result *mcp.CallToolResult) string {
	for _, content := range result.Content {
		if tc, ok := content.(*mcp.TextContent); ok {
			return tc.Text
		}
	}
	return ""
}

// newProgressivePolicyTestHandler builds a ProgressiveHandler whose profile
// declares one header and one query parameter, so the recorded API call shows
// whether progressive mode applies the same profile overrides as the CLI.
func newProgressivePolicyTestHandler(t *testing.T, server *httptest.Server) *ProgressiveHandler {
	t.Helper()

	handler, err := NewProgressiveHandler(request.NewBuilder(nil), server.Client(), SearchEnginePredicate)
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = handler.Close()
	})

	require.NoError(t, handler.SetSpec(createTestOpenAPISpec(), nil))
	handler.SetAppConfig(&config.AppConfig{
		Name:           "testapp",
		DefaultProfile: "default",
		Profiles: map[string]config.Profile{
			"default": {
				Name:        "default",
				BaseURL:     server.URL,
				Headers:     map[string]string{"X-Profile": "staging"},
				QueryParams: map[string]string{"api-version": "v2"},
			},
		},
	}, "default")

	return handler
}

// TestProgressiveHandler_InvokeTool_AppliesProfilePolicy proves progressive
// mode applies the same profile overrides as the CLI.
func TestProgressiveHandler_InvokeTool_AppliesProfilePolicy(t *testing.T) {
	server, recorder := newSafetyMockServer(t)
	handler := newProgressivePolicyTestHandler(t, server)

	loadProgressiveTool(t, handler, "listPets")
	result := invokeProgressiveTool(t, handler, `{"toolId": "listPets", "arguments": {"limit": 5}}`)
	if result.IsError {
		t.Fatalf("tool invocation failed: %s", getTextContent(result))
	}

	query, err := url.ParseQuery(recorder.LastQuery())
	require.NoError(t, err)
	assert.Equal(t, "v2", query.Get("api-version"))
	assert.Equal(t, "5", query.Get("limit"))
	assert.Equal(t, "staging", recorder.LastHeader("X-Profile"))
}

// TestProgressiveHandler_InvokeTool_RejectsMissingRequiredParameter proves
// progressive mode validates arguments against the operation schema before
// sending a request.
func TestProgressiveHandler_InvokeTool_RejectsMissingRequiredParameter(t *testing.T) {
	server, recorder := newSafetyMockServer(t)
	handler := newProgressivePolicyTestHandler(t, server)

	loadProgressiveTool(t, handler, "getPetById")
	result := invokeProgressiveTool(t, handler, `{"toolId": "getPetById", "arguments": {}}`)
	assert.True(t, result.IsError)
	assert.Contains(t, getTextContent(result), "petId")
	assert.Equal(t, 0, recorder.Count())
}

// TestProgressiveHandler_InvokeTool_CancelledContextSkipsRequest proves
// progressive mode binds the call context to the outgoing HTTP request.
func TestProgressiveHandler_InvokeTool_CancelledContextSkipsRequest(t *testing.T) {
	server, recorder := newSafetyMockServer(t)
	handler := newProgressivePolicyTestHandler(t, server)

	loadProgressiveTool(t, handler, "listPets")

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	result, err := handler.handleInvokeTool(ctx, createCallToolRequest(MetaToolInvoke, `{"toolId": "listPets", "arguments": {}}`))
	require.NoError(t, err)
	assert.True(t, result.IsError)
	assert.Equal(t, 0, recorder.Count())
}
