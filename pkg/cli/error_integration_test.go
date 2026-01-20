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

// TestErrorHandling_HTTPErrors tests HTTP error formatting.
func TestErrorHandling_HTTPErrors(t *testing.T) {
	formatter := NewErrorFormatter()

	tests := []struct {
		name            string
		statusCode      int
		body            []byte
		expectedPhrases []string
	}{
		{
			name:       "400 Bad Request with JSON error",
			statusCode: http.StatusBadRequest,
			body:       []byte(`{"error": "Invalid input", "field": "email"}`),
			expectedPhrases: []string{
				"HTTP 400",
				"invalid or malformed",
				"Invalid input",
			},
		},
		{
			name:       "401 Unauthorized",
			statusCode: http.StatusUnauthorized,
			body:       []byte(`{"message": "Invalid token"}`),
			expectedPhrases: []string{
				"HTTP 401",
				"Authentication",
				"credentials",
				"Invalid token",
			},
		},
		{
			name:       "404 Not Found",
			statusCode: http.StatusNotFound,
			body:       []byte(`{"error": "User not found"}`),
			expectedPhrases: []string{
				"HTTP 404",
				"not found",
				"User not found",
			},
		},
		{
			name:       "429 Too Many Requests",
			statusCode: http.StatusTooManyRequests,
			body:       []byte(`{"error": "Rate limit exceeded"}`),
			expectedPhrases: []string{
				"HTTP 429",
				"Too many requests",
				"Rate limit exceeded",
			},
		},
		{
			name:       "500 Internal Server Error",
			statusCode: http.StatusInternalServerError,
			body:       []byte(`{"error": "Database connection failed"}`),
			expectedPhrases: []string{
				"HTTP 500",
				"internal error",
				"Database connection failed",
			},
		},
		{
			name:       "503 Service Unavailable",
			statusCode: http.StatusServiceUnavailable,
			body:       []byte(`{"error": "Service is down for maintenance"}`),
			expectedPhrases: []string{
				"HTTP 503",
				"temporarily unavailable",
				"Service is down for maintenance",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp := &http.Response{
				StatusCode: tt.statusCode,
				Status:     http.StatusText(tt.statusCode),
			}

			result := formatter.FormatHTTPError(resp, tt.body)

			for _, phrase := range tt.expectedPhrases {
				if !strings.Contains(result, phrase) {
					t.Errorf("Expected error to contain '%s', got: %s", phrase, result)
				}
			}
		})
	}
}

// TestErrorHandling_NetworkErrors tests network error formatting.
func TestErrorHandling_NetworkErrors(t *testing.T) {
	formatter := NewErrorFormatter()

	tests := []struct {
		name            string
		err             error
		expectedPhrases []string
	}{
		{
			name: "Connection timeout",
			err: &url.Error{
				Op:  "Get",
				URL: "https://api.example.com/users",
				Err: &net.OpError{
					Op:  "dial",
					Net: "tcp",
					Err: errors.New("i/o timeout"),
				},
			},
			expectedPhrases: []string{
				"Network connectivity",
				"api.example.com",
				"Troubleshooting",
				"internet connection",
			},
		},
		{
			name: "Connection refused",
			err: &net.OpError{
				Op:  "dial",
				Net: "tcp",
				Err: errors.New("connection refused"),
			},
			expectedPhrases: []string{
				"Network connectivity",
				"connection refused",
				"Troubleshooting",
			},
		},
		{
			name: "DNS resolution failure",
			err: &url.Error{
				Op:  "Get",
				URL: "https://nonexistent.example.com",
				Err: errors.New("no such host"),
			},
			expectedPhrases: []string{
				"Network connectivity",
				"nonexistent.example.com",
				"no such host",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := formatter.FormatError(tt.err)

			for _, phrase := range tt.expectedPhrases {
				if !strings.Contains(result, phrase) {
					t.Errorf("Expected error to contain '%s', got: %s", phrase, result)
				}
			}
		})
	}
}

// TestErrorHandling_AppNotFound tests app not found error with suggestions.
func TestErrorHandling_AppNotFound(t *testing.T) {
	formatter := NewErrorFormatter()

	tests := []struct {
		name             string
		appName          string
		installedApps    []string
		expectSuggestion bool
		expectedApps     []string
	}{
		{
			name:             "Typo in app name",
			appName:          "myap",
			installedApps:    []string{"myapi", "petstore", "github"},
			expectSuggestion: true,
			expectedApps:     []string{"myapi"},
		},
		{
			name:             "Prefix match",
			appName:          "pet",
			installedApps:    []string{"myapi", "petstore", "github"},
			expectSuggestion: true,
			expectedApps:     []string{"petstore"},
		},
		{
			name:             "No similar apps",
			appName:          "completely-different",
			installedApps:    []string{"myapi", "petstore", "github"},
			expectSuggestion: false,
			expectedApps:     nil,
		},
		{
			name:             "No installed apps",
			appName:          "myapp",
			installedApps:    []string{},
			expectSuggestion: false,
			expectedApps:     nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := &config.AppNotFoundError{AppName: tt.appName}
			result := formatter.FormatErrorWithContext(err, tt.installedApps)

			// Check for app name
			if !strings.Contains(result, tt.appName) {
				t.Errorf("Expected error to contain app name '%s', got: %s", tt.appName, result)
			}

			// Check for installation instructions
			if !strings.Contains(result, "ob install") {
				t.Errorf("Expected error to suggest installation, got: %s", result)
			}

			// Check for suggestions
			if tt.expectSuggestion {
				if !strings.Contains(result, "Did you mean") {
					t.Errorf("Expected error to suggest similar apps, got: %s", result)
				}
				for _, app := range tt.expectedApps {
					if !strings.Contains(result, app) {
						t.Errorf("Expected error to suggest '%s', got: %s", app, result)
					}
				}
			} else {
				if strings.Contains(result, "Did you mean") {
					t.Errorf("Did not expect suggestions, got: %s", result)
				}
			}
		})
	}
}

// TestErrorHandling_UsageHelp tests usage help formatting.
func TestErrorHandling_UsageHelp(t *testing.T) {
	formatter := NewErrorFormatter()

	operation := &openapi3.Operation{
		Summary:     "Create a new user",
		Description: "Creates a new user in the system with the provided details",
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
				Description: "User's age (optional)",
				Schema: &openapi3.SchemaRef{
					Value: &openapi3.Schema{
						Type: &openapi3.Types{"integer"},
					},
				},
			},
		},
		&openapi3.ParameterRef{
			Value: &openapi3.Parameter{
				Name:        "role",
				In:          "query",
				Required:    false,
				Description: "User's role",
				Schema: &openapi3.SchemaRef{
					Value: &openapi3.Schema{
						Type: &openapi3.Types{"string"},
						Enum: []any{"admin", "user", "guest"},
					},
				},
			},
		},
	}

	result := formatter.FormatUsageHelp("myapp", "create", "user", operation, params)

	expectedContent := []string{
		"Usage: myapp create user [flags]",
		"Description: Create a new user",
		"Required Parameters:",
		"--name",
		"User's full name",
		"--email",
		"User's email address",
		"Optional Parameters:",
		"--age",
		"User's age (optional)",
		"--role",
		"Allowed values: admin, user, guest",
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

// TestErrorHandling_MissingParameters tests missing parameter error formatting.
func TestErrorHandling_MissingParameters(t *testing.T) {
	formatter := NewErrorFormatter()

	tests := []struct {
		name            string
		err             error
		expectedPhrases []string
	}{
		{
			name: "Single missing parameter",
			err:  errors.New("required parameter 'user_id' is missing"),
			expectedPhrases: []string{
				"required parameter",
				"user_id",
				"missing",
				"Usage:",
				"--help",
			},
		},
		{
			name: "Multiple missing parameters",
			err:  errors.New("required parameter 'name' is missing, required parameter 'email' is missing"),
			expectedPhrases: []string{
				"required parameter",
				"missing",
				"Usage:",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := formatter.FormatError(tt.err)

			for _, phrase := range tt.expectedPhrases {
				if !strings.Contains(result, phrase) {
					t.Errorf("Expected error to contain '%s', got: %s", phrase, result)
				}
			}
		})
	}
}

// TestErrorHandling_UnknownResourceAndVerb tests unknown resource/verb errors.
func TestErrorHandling_UnknownResourceAndVerb(t *testing.T) {
	formatter := NewErrorFormatter()

	tests := []struct {
		name            string
		err             error
		expectedPhrases []string
	}{
		{
			name: "Unknown resource",
			err:  errors.New("unknown resource: users"),
			expectedPhrases: []string{
				"Unknown resource",
				"users",
				"--help",
			},
		},
		{
			name: "Unknown verb",
			err:  errors.New("unknown verb 'create' for resource 'users'"),
			expectedPhrases: []string{
				"unknown verb",
				"create",
				"Common verbs",
				"list",
				"get",
				"delete",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := formatter.FormatError(tt.err)

			for _, phrase := range tt.expectedPhrases {
				if !strings.Contains(result, phrase) {
					t.Errorf("Expected error to contain '%s', got: %s", phrase, result)
				}
			}
		})
	}
}

// TestErrorHandling_SpecLoadErrors tests spec loading error formatting.
func TestErrorHandling_SpecLoadErrors(t *testing.T) {
	formatter := NewErrorFormatter()

	err := errors.New("failed to load spec: file not found")
	result := formatter.FormatError(err)

	expectedPhrases := []string{
		"failed to load spec",
		"Possible causes:",
		"specification file",
		"ob install",
	}

	for _, phrase := range expectedPhrases {
		if !strings.Contains(result, phrase) {
			t.Errorf("Expected error to contain '%s', got: %s", phrase, result)
		}
	}
}
