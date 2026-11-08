package mcp

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/nomagicln/open-bridge/pkg/config"
	"github.com/nomagicln/open-bridge/pkg/request"
	"github.com/nomagicln/open-bridge/pkg/semantic"
)

func TestHandleCallTool_Success(t *testing.T) {
	// Create a test HTTP server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify request
		if r.Method != "POST" {
			t.Errorf("Expected POST method, got %s", r.Method)
		}
		if r.URL.Path != "/api/v1/users" {
			t.Errorf("Expected path /api/v1/users, got %s", r.URL.Path)
		}

		// Return success response
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":   123,
			"name": "John Doe",
		})
	}))
	defer server.Close()

	// Create OpenAPI spec
	spec := &openapi3.T{
		OpenAPI: "3.0.0",
		Info: &openapi3.Info{
			Title:   "Test API",
			Version: "1.0.0",
		},
		Paths: &openapi3.Paths{},
	}

	// Add operation
	spec.Paths.Set("/api/v1/users", &openapi3.PathItem{
		Post: &openapi3.Operation{
			OperationID: "createUser",
			Summary:     "Create a user",
			Parameters:  openapi3.Parameters{},
			RequestBody: &openapi3.RequestBodyRef{
				Value: &openapi3.RequestBody{
					Content: openapi3.Content{
						"application/json": &openapi3.MediaType{
							Schema: &openapi3.SchemaRef{
								Value: &openapi3.Schema{
									Type: &openapi3.Types{"object"},
									Properties: openapi3.Schemas{
										"name": &openapi3.SchemaRef{
											Value: &openapi3.Schema{
												Type: &openapi3.Types{"string"},
											},
										},
									},
								},
							},
						},
					},
				},
			},
		},
	})

	// Create app config
	appConfig := &config.AppConfig{
		Name: "testapp",
		Profiles: map[string]config.Profile{
			"default": {
				Name:    "default",
				BaseURL: server.URL,
			},
		},
		DefaultProfile: "default",
	}

	// Create handler without credential manager
	mapper := semantic.NewMapper()
	builder := request.NewBuilder(nil)
	handler := NewHandler(mapper, builder, server.Client())
	handler.SetSpec(spec)
	handler.SetAppConfig(appConfig, "default")

	// Create request
	argsJSON := json.RawMessage(`{"name": "John Doe"}`)
	reqData := mcp.CallToolRequest{
		Params: &mcp.CallToolParamsRaw{
			Name:      "createUser",
			Arguments: argsJSON,
		},
	}

	// Handle request
	ctx := context.Background()
	result, err := handler.HandleCallTool(ctx, &reqData)
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	// Verify response
	if result == nil {
		t.Fatal("Expected result, got nil")
	}

	if result.IsError {
		t.Error("Expected IsError to be false")
	}

	if len(result.Content) == 0 {
		t.Fatal("Expected content to have at least one item")
	}

	textContent, ok := result.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatalf("Expected content to be TextContent, got %T", result.Content[0])
	}

	if textContent.Text == "" {
		t.Error("Expected non-empty text")
	}
}

func TestHandleCallTool_ToolNotFound(t *testing.T) {
	// Create OpenAPI spec
	spec := &openapi3.T{
		OpenAPI: "3.0.0",
		Info: &openapi3.Info{
			Title:   "Test API",
			Version: "1.0.0",
		},
		Paths: &openapi3.Paths{},
	}

	// Create app config
	appConfig := &config.AppConfig{
		Name: "testapp",
		Profiles: map[string]config.Profile{
			"default": {
				Name:    "default",
				BaseURL: "http://localhost",
			},
		},
		DefaultProfile: "default",
	}

	// Create handler
	mapper := semantic.NewMapper()
	builder := request.NewBuilder(nil)
	handler := NewHandler(mapper, builder, nil)
	handler.SetSpec(spec)
	handler.SetAppConfig(appConfig, "default")

	// Create request with non-existent tool
	reqData := mcp.CallToolRequest{
		Params: &mcp.CallToolParamsRaw{
			Name: "nonExistentTool",
		},
	}

	// Handle request
	ctx := context.Background()
	result, err := handler.HandleCallTool(ctx, &reqData)
	if err != nil {
		t.Fatalf("Expected no error (handled internally), got: %v", err)
	}

	if result == nil {
		t.Fatal("Expected result, got nil")
	}

	if !result.IsError {
		t.Error("Expected IsError to be true")
	}

	if len(result.Content) > 0 {
		textContent, ok := result.Content[0].(*mcp.TextContent)
		if ok {
			if textContent.Text == "" {
				t.Error("Expected error message in text content")
			}
		}
	}
}

func TestHandleCallTool_MissingToolName(t *testing.T) {
	// Create handler
	mapper := semantic.NewMapper()
	builder := request.NewBuilder(nil)
	handler := NewHandler(mapper, builder, nil)

	// Create request without tool name
	reqData := mcp.CallToolRequest{
		Params: &mcp.CallToolParamsRaw{
			Name: "",
		},
	}

	// Handle request
	ctx := context.Background()
	result, err := handler.HandleCallTool(ctx, &reqData)
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	if !result.IsError {
		t.Error("Expected IsError to be true for missing tool name")
	}
}

type toolTestTransport func(*http.Request) (*http.Response, error)

func (f toolTestTransport) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

func connectToolTestClient(t *testing.T, server *mcp.Server) *mcp.ClientSession {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)
	serverTransport, clientTransport := mcp.NewInMemoryTransports()
	serverSession, err := server.Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = serverSession.Close() })
	client := mcp.NewClient(&mcp.Implementation{Name: "binding-test", Version: "1"}, nil)
	session, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = session.Close() })
	return session
}

func TestRegisterCheckedBindsOnlyPublishedOperation(t *testing.T) {
	methods := make(chan string, 1)
	client := &http.Client{Transport: toolTestTransport(func(req *http.Request) (*http.Response, error) {
		methods <- req.Method
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{}`)), Header: make(http.Header)}, nil
	})}
	handler := NewHandler(semantic.NewMapper(), request.NewBuilder(nil), client)
	handler.SetSpec(collidingToolSpec())
	handler.SetAppConfig(safetyTestAppConfig("https://api.example.test"), "default")
	server := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "1"}, nil)
	if err := handler.RegisterChecked(server, &config.SafetyConfig{ReadOnlyMode: true}); err != nil {
		t.Fatal(err)
	}
	session := connectToolTestClient(t, server)
	result, err := session.CallTool(t.Context(), &mcp.CallToolParams{
		Name: "users_posts", Arguments: map[string]any{"userId": "7"},
	})
	if err != nil || result.IsError {
		t.Fatalf("published GET failed: %v, %v", result, err)
	}
	if method := <-methods; method != http.MethodGet {
		t.Fatalf("published read-only tool executed %s", method)
	}
	result, err = session.CallTool(t.Context(), &mcp.CallToolParams{
		Name: "delete_posts_users__userid__posts_delete", Arguments: map[string]any{"userId": "7"},
	})
	if err == nil && !result.IsError {
		t.Fatal("unpublished alias must not be callable through the protocol")
	}
	if err := handler.RegisterChecked(server, nil); err == nil {
		t.Fatal("ambiguous replacement must fail before publishing")
	}
	result, err = session.CallTool(t.Context(), &mcp.CallToolParams{
		Name: "users_posts", Arguments: map[string]any{"userId": "7"},
	})
	if err != nil || !result.IsError {
		t.Fatalf("old registration must fail closed after replacement: %v, %v", result, err)
	}
	select {
	case method := <-methods:
		t.Fatalf("blocked call reached transport: %s", method)
	default:
	}
}

func TestMapToolToOperationUsesAuthorizedBindings(t *testing.T) {
	handler := NewHandler(semantic.NewMapper(), request.NewBuilder(nil), nil)
	handler.SetSpec(collidingToolSpec())
	if err := handler.SetSafetyConfig(&config.SafetyConfig{ReadOnlyMode: true}); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := handler.MapToolToOperation("delete_posts_users__userid__posts_delete"); err == nil {
		t.Fatal("hidden DELETE operation must not resolve through an operationId alias")
	}
	_, method, _, err := handler.MapToolToOperation("users_posts")
	if err != nil || method != http.MethodGet {
		t.Fatalf("read-only binding = %s, %v; want GET", method, err)
	}
	if err := handler.SetSafetyConfig(nil); err == nil {
		t.Fatal("removing the filter must report the now-ambiguous catalog")
	}
	if _, _, _, err := handler.MapToolToOperation("users_posts"); err == nil {
		t.Fatal("failed catalog must not retain a previous binding")
	}
}

func TestMapToolToOperation(t *testing.T) {
	// Create OpenAPI spec
	spec := &openapi3.T{
		OpenAPI: "3.0.0",
		Info: &openapi3.Info{
			Title:   "Test API",
			Version: "1.0.0",
		},
		Paths: &openapi3.Paths{},
	}

	// Add operations
	spec.Paths.Set("/api/v1/users", &openapi3.PathItem{
		Get: &openapi3.Operation{
			OperationID: "listUsers",
			Summary:     "List users",
		},
		Post: &openapi3.Operation{
			OperationID: "createUser",
			Summary:     "Create a user",
		},
	})

	spec.Paths.Set("/api/v1/users/{id}", &openapi3.PathItem{
		Get: &openapi3.Operation{
			OperationID: "getUser",
			Summary:     "Get a user",
		},
	})

	// Create handler
	mapper := semantic.NewMapper()
	builder := request.NewBuilder(nil)
	handler := NewHandler(mapper, builder, nil)
	handler.SetSpec(spec)

	tests := []struct {
		name           string
		toolName       string
		expectError    bool
		expectedMethod string
		expectedPath   string
	}{
		{
			name:           "Find by operationId - listUsers",
			toolName:       "listUsers",
			expectError:    false,
			expectedMethod: "GET",
			expectedPath:   "/api/v1/users",
		},
		{
			name:           "Find by operationId - createUser",
			toolName:       "createUser",
			expectError:    false,
			expectedMethod: "POST",
			expectedPath:   "/api/v1/users",
		},
		{
			name:           "Find by operationId - getUser",
			toolName:       "getUser",
			expectError:    false,
			expectedMethod: "GET",
			expectedPath:   "/api/v1/users/{id}",
		},
		{
			name:        "Tool not found",
			toolName:    "nonExistent",
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			op, method, path, err := handler.MapToolToOperation(tt.toolName)

			if tt.expectError {
				if err == nil {
					t.Error("Expected error, got nil")
				}
				return
			}

			if err != nil {
				t.Fatalf("Expected no error, got: %v", err)
			}

			if op == nil {
				t.Fatal("Expected operation, got nil")
			}

			if method != tt.expectedMethod {
				t.Errorf("Expected method %s, got %s", tt.expectedMethod, method)
			}

			if path != tt.expectedPath {
				t.Errorf("Expected path %s, got %s", tt.expectedPath, path)
			}
		})
	}
}

func TestFormatMCPResult(t *testing.T) {
	mapper := semantic.NewMapper()
	builder := request.NewBuilder(nil)
	handler := NewHandler(mapper, builder, nil)

	tests := []struct {
		name       string
		statusCode int
		body       []byte
		expectJSON bool
	}{
		{
			name:       "JSON response",
			statusCode: 200,
			body:       []byte(`{"id": 123, "name": "John"}`),
			expectJSON: true,
		},
		{
			name:       "Text response",
			statusCode: 200,
			body:       []byte("Plain text response"),
			expectJSON: false,
		},
		{
			name:       "Empty response",
			statusCode: 204,
			body:       []byte(""),
			expectJSON: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := handler.FormatMCPResult(tt.statusCode, tt.body)

			if result == nil {
				t.Fatal("Expected result, got nil")
			}

			if len(result.Content) == 0 {
				t.Fatal("Expected content to have at least one item")
			}

			textContent, ok := result.Content[0].(*mcp.TextContent)
			if !ok {
				t.Fatalf("Expected content to be TextContent, got %T", result.Content[0])
			}

			if textContent.Text == "" && len(tt.body) > 0 {
				t.Error("Expected non-empty text")
			}
		})
	}
}

// TestNewHandlerDefaultClientTimeout verifies the default HTTP client has a
// positive timeout so a slow upstream API cannot hang an MCP tool call forever.
func TestNewHandlerDefaultClientTimeout(t *testing.T) {
	handler := NewHandler(nil, nil, nil)
	if handler.httpClient == nil {
		t.Fatal("expected default http client to be initialized")
	}
	if handler.httpClient.Timeout <= 0 {
		t.Errorf("expected default http client timeout > 0, got %v", handler.httpClient.Timeout)
	}
	if handler.httpClient.Timeout != defaultHTTPTimeout {
		t.Errorf("expected default http client timeout %v, got %v", defaultHTTPTimeout, handler.httpClient.Timeout)
	}
}

// TestNewHandlerPreservesProvidedClient verifies a caller-supplied client is
// not replaced by the default timeout client.
func TestNewHandlerPreservesProvidedClient(t *testing.T) {
	custom := &http.Client{Timeout: 5 * time.Second}
	handler := NewHandler(nil, nil, custom)
	if handler.httpClient != custom {
		t.Error("expected provided http client to be preserved")
	}
}

// TestNewProgressiveHandlerDefaultClientTimeout verifies the progressive handler
// applies the same default timeout as NewHandler.
func TestNewProgressiveHandlerDefaultClientTimeout(t *testing.T) {
	handler, err := NewProgressiveHandler(request.NewBuilder(nil), nil, SearchEnginePredicate)
	if err != nil {
		t.Fatalf("NewProgressiveHandler failed: %v", err)
	}
	t.Cleanup(func() {
		_ = handler.Close()
	})

	if handler.httpClient == nil {
		t.Fatal("expected default http client to be initialized")
	}
	if handler.httpClient.Timeout <= 0 {
		t.Errorf("expected default http client timeout > 0, got %v", handler.httpClient.Timeout)
	}
	if handler.httpClient.Timeout != defaultHTTPTimeout {
		t.Errorf("expected default http client timeout %v, got %v", defaultHTTPTimeout, handler.httpClient.Timeout)
	}
}

// newPolicyTestHandler builds a Handler whose profile declares one header and
// one query parameter, so the recorded API call shows whether the MCP surface
// applies the same profile overrides as the CLI.
func newPolicyTestHandler(t *testing.T, server *httptest.Server) *Handler {
	t.Helper()

	handler := NewHandler(semantic.NewMapper(), request.NewBuilder(nil), server.Client())
	handler.SetSpec(createTestOpenAPISpec())
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

// TestHandleCallTool_AppliesProfilePolicy proves the MCP surface applies the
// same profile overrides as the CLI: profile headers and query parameters
// reach the API call, and the operation's own query parameter survives.
func TestHandleCallTool_AppliesProfilePolicy(t *testing.T) {
	server, recorder := newSafetyMockServer(t)
	handler := newPolicyTestHandler(t, server)

	result := callTool(t, handler, "listPets", `{"limit": 5}`)
	if result.IsError {
		t.Fatalf("tool call failed: %s", getTextContent(result))
	}

	query, err := url.ParseQuery(recorder.LastQuery())
	if err != nil {
		t.Fatalf("recorded query %q is not parsable: %v", recorder.LastQuery(), err)
	}
	if got := query.Get("api-version"); got != "v2" {
		t.Errorf("profile query parameter api-version = %q, want v2", got)
	}
	if got := query.Get("limit"); got != "5" {
		t.Errorf("operation query parameter limit = %q, want 5", got)
	}
	if got := recorder.LastHeader("X-Profile"); got != "staging" {
		t.Errorf("profile header X-Profile = %q, want staging", got)
	}
}

// TestHandleCallTool_RejectsMissingRequiredParameter proves the MCP surface
// validates arguments against the operation schema before sending a request.
func TestHandleCallTool_RejectsMissingRequiredParameter(t *testing.T) {
	server, recorder := newSafetyMockServer(t)
	handler := newPolicyTestHandler(t, server)

	result := callTool(t, handler, "getPetById", `{}`)
	if !result.IsError {
		t.Fatal("expected a missing required parameter to be rejected")
	}
	if !strings.Contains(getTextContent(result), "petId") {
		t.Errorf("error message = %q, want it to mention petId", getTextContent(result))
	}
	if recorder.Count() != 0 {
		t.Errorf("expected no API call for invalid arguments, got %d", recorder.Count())
	}
}

// TestHandleCallTool_CancelledContextSkipsRequest proves the MCP call context
// is bound to the outgoing HTTP request: a cancelled context stops the call
// before it reaches the API.
func TestHandleCallTool_CancelledContextSkipsRequest(t *testing.T) {
	server, recorder := newSafetyMockServer(t)
	handler := newPolicyTestHandler(t, server)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	result, err := handler.HandleCallTool(ctx, createCallToolRequest("listPets", `{}`))
	if err != nil {
		t.Fatalf("HandleCallTool() error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected a cancelled context to fail the tool call")
	}
	if recorder.Count() != 0 {
		t.Errorf("expected no API call for a cancelled context, got %d", recorder.Count())
	}
}
