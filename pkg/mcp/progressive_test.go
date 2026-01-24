//nolint:dupl // Test code duplication is acceptable for readability and isolation.
package mcp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
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
