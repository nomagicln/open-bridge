package cli

import (
	"errors"
	"net"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/nomagicln/open-bridge/pkg/config"
)

func TestErrorFormatter_FormatAppNotFoundError(t *testing.T) {
	formatter := NewErrorFormatter()
	err := &config.AppNotFoundError{AppName: "myapp"}

	// Test without installed apps
	result := formatter.FormatError(err)

	if !strings.Contains(result, "myapp") {
		t.Errorf("Expected error to contain app name 'myapp', got: %s", result)
	}
	if !strings.Contains(result, "ob install") {
		t.Errorf("Expected error to suggest installation, got: %s", result)
	}
	if !strings.Contains(result, "ob list") {
		t.Errorf("Expected error to suggest listing apps, got: %s", result)
	}

	// Test with installed apps (similar names)
	installedApps := []string{"myapi", "yourapp", "otherapp"}
	resultWithSuggestions := formatter.FormatErrorWithContext(err, installedApps)

	if !strings.Contains(resultWithSuggestions, "myapp") {
		t.Errorf("Expected error to contain app name 'myapp', got: %s", resultWithSuggestions)
	}
	if !strings.Contains(resultWithSuggestions, "Did you mean") {
		t.Errorf("Expected error to suggest similar apps, got: %s", resultWithSuggestions)
	}
	if !strings.Contains(resultWithSuggestions, "myapi") {
		t.Errorf("Expected error to suggest 'myapi', got: %s", resultWithSuggestions)
	}
}

func TestErrorFormatter_FormatNetworkError(t *testing.T) {
	formatter := NewErrorFormatter()

	tests := []struct {
		name     string
		err      error
		contains []string
	}{
		{
			name: "URL error with timeout",
			err: &url.Error{
				Op:  "Get",
				URL: "https://api.example.com",
				Err: errors.New("timeout"),
			},
			contains: []string{"Network connectivity", "api.example.com", "Troubleshooting"},
		},
		{
			name: "Net operation error",
			err: &net.OpError{
				Op:  "dial",
				Net: "tcp",
				Err: errors.New("connection refused"),
			},
			contains: []string{"Network connectivity", "connection refused"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := formatter.FormatError(tt.err)

			for _, expected := range tt.contains {
				if !strings.Contains(result, expected) {
					t.Errorf("Expected error to contain '%s', got: %s", expected, result)
				}
			}
		})
	}
}

func TestErrorFormatter_FormatHTTPError(t *testing.T) {
	formatter := NewErrorFormatter()

	tests := []struct {
		name       string
		statusCode int
		body       []byte
		contains   []string
	}{
		{
			name:       "400 Bad Request",
			statusCode: http.StatusBadRequest,
			body:       []byte(`{"error": "Invalid input"}`),
			contains:   []string{"HTTP 400", "invalid or malformed", "Invalid input"},
		},
		{
			name:       "401 Unauthorized",
			statusCode: http.StatusUnauthorized,
			body:       []byte(`{"error": "Authentication required"}`),
			contains:   []string{"HTTP 401", "Authentication", "credentials"},
		},
		{
			name:       "404 Not Found",
			statusCode: http.StatusNotFound,
			body:       []byte(`{"error": "Resource not found"}`),
			contains:   []string{"HTTP 404", "not found"},
		},
		{
			name:       "500 Internal Server Error",
			statusCode: http.StatusInternalServerError,
			body:       []byte(`{"error": "Server error"}`),
			contains:   []string{"HTTP 500", "internal error"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp := &http.Response{
				StatusCode: tt.statusCode,
				Status:     http.StatusText(tt.statusCode),
			}

			result := formatter.FormatHTTPError(resp, tt.body)

			for _, expected := range tt.contains {
				if !strings.Contains(result, expected) {
					t.Errorf("Expected error to contain '%s', got: %s", expected, result)
				}
			}
		})
	}
}

func TestErrorFormatter_FormatUsageHelp(t *testing.T) {
	formatter := NewErrorFormatter()

	// Create a simple operation with parameters
	operation := &openapi3.Operation{
		Summary:     "Create a new user",
		Description: "Creates a new user in the system",
	}

	params := openapi3.Parameters{
		&openapi3.ParameterRef{
			Value: &openapi3.Parameter{
				Name:        "name",
				In:          "query",
				Required:    true,
				Description: "User's full name",
				Schema: &openapi3.SchemaRef{
					Value: &openapi3.Schema{
						Type: &openapi3.Types{"string"},
					},
				},
			},
		},
		&openapi3.ParameterRef{
			Value: &openapi3.Parameter{
				Name:        "email",
				In:          "query",
				Required:    true,
				Description: "User's email address",
				Schema: &openapi3.SchemaRef{
					Value: &openapi3.Schema{
						Type: &openapi3.Types{"string"},
					},
				},
			},
		},
		&openapi3.ParameterRef{
			Value: &openapi3.Parameter{
				Name:        "age",
				In:          "query",
				Required:    false,
				Description: "User's age",
				Schema: &openapi3.SchemaRef{
					Value: &openapi3.Schema{
						Type: &openapi3.Types{"integer"},
					},
				},
			},
		},
	}

	result := formatter.FormatUsageHelp("myapp", "create", "user", operation, params)

	// Check for expected content
	expectedContent := []string{
		"Usage: myapp create user [flags]",
		"Description: Create a new user",
		"Required Parameters:",
		"--name",
		"--email",
		"Optional Parameters:",
		"--age",
		"Output Flags:",
		"--json",
		"--yaml",
		"Example:",
	}

	for _, expected := range expectedContent {
		if !strings.Contains(result, expected) {
			t.Errorf("Expected help to contain '%s', got: %s", expected, result)
		}
	}
}

func TestErrorFormatter_SuggestSimilarApps(t *testing.T) {
	formatter := NewErrorFormatter()

	installedApps := []string{"myapi", "petstore", "github", "stripe"}

	tests := []struct {
		name     string
		input    string
		expected []string
	}{
		{
			name:     "Exact match case insensitive",
			input:    "MyAPI",
			expected: []string{"myapi"},
		},
		{
			name:     "Prefix match",
			input:    "pet",
			expected: []string{"petstore"},
		},
		{
			name:     "Contains match",
			input:    "store",
			expected: []string{"petstore"},
		},
		{
			name:     "Typo with small distance",
			input:    "myap",
			expected: []string{"myapi"},
		},
		{
			name:     "No match",
			input:    "completely-different",
			expected: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := formatter.SuggestSimilarApps(tt.input, installedApps)

			if len(result) == 0 && len(tt.expected) == 0 {
				return // Both empty, test passes
			}

			if len(result) != len(tt.expected) {
				t.Errorf("Expected %d suggestions, got %d: %v", len(tt.expected), len(result), result)
				return
			}

			// Check if expected apps are in result
			for _, expected := range tt.expected {
				found := false
				for _, r := range result {
					if r == expected {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("Expected suggestion '%s' not found in result: %v", expected, result)
				}
			}
		})
	}
}

func TestErrorFormatter_LevenshteinDistance(t *testing.T) {
	formatter := NewErrorFormatter()

	tests := []struct {
		s1       string
		s2       string
		expected int
	}{
		{"", "", 0},
		{"a", "", 1},
		{"", "a", 1},
		{"abc", "abc", 0},
		{"abc", "abd", 1},
		{"abc", "adc", 1},
		{"abc", "def", 3},
		{"kitten", "sitting", 3},
		{"saturday", "sunday", 3},
	}

	for _, tt := range tests {
		t.Run(tt.s1+"_"+tt.s2, func(t *testing.T) {
			result := formatter.levenshteinDistance(tt.s1, tt.s2)
			if result != tt.expected {
				t.Errorf("levenshteinDistance(%q, %q) = %d, expected %d", tt.s1, tt.s2, result, tt.expected)
			}
		})
	}
}

func TestErrorFormatter_FormatMissingParameterError(t *testing.T) {
	formatter := NewErrorFormatter()
	err := errors.New("required parameter 'user_id' is missing")

	result := formatter.FormatError(err)

	if !strings.Contains(result, "required parameter") {
		t.Errorf("Expected error to mention required parameter, got: %s", result)
	}
	if !strings.Contains(result, "Usage:") {
		t.Errorf("Expected error to show usage, got: %s", result)
	}
	if !strings.Contains(result, "--help") {
		t.Errorf("Expected error to suggest help, got: %s", result)
	}
}

func TestErrorFormatter_FormatUnknownResourceError(t *testing.T) {
	formatter := NewErrorFormatter()
	err := errors.New("unknown resource: users")

	result := formatter.FormatError(err)

	if !strings.Contains(result, "Unknown resource") {
		t.Errorf("Expected error to mention unknown resource, got: %s", result)
	}
	if !strings.Contains(result, "users") {
		t.Errorf("Expected error to contain resource name, got: %s", result)
	}
	if !strings.Contains(result, "--help") {
		t.Errorf("Expected error to suggest help, got: %s", result)
	}
}

func TestErrorFormatter_FormatUnknownVerbError(t *testing.T) {
	formatter := NewErrorFormatter()
	err := errors.New("unknown verb 'create' for resource 'users'")

	result := formatter.FormatError(err)

	if !strings.Contains(result, "unknown verb") {
		t.Errorf("Expected error to mention unknown verb, got: %s", result)
	}
	if !strings.Contains(result, "Common verbs") {
		t.Errorf("Expected error to list common verbs, got: %s", result)
	}
	if !strings.Contains(result, "create") {
		t.Errorf("Expected error to mention create verb, got: %s", result)
	}
}
