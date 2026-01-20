package mcp

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
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
	reqData := Request{
		JSONRPC: "2.0",
		ID:      1,
		Method:  "tools/call",
		Params: map[string]any{
			"name": "createUser",
			"arguments": map[string]any{
				"name": "John Doe",
			},
		},
	}

	// Handle request
	resp := handler.handleCallTool(&reqData)

	// Verify response
	if resp.Error != nil {
		t.Fatalf("Expected no error, got: %v", resp.Error)
	}

	if resp.Result == nil {
		t.Fatal("Expected result, got nil")
	}

	result, ok := resp.Result.(map[string]any)
	if !ok {
		t.Fatalf("Expected result to be map[string]any, got %T", resp.Result)
	}

	content, ok := result["content"].([]map[string]any)
	if !ok {
		t.Fatalf("Expected content to be []map[string]any, got %T", result["content"])
	}

	if len(content) == 0 {
		t.Fatal("Expected content to have at least one item")
	}

	if content[0]["type"] != "text" {
		t.Errorf("Expected content type 'text', got %v", content[0]["type"])
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
	reqData := Request{
		JSONRPC: "2.0",
		ID:      1,
		Method:  "tools/call",
		Params: map[string]any{
			"name": "nonExistentTool",
		},
	}

	// Handle request
	resp := handler.handleCallTool(&reqData)

	// Verify error response
	if resp.Error == nil {
		t.Fatal("Expected error, got nil")
	}

	if resp.Error.Code != ErrCodeInvalidParams {
		t.Errorf("Expected error code %d, got %d", ErrCodeInvalidParams, resp.Error.Code)
	}

	if resp.Error.Message != "Tool not found: nonExistentTool" {
		t.Errorf("Expected 'Tool not found' message, got: %s", resp.Error.Message)
	}
}

func TestHandleCallTool_MissingToolName(t *testing.T) {
	// Create handler
	mapper := semantic.NewMapper()
	builder := request.NewBuilder(nil)
	handler := NewHandler(mapper, builder, nil)

	// Create request without tool name
	reqData := Request{
		JSONRPC: "2.0",
		ID:      1,
		Method:  "tools/call",
		Params:  map[string]any{},
	}

	// Handle request
	resp := handler.handleCallTool(&reqData)

	// Verify error response
	if resp.Error == nil {
		t.Fatal("Expected error, got nil")
	}

	if resp.Error.Code != ErrCodeInvalidParams {
		t.Errorf("Expected error code %d, got %d", ErrCodeInvalidParams, resp.Error.Code)
	}
}

func TestHandleCallTool_APIError(t *testing.T) {
	// Create a test HTTP server that returns an error
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"error": "Invalid request",
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

	// Create handler
	mapper := semantic.NewMapper()
	builder := request.NewBuilder(nil)
	handler := NewHandler(mapper, builder, server.Client())
	handler.SetSpec(spec)
	handler.SetAppConfig(appConfig, "default")

	// Create request
	reqData := Request{
		JSONRPC: "2.0",
		ID:      1,
		Method:  "tools/call",
		Params: map[string]any{
			"name": "createUser",
		},
	}

	// Handle request
	resp := handler.handleCallTool(&reqData)

	// Verify error response
	if resp.Error == nil {
		t.Fatal("Expected error, got nil")
	}

	if resp.Error.Code != ErrCodeInternalError {
		t.Errorf("Expected error code %d, got %d", ErrCodeInternalError, resp.Error.Code)
	}

	// Verify error data contains status code
	errorData, ok := resp.Error.Data.(map[string]any)
	if !ok {
		t.Fatalf("Expected error data to be map[string]any, got %T", resp.Error.Data)
	}

	if errorData["status_code"] != 400 {
		t.Errorf("Expected status code 400, got %v", errorData["status_code"])
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
			op, method, path, err := handler.mapToolToOperation(tt.toolName)

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
			result := handler.formatMCPResult(tt.statusCode, tt.body)

			if result == nil {
				t.Fatal("Expected result, got nil")
			}

			content, ok := result["content"].([]map[string]any)
			if !ok {
				t.Fatalf("Expected content to be []map[string]any, got %T", result["content"])
			}

			if len(content) == 0 {
				t.Fatal("Expected content to have at least one item")
			}

			if content[0]["type"] != "text" {
				t.Errorf("Expected content type 'text', got %v", content[0]["type"])
			}

			text, ok := content[0]["text"].(string)
			if !ok {
				t.Fatalf("Expected text to be string, got %T", content[0]["text"])
			}

			if text == "" && len(tt.body) > 0 {
				t.Error("Expected non-empty text")
			}
		})
	}
}
