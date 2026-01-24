package spec

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestNewParser(t *testing.T) {
	p := NewParser()
	if p == nil {
		t.Fatal("NewParser returned nil")
	}

	if p.cacheTTL != 5*time.Minute {
		t.Errorf("expected default cacheTTL of 5m, got %v", p.cacheTTL)
	}

	if p.loader == nil {
		t.Error("expected loader to be initialized")
	}
}

func TestNewParserWithOptions(t *testing.T) {
	customTTL := 10 * time.Minute
	customClient := &http.Client{Timeout: 60 * time.Second}

	p := NewParser(
		WithCacheTTL(customTTL),
		WithHTTPClient(customClient),
	)

	if p.cacheTTL != customTTL {
		t.Errorf("expected cacheTTL of %v, got %v", customTTL, p.cacheTTL)
	}

	if p.client != customClient {
		t.Error("expected custom HTTP client to be set")
	}
}

func TestIsURL(t *testing.T) {
	tests := []struct {
		input    string
		expected bool
	}{
		{"https://example.com/spec.yaml", true},
		{"http://localhost:8080/api.json", true},
		{"https://petstore.swagger.io/v2/swagger.json", true},
		{"/path/to/file.yaml", false},
		{"./relative/path.json", false},
		{"file.yaml", false},
		{"", false},
		{"ftp://example.com/spec.yaml", false},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := isURL(tt.input)
			if result != tt.expected {
				t.Errorf("isURL(%q) = %v, expected %v", tt.input, result, tt.expected)
			}
		})
	}
}

func TestDetectVersion(t *testing.T) {
	tests := []struct {
		name     string
		content  string
		expected SpecVersion
	}{
		{
			name:     "OpenAPI 3.1",
			content:  `{"openapi": "3.1.0", "info": {}}`,
			expected: Version31,
		},
		{
			name:     "OpenAPI 3.0",
			content:  `{"openapi": "3.0.3", "info": {}}`,
			expected: Version30,
		},
		{
			name:     "OpenAPI 3.0 YAML style",
			content:  `openapi: "3.0.0"`,
			expected: Version30,
		},
		{
			name:     "Swagger 2.0",
			content:  `{"swagger": "2.0", "info": {}}`,
			expected: Version20,
		},
		{
			name:     "Unknown",
			content:  `{"random": "data"}`,
			expected: VersionUnknown,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := detectVersion([]byte(tt.content))
			if result != tt.expected {
				t.Errorf("detectVersion() = %v, expected %v", result, tt.expected)
			}
		})
	}
}

func TestCacheSpec(t *testing.T) {
	p := NewParser(WithCacheTTL(1 * time.Hour))

	// Initially cache should be empty
	_, found := p.GetCachedSpec("testapp")
	if found {
		t.Error("expected cache to be empty initially")
	}

	// Cache a spec (using nil for simplicity in this test)
	p.CacheSpec("testapp", nil)

	// Should now be cached
	_, found = p.GetCachedSpec("testapp")
	if !found {
		t.Error("expected spec to be cached")
	}
}

func TestCacheSpecWithSource(t *testing.T) {
	p := NewParser(WithCacheTTL(1 * time.Hour))

	p.CacheSpecWithSource("testapp", nil, "/path/to/spec.yaml", Version30)

	cached, found := p.GetCachedSpecInfo("testapp")
	if !found {
		t.Fatal("expected spec to be cached")
	}

	if cached.Source != "/path/to/spec.yaml" {
		t.Errorf("expected source '/path/to/spec.yaml', got '%s'", cached.Source)
	}

	if cached.Version != Version30 {
		t.Errorf("expected version 3.0, got %v", cached.Version)
	}
}

func TestCacheExpiry(t *testing.T) {
	p := NewParser(WithCacheTTL(1 * time.Millisecond))

	p.CacheSpec("testapp", nil)

	// Wait for cache to expire
	time.Sleep(10 * time.Millisecond)

	_, found := p.GetCachedSpec("testapp")
	if found {
		t.Error("expected cache to be expired")
	}
}

func TestInvalidateCache(t *testing.T) {
	p := NewParser()

	p.CacheSpec("testapp", nil)

	_, found := p.GetCachedSpec("testapp")
	if !found {
		t.Fatal("expected spec to be cached")
	}

	p.InvalidateCache("testapp")

	_, found = p.GetCachedSpec("testapp")
	if found {
		t.Error("expected cache to be invalidated")
	}
}

func TestClearCache(t *testing.T) {
	p := NewParser()

	p.CacheSpec("app1", nil)
	p.CacheSpec("app2", nil)

	p.ClearCache()

	_, found1 := p.GetCachedSpec("app1")
	_, found2 := p.GetCachedSpec("app2")

	if found1 || found2 {
		t.Error("expected cache to be cleared")
	}
}

func TestGetCacheStats(t *testing.T) {
	p := NewParser(WithCacheTTL(1 * time.Hour))

	p.CacheSpec("app1", nil)
	p.CacheSpec("app2", nil)

	stats := p.GetCacheStats()

	if stats.TotalEntries != 2 {
		t.Errorf("expected 2 total entries, got %d", stats.TotalEntries)
	}

	if stats.ActiveEntries != 2 {
		t.Errorf("expected 2 active entries, got %d", stats.ActiveEntries)
	}

	if stats.ExpiredEntries != 0 {
		t.Errorf("expected 0 expired entries, got %d", stats.ExpiredEntries)
	}
}

func TestLoadSpecFromFile(t *testing.T) {
	// Create a temporary OpenAPI spec file
	tmpDir := t.TempDir()
	specPath := filepath.Join(tmpDir, "spec.yaml")

	specContent := `
openapi: "3.0.0"
info:
  title: Test API
  version: "1.0.0"
paths:
  /users:
    get:
      operationId: listUsers
      summary: List all users
      responses:
        "200":
          description: Success
`

	if err := os.WriteFile(specPath, []byte(specContent), 0644); err != nil {
		t.Fatalf("failed to write test spec: %v", err)
	}

	p := NewParser()
	spec, err := p.LoadSpec(specPath)
	if err != nil {
		t.Fatalf("failed to load spec: %v", err)
	}

	if spec.Info.Title != "Test API" {
		t.Errorf("expected title 'Test API', got '%s'", spec.Info.Title)
	}

	if spec.Paths.Len() != 1 {
		t.Errorf("expected 1 path, got %d", spec.Paths.Len())
	}
}

func TestLoadSpecFromFileJSON(t *testing.T) {
	tmpDir := t.TempDir()
	specPath := filepath.Join(tmpDir, "spec.json")

	specContent := `{
  "openapi": "3.0.0",
  "info": {
    "title": "JSON Test API",
    "version": "1.0.0"
  },
  "paths": {
    "/items": {
      "get": {
        "operationId": "listItems",
        "summary": "List items",
        "responses": {
          "200": {
            "description": "Success"
          }
        }
      }
    }
  }
}`

	if err := os.WriteFile(specPath, []byte(specContent), 0644); err != nil {
		t.Fatalf("failed to write test spec: %v", err)
	}

	p := NewParser()
	spec, err := p.LoadSpec(specPath)
	if err != nil {
		t.Fatalf("failed to load spec: %v", err)
	}

	if spec.Info.Title != "JSON Test API" {
		t.Errorf("expected title 'JSON Test API', got '%s'", spec.Info.Title)
	}
}

func TestLoadSpecInvalidFile(t *testing.T) {
	p := NewParser()
	_, err := p.LoadSpec("/nonexistent/path/spec.yaml")
	if err == nil {
		t.Error("expected error for nonexistent file")
	}
}

func TestLoadSpecEmptyFile(t *testing.T) {
	tmpDir := t.TempDir()
	specPath := filepath.Join(tmpDir, "empty.yaml")

	if err := os.WriteFile(specPath, []byte(""), 0644); err != nil {
		t.Fatalf("failed to write empty file: %v", err)
	}

	p := NewParser()
	_, err := p.LoadSpec(specPath)
	if err == nil {
		t.Error("expected error for empty file")
	}
}

func TestLoadSpecFromURL(t *testing.T) {
	// Create a test server
	spec := map[string]any{
		"openapi": "3.0.0",
		"info": map[string]any{
			"title":   "Remote API",
			"version": "1.0.0",
		},
		"paths": map[string]any{
			"/test": map[string]any{
				"get": map[string]any{
					"operationId": "test",
					"responses": map[string]any{
						"200": map[string]any{
							"description": "OK",
						},
					},
				},
			},
		},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(spec)
	}))
	defer server.Close()

	p := NewParser()
	loadedSpec, err := p.LoadSpec(server.URL)
	if err != nil {
		t.Fatalf("failed to load spec from URL: %v", err)
	}

	if loadedSpec.Info.Title != "Remote API" {
		t.Errorf("expected title 'Remote API', got '%s'", loadedSpec.Info.Title)
	}
}

func TestLoadSpecFromURLError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	p := NewParser()
	_, err := p.LoadSpec(server.URL)
	if err == nil {
		t.Error("expected error for 404 response")
	}
}

func TestValidateSpec(t *testing.T) {
	tmpDir := t.TempDir()
	specPath := filepath.Join(tmpDir, "spec.yaml")

	specContent := `
openapi: "3.0.0"
info:
  title: Valid API
  version: "1.0.0"
paths:
  /test:
    get:
      operationId: test
      responses:
        "200":
          description: OK
`

	if err := os.WriteFile(specPath, []byte(specContent), 0644); err != nil {
		t.Fatalf("failed to write test spec: %v", err)
	}

	p := NewParser()
	spec, err := p.LoadSpec(specPath)
	if err != nil {
		t.Fatalf("failed to load spec: %v", err)
	}

	if err := p.ValidateSpec(spec); err != nil {
		t.Errorf("expected valid spec, got error: %v", err)
	}
}

func TestValidateSpecNil(t *testing.T) {
	p := NewParser()
	err := p.ValidateSpec(nil)
	if err == nil {
		t.Error("expected error for nil spec")
	}
}

func TestValidateSpecWithOptions(t *testing.T) {
	tmpDir := t.TempDir()
	specPath := filepath.Join(tmpDir, "spec.yaml")

	specContent := `
openapi: "3.0.0"
info:
  title: Test API
  version: "1.0.0"
paths:
  /test:
    get:
      operationId: test
      responses:
        "200":
          description: OK
`

	if err := os.WriteFile(specPath, []byte(specContent), 0644); err != nil {
		t.Fatalf("failed to write test spec: %v", err)
	}

	p := NewParser()
	spec, err := p.LoadSpec(specPath)
	if err != nil {
		t.Fatalf("failed to load spec: %v", err)
	}

	result := p.ValidateSpecWithOptions(spec)
	if !result.Valid {
		t.Errorf("expected valid result, got errors: %v", result.Errors)
	}
}

func TestValidateSpecWithOptionsNil(t *testing.T) {
	p := NewParser()
	result := p.ValidateSpecWithOptions(nil)
	if result.Valid {
		t.Error("expected invalid result for nil spec")
	}
	if len(result.Errors) == 0 {
		t.Error("expected at least one error")
	}
}

func TestGetSpecInfo(t *testing.T) {
	tmpDir := t.TempDir()
	specPath := filepath.Join(tmpDir, "spec.yaml")

	specContent := `
openapi: "3.0.0"
info:
  title: Info Test API
  version: "2.0.0"
paths:
  /users:
    get:
      operationId: listUsers
      responses:
        "200":
          description: OK
    post:
      operationId: createUser
      responses:
        "201":
          description: Created
  /items:
    get:
      operationId: listItems
      responses:
        "200":
          description: OK
`

	if err := os.WriteFile(specPath, []byte(specContent), 0644); err != nil {
		t.Fatalf("failed to write test spec: %v", err)
	}

	p := NewParser()
	spec, err := p.LoadSpec(specPath)
	if err != nil {
		t.Fatalf("failed to load spec: %v", err)
	}

	info := GetSpecInfo(spec, specPath)
	if info == nil {
		t.Fatal("expected non-nil info")
	}

	if info.Title != "Info Test API" {
		t.Errorf("expected title 'Info Test API', got '%s'", info.Title)
	}

	if info.Version != "2.0.0" {
		t.Errorf("expected version '2.0.0', got '%s'", info.Version)
	}

	if info.PathCount != 2 {
		t.Errorf("expected 2 paths, got %d", info.PathCount)
	}

	if info.Operations != 3 {
		t.Errorf("expected 3 operations, got %d", info.Operations)
	}
}

func TestGetSpecInfoNil(t *testing.T) {
	info := GetSpecInfo(nil, "")
	if info != nil {
		t.Error("expected nil info for nil spec")
	}
}

func TestLoadSpecWithContext(t *testing.T) {
	tmpDir := t.TempDir()
	specPath := filepath.Join(tmpDir, "spec.yaml")

	specContent := `
openapi: "3.0.0"
info:
  title: Context Test
  version: "1.0.0"
paths: {}
`

	if err := os.WriteFile(specPath, []byte(specContent), 0644); err != nil {
		t.Fatalf("failed to write test spec: %v", err)
	}

	p := NewParser()
	ctx := context.Background()
	spec, err := p.LoadSpecWithContext(ctx, specPath)
	if err != nil {
		t.Fatalf("failed to load spec: %v", err)
	}

	if spec.Info.Title != "Context Test" {
		t.Errorf("expected title 'Context Test', got '%s'", spec.Info.Title)
	}
}

func TestLoadSpecWithContextCanceled(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(100 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	p := NewParser(WithHTTPClient(&http.Client{Timeout: 50 * time.Millisecond}))
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	_, err := p.LoadSpecWithContext(ctx, server.URL)
	if err == nil {
		t.Error("expected error for canceled context")
	}
}

// =============================================================================
// Complex OpenAPI 3.x Tests
// =============================================================================

func TestLoadComplexOpenAPI3Spec(t *testing.T) {
	// Load the complex OpenAPI 3.x spec from testdata
	specPath := filepath.Join("..", "..", "internal", "integration", "testdata", "complex_openapi3.json")
	p := NewParser()
	spec, err := p.LoadSpec(specPath)
	if err != nil {
		t.Fatalf("failed to load complex OpenAPI 3.x spec: %v", err)
	}

	// Verify basic info
	if spec.Info.Title != "Complex API for Testing" {
		t.Errorf("expected title 'Complex API for Testing', got '%s'", spec.Info.Title)
	}

	if spec.Info.Version != "1.0.0" {
		t.Errorf("expected version '1.0.0', got '%s'", spec.Info.Version)
	}
}

func TestComplexOpenAPI3MultipleServers(t *testing.T) {
	specPath := filepath.Join("..", "..", "internal", "integration", "testdata", "complex_openapi3.json")
	p := NewParser()
	spec, err := p.LoadSpec(specPath)
	if err != nil {
		t.Fatalf("failed to load spec: %v", err)
	}

	// Verify multiple servers
	if len(spec.Servers) != 3 {
		t.Errorf("expected 3 servers, got %d", len(spec.Servers))
	}

	expectedServers := []struct {
		url         string
		description string
	}{
		{"https://api.example.com/v1", "Production server"},
		{"https://staging.example.com/v1", "Staging server"},
		{"http://localhost:8080/v1", "Local development"},
	}

	for i, expected := range expectedServers {
		if i >= len(spec.Servers) {
			break
		}
		if spec.Servers[i].URL != expected.url {
			t.Errorf("server[%d] URL: expected '%s', got '%s'", i, expected.url, spec.Servers[i].URL)
		}
		if spec.Servers[i].Description != expected.description {
			t.Errorf("server[%d] description: expected '%s', got '%s'", i, expected.description, spec.Servers[i].Description)
		}
	}
}

func TestComplexOpenAPI3AllOfSchema(t *testing.T) {
	specPath := filepath.Join("..", "..", "internal", "integration", "testdata", "complex_openapi3.json")
	p := NewParser()
	spec, err := p.LoadSpec(specPath)
	if err != nil {
		t.Fatalf("failed to load spec: %v", err)
	}

	// Verify Pet schema uses allOf
	petSchema := spec.Components.Schemas["Pet"]
	if petSchema == nil {
		t.Fatal("Pet schema not found")
	}

	if len(petSchema.Value.AllOf) == 0 {
		t.Error("Pet schema should have allOf composition")
	}

	// Verify Owner schema also uses allOf with nested $ref
	ownerSchema := spec.Components.Schemas["Owner"]
	if ownerSchema == nil {
		t.Fatal("Owner schema not found")
	}

	if len(ownerSchema.Value.AllOf) == 0 {
		t.Error("Owner schema should have allOf composition")
	}
}

func TestComplexOpenAPI3OneOfSchema(t *testing.T) {
	specPath := filepath.Join("..", "..", "internal", "integration", "testdata", "complex_openapi3.json")
	p := NewParser()
	spec, err := p.LoadSpec(specPath)
	if err != nil {
		t.Fatalf("failed to load spec: %v", err)
	}

	// Verify Animal schema uses oneOf with discriminator
	animalSchema := spec.Components.Schemas["Animal"]
	if animalSchema == nil {
		t.Fatal("Animal schema not found")
	}

	if len(animalSchema.Value.OneOf) == 0 {
		t.Error("Animal schema should have oneOf composition")
	}

	// Verify discriminator
	if animalSchema.Value.Discriminator == nil {
		t.Error("Animal schema should have discriminator")
	} else {
		if animalSchema.Value.Discriminator.PropertyName != "animalType" {
			t.Errorf("expected discriminator property 'animalType', got '%s'",
				animalSchema.Value.Discriminator.PropertyName)
		}

		if len(animalSchema.Value.Discriminator.Mapping) != 3 {
			t.Errorf("expected 3 discriminator mappings, got %d",
				len(animalSchema.Value.Discriminator.Mapping))
		}
	}
}

func TestComplexOpenAPI3AnyOfSchema(t *testing.T) {
	specPath := filepath.Join("..", "..", "internal", "integration", "testdata", "complex_openapi3.json")
	p := NewParser()
	spec, err := p.LoadSpec(specPath)
	if err != nil {
		t.Fatalf("failed to load spec: %v", err)
	}

	// Verify Vehicle schema uses anyOf
	vehicleSchema := spec.Components.Schemas["Vehicle"]
	if vehicleSchema == nil {
		t.Fatal("Vehicle schema not found")
	}

	if len(vehicleSchema.Value.AnyOf) == 0 {
		t.Error("Vehicle schema should have anyOf composition")
	}

	// Should have 3 vehicle types
	if len(vehicleSchema.Value.AnyOf) != 3 {
		t.Errorf("expected 3 anyOf schemas, got %d", len(vehicleSchema.Value.AnyOf))
	}
}

func TestComplexOpenAPI3SecuritySchemes(t *testing.T) {
	specPath := filepath.Join("..", "..", "internal", "integration", "testdata", "complex_openapi3.json")
	p := NewParser()
	spec, err := p.LoadSpec(specPath)
	if err != nil {
		t.Fatalf("failed to load spec: %v", err)
	}

	// Verify security schemes exist
	if spec.Components.SecuritySchemes == nil {
		t.Fatal("SecuritySchemes not found")
	}

	expectedSchemes := []string{
		"bearerAuth",
		"basicAuth",
		"apiKeyHeader",
		"apiKeyQuery",
		"apiKeyCookie",
		"oauth2",
	}

	for _, name := range expectedSchemes {
		scheme := spec.Components.SecuritySchemes[name]
		if scheme == nil {
			t.Errorf("security scheme '%s' not found", name)
			continue
		}

		switch name {
		case "bearerAuth":
			if scheme.Value.Type != "http" || scheme.Value.Scheme != "bearer" {
				t.Errorf("bearerAuth: expected type=http, scheme=bearer")
			}
		case "basicAuth":
			if scheme.Value.Type != "http" || scheme.Value.Scheme != "basic" {
				t.Errorf("basicAuth: expected type=http, scheme=basic")
			}
		case "apiKeyHeader":
			if scheme.Value.Type != "apiKey" || scheme.Value.In != "header" {
				t.Errorf("apiKeyHeader: expected type=apiKey, in=header")
			}
		case "apiKeyQuery":
			if scheme.Value.Type != "apiKey" || scheme.Value.In != "query" {
				t.Errorf("apiKeyQuery: expected type=apiKey, in=query")
			}
		case "apiKeyCookie":
			if scheme.Value.Type != "apiKey" || scheme.Value.In != "cookie" {
				t.Errorf("apiKeyCookie: expected type=apiKey, in=cookie")
			}
		case "oauth2":
			if scheme.Value.Type != "oauth2" {
				t.Errorf("oauth2: expected type=oauth2")
			}
			if scheme.Value.Flows == nil {
				t.Error("oauth2: flows should not be nil")
			}
		default:
			t.Errorf("unexpected security scheme: %s", name)
		}
	}
}

func TestComplexOpenAPI3ParameterLocations(t *testing.T) {
	specPath := filepath.Join("..", "..", "internal", "integration", "testdata", "complex_openapi3.json")
	p := NewParser()
	spec, err := p.LoadSpec(specPath)
	if err != nil {
		t.Fatalf("failed to load spec: %v", err)
	}

	// Check GET /pets parameters
	petsPath := spec.Paths.Find("/pets")
	if petsPath == nil {
		t.Fatal("/pets path not found")
	}

	getOp := petsPath.Get
	if getOp == nil {
		t.Fatal("GET /pets operation not found")
	}

	// Verify we have query, header, and cookie parameters
	paramLocations := make(map[string]bool)
	for _, param := range getOp.Parameters {
		paramLocations[param.Value.In] = true
	}

	expectedLocations := []string{"query", "header", "cookie"}
	for _, loc := range expectedLocations {
		if !paramLocations[loc] {
			t.Errorf("expected parameter in '%s' location", loc)
		}
	}

	// Check path parameter in /pets/{petId}
	petByIdPath := spec.Paths.Find("/pets/{petId}")
	if petByIdPath == nil {
		t.Fatal("/pets/{petId} path not found")
	}

	// Path-level parameters
	hasPathParam := false
	for _, param := range petByIdPath.Parameters {
		if param.Value.In == "path" && param.Value.Name == "petId" {
			hasPathParam = true
			if !param.Value.Required {
				t.Error("path parameter should be required")
			}
		}
	}
	if !hasPathParam {
		t.Error("expected path parameter 'petId'")
	}
}

func TestComplexOpenAPI3MultipleContentTypes(t *testing.T) {
	specPath := filepath.Join("..", "..", "internal", "integration", "testdata", "complex_openapi3.json")
	p := NewParser()
	spec, err := p.LoadSpec(specPath)
	if err != nil {
		t.Fatalf("failed to load spec: %v", err)
	}

	// Check POST /pets requestBody
	petsPath := spec.Paths.Find("/pets")
	if petsPath == nil {
		t.Fatal("/pets path not found")
	}

	postOp := petsPath.Post
	if postOp == nil || postOp.RequestBody == nil {
		t.Fatal("POST /pets operation or requestBody not found")
	}

	content := postOp.RequestBody.Value.Content
	expectedContentTypes := []string{
		"application/json",
		"application/x-www-form-urlencoded",
		"multipart/form-data",
	}

	for _, ct := range expectedContentTypes {
		if content[ct] == nil {
			t.Errorf("expected content type '%s' in requestBody", ct)
		}
	}
}

func TestComplexOpenAPI3ResponseHeaders(t *testing.T) {
	specPath := filepath.Join("..", "..", "internal", "integration", "testdata", "complex_openapi3.json")
	p := NewParser()
	spec, err := p.LoadSpec(specPath)
	if err != nil {
		t.Fatalf("failed to load spec: %v", err)
	}

	// Check GET /pets response headers
	petsPath := spec.Paths.Find("/pets")
	if petsPath == nil {
		t.Fatal("/pets path not found")
	}

	getOp := petsPath.Get
	if getOp == nil {
		t.Fatal("GET /pets operation not found")
	}

	resp200 := getOp.Responses.Status(200)
	if resp200 == nil {
		t.Fatal("200 response not found")
	}

	expectedHeaders := []string{"X-Total-Count", "X-Rate-Limit-Remaining"}
	for _, h := range expectedHeaders {
		if resp200.Value.Headers[h] == nil {
			t.Errorf("expected response header '%s'", h)
		}
	}
}

func TestComplexOpenAPI3NestedRefs(t *testing.T) {
	specPath := filepath.Join("..", "..", "internal", "integration", "testdata", "complex_openapi3.json")
	p := NewParser()
	spec, err := p.LoadSpec(specPath)
	if err != nil {
		t.Fatalf("failed to load spec: %v", err)
	}

	// Verify nested $ref chain: Pet -> Owner -> Address
	petSchema := spec.Components.Schemas["Pet"]
	if petSchema == nil {
		t.Fatal("Pet schema not found")
	}

	ownerSchema := spec.Components.Schemas["Owner"]
	if ownerSchema == nil {
		t.Fatal("Owner schema not found")
	}

	addressSchema := spec.Components.Schemas["Address"]
	if addressSchema == nil {
		t.Fatal("Address schema not found")
	}

	// Address should have expected properties
	addressProps := addressSchema.Value.Properties
	expectedProps := []string{"street", "city", "country", "postalCode"}
	for _, prop := range expectedProps {
		if addressProps[prop] == nil {
			t.Errorf("Address schema missing property '%s'", prop)
		}
	}
}

func TestComplexOpenAPI3SharedResponses(t *testing.T) {
	specPath := filepath.Join("..", "..", "internal", "integration", "testdata", "complex_openapi3.json")
	p := NewParser()
	spec, err := p.LoadSpec(specPath)
	if err != nil {
		t.Fatalf("failed to load spec: %v", err)
	}

	// Verify shared responses in components
	if spec.Components.Responses == nil {
		t.Fatal("Components.Responses not found")
	}

	expectedResponses := []string{"BadRequest", "Unauthorized", "NotFound", "InternalError"}
	for _, name := range expectedResponses {
		if spec.Components.Responses[name] == nil {
			t.Errorf("shared response '%s' not found", name)
		}
	}
}

// =============================================================================
// Swagger 2.0 (OpenAPI 2.x) Tests
// =============================================================================

func TestLoadSwagger20Spec(t *testing.T) {
	// Load the Swagger 2.0 spec from testdata
	specPath := filepath.Join("..", "..", "internal", "integration", "testdata", "swagger20.json")
	p := NewParser()
	spec, err := p.LoadSpec(specPath)
	if err != nil {
		t.Fatalf("failed to load Swagger 2.0 spec: %v", err)
	}

	// Verify basic info (should be preserved after conversion)
	if spec.Info.Title != "Swagger 2.0 Pet Store" {
		t.Errorf("expected title 'Swagger 2.0 Pet Store', got '%s'", spec.Info.Title)
	}

	if spec.Info.Version != "1.0.0" {
		t.Errorf("expected version '1.0.0', got '%s'", spec.Info.Version)
	}

	if spec.Info.Description == "" {
		t.Error("description should not be empty after conversion")
	}
}

func TestSwagger20ServerConversion(t *testing.T) {
	// Swagger 2.0 host + basePath + schemes should be converted to servers
	specPath := filepath.Join("..", "..", "internal", "integration", "testdata", "swagger20.json")
	p := NewParser()
	spec, err := p.LoadSpec(specPath)
	if err != nil {
		t.Fatalf("failed to load spec: %v", err)
	}

	// After conversion, servers should be populated from host/basePath/schemes
	if len(spec.Servers) == 0 {
		t.Error("expected at least one server after conversion from Swagger 2.0")
	}

	// The first server should contain the host and basePath
	if len(spec.Servers) > 0 {
		serverURL := spec.Servers[0].URL
		// Should contain the host "api.petstore.example.com" and basePath "/v1"
		if serverURL == "" {
			t.Error("server URL should not be empty")
		}
	}
}

func TestSwagger20DefinitionsConversion(t *testing.T) {
	// Swagger 2.0 definitions should be converted to components/schemas
	specPath := filepath.Join("..", "..", "internal", "integration", "testdata", "swagger20.json")
	p := NewParser()
	spec, err := p.LoadSpec(specPath)
	if err != nil {
		t.Fatalf("failed to load spec: %v", err)
	}

	// Verify schemas exist in components
	if spec.Components == nil || spec.Components.Schemas == nil {
		t.Fatal("Components.Schemas not found after conversion")
	}

	expectedSchemas := []string{"Pet", "NewPet", "UpdatePet", "Category", "Tag", "Order", "User", "Error"}
	for _, name := range expectedSchemas {
		if spec.Components.Schemas[name] == nil {
			t.Errorf("schema '%s' not found after conversion", name)
		}
	}
}

func TestSwagger20SecurityDefinitionsConversion(t *testing.T) {
	// Swagger 2.0 securityDefinitions should be converted to components/securitySchemes
	specPath := filepath.Join("..", "..", "internal", "integration", "testdata", "swagger20.json")
	p := NewParser()
	spec, err := p.LoadSpec(specPath)
	if err != nil {
		t.Fatalf("failed to load spec: %v", err)
	}

	if spec.Components == nil || spec.Components.SecuritySchemes == nil {
		t.Fatal("Components.SecuritySchemes not found after conversion")
	}

	// Check api_key conversion
	apiKey := spec.Components.SecuritySchemes["api_key"]
	if apiKey == nil {
		t.Error("api_key security scheme not found")
	} else {
		if apiKey.Value.Type != "apiKey" {
			t.Errorf("expected api_key type 'apiKey', got '%s'", apiKey.Value.Type)
		}
		if apiKey.Value.In != "header" {
			t.Errorf("expected api_key in 'header', got '%s'", apiKey.Value.In)
		}
	}

	// Check basic_auth conversion
	basicAuth := spec.Components.SecuritySchemes["basic_auth"]
	if basicAuth == nil {
		t.Error("basic_auth security scheme not found")
	} else {
		if basicAuth.Value.Type != "http" {
			t.Errorf("expected basic_auth type 'http', got '%s'", basicAuth.Value.Type)
		}
		if basicAuth.Value.Scheme != "basic" {
			t.Errorf("expected basic_auth scheme 'basic', got '%s'", basicAuth.Value.Scheme)
		}
	}

	// Check oauth2 conversion
	oauth2 := spec.Components.SecuritySchemes["oauth2"]
	if oauth2 == nil {
		t.Error("oauth2 security scheme not found")
	} else {
		if oauth2.Value.Type != "oauth2" {
			t.Errorf("expected oauth2 type 'oauth2', got '%s'", oauth2.Value.Type)
		}
		if oauth2.Value.Flows == nil {
			t.Error("oauth2 flows should not be nil")
		}
	}
}

func TestSwagger20PathsConversion(t *testing.T) {
	specPath := filepath.Join("..", "..", "internal", "integration", "testdata", "swagger20.json")
	p := NewParser()
	spec, err := p.LoadSpec(specPath)
	if err != nil {
		t.Fatalf("failed to load spec: %v", err)
	}

	// Verify paths are preserved
	expectedPaths := []string{
		"/pets",
		"/pets/{petId}",
		"/pets/{petId}/photo",
		"/store/orders",
		"/store/orders/{orderId}",
		"/users",
		"/users/{username}",
	}

	for _, path := range expectedPaths {
		if spec.Paths.Find(path) == nil {
			t.Errorf("path '%s' not found after conversion", path)
		}
	}
}

func TestSwagger20OperationsConversion(t *testing.T) {
	specPath := filepath.Join("..", "..", "internal", "integration", "testdata", "swagger20.json")
	p := NewParser()
	spec, err := p.LoadSpec(specPath)
	if err != nil {
		t.Fatalf("failed to load spec: %v", err)
	}

	// Check /pets operations
	petsPath := spec.Paths.Find("/pets")
	if petsPath == nil {
		t.Fatal("/pets path not found")
	}

	if petsPath.Get == nil {
		t.Error("GET /pets operation not found")
	} else {
		if petsPath.Get.OperationID != "listPets" {
			t.Errorf("expected operationId 'listPets', got '%s'", petsPath.Get.OperationID)
		}
	}

	if petsPath.Post == nil {
		t.Error("POST /pets operation not found")
	} else {
		if petsPath.Post.OperationID != "createPet" {
			t.Errorf("expected operationId 'createPet', got '%s'", petsPath.Post.OperationID)
		}
	}

	// Check /pets/{petId} operations
	petByIdPath := spec.Paths.Find("/pets/{petId}")
	if petByIdPath == nil {
		t.Fatal("/pets/{petId} path not found")
	}

	if petByIdPath.Get == nil {
		t.Error("GET /pets/{petId} operation not found")
	}
	if petByIdPath.Put == nil {
		t.Error("PUT /pets/{petId} operation not found")
	}
	if petByIdPath.Delete == nil {
		t.Error("DELETE /pets/{petId} operation not found")
	}
}

func TestSwagger20BodyParameterConversion(t *testing.T) {
	// Swagger 2.0 body parameters should be converted to requestBody
	specPath := filepath.Join("..", "..", "internal", "integration", "testdata", "swagger20.json")
	p := NewParser()
	spec, err := p.LoadSpec(specPath)
	if err != nil {
		t.Fatalf("failed to load spec: %v", err)
	}

	// Check POST /pets has requestBody
	petsPath := spec.Paths.Find("/pets")
	if petsPath == nil {
		t.Fatal("/pets path not found")
	}

	postOp := petsPath.Post
	if postOp == nil {
		t.Fatal("POST /pets operation not found")
	}

	if postOp.RequestBody == nil {
		t.Error("POST /pets should have requestBody after conversion")
	} else {
		// Should have content
		if len(postOp.RequestBody.Value.Content) == 0 {
			t.Error("requestBody should have content")
		}
	}
}

func TestSwagger20FormDataConversion(t *testing.T) {
	// Swagger 2.0 formData parameters should be converted to requestBody
	specPath := filepath.Join("..", "..", "internal", "integration", "testdata", "swagger20.json")
	p := NewParser()
	spec, err := p.LoadSpec(specPath)
	if err != nil {
		t.Fatalf("failed to load spec: %v", err)
	}

	// Check POST /pets/{petId}/photo (file upload)
	photoPath := spec.Paths.Find("/pets/{petId}/photo")
	if photoPath == nil {
		t.Fatal("/pets/{petId}/photo path not found")
	}

	postOp := photoPath.Post
	if postOp == nil {
		t.Fatal("POST /pets/{petId}/photo operation not found")
	}

	if postOp.RequestBody == nil {
		t.Error("file upload endpoint should have requestBody after conversion")
	} else {
		// Should have multipart/form-data content
		if postOp.RequestBody.Value.Content["multipart/form-data"] == nil {
			t.Error("file upload should have multipart/form-data content type")
		}
	}
}

func TestSwagger20ResponseConversion(t *testing.T) {
	specPath := filepath.Join("..", "..", "internal", "integration", "testdata", "swagger20.json")
	p := NewParser()
	spec, err := p.LoadSpec(specPath)
	if err != nil {
		t.Fatalf("failed to load spec: %v", err)
	}

	// Check GET /pets response
	petsPath := spec.Paths.Find("/pets")
	if petsPath == nil {
		t.Fatal("/pets path not found")
	}

	getOp := petsPath.Get
	if getOp == nil {
		t.Fatal("GET /pets operation not found")
	}

	resp200 := getOp.Responses.Status(200)
	if resp200 == nil {
		t.Fatal("200 response not found")
	}

	// Should have content after conversion
	if len(resp200.Value.Content) == 0 {
		t.Error("response should have content after conversion")
	}

	// Should have response headers
	if resp200.Value.Headers == nil {
		t.Error("response headers should be preserved")
	} else if resp200.Value.Headers["X-Total-Count"] == nil {
		t.Error("X-Total-Count header should be preserved")
	}
}

func TestSwagger20ParameterConversion(t *testing.T) {
	specPath := filepath.Join("..", "..", "internal", "integration", "testdata", "swagger20.json")
	p := NewParser()
	spec, err := p.LoadSpec(specPath)
	if err != nil {
		t.Fatalf("failed to load spec: %v", err)
	}

	// Check GET /pets parameters
	petsPath := spec.Paths.Find("/pets")
	if petsPath == nil {
		t.Fatal("/pets path not found")
	}

	getOp := petsPath.Get
	if getOp == nil {
		t.Fatal("GET /pets operation not found")
	}

	// Should have query and header parameters (body parameter should become requestBody)
	paramNames := make(map[string]string)
	for _, param := range getOp.Parameters {
		paramNames[param.Value.Name] = param.Value.In
	}

	// Verify query parameters
	if loc, ok := paramNames["limit"]; !ok || loc != "query" {
		t.Error("'limit' query parameter not found")
	}
	if loc, ok := paramNames["status"]; !ok || loc != "query" {
		t.Error("'status' query parameter not found")
	}

	// Verify header parameter
	if loc, ok := paramNames["X-Request-ID"]; !ok || loc != "header" {
		t.Error("'X-Request-ID' header parameter not found")
	}
}

func TestSwagger20SchemaPropertiesPreserved(t *testing.T) {
	specPath := filepath.Join("..", "..", "internal", "integration", "testdata", "swagger20.json")
	p := NewParser()
	spec, err := p.LoadSpec(specPath)
	if err != nil {
		t.Fatalf("failed to load spec: %v", err)
	}

	// Check Pet schema properties are preserved
	petSchema := spec.Components.Schemas["Pet"]
	if petSchema == nil {
		t.Fatal("Pet schema not found")
	}

	// Check required fields
	requiredFound := make(map[string]bool)
	for _, req := range petSchema.Value.Required {
		requiredFound[req] = true
	}

	expectedRequired := []string{"id", "name"}
	for _, req := range expectedRequired {
		if !requiredFound[req] {
			t.Errorf("Pet schema should have '%s' as required field", req)
		}
	}

	// Check properties
	expectedProps := []string{"id", "name", "category", "status", "tags", "createdAt"}
	for _, prop := range expectedProps {
		if petSchema.Value.Properties[prop] == nil {
			t.Errorf("Pet schema missing property '%s'", prop)
		}
	}

	// Check enum values are preserved
	statusProp := petSchema.Value.Properties["status"]
	if statusProp != nil && statusProp.Value.Enum != nil {
		if len(statusProp.Value.Enum) != 3 {
			t.Errorf("expected 3 enum values for status, got %d", len(statusProp.Value.Enum))
		}
	}
}

func TestSwagger20NestedRefsConversion(t *testing.T) {
	specPath := filepath.Join("..", "..", "internal", "integration", "testdata", "swagger20.json")
	p := NewParser()
	spec, err := p.LoadSpec(specPath)
	if err != nil {
		t.Fatalf("failed to load spec: %v", err)
	}

	// Verify nested $refs are preserved: Pet -> Category, Pet -> Tag
	petSchema := spec.Components.Schemas["Pet"]
	if petSchema == nil {
		t.Fatal("Pet schema not found")
	}

	// Category reference
	categoryProp := petSchema.Value.Properties["category"]
	if categoryProp == nil {
		t.Error("Pet.category property not found")
	}

	// Tags reference (array of Tag)
	tagsProp := petSchema.Value.Properties["tags"]
	if tagsProp == nil {
		t.Error("Pet.tags property not found")
	}

	// Verify Category schema exists
	categorySchema := spec.Components.Schemas["Category"]
	if categorySchema == nil {
		t.Error("Category schema not found")
	}

	// Verify Tag schema exists
	tagSchema := spec.Components.Schemas["Tag"]
	if tagSchema == nil {
		t.Error("Tag schema not found")
	}
}

func TestSwagger20SpecInfo(t *testing.T) {
	specPath := filepath.Join("..", "..", "internal", "integration", "testdata", "swagger20.json")
	p := NewParser()
	spec, err := p.LoadSpec(specPath)
	if err != nil {
		t.Fatalf("failed to load spec: %v", err)
	}

	info := GetSpecInfo(spec, specPath)
	if info == nil {
		t.Fatal("GetSpecInfo returned nil")
	}

	if info.Title != "Swagger 2.0 Pet Store" {
		t.Errorf("expected title 'Swagger 2.0 Pet Store', got '%s'", info.Title)
	}

	if info.Version != "1.0.0" {
		t.Errorf("expected version '1.0.0', got '%s'", info.Version)
	}

	// Should have multiple paths
	if info.PathCount < 7 {
		t.Errorf("expected at least 7 paths, got %d", info.PathCount)
	}

	// Should have multiple operations
	if info.Operations < 10 {
		t.Errorf("expected at least 10 operations, got %d", info.Operations)
	}
}

// =============================================================================
// Swagger 2.0 YAML Format Tests
// =============================================================================

func TestLoadSwagger20YAMLSpec(t *testing.T) {
	// Load the Swagger 2.0 YAML spec from testdata
	specPath := filepath.Join("..", "..", "internal", "integration", "testdata", "swagger20.yaml")
	p := NewParser()
	spec, err := p.LoadSpec(specPath)
	if err != nil {
		t.Fatalf("failed to load Swagger 2.0 YAML spec: %v", err)
	}

	// Verify basic info (should be preserved after conversion)
	if spec.Info.Title != "Swagger 2.0 YAML Test" {
		t.Errorf("expected title 'Swagger 2.0 YAML Test', got '%s'", spec.Info.Title)
	}

	if spec.Info.Version != "1.0.0" {
		t.Errorf("expected version '1.0.0', got '%s'", spec.Info.Version)
	}

	if spec.Info.Description == "" {
		t.Error("description should not be empty after conversion")
	}
}

func TestSwagger20YAMLServerConversion(t *testing.T) {
	// Swagger 2.0 YAML host + basePath + schemes should be converted to servers
	specPath := filepath.Join("..", "..", "internal", "integration", "testdata", "swagger20.yaml")
	p := NewParser()
	spec, err := p.LoadSpec(specPath)
	if err != nil {
		t.Fatalf("failed to load spec: %v", err)
	}

	// After conversion, servers should be populated from host/basePath/schemes
	if len(spec.Servers) == 0 {
		t.Error("expected at least one server after conversion from Swagger 2.0 YAML")
	}

	// The first server should contain the host and basePath
	if len(spec.Servers) > 0 {
		serverURL := spec.Servers[0].URL
		if serverURL == "" {
			t.Error("server URL should not be empty")
		}
		// Should contain the host and basePath
		if !strings.Contains(serverURL, "api.example.com") {
			t.Errorf("server URL should contain 'api.example.com', got '%s'", serverURL)
		}
	}
}

func TestSwagger20YAMLPathsConversion(t *testing.T) {
	specPath := filepath.Join("..", "..", "internal", "integration", "testdata", "swagger20.yaml")
	p := NewParser()
	spec, err := p.LoadSpec(specPath)
	if err != nil {
		t.Fatalf("failed to load spec: %v", err)
	}

	// Verify paths are preserved
	expectedPaths := []string{
		"/pets",
		"/pets/{petId}",
	}

	for _, path := range expectedPaths {
		if spec.Paths.Find(path) == nil {
			t.Errorf("path '%s' not found after conversion", path)
		}
	}
}

func TestSwagger20YAMLOperationsConversion(t *testing.T) {
	specPath := filepath.Join("..", "..", "internal", "integration", "testdata", "swagger20.yaml")
	p := NewParser()
	spec, err := p.LoadSpec(specPath)
	if err != nil {
		t.Fatalf("failed to load spec: %v", err)
	}

	// Check /pets operations
	petsPath := spec.Paths.Find("/pets")
	if petsPath == nil {
		t.Fatal("/pets path not found")
	}

	if petsPath.Get == nil {
		t.Error("GET /pets operation not found")
	} else {
		if petsPath.Get.OperationID != "listPets" {
			t.Errorf("expected operationId 'listPets', got '%s'", petsPath.Get.OperationID)
		}
	}

	if petsPath.Post == nil {
		t.Error("POST /pets operation not found")
	} else {
		if petsPath.Post.OperationID != "createPet" {
			t.Errorf("expected operationId 'createPet', got '%s'", petsPath.Post.OperationID)
		}
	}

	// Check /pets/{petId} operations
	petByIdPath := spec.Paths.Find("/pets/{petId}")
	if petByIdPath == nil {
		t.Fatal("/pets/{petId} path not found")
	}

	if petByIdPath.Get == nil {
		t.Error("GET /pets/{petId} operation not found")
	}
	if petByIdPath.Delete == nil {
		t.Error("DELETE /pets/{petId} operation not found")
	}
}

func TestSwagger20YAMLDefinitionsConversion(t *testing.T) {
	// Swagger 2.0 YAML definitions should be converted to components/schemas
	specPath := filepath.Join("..", "..", "internal", "integration", "testdata", "swagger20.yaml")
	p := NewParser()
	spec, err := p.LoadSpec(specPath)
	if err != nil {
		t.Fatalf("failed to load spec: %v", err)
	}

	// Verify schemas exist in components
	if spec.Components == nil || spec.Components.Schemas == nil {
		t.Fatal("Components.Schemas not found after conversion")
	}

	expectedSchemas := []string{"Pet", "NewPet", "Error"}
	for _, name := range expectedSchemas {
		if spec.Components.Schemas[name] == nil {
			t.Errorf("schema '%s' not found after conversion", name)
		}
	}
}

func TestSwagger20YAMLSecurityDefinitionsConversion(t *testing.T) {
	// Swagger 2.0 YAML securityDefinitions should be converted to components/securitySchemes
	specPath := filepath.Join("..", "..", "internal", "integration", "testdata", "swagger20.yaml")
	p := NewParser()
	spec, err := p.LoadSpec(specPath)
	if err != nil {
		t.Fatalf("failed to load spec: %v", err)
	}

	if spec.Components == nil || spec.Components.SecuritySchemes == nil {
		t.Fatal("Components.SecuritySchemes not found after conversion")
	}

	// Check api_key conversion
	apiKey := spec.Components.SecuritySchemes["api_key"]
	if apiKey == nil {
		t.Error("api_key security scheme not found")
	} else {
		if apiKey.Value.Type != "apiKey" {
			t.Errorf("expected api_key type 'apiKey', got '%s'", apiKey.Value.Type)
		}
		if apiKey.Value.In != "header" {
			t.Errorf("expected api_key in 'header', got '%s'", apiKey.Value.In)
		}
	}
}

// =============================================================================
// SpecFetchOptions Tests
// =============================================================================

func TestSpecFetchOptionsClone(t *testing.T) {
	original := &SpecFetchOptions{
		Headers: map[string]string{
			"X-Custom": "value",
		},
		AuthType:     "bearer",
		AuthToken:    "secret",
		AuthKeyName:  "X-API-Key",
		AuthLocation: "header",
	}

	clone := original.Clone()

	// Verify values are copied
	if clone.AuthType != original.AuthType {
		t.Errorf("expected AuthType %q, got %q", original.AuthType, clone.AuthType)
	}
	if clone.AuthToken != original.AuthToken {
		t.Errorf("expected AuthToken %q, got %q", original.AuthToken, clone.AuthToken)
	}
	if clone.Headers["X-Custom"] != "value" {
		t.Error("expected headers to be copied")
	}

	// Verify it's a deep copy (modifying clone doesn't affect original)
	clone.Headers["X-Custom"] = "modified"
	if original.Headers["X-Custom"] == "modified" {
		t.Error("modifying clone should not affect original")
	}
}

func TestSpecFetchOptionsCloneNil(t *testing.T) {
	var opts *SpecFetchOptions
	clone := opts.Clone()
	if clone != nil {
		t.Error("Clone of nil should return nil")
	}
}

func TestSpecFetchOptionsMerge(t *testing.T) {
	base := &SpecFetchOptions{
		Headers: map[string]string{
			"X-Base":   "base-value",
			"X-Shared": "base-shared",
		},
		AuthType:  "bearer",
		AuthToken: "base-token",
	}

	override := &SpecFetchOptions{
		Headers: map[string]string{
			"X-Override": "override-value",
			"X-Shared":   "override-shared",
		},
		AuthType:     "api_key",
		AuthToken:    "override-token",
		AuthKeyName:  "X-API-Key",
		AuthLocation: "query",
	}

	merged := base.Merge(override)

	// Override auth should take precedence
	if merged.AuthType != "api_key" {
		t.Errorf("expected AuthType 'api_key', got %q", merged.AuthType)
	}
	if merged.AuthToken != "override-token" {
		t.Errorf("expected AuthToken 'override-token', got %q", merged.AuthToken)
	}
	if merged.AuthKeyName != "X-API-Key" {
		t.Errorf("expected AuthKeyName 'X-API-Key', got %q", merged.AuthKeyName)
	}
	if merged.AuthLocation != "query" {
		t.Errorf("expected AuthLocation 'query', got %q", merged.AuthLocation)
	}

	// Headers should be merged
	if merged.Headers["X-Base"] != "base-value" {
		t.Error("expected base header to be preserved")
	}
	if merged.Headers["X-Override"] != "override-value" {
		t.Error("expected override header to be present")
	}
	if merged.Headers["X-Shared"] != "override-shared" {
		t.Error("expected override to take precedence for shared header")
	}
}

func TestSpecFetchOptionsMergeNilBase(t *testing.T) {
	var base *SpecFetchOptions
	override := &SpecFetchOptions{
		AuthType: "bearer",
	}

	merged := base.Merge(override)
	if merged.AuthType != "bearer" {
		t.Errorf("expected AuthType 'bearer', got %q", merged.AuthType)
	}
}

func TestSpecFetchOptionsMergeNilOverride(t *testing.T) {
	base := &SpecFetchOptions{
		AuthType: "bearer",
	}
	var override *SpecFetchOptions

	merged := base.Merge(override)
	if merged.AuthType != "bearer" {
		t.Errorf("expected AuthType 'bearer', got %q", merged.AuthType)
	}
}

func TestSpecFetchOptionsMergeBothNil(t *testing.T) {
	var base, override *SpecFetchOptions
	merged := base.Merge(override)
	if merged != nil {
		t.Error("expected nil when both are nil")
	}
}

func TestNewParserWithFetchOptions(t *testing.T) {
	opts := &SpecFetchOptions{
		Headers: map[string]string{
			"X-Custom": "value",
		},
		AuthType:  "bearer",
		AuthToken: "test-token",
	}

	p := NewParser(WithFetchOptions(opts))

	if p.fetchOptions == nil {
		t.Fatal("expected fetchOptions to be set")
	}
	if p.fetchOptions.AuthType != "bearer" {
		t.Errorf("expected AuthType 'bearer', got %q", p.fetchOptions.AuthType)
	}
}

func TestLoadSpecWithFetchOptions(t *testing.T) {
	var receivedHeaders http.Header

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedHeaders = r.Header.Clone()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"openapi":"3.0.0","info":{"title":"Test","version":"1.0.0"},"paths":{}}`))
	}))
	defer server.Close()

	// Test bearer auth
	opts := &SpecFetchOptions{
		Headers: map[string]string{
			"X-Custom-Header": "custom-value",
		},
		AuthType:  "bearer",
		AuthToken: "my-bearer-token",
	}

	p := NewParser(WithFetchOptions(opts))
	_, err := p.LoadSpec(server.URL)
	if err != nil {
		t.Fatalf("failed to load spec: %v", err)
	}

	// Verify custom header was sent
	if receivedHeaders.Get("X-Custom-Header") != "custom-value" {
		t.Errorf("expected custom header 'custom-value', got %q", receivedHeaders.Get("X-Custom-Header"))
	}

	// Verify bearer auth was sent
	authHeader := receivedHeaders.Get("Authorization")
	if authHeader != "Bearer my-bearer-token" {
		t.Errorf("expected Authorization 'Bearer my-bearer-token', got %q", authHeader)
	}
}

func TestLoadSpecWithOptionsOverride(t *testing.T) {
	var receivedHeaders http.Header

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedHeaders = r.Header.Clone()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"openapi":"3.0.0","info":{"title":"Test","version":"1.0.0"},"paths":{}}`))
	}))
	defer server.Close()

	// Parser with default options
	defaultOpts := &SpecFetchOptions{
		Headers: map[string]string{
			"X-Default": "default-value",
		},
		AuthType:  "bearer",
		AuthToken: "default-token",
	}
	p := NewParser(WithFetchOptions(defaultOpts))

	// Per-spec override
	overrideOpts := &SpecFetchOptions{
		Headers: map[string]string{
			"X-Override": "override-value",
		},
		AuthType:  "api_key",
		AuthToken: "override-key",
	}

	ctx := context.Background()
	_, err := p.LoadSpecWithOptions(ctx, server.URL, overrideOpts)
	if err != nil {
		t.Fatalf("failed to load spec: %v", err)
	}

	// Verify default header is present
	if receivedHeaders.Get("X-Default") != "default-value" {
		t.Errorf("expected default header to be present")
	}

	// Verify override header is present
	if receivedHeaders.Get("X-Override") != "override-value" {
		t.Errorf("expected override header to be present")
	}

	// Verify override auth takes precedence (api_key defaults to X-API-Key header)
	if receivedHeaders.Get("X-API-Key") != "override-key" {
		t.Errorf("expected X-API-Key header with override-key, got %q", receivedHeaders.Get("X-API-Key"))
	}

	// Bearer token should NOT be present (api_key override)
	if authHeader := receivedHeaders.Get("Authorization"); authHeader != "" {
		t.Errorf("expected no Authorization header when using api_key, got %q", authHeader)
	}
}

func TestLoadSpecWithAPIKeyQueryAuth(t *testing.T) {
	var receivedQuery string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedQuery = r.URL.RawQuery
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"openapi":"3.0.0","info":{"title":"Test","version":"1.0.0"},"paths":{}}`))
	}))
	defer server.Close()

	opts := &SpecFetchOptions{
		AuthType:     "api_key",
		AuthToken:    "my-api-key",
		AuthKeyName:  "api_key",
		AuthLocation: "query",
	}

	p := NewParser(WithFetchOptions(opts))
	_, err := p.LoadSpec(server.URL)
	if err != nil {
		t.Fatalf("failed to load spec: %v", err)
	}

	// Verify api_key was added to query
	if receivedQuery != "api_key=my-api-key" {
		t.Errorf("expected query 'api_key=my-api-key', got %q", receivedQuery)
	}
}

func TestLoadSpecWithBasicAuth(t *testing.T) {
	var receivedHeaders http.Header

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedHeaders = r.Header.Clone()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"openapi":"3.0.0","info":{"title":"Test","version":"1.0.0"},"paths":{}}`))
	}))
	defer server.Close()

	opts := &SpecFetchOptions{
		AuthType:  "basic",
		AuthToken: "dXNlcjpwYXNz", // base64("user:pass")
	}

	p := NewParser(WithFetchOptions(opts))
	_, err := p.LoadSpec(server.URL)
	if err != nil {
		t.Fatalf("failed to load spec: %v", err)
	}

	authHeader := receivedHeaders.Get("Authorization")
	if authHeader != "Basic dXNlcjpwYXNz" {
		t.Errorf("expected Authorization 'Basic dXNlcjpwYXNz', got %q", authHeader)
	}
}

func TestGetHTTPClient(t *testing.T) {
	customClient := &http.Client{Timeout: 60 * time.Second}
	p := NewParser(WithHTTPClient(customClient))

	if p.GetHTTPClient() != customClient {
		t.Error("GetHTTPClient should return the configured client")
	}
}

func TestGetFetchOptions(t *testing.T) {
	opts := &SpecFetchOptions{AuthType: "bearer"}
	p := NewParser(WithFetchOptions(opts))

	if p.GetFetchOptions() != opts {
		t.Error("GetFetchOptions should return the configured options")
	}
}
