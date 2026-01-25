package cli

import (
	"errors"
	"net"
	"net/http"
	"net/url"
	"slices"
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

	tests := []httpErrorCase{
		{
			name:       "400 Bad Request",
			statusCode: http.StatusBadRequest,
			body:       []byte(`{"error": "Invalid input"}`),
			expected:   []string{"HTTP 400", "invalid or malformed", "Invalid input"},
		},
		{
			name:       "401 Unauthorized",
			statusCode: http.StatusUnauthorized,
			body:       []byte(`{"error": "Authentication required"}`),
			expected:   []string{"HTTP 401", "Authentication", "credentials"},
		},
		{
			name:       "404 Not Found",
			statusCode: http.StatusNotFound,
			body:       []byte(`{"error": "Resource not found"}`),
			expected:   []string{"HTTP 404", "not found"},
		},
		{
			name:       "500 Internal Server Error",
			statusCode: http.StatusInternalServerError,
			body:       []byte(`{"error": "Server error"}`),
			expected:   []string{"HTTP 500", "internal error"},
		},
	}

	runHTTPErrorCases(t, formatter, tests)
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
				found := slices.Contains(result, expected)
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

func TestErrorFormatter_FormatErrorMessages(t *testing.T) {
	formatter := NewErrorFormatter()

	tests := []struct {
		name     string
		err      error
		expected []string
	}{
		{
			name:     "missing parameter",
			err:      errors.New("required parameter 'user_id' is missing"),
			expected: []string{"required parameter", "Usage:", "--help"},
		},
		{
			name:     "unknown resource",
			err:      errors.New("unknown resource: users"),
			expected: []string{"Unknown resource", "users", "--help"},
		},
		{
			name:     "unknown verb",
			err:      errors.New("unknown verb 'create' for resource 'users'"),
			expected: []string{"unknown verb", "Common verbs", "create"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := formatter.FormatError(tt.err)

			for _, expected := range tt.expected {
				if !strings.Contains(result, expected) {
					t.Errorf("Expected error to contain '%s', got: %s", expected, result)
				}
			}
		})
	}
}

func TestExtractBodySchema(t *testing.T) {
	tests := []struct {
		name            string
		requestBody     *openapi3.RequestBody
		expectedProps   map[string]*openapi3.SchemaRef
		expectedReq     []string
		expectedBodyReq bool
	}{
		{
			name:            "nil request body",
			requestBody:     nil,
			expectedProps:   nil,
			expectedReq:     nil,
			expectedBodyReq: false,
		},
		{
			name: "body optional with required properties",
			requestBody: &openapi3.RequestBody{
				Required: false,
				Content: openapi3.Content{
					"application/json": &openapi3.MediaType{
						Schema: &openapi3.SchemaRef{
							Value: &openapi3.Schema{
								Properties: map[string]*openapi3.SchemaRef{
									"name": {
										Value: &openapi3.Schema{
											Type: &openapi3.Types{"string"},
										},
									},
									"email": {
										Value: &openapi3.Schema{
											Type: &openapi3.Types{"string"},
										},
									},
								},
								Required: []string{"name"},
							},
						},
					},
				},
			},
			expectedProps: map[string]*openapi3.SchemaRef{
				"name": {
					Value: &openapi3.Schema{
						Type: &openapi3.Types{"string"},
					},
				},
				"email": {
					Value: &openapi3.Schema{
						Type: &openapi3.Types{"string"},
					},
				},
			},
			expectedReq:     []string{"name"},
			expectedBodyReq: false,
		},
		{
			name: "body optional without required properties",
			requestBody: &openapi3.RequestBody{
				Required: false,
				Content: openapi3.Content{
					"application/json": &openapi3.MediaType{
						Schema: &openapi3.SchemaRef{
							Value: &openapi3.Schema{
								Properties: map[string]*openapi3.SchemaRef{
									"name": {
										Value: &openapi3.Schema{
											Type: &openapi3.Types{"string"},
										},
									},
									"email": {
										Value: &openapi3.Schema{
											Type: &openapi3.Types{"string"},
										},
									},
								},
								Required: []string{},
							},
						},
					},
				},
			},
			expectedProps: map[string]*openapi3.SchemaRef{
				"name": {
					Value: &openapi3.Schema{
						Type: &openapi3.Types{"string"},
					},
				},
				"email": {
					Value: &openapi3.Schema{
						Type: &openapi3.Types{"string"},
					},
				},
			},
			expectedReq:     []string{},
			expectedBodyReq: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			props, reqProps, bodyReq := extractBodySchema(tt.requestBody)

			// Compare properties
			if len(props) != len(tt.expectedProps) {
				t.Errorf("Expected %d properties, got %d", len(tt.expectedProps), len(props))
			} else {
				for key, expectedSchema := range tt.expectedProps {
					actualSchema, ok := props[key]
					if !ok {
						t.Errorf("Expected property %q not found", key)
						continue
					}
					// Compare Types by checking their slice representation
					expectedTypeSlice := expectedSchema.Value.Type.Slice()
					actualTypeSlice := actualSchema.Value.Type.Slice()
					if len(expectedTypeSlice) != len(actualTypeSlice) {
						t.Errorf("Expected property %q type length %d, got %d", key, len(expectedTypeSlice), len(actualTypeSlice))
						continue
					}
					for i, expectedType := range expectedTypeSlice {
						if i >= len(actualTypeSlice) || actualTypeSlice[i] != expectedType {
							t.Errorf("Expected property %q type %v, got %v", key, expectedTypeSlice, actualTypeSlice)
							break
						}
					}
				}
			}

			// Compare required properties
			if len(reqProps) != len(tt.expectedReq) {
				t.Errorf("Expected %d required properties, got %d", len(tt.expectedReq), len(reqProps))
			} else {
				for _, expectedReq := range tt.expectedReq {
					found := slices.Contains(reqProps, expectedReq)
					if !found {
						t.Errorf("Expected required property %q not found in %v", expectedReq, reqProps)
					}
				}
			}

			// Compare body required flag
			if bodyReq != tt.expectedBodyReq {
				t.Errorf("Expected bodyRequired %v, got %v", tt.expectedBodyReq, bodyReq)
			}
		})
	}
}
