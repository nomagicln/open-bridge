package codegen

import (
	"bytes"
	"io"
	"net/http"
	"slices"
	"testing"
)

func TestNewGenerator_ValidFormats(t *testing.T) {
	tests := []struct {
		format OutputFormat
		name   string
	}{
		{FormatCurl, "curl"},
		{FormatNodeJS, "nodejs"},
		{FormatGo, "go"},
		{FormatPython, "python"},
	}

	for _, tt := range tests {
		t.Run(string(tt.format), func(t *testing.T) {
			gen, err := NewGenerator(tt.format, Options{})
			if err != nil {
				t.Fatalf("NewGenerator(%s) failed: %v", tt.format, err)
			}
			if gen == nil {
				t.Fatalf("NewGenerator(%s) returned nil generator", tt.format)
			}
		})
	}
}

func TestNewGenerator_InvalidFormat(t *testing.T) {
	invalidFormat := OutputFormat("invalid")
	gen, err := NewGenerator(invalidFormat, Options{})

	if err == nil {
		t.Fatal("NewGenerator(invalid) should return error")
	}
	if gen != nil {
		t.Fatal("NewGenerator(invalid) should return nil generator")
	}
}

func TestValidateFormat(t *testing.T) {
	tests := []struct {
		format string
		valid  bool
	}{
		{"curl", true},
		{"nodejs", true},
		{"go", true},
		{"python", true},
		{"invalid", false},
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.format, func(t *testing.T) {
			result := ValidateFormat(tt.format)
			if result != tt.valid {
				t.Fatalf("ValidateFormat(%q) = %v, want %v", tt.format, result, tt.valid)
			}
		})
	}
}

func TestListFormats(t *testing.T) {
	formats := ListFormats()

	if len(formats) == 0 {
		t.Fatal("ListFormats() returned empty list")
	}

	expectedFormats := map[string]bool{
		"curl":   true,
		"nodejs": true,
		"go":     true,
		"python": true,
	}

	for _, format := range formats {
		if !expectedFormats[format] {
			t.Errorf("ListFormats() contains unexpected format: %s", format)
		}
	}

	for expected := range expectedFormats {
		found := slices.Contains(formats, expected)
		if !found {
			t.Errorf("ListFormats() missing format: %s", expected)
		}
	}
}

func TestMaskSensitiveValue(t *testing.T) {
	tests := []struct {
		name        string
		value       string
		maskSecrets bool
		expected    string
	}{
		{
			name:        "Bearer token masking",
			value:       "Bearer sk_live_1234567890abcdef",
			maskSecrets: true,
			expected:    "Bearer <YOUR_API_KEY>",
		},
		{
			name:        "Bearer token no masking",
			value:       "Bearer sk_live_1234567890abcdef",
			maskSecrets: false,
			expected:    "Bearer sk_live_1234567890abcdef",
		},
		{
			name:        "Basic auth masking",
			value:       "Basic dXNlcjpwYXNzd29yZA==",
			maskSecrets: true,
			expected:    "Basic <YOUR_CREDENTIALS>",
		},
		{
			name:        "Long token masking",
			value:       "1234567890123456789012345678901234567890",
			maskSecrets: true,
			expected:    "<YOUR_API_KEY>",
		},
		{
			name:        "Short string no masking",
			value:       "short",
			maskSecrets: true,
			expected:    "short",
		},
		{
			name:        "String with spaces no masking",
			value:       "this is a string with spaces",
			maskSecrets: true,
			expected:    "this is a string with spaces",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := maskSensitiveValue(tt.value, tt.maskSecrets)
			if result != tt.expected {
				t.Errorf("maskSensitiveValue(%q, %v) = %q, want %q",
					tt.value, tt.maskSecrets, result, tt.expected)
			}
		})
	}
}

func TestMaskRequestHeaders(t *testing.T) {
	// Create headers with sensitive data
	headers := http.Header{}
	headers.Set("Authorization", "Bearer secret_token_123456789")
	headers.Set("X-API-Key", "api_key_1234567890")
	headers.Set("Content-Type", "application/json")
	headers.Set("X-Auth-Token", "auth_token_abcdefghij")

	// Test with masking disabled
	masked := maskRequestHeaders(headers, false)
	if masked.Get("Authorization") != "Bearer secret_token_123456789" {
		t.Error("maskRequestHeaders with maskSecrets=false should not mask")
	}

	// Test with masking enabled
	masked = maskRequestHeaders(headers, true)
	authValue := masked.Get("Authorization")
	if authValue != "Bearer <YOUR_API_KEY>" {
		t.Errorf("Authorization header not masked correctly: %s", authValue)
	}

	apiKeyValue := masked.Get("X-API-Key")
	if apiKeyValue != "<YOUR_API_KEY>" {
		t.Errorf("X-API-Key header not masked correctly: %s", apiKeyValue)
	}

	contentType := masked.Get("Content-Type")
	if contentType != "application/json" {
		t.Errorf("Content-Type header should not be masked: %s", contentType)
	}

	authTokenValue := masked.Get("X-Auth-Token")
	if authTokenValue != "<YOUR_API_KEY>" {
		t.Errorf("X-Auth-Token header not masked correctly: %s", authTokenValue)
	}
}

func TestGenerateCurl(t *testing.T) {
	req, _ := http.NewRequest("GET", "https://api.example.com/users", nil)
	req.Header.Set("Authorization", "Bearer secret_token")
	req.Header.Set("Content-Type", "application/json")

	gen, _ := NewGenerator(FormatCurl, Options{MaskSecrets: true})
	code, err := gen.Generate(req)

	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}

	verifyGeneratorOutput(t, code, []string{"curl -X GET", "<YOUR_API_KEY>"})
}

func TestGenerateNodeJS(t *testing.T) {
	req, _ := http.NewRequest("POST", "https://api.example.com/users", io.NopCloser(bytes.NewBufferString(`{"name":"test"}`)))
	req.Header.Set("Content-Type", "application/json")
	req.ContentLength = int64(len(`{"name":"test"}`))

	gen, _ := NewGenerator(FormatNodeJS, Options{MaskSecrets: false})
	code, err := gen.Generate(req)

	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}

	verifyGeneratorOutput(t, code, []string{"fetch(", "method: 'POST'"})
}

func TestGenerateGo(t *testing.T) { //nolint:dupl // Go-specific test setup
	req, _ := http.NewRequest("DELETE", "https://api.example.com/users/123", nil)
	req.Header.Set("Authorization", "Bearer secret")

	gen, _ := NewGenerator(FormatGo, Options{MaskSecrets: true})
	code, err := gen.Generate(req)

	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}

	verifyGeneratorOutput(t, code, []string{"package main", "http.NewRequest"})
}

func TestGeneratePython(t *testing.T) { //nolint:dupl // Python-specific test setup
	req, _ := http.NewRequest("GET", "https://api.example.com/items", nil)
	req.Header.Set("Authorization", "Bearer token")

	gen, _ := NewGenerator(FormatPython, Options{MaskSecrets: true})
	code, err := gen.Generate(req)

	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}

	verifyGeneratorOutput(t, code, []string{"import requests", "requests."})
}

func TestMaskURL(t *testing.T) {
	tests := []struct {
		name        string
		raw         string
		maskSecrets bool
		expected    string
	}{
		{
			name:        "masks api key query value and keeps other parameters",
			raw:         "https://api.example.com/v1/pets?api_key=SECRET&limit=10",
			maskSecrets: true,
			expected:    "https://api.example.com/v1/pets?api_key=<YOUR_API_KEY>&limit=10",
		},
		{
			name:        "masks token-like parameter names case-insensitively",
			raw:         "https://api.example.com/items?X-Api-Key=SECRET&access_token=abc123&offset=5",
			maskSecrets: true,
			expected:    "https://api.example.com/items?X-Api-Key=<YOUR_API_KEY>&access_token=<YOUR_API_KEY>&offset=5",
		},
		{
			name:        "does not mask unrelated parameters",
			raw:         "https://api.example.com/items?monkey=friendly&limit=10",
			maskSecrets: true,
			expected:    "https://api.example.com/items?monkey=friendly&limit=10",
		},
		{
			name:        "no query string is unchanged",
			raw:         "https://api.example.com/v1/pets",
			maskSecrets: true,
			expected:    "https://api.example.com/v1/pets",
		},
		{
			name:        "masking disabled leaves url untouched",
			raw:         "https://api.example.com/v1/pets?api_key=SECRET",
			maskSecrets: false,
			expected:    "https://api.example.com/v1/pets?api_key=SECRET",
		},
		{
			name:        "empty url",
			raw:         "",
			maskSecrets: true,
			expected:    "",
		},
		{
			name:        "malformed url does not leak query values",
			raw:         "http://[::1?api_key=SECRET",
			maskSecrets: true,
			expected:    "http://[::1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := maskURL(tt.raw, tt.maskSecrets)
			if result != tt.expected {
				t.Errorf("maskURL(%q, %v) = %q, want %q", tt.raw, tt.maskSecrets, result, tt.expected)
			}
		})
	}
}

func TestGeneratorsMaskURLSecrets(t *testing.T) {
	formats := []OutputFormat{FormatCurl, FormatGo, FormatNodeJS, FormatPython}
	const sensitiveURL = "https://api.example.com/v1/pets?api_key=SECRET&limit=10"

	for _, format := range formats {
		t.Run(string(format), func(t *testing.T) {
			req, err := http.NewRequest(http.MethodGet, sensitiveURL, nil)
			if err != nil {
				t.Fatalf("failed to create request: %v", err)
			}

			maskedGen, err := NewGenerator(format, Options{MaskSecrets: true})
			if err != nil {
				t.Fatalf("NewGenerator(%s) failed: %v", format, err)
			}
			maskedCode, err := maskedGen.Generate(req)
			if err != nil {
				t.Fatalf("Generate(%s) failed: %v", format, err)
			}
			if bytes.Contains([]byte(maskedCode), []byte("SECRET")) {
				t.Errorf("%s generator leaked a sensitive query value:\n%s", format, maskedCode)
			}
			if !bytes.Contains([]byte(maskedCode), []byte("<YOUR_API_KEY>")) {
				t.Errorf("%s generator did not mask the token query value:\n%s", format, maskedCode)
			}
			if !bytes.Contains([]byte(maskedCode), []byte("limit=10")) {
				t.Errorf("%s generator dropped non-sensitive query parameters:\n%s", format, maskedCode)
			}

			rawGen, err := NewGenerator(format, Options{MaskSecrets: false})
			if err != nil {
				t.Fatalf("NewGenerator(%s) failed: %v", format, err)
			}
			rawCode, err := rawGen.Generate(req)
			if err != nil {
				t.Fatalf("Generate(%s) failed: %v", format, err)
			}
			if !bytes.Contains([]byte(rawCode), []byte("api_key=SECRET")) {
				t.Errorf("%s generator must keep the URL untouched when masking is disabled:\n%s", format, rawCode)
			}
		})
	}
}

func verifyGeneratorOutput(t *testing.T, code string, requiredStrings []string) {
	if len(code) == 0 {
		t.Fatal("Generated code is empty")
	}
	for _, str := range requiredStrings {
		if !bytes.Contains([]byte(code), []byte(str)) {
			t.Errorf("Generated code should contain '%s'", str)
		}
	}
}

func TestRegisterCustomGenerator(t *testing.T) {
	// Define a mock generator format
	mockFormat := OutputFormat("mock")

	// Create a mock generator
	mockGen := &mockGenerator{}

	// Register the mock generator
	register(mockFormat, func(opts Options) Generator {
		return mockGen
	})

	// Verify it can be retrieved
	gen, err := NewGenerator(mockFormat, Options{})
	if err != nil {
		t.Fatalf("Failed to retrieve registered mock generator: %v", err)
	}

	if gen != mockGen {
		t.Error("Retrieved generator is not the registered mock")
	}

	// Verify ValidateFormat works
	if !ValidateFormat("mock") {
		t.Error("ValidateFormat should recognize registered format")
	}
}

// mockGenerator is a mock implementation of Generator for testing.
type mockGenerator struct{}

func (m *mockGenerator) Generate(req *http.Request) (string, error) {
	return "mock code", nil
}
