package request

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
)

func TestHandleBodyFlag_DirectJSON(t *testing.T) {
	b := NewBuilder(nil)

	tests := []struct {
		name    string
		input   any
		wantErr bool
	}{
		{
			name:    "valid JSON object",
			input:   `{"name":"John","age":30}`,
			wantErr: false,
		},
		{
			name:    "valid JSON array",
			input:   `["item1","item2"]`,
			wantErr: false,
		},
		{
			name:    "JSON with whitespace",
			input:   `  {"name":"John"}  `,
			wantErr: false,
		},
		{
			name:    "invalid JSON",
			input:   `{invalid}`,
			wantErr: true,
		},
		{
			name:    "map input",
			input:   map[string]any{"name": "John", "age": 30},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := b.handleBodyFlag(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("handleBodyFlag() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && result == nil {
				t.Error("handleBodyFlag() returned nil result")
			}
		})
	}
}

func TestHandleBodyFlag_FileInput(t *testing.T) {
	b := NewBuilder(nil)

	// Create temp directory
	tmpDir := t.TempDir()

	// Create test file with valid JSON
	validFile := filepath.Join(tmpDir, "valid.json")
	validJSON := `{"name":"John","email":"john@example.com"}`
	if err := os.WriteFile(validFile, []byte(validJSON), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	// Create test file with invalid JSON
	invalidFile := filepath.Join(tmpDir, "invalid.json")
	if err := os.WriteFile(invalidFile, []byte(`{invalid}`), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{
			name:    "valid file",
			input:   "@" + validFile,
			wantErr: false,
		},
		{
			name:    "invalid JSON file",
			input:   "@" + invalidFile,
			wantErr: true,
		},
		{
			name:    "non-existent file",
			input:   "@/nonexistent/file.json",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := b.handleBodyFlag(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("handleBodyFlag() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr {
				if result == nil {
					t.Error("handleBodyFlag() returned nil result")
				}
				// Verify it's valid JSON
				var temp any
				if err := json.Unmarshal(result, &temp); err != nil {
					t.Errorf("Result is not valid JSON: %v", err)
				}
			}
		})
	}
}

func TestConstructArrayValue(t *testing.T) {
	b := NewBuilder(nil)
	schema := &openapi3.Schema{Type: &openapi3.Types{"array"}}

	tests := []struct {
		name     string
		input    any
		expected int // expected length
	}{
		{
			name:     "string array",
			input:    []string{"admin", "developer"},
			expected: 2,
		},
		{
			name:     "any array",
			input:    []any{"admin", "developer"},
			expected: 2,
		},
		{
			name:     "comma-separated string",
			input:    "admin,developer,user",
			expected: 3,
		},
		{
			name:     "single string",
			input:    "admin",
			expected: 1,
		},
		{
			name:     "other type",
			input:    123,
			expected: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := b.constructArrayValue(tt.input, schema)
			if len(result) != tt.expected {
				t.Errorf("constructArrayValue() length = %d, want %d", len(result), tt.expected)
			}
		})
	}
}

func TestBuildBodyFromSchema_NestedObjects(t *testing.T) {
	b := NewBuilder(nil)

	// Define schema with nested object
	schema := &openapi3.Schema{
		Type: &openapi3.Types{"object"},
		Properties: map[string]*openapi3.SchemaRef{
			"name": {
				Value: &openapi3.Schema{Type: &openapi3.Types{"string"}},
			},
			"address": {
				Value: &openapi3.Schema{
					Type: &openapi3.Types{"object"},
					Properties: map[string]*openapi3.SchemaRef{
						"city": {
							Value: &openapi3.Schema{Type: &openapi3.Types{"string"}},
						},
						"zip": {
							Value: &openapi3.Schema{Type: &openapi3.Types{"string"}},
						},
					},
				},
			},
		},
	}

	params := map[string]any{
		"name": "John",
		"address": map[string]any{
			"city": "NYC",
			"zip":  "10001",
		},
	}

	result, err := b.buildBodyFromSchema(params, schema)
	if err != nil {
		t.Fatalf("buildBodyFromSchema() error = %v", err)
	}

	var body map[string]any
	if err := json.Unmarshal(result, &body); err != nil {
		t.Fatalf("Failed to unmarshal result: %v", err)
	}

	if body["name"] != "John" {
		t.Errorf("Expected name=John, got %v", body["name"])
	}

	address, ok := body["address"].(map[string]any)
	if !ok {
		t.Fatal("address is not a map")
	}

	if address["city"] != "NYC" {
		t.Errorf("Expected city=NYC, got %v", address["city"])
	}
	if address["zip"] != "10001" {
		t.Errorf("Expected zip=10001, got %v", address["zip"])
	}
}

func TestBuildBodyFromSchema_Arrays(t *testing.T) {
	b := NewBuilder(nil)

	// Define schema with array property
	schema := &openapi3.Schema{
		Type: &openapi3.Types{"object"},
		Properties: map[string]*openapi3.SchemaRef{
			"name": {
				Value: &openapi3.Schema{Type: &openapi3.Types{"string"}},
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

	tests := []struct {
		name         string
		params       map[string]any
		expectedTags int
	}{
		{
			name: "array from repeated flags",
			params: map[string]any{
				"name": "John",
				"tags": []string{"admin", "developer"},
			},
			expectedTags: 2,
		},
		{
			name: "array from comma-separated",
			params: map[string]any{
				"name": "John",
				"tags": "admin,developer,user",
			},
			expectedTags: 3,
		},
		{
			name: "single tag",
			params: map[string]any{
				"name": "John",
				"tags": "admin",
			},
			expectedTags: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := b.buildBodyFromSchema(tt.params, schema)
			if err != nil {
				t.Fatalf("buildBodyFromSchema() error = %v", err)
			}

			var body map[string]any
			if err := json.Unmarshal(result, &body); err != nil {
				t.Fatalf("Failed to unmarshal result: %v", err)
			}

			tags, ok := body["tags"].([]any)
			if !ok {
				t.Fatal("tags is not an array")
			}

			if len(tags) != tt.expectedTags {
				t.Errorf("Expected %d tags, got %d", tt.expectedTags, len(tags))
			}
		})
	}
}

func TestBuildRequestBody_Integration(t *testing.T) {
	b := NewBuilder(nil)

	// Test with no request body schema
	t.Run("no schema", func(t *testing.T) {
		params := map[string]any{
			"name":  "John",
			"email": "john@example.com",
		}

		result, err := b.buildRequestBody(params, openapi3.Parameters{}, nil)
		if err != nil {
			t.Fatalf("buildRequestBody() error = %v", err)
		}

		var body map[string]any
		if err := json.Unmarshal(result, &body); err != nil {
			t.Fatalf("Failed to unmarshal result: %v", err)
		}

		if body["name"] != "John" {
			t.Errorf("Expected name=John, got %v", body["name"])
		}
	})

	// Test with --body flag
	t.Run("body flag", func(t *testing.T) {
		params := map[string]any{
			"body": `{"name":"Jane","age":25}`,
		}

		result, err := b.buildRequestBody(params, openapi3.Parameters{}, nil)
		if err != nil {
			t.Fatalf("buildRequestBody() error = %v", err)
		}

		var body map[string]any
		if err := json.Unmarshal(result, &body); err != nil {
			t.Fatalf("Failed to unmarshal result: %v", err)
		}

		if body["name"] != "Jane" {
			t.Errorf("Expected name=Jane, got %v", body["name"])
		}
	})
}

func TestValidateParams_RequiredParameters(t *testing.T) {
	b := NewBuilder(nil)

	tests := []struct {
		name    string
		params  map[string]any
		opParams openapi3.Parameters
		wantErr bool
		errMsg  string
	}{
		{
			name: "all required params present",
			params: map[string]any{
				"userId": 123,
				"name":   "John",
			},
			opParams: openapi3.Parameters{
				&openapi3.ParameterRef{
					Value: &openapi3.Parameter{
						Name:     "userId",
						In:       "path",
						Required: true,
						Schema: &openapi3.SchemaRef{
							Value: &openapi3.Schema{Type: &openapi3.Types{"integer"}},
						},
					},
				},
				&openapi3.ParameterRef{
					Value: &openapi3.Parameter{
						Name:     "name",
						In:       "query",
						Required: true,
						Schema: &openapi3.SchemaRef{
							Value: &openapi3.Schema{Type: &openapi3.Types{"string"}},
						},
					},
				},
			},
			wantErr: false,
		},
		{
			name: "missing required parameter",
			params: map[string]any{
				"name": "John",
			},
			opParams: openapi3.Parameters{
				&openapi3.ParameterRef{
					Value: &openapi3.Parameter{
						Name:     "userId",
						In:       "path",
						Required: true,
						Schema: &openapi3.SchemaRef{
							Value: &openapi3.Schema{Type: &openapi3.Types{"integer"}},
						},
					},
				},
			},
			wantErr: true,
			errMsg:  "required parameter 'userId' is missing",
		},
		{
			name: "optional parameter missing is ok",
			params: map[string]any{
				"userId": 123,
			},
			opParams: openapi3.Parameters{
				&openapi3.ParameterRef{
					Value: &openapi3.Parameter{
						Name:     "userId",
						In:       "path",
						Required: true,
						Schema: &openapi3.SchemaRef{
							Value: &openapi3.Schema{Type: &openapi3.Types{"integer"}},
						},
					},
				},
				&openapi3.ParameterRef{
					Value: &openapi3.Parameter{
						Name:     "filter",
						In:       "query",
						Required: false,
						Schema: &openapi3.SchemaRef{
							Value: &openapi3.Schema{Type: &openapi3.Types{"string"}},
						},
					},
				},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := b.ValidateParams(tt.params, tt.opParams, nil)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateParams() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.wantErr && err != nil && err.Error() != tt.errMsg {
				t.Errorf("ValidateParams() error message = %v, want %v", err.Error(), tt.errMsg)
			}
		})
	}
}

func TestValidateParams_TypeValidation(t *testing.T) {
	b := NewBuilder(nil)

	tests := []struct {
		name    string
		params  map[string]any
		opParams openapi3.Parameters
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid string parameter",
			params: map[string]any{
				"name": "John",
			},
			opParams: openapi3.Parameters{
				&openapi3.ParameterRef{
					Value: &openapi3.Parameter{
						Name:     "name",
						In:       "query",
						Required: true,
						Schema: &openapi3.SchemaRef{
							Value: &openapi3.Schema{Type: &openapi3.Types{"string"}},
						},
					},
				},
			},
			wantErr: false,
		},
		{
			name: "invalid string parameter - got integer",
			params: map[string]any{
				"name": 123,
			},
			opParams: openapi3.Parameters{
				&openapi3.ParameterRef{
					Value: &openapi3.Parameter{
						Name:     "name",
						In:       "query",
						Required: true,
						Schema: &openapi3.SchemaRef{
							Value: &openapi3.Schema{Type: &openapi3.Types{"string"}},
						},
					},
				},
			},
			wantErr: true,
			errMsg:  "parameter 'name' must be a string, got integer",
		},
		{
			name: "valid integer parameter",
			params: map[string]any{
				"age": 30,
			},
			opParams: openapi3.Parameters{
				&openapi3.ParameterRef{
					Value: &openapi3.Parameter{
						Name:     "age",
						In:       "query",
						Required: true,
						Schema: &openapi3.SchemaRef{
							Value: &openapi3.Schema{Type: &openapi3.Types{"integer"}},
						},
					},
				},
			},
			wantErr: false,
		},
		{
			name: "invalid integer parameter - got string",
			params: map[string]any{
				"age": "thirty",
			},
			opParams: openapi3.Parameters{
				&openapi3.ParameterRef{
					Value: &openapi3.Parameter{
						Name:     "age",
						In:       "query",
						Required: true,
						Schema: &openapi3.SchemaRef{
							Value: &openapi3.Schema{Type: &openapi3.Types{"integer"}},
						},
					},
				},
			},
			wantErr: true,
			errMsg:  "parameter 'age' must be an integer, got string",
		},
		{
			name: "valid boolean parameter",
			params: map[string]any{
				"active": true,
			},
			opParams: openapi3.Parameters{
				&openapi3.ParameterRef{
					Value: &openapi3.Parameter{
						Name:     "active",
						In:       "query",
						Required: true,
						Schema: &openapi3.SchemaRef{
							Value: &openapi3.Schema{Type: &openapi3.Types{"boolean"}},
						},
					},
				},
			},
			wantErr: false,
		},
		{
			name: "invalid boolean parameter - got string",
			params: map[string]any{
				"active": "yes",
			},
			opParams: openapi3.Parameters{
				&openapi3.ParameterRef{
					Value: &openapi3.Parameter{
						Name:     "active",
						In:       "query",
						Required: true,
						Schema: &openapi3.SchemaRef{
							Value: &openapi3.Schema{Type: &openapi3.Types{"boolean"}},
						},
					},
				},
			},
			wantErr: true,
			errMsg:  "parameter 'active' must be a boolean, got string",
		},
		{
			name: "valid array parameter",
			params: map[string]any{
				"tags": []string{"admin", "user"},
			},
			opParams: openapi3.Parameters{
				&openapi3.ParameterRef{
					Value: &openapi3.Parameter{
						Name:     "tags",
						In:       "query",
						Required: true,
						Schema: &openapi3.SchemaRef{
							Value: &openapi3.Schema{Type: &openapi3.Types{"array"}},
						},
					},
				},
			},
			wantErr: false,
		},
		{
			name: "invalid array parameter - got string",
			params: map[string]any{
				"tags": "admin",
			},
			opParams: openapi3.Parameters{
				&openapi3.ParameterRef{
					Value: &openapi3.Parameter{
						Name:     "tags",
						In:       "query",
						Required: true,
						Schema: &openapi3.SchemaRef{
							Value: &openapi3.Schema{Type: &openapi3.Types{"array"}},
						},
					},
				},
			},
			wantErr: true,
			errMsg:  "parameter 'tags' must be an array, got string",
		},
		{
			name: "valid number parameter - float",
			params: map[string]any{
				"price": 19.99,
			},
			opParams: openapi3.Parameters{
				&openapi3.ParameterRef{
					Value: &openapi3.Parameter{
						Name:     "price",
						In:       "query",
						Required: true,
						Schema: &openapi3.SchemaRef{
							Value: &openapi3.Schema{Type: &openapi3.Types{"number"}},
						},
					},
				},
			},
			wantErr: false,
		},
		{
			name: "valid number parameter - integer",
			params: map[string]any{
				"price": 20,
			},
			opParams: openapi3.Parameters{
				&openapi3.ParameterRef{
					Value: &openapi3.Parameter{
						Name:     "price",
						In:       "query",
						Required: true,
						Schema: &openapi3.SchemaRef{
							Value: &openapi3.Schema{Type: &openapi3.Types{"number"}},
						},
					},
				},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := b.ValidateParams(tt.params, tt.opParams, nil)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateParams() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.wantErr && err != nil && err.Error() != tt.errMsg {
				t.Errorf("ValidateParams() error message = %v, want %v", err.Error(), tt.errMsg)
			}
		})
	}
}

func TestValidateParams_EnumValidation(t *testing.T) {
	b := NewBuilder(nil)

	tests := []struct {
		name    string
		params  map[string]any
		opParams openapi3.Parameters
		wantErr bool
	}{
		{
			name: "valid enum value",
			params: map[string]any{
				"status": "active",
			},
			opParams: openapi3.Parameters{
				&openapi3.ParameterRef{
					Value: &openapi3.Parameter{
						Name:     "status",
						In:       "query",
						Required: true,
						Schema: &openapi3.SchemaRef{
							Value: &openapi3.Schema{
								Type: &openapi3.Types{"string"},
								Enum: []any{"active", "inactive", "pending"},
							},
						},
					},
				},
			},
			wantErr: false,
		},
		{
			name: "invalid enum value",
			params: map[string]any{
				"status": "deleted",
			},
			opParams: openapi3.Parameters{
				&openapi3.ParameterRef{
					Value: &openapi3.Parameter{
						Name:     "status",
						In:       "query",
						Required: true,
						Schema: &openapi3.SchemaRef{
							Value: &openapi3.Schema{
								Type: &openapi3.Types{"string"},
								Enum: []any{"active", "inactive", "pending"},
							},
						},
					},
				},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := b.ValidateParams(tt.params, tt.opParams, nil)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateParams() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestValidateParams_RequestBodyRequired(t *testing.T) {
	b := NewBuilder(nil)

	tests := []struct {
		name        string
		params      map[string]any
		opParams    openapi3.Parameters
		requestBody *openapi3.RequestBody
		wantErr     bool
		errMsg      string
	}{
		{
			name: "required body present via body flag",
			params: map[string]any{
				"body": `{"name":"John"}`,
			},
			opParams: openapi3.Parameters{},
			requestBody: &openapi3.RequestBody{
				Required: true,
			},
			wantErr: false,
		},
		{
			name: "required body present via body params",
			params: map[string]any{
				"name": "John",
				"age":  30,
			},
			opParams: openapi3.Parameters{},
			requestBody: &openapi3.RequestBody{
				Required: true,
			},
			wantErr: false,
		},
		{
			name:     "required body missing",
			params:   map[string]any{},
			opParams: openapi3.Parameters{},
			requestBody: &openapi3.RequestBody{
				Required: true,
			},
			wantErr: true,
			errMsg:  "request body is required for this operation",
		},
		{
			name:     "optional body missing is ok",
			params:   map[string]any{},
			opParams: openapi3.Parameters{},
			requestBody: &openapi3.RequestBody{
				Required: false,
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := b.ValidateParams(tt.params, tt.opParams, tt.requestBody)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateParams() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.wantErr && err != nil && err.Error() != tt.errMsg {
				t.Errorf("ValidateParams() error message = %v, want %v", err.Error(), tt.errMsg)
			}
		})
	}
}

func TestGetValueType(t *testing.T) {
	b := NewBuilder(nil)

	tests := []struct {
		name     string
		value    any
		expected string
	}{
		{"string", "hello", "string"},
		{"int", 42, "integer"},
		{"int64", int64(42), "integer"},
		{"float64", 3.14, "number"},
		{"float32", float32(3.14), "number"},
		{"bool", true, "boolean"},
		{"string array", []string{"a", "b"}, "array"},
		{"any array", []any{1, 2, 3}, "array"},
		{"map", map[string]any{"key": "value"}, "object"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := b.getValueType(tt.value)
			if result != tt.expected {
				t.Errorf("getValueType() = %v, want %v", result, tt.expected)
			}
		})
	}
}
