package spec

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
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
	spec := map[string]interface{}{
		"openapi": "3.0.0",
		"info": map[string]interface{}{
			"title":   "Remote API",
			"version": "1.0.0",
		},
		"paths": map[string]interface{}{
			"/test": map[string]interface{}{
				"get": map[string]interface{}{
					"operationId": "test",
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "OK",
						},
					},
				},
			},
		},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(spec)
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
