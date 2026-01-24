package spec

import (
	"encoding/json"
	"testing"
)

// =============================================================================
// ContentFormat Detection Tests
// =============================================================================

func TestContentDetector_DetectFormat_JSON(t *testing.T) {
	detector := NewContentDetector()

	testCases := []struct {
		name     string
		input    []byte
		expected ContentFormat
	}{
		{
			name:     "JSON object",
			input:    []byte(`{"key": "value"}`),
			expected: FormatJSON,
		},
		{
			name:     "JSON array",
			input:    []byte(`[1, 2, 3]`),
			expected: FormatJSON,
		},
		{
			name:     "JSON with whitespace",
			input:    []byte(`  { "key": "value" }  `),
			expected: FormatJSON,
		},
		{
			name:     "OpenAPI 3 JSON",
			input:    []byte(`{"openapi": "3.0.0", "info": {"title": "Test"}}`),
			expected: FormatJSON,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			format := detector.DetectFormat(tc.input)
			if format != tc.expected {
				t.Errorf("expected %s, got %s", tc.expected, format)
			}
		})
	}
}

func TestContentDetector_DetectFormat_YAML(t *testing.T) {
	detector := NewContentDetector()

	testCases := []struct {
		name     string
		input    []byte
		expected ContentFormat
	}{
		{
			name:     "YAML key-value",
			input:    []byte("key: value"),
			expected: FormatYAML,
		},
		{
			name:     "YAML with list",
			input:    []byte("- item1\n- item2"),
			expected: FormatYAML,
		},
		{
			name:     "YAML document separator",
			input:    []byte("---\nkey: value"),
			expected: FormatYAML,
		},
		{
			name:     "YAML comment",
			input:    []byte("# comment\nkey: value"),
			expected: FormatYAML,
		},
		{
			name:     "Swagger 2.0 YAML",
			input:    []byte("swagger: '2.0'\ninfo:\n  title: Test"),
			expected: FormatYAML,
		},
		{
			name:     "OpenAPI 3 YAML",
			input:    []byte("openapi: 3.0.0\ninfo:\n  title: Test"),
			expected: FormatYAML,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			format := detector.DetectFormat(tc.input)
			if format != tc.expected {
				t.Errorf("expected %s, got %s", tc.expected, format)
			}
		})
	}
}

func TestContentDetector_DetectFormatFromContentType(t *testing.T) {
	detector := NewContentDetector()

	testCases := []struct {
		contentType string
		expected    ContentFormat
	}{
		{"application/json", FormatJSON},
		{"application/json; charset=utf-8", FormatJSON},
		{"text/json", FormatJSON},
		{"application/yaml", FormatYAML},
		{"text/yaml", FormatYAML},
		{"application/x-yaml", FormatYAML},
		{"text/x-yaml", FormatYAML},
		{"text/plain", FormatUnknown},
		{"application/octet-stream", FormatUnknown},
	}

	for _, tc := range testCases {
		t.Run(tc.contentType, func(t *testing.T) {
			format := detector.DetectFormatFromContentType(tc.contentType)
			if format != tc.expected {
				t.Errorf("expected %s, got %s", tc.expected, format)
			}
		})
	}
}

func TestContentDetector_DetectFormatFromExtension(t *testing.T) {
	detector := NewContentDetector()

	testCases := []struct {
		filename string
		expected ContentFormat
	}{
		{"spec.json", FormatJSON},
		{"openapi.JSON", FormatJSON},
		{"swagger.yaml", FormatYAML},
		{"api.yml", FormatYAML},
		{"API.YAML", FormatYAML},
		{"file.txt", FormatUnknown},
		{"noextension", FormatUnknown},
	}

	for _, tc := range testCases {
		t.Run(tc.filename, func(t *testing.T) {
			format := detector.DetectFormatFromExtension(tc.filename)
			if format != tc.expected {
				t.Errorf("expected %s, got %s", tc.expected, format)
			}
		})
	}
}

// =============================================================================
// JSON Strategy Tests
// =============================================================================

func TestJSONStrategy_CanHandle(t *testing.T) {
	strategy := &JSONStrategy{}

	testCases := []struct {
		name     string
		input    []byte
		expected bool
	}{
		{"JSON object", []byte(`{"key": "value"}`), true},
		{"JSON array", []byte(`[1, 2, 3]`), true},
		{"JSON with whitespace", []byte(`  {"key": "value"}`), true},
		{"YAML content", []byte("key: value"), false},
		{"Empty", []byte(""), false},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := strategy.CanHandle(tc.input)
			if result != tc.expected {
				t.Errorf("expected %v, got %v", tc.expected, result)
			}
		})
	}
}

func TestJSONStrategy_Unmarshal(t *testing.T) {
	strategy := &JSONStrategy{}

	var result map[string]string
	err := strategy.Unmarshal([]byte(`{"key": "value"}`), &result)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result["key"] != "value" {
		t.Errorf("expected 'value', got '%s'", result["key"])
	}
}

func TestJSONStrategy_ToJSON(t *testing.T) {
	strategy := &JSONStrategy{}

	input := []byte(`{"key": "value"}`)
	output, err := strategy.ToJSON(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should return the same content
	if string(output) != string(input) {
		t.Errorf("expected same content, got different")
	}
}

// =============================================================================
// YAML Strategy Tests
// =============================================================================

func TestYAMLStrategy_CanHandle(t *testing.T) {
	strategy := &YAMLStrategy{}

	testCases := []struct {
		name     string
		input    []byte
		expected bool
	}{
		{"YAML key-value", []byte("key: value"), true},
		{"YAML list", []byte("- item1\n- item2"), true},
		{"YAML document", []byte("---\nkey: value"), true},
		{"JSON object", []byte(`{"key": "value"}`), false},
		{"JSON array", []byte(`[1, 2, 3]`), false},
		{"Empty", []byte(""), false},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := strategy.CanHandle(tc.input)
			if result != tc.expected {
				t.Errorf("expected %v, got %v", tc.expected, result)
			}
		})
	}
}

func TestYAMLStrategy_Unmarshal(t *testing.T) {
	strategy := &YAMLStrategy{}

	var result map[string]string
	err := strategy.Unmarshal([]byte("key: value"), &result)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result["key"] != "value" {
		t.Errorf("expected 'value', got '%s'", result["key"])
	}
}

func TestYAMLStrategy_ToJSON(t *testing.T) {
	strategy := &YAMLStrategy{}

	input := []byte("key: value")
	output, err := strategy.ToJSON(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should convert to JSON
	expected := `{"key":"value"}`
	if string(output) != expected {
		t.Errorf("expected '%s', got '%s'", expected, string(output))
	}
}

func TestYAMLStrategy_ToJSON_NestedStructure(t *testing.T) {
	strategy := &YAMLStrategy{}

	input := []byte(`
swagger: '2.0'
info:
  title: Test API
  version: '1.0'
`)

	output, err := strategy.ToJSON(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify it's valid JSON with expected structure
	var result map[string]any
	if err := json.Unmarshal(output, &result); err != nil {
		t.Fatalf("output is not valid JSON: %v", err)
	}

	if result["swagger"] != "2.0" {
		t.Errorf("expected swagger '2.0', got '%v'", result["swagger"])
	}

	info, ok := result["info"].(map[string]any)
	if !ok {
		t.Fatal("expected info to be a map")
	}
	if info["title"] != "Test API" {
		t.Errorf("expected title 'Test API', got '%v'", info["title"])
	}
}

// =============================================================================
// Content Detector Fallback Tests
// =============================================================================

func TestContentDetector_UnmarshalWithFallback_JSON(t *testing.T) {
	detector := NewContentDetector()

	var result map[string]string
	err := detector.UnmarshalWithFallback([]byte(`{"key": "value"}`), &result)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result["key"] != "value" {
		t.Errorf("expected 'value', got '%s'", result["key"])
	}
}

func TestContentDetector_UnmarshalWithFallback_YAML(t *testing.T) {
	detector := NewContentDetector()

	var result map[string]string
	err := detector.UnmarshalWithFallback([]byte("key: value"), &result)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result["key"] != "value" {
		t.Errorf("expected 'value', got '%s'", result["key"])
	}
}

func TestContentDetector_ToJSONWithFallback(t *testing.T) {
	tests := []struct {
		name          string
		input         []byte
		expectedKey   string
		expectedValue string
	}{
		{
			name:          "JSON input",
			input:         []byte(`{"key": "value"}`),
			expectedKey:   "key",
			expectedValue: "value",
		},
		{
			name:          "YAML input",
			input:         []byte("key: value"),
			expectedKey:   "key",
			expectedValue: "value",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			detector := NewContentDetector()
			output, err := detector.ToJSONWithFallback(tt.input)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			var result map[string]string
			if err := json.Unmarshal(output, &result); err != nil {
				t.Fatalf("output is not valid JSON: %v", err)
			}

			if result[tt.expectedKey] != tt.expectedValue {
				t.Errorf("expected '%s', got '%s'", tt.expectedValue, result[tt.expectedKey])
			}
		})
	}
}

func TestContentDetector_GetStrategy(t *testing.T) {
	detector := NewContentDetector()

	jsonStrategy := detector.GetStrategy(FormatJSON)
	if jsonStrategy == nil || jsonStrategy.Format() != FormatJSON {
		t.Error("expected JSON strategy")
	}

	yamlStrategy := detector.GetStrategy(FormatYAML)
	if yamlStrategy == nil || yamlStrategy.Format() != FormatYAML {
		t.Error("expected YAML strategy")
	}

	unknownStrategy := detector.GetStrategy(FormatUnknown)
	if unknownStrategy != nil {
		t.Error("expected nil for unknown format")
	}
}
