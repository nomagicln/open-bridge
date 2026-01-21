package request

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
)

// TestIntegration_NestedObjectsAndArrays demonstrates the complete workflow
// of building request bodies with nested objects and arrays.
func TestIntegration_NestedObjectsAndArrays(t *testing.T) {
	b := NewBuilder(nil)

	// Define a realistic schema for creating a user with nested address and tags
	schema := &openapi3.Schema{
		Type: &openapi3.Types{"object"},
		Properties: map[string]*openapi3.SchemaRef{
			"name": {
				Value: &openapi3.Schema{Type: &openapi3.Types{"string"}},
			},
			"email": {
				Value: &openapi3.Schema{Type: &openapi3.Types{"string"}},
			},
			"age": {
				Value: &openapi3.Schema{Type: &openapi3.Types{"integer"}},
			},
			"address": {
				Value: &openapi3.Schema{
					Type: &openapi3.Types{"object"},
					Properties: map[string]*openapi3.SchemaRef{
						"street": {
							Value: &openapi3.Schema{Type: &openapi3.Types{"string"}},
						},
						"city": {
							Value: &openapi3.Schema{Type: &openapi3.Types{"string"}},
						},
						"zip": {
							Value: &openapi3.Schema{Type: &openapi3.Types{"string"}},
						},
					},
				},
			},
			"tags": {
				Value: &openapi3.Schema{
					Type: &openapi3.Types{"array"},
					Items: &openapi3.SchemaRef{
						Value: &openapi3.Schema{Type: &openapi3.Types{"string"}},
					},
				},
			},
		},
	}

	// Simulate CLI flags parsed from:
	// myapp create user --name "John Doe" --email "john@example.com" --age 30 \
	//   --address.street "123 Main St" --address.city "NYC" --address.zip "10001" \
	//   --tags "admin" --tags "developer"
	params := map[string]any{
		"name":  "John Doe",
		"email": "john@example.com",
		"age":   "30",
		"address": map[string]any{
			"street": "123 Main St",
			"city":   "NYC",
			"zip":    "10001",
		},
		"tags": []string{"admin", "developer"},
	}

	requestBody := &openapi3.RequestBody{
		Content: openapi3.Content{
			"application/json": &openapi3.MediaType{
				Schema: &openapi3.SchemaRef{Value: schema},
			},
		},
	}

	result, err := b.buildRequestBody(params, openapi3.Parameters{}, requestBody)
	if err != nil {
		t.Fatalf("buildRequestBody() error = %v", err)
	}

	var body map[string]any
	if err := json.Unmarshal(result, &body); err != nil {
		t.Fatalf("Failed to unmarshal result: %v", err)
	}

	// Verify top-level fields
	if body["name"] != "John Doe" {
		t.Errorf("Expected name='John Doe', got %v", body["name"])
	}
	if body["email"] != "john@example.com" {
		t.Errorf("Expected email='john@example.com', got %v", body["email"])
	}

	// Verify nested address object
	address, ok := body["address"].(map[string]any)
	if !ok {
		t.Fatal("address is not a map")
	}
	if address["street"] != "123 Main St" {
		t.Errorf("Expected street='123 Main St', got %v", address["street"])
	}
	if address["city"] != "NYC" {
		t.Errorf("Expected city='NYC', got %v", address["city"])
	}

	// Verify tags array
	tags, ok := body["tags"].([]any)
	if !ok {
		t.Fatal("tags is not an array")
	}
	if len(tags) != 2 {
		t.Errorf("Expected 2 tags, got %d", len(tags))
	}
}

// TestIntegration_FileInput demonstrates reading request body from a file.
func TestIntegration_FileInput(t *testing.T) {
	b := NewBuilder(nil)

	// Create temp file with JSON
	tmpDir := t.TempDir()
	bodyFile := filepath.Join(tmpDir, "user.json")
	bodyJSON := `{
		"name": "Jane Smith",
		"email": "jane@example.com",
		"age": 28,
		"address": {
			"street": "456 Oak Ave",
			"city": "Boston",
			"zip": "02101"
		},
		"tags": ["user", "premium"]
	}`
	if err := os.WriteFile(bodyFile, []byte(bodyJSON), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	// Simulate CLI flag: --body @user.json
	params := map[string]any{
		"body": "@" + bodyFile,
	}

	result, err := b.buildRequestBody(params, openapi3.Parameters{}, nil)
	if err != nil {
		t.Fatalf("buildRequestBody() error = %v", err)
	}

	var body map[string]any
	if err := json.Unmarshal(result, &body); err != nil {
		t.Fatalf("Failed to unmarshal result: %v", err)
	}

	if body["name"] != "Jane Smith" {
		t.Errorf("Expected name='Jane Smith', got %v", body["name"])
	}

	address, ok := body["address"].(map[string]any)
	if !ok {
		t.Fatal("address is not a map")
	}
	if address["city"] != "Boston" {
		t.Errorf("Expected city='Boston', got %v", address["city"])
	}

	tags, ok := body["tags"].([]any)
	if !ok {
		t.Fatal("tags is not an array")
	}
	if len(tags) != 2 {
		t.Errorf("Expected 2 tags, got %d", len(tags))
	}
}

// TestIntegration_CommaSeparatedArrays demonstrates comma-separated array values.
func TestIntegration_CommaSeparatedArrays(t *testing.T) {
	b := NewBuilder(nil)

	schema := objectSchemaWithStringArray("name", "roles")

	// Simulate CLI flag: --roles "admin,developer,user"
	params := map[string]any{
		"name":  "Bob",
		"roles": "admin,developer,user",
	}

	requestBody := &openapi3.RequestBody{
		Content: openapi3.Content{
			"application/json": &openapi3.MediaType{
				Schema: &openapi3.SchemaRef{Value: schema},
			},
		},
	}

	result, err := b.buildRequestBody(params, openapi3.Parameters{}, requestBody)
	if err != nil {
		t.Fatalf("buildRequestBody() error = %v", err)
	}

	var body map[string]any
	if err := json.Unmarshal(result, &body); err != nil {
		t.Fatalf("Failed to unmarshal result: %v", err)
	}

	roles, ok := body["roles"].([]any)
	if !ok {
		t.Fatal("roles is not an array")
	}
	if len(roles) != 3 {
		t.Errorf("Expected 3 roles, got %d", len(roles))
	}
	if roles[0] != "admin" || roles[1] != "developer" || roles[2] != "user" {
		t.Errorf("Unexpected roles: %v", roles)
	}
}
