package mcp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

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
