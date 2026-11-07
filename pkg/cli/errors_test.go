package cli

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/nomagicln/open-bridge/pkg/config"
	"github.com/nomagicln/open-bridge/pkg/request"
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

func TestRedactURL(t *testing.T) {
	tests := []struct {
		name     string
		rawURL   string
		expected string
	}{
		{
			name:     "redacts query values and userinfo",
			rawURL:   "https://user:pass@api.example.com/v1/pets?api_key=SECRET&limit=10",
			expected: "https://api.example.com/v1/pets?api_key=REDACTED&limit=REDACTED",
		},
		{
			name:     "redacts query values without userinfo",
			rawURL:   "https://api.example.com/v1/pets?token=SECRET",
			expected: "https://api.example.com/v1/pets?token=REDACTED",
		},
		{
			name:     "removes userinfo without query",
			rawURL:   "https://user:pass@api.example.com/path",
			expected: "https://api.example.com/path",
		},
		{
			name:     "url without secrets is unchanged",
			rawURL:   "https://api.example.com/v1/pets",
			expected: "https://api.example.com/v1/pets",
		},
		{
			name:     "empty url",
			rawURL:   "",
			expected: "",
		},
		{
			name:     "malformed url does not leak query values",
			rawURL:   "http://[::1?api_key=SECRET",
			expected: "http://[::1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := redactURL(tt.rawURL)
			if result != tt.expected {
				t.Errorf("redactURL(%q) = %q, want %q", tt.rawURL, result, tt.expected)
			}
		})
	}
}

func TestErrorFormatter_FormatNetworkError_RedactsSensitiveURL(t *testing.T) {
	formatter := NewErrorFormatter()
	err := &url.Error{
		Op:  "Get",
		URL: "https://user:pass@api.example.com/v1/pets?api_key=SECRET&limit=10",
		Err: errors.New("dial tcp: connection refused"),
	}

	result := formatter.FormatError(err)

	for _, leaked := range []string{"SECRET", "user:pass", "pass@"} {
		if strings.Contains(result, leaked) {
			t.Errorf("formatted error leaked %q from the target URL:\n%s", leaked, result)
		}
	}
	for _, expected := range []string{"Failed to connect to", "api.example.com", "api_key=REDACTED", "Troubleshooting"} {
		if !strings.Contains(result, expected) {
			t.Errorf("formatted error should contain %q, got:\n%s", expected, result)
		}
	}

	// Both "Failed to connect to" and "Try accessing" lines must be redacted.
	if count := strings.Count(result, "api_key=REDACTED"); count != 2 {
		t.Errorf("expected both URL print sites to be redacted, found %d redacted URLs:\n%s", count, result)
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
			err:      &request.MissingParameterError{Name: "user_id"},
			expected: []string{"required parameter", "Usage:", "--help"},
		},
		{
			name:     "unknown resource",
			err:      &UnknownResourceError{Resource: "users"},
			expected: []string{"Unknown resource", "users", "--help"},
		},
		{
			name:     "unknown verb",
			err:      &UnknownVerbError{Resource: "users", Verb: "create"},
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

// Batch-3 user-visible formatter outputs, captured before the typed error
// migration. They must stay byte-for-byte identical.
const (
	batch3MissingParameterText = "Error: required parameter 'user_id' is missing\n\nUsage:\n  <app> <resource> <verb> --<param-name> <value>\n\nTo see all required parameters for this operation, use:\n  <app> <resource> <verb> --help"

	batch3UnknownResourceText = "Error: Unknown resource 'users'.\n\nTo see available resources, use:\n  <app> --help\n\nOr list all operations:\n  <app> list operations"

	batch3UnknownVerbText = "Error: unknown verb 'create' for resource 'users'\n\nCommon verbs include:\n  - create: Create a new resource\n  - list: List all resources\n  - get: Get a specific resource\n  - update: Update a resource\n  - delete: Delete a resource\n\n\nTo see available verbs for a resource, use:\n  <app> <resource> --help"

	batch3SpecLoadText = "Error: failed to load spec: file not found\n\nPossible causes:\n  - The OpenAPI specification file is not accessible\n  - The specification URL is incorrect or unreachable\n  - The specification file is malformed or invalid\n\nTo update the spec source, use:\n  ob install <app> --spec <new-path-or-url>"

	batch3TypeMismatchText = "Error: parameter 'limit' type mismatch: got string, expected one of [integer]"

	batch3BodyMissingText = "Error: required body parameter 'name' is missing"
)

// TestTypedErrorMessages pins the Error() text of every typed error: the
// migration must not change user-visible messages.
func TestTypedErrorMessages(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want string
	}{
		{
			name: "unknown resource",
			err:  &UnknownResourceError{Resource: "users"},
			want: "unknown resource: users",
		},
		{
			name: "unknown verb",
			err:  &UnknownVerbError{Resource: "users", Verb: "create"},
			want: "unknown verb 'create' for resource 'users'",
		},
		{
			name: "spec load",
			err:  &SpecLoadError{Source: "/tmp/spec.yaml", Err: errors.New("file not found")},
			want: "failed to load spec: file not found",
		},
		{
			name: "spec load without cause",
			err:  &SpecLoadError{Source: "/tmp/spec.yaml"},
			want: "failed to load spec",
		},
		{
			name: "missing parameter",
			err:  &request.MissingParameterError{Name: "user_id"},
			want: "required parameter 'user_id' is missing",
		},
		{
			name: "missing body field",
			err:  &request.MissingParameterError{Name: "name", BodyField: true},
			want: "required body parameter 'name' is missing",
		},
		{
			name: "parameter type mismatch",
			err:  &request.ParameterValidationError{Parameter: "limit", Reason: "type mismatch: got string, expected one of [integer]"},
			want: "parameter 'limit' type mismatch: got string, expected one of [integer]",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.err.Error(); got != tt.want {
				t.Errorf("Error() = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestTypedErrorUnwrap verifies that the typed errors expose their cause so
// errors.Is/errors.As keep working through wrappers.
func TestTypedErrorUnwrap(t *testing.T) {
	inner := errors.New("disk on fire")
	specErr := &SpecLoadError{Source: "/tmp/spec.yaml", Err: inner}
	if !errors.Is(specErr, inner) {
		t.Error("SpecLoadError must unwrap to its cause")
	}

	wrapped := fmt.Errorf("outer context: %w", &UnknownResourceError{Resource: "users"})
	var unknown *UnknownResourceError
	if !errors.As(wrapped, &unknown) {
		t.Error("wrapped UnknownResourceError must be discoverable with errors.As")
	}
}

// TestFormatErrorWithContextTypedClassification verifies that the formatter
// classifies typed errors (including %w-wrapped ones) via errors.As and keeps
// the batch-3 rendering byte-for-byte.
func TestFormatErrorWithContextTypedClassification(t *testing.T) {
	formatter := NewErrorFormatter()

	tests := []struct {
		name string
		err  error
		want string
	}{
		{
			name: "missing parameter",
			err:  &request.MissingParameterError{Name: "user_id"},
			want: batch3MissingParameterText,
		},
		{
			name: "wrapped missing parameter",
			err:  fmt.Errorf("validation failed: %w", &request.MissingParameterError{Name: "user_id"}),
			want: batch3MissingParameterText,
		},
		{
			name: "unknown resource",
			err:  &UnknownResourceError{Resource: "users"},
			want: batch3UnknownResourceText,
		},
		{
			name: "wrapped unknown resource",
			err:  fmt.Errorf("command failed: %w", &UnknownResourceError{Resource: "users"}),
			want: batch3UnknownResourceText,
		},
		{
			name: "unknown verb",
			err:  &UnknownVerbError{Resource: "users", Verb: "create"},
			want: batch3UnknownVerbText,
		},
		{
			name: "spec load",
			err:  &SpecLoadError{Source: "/tmp/spec.yaml", Err: errors.New("file not found")},
			want: batch3SpecLoadText,
		},
		{
			name: "wrapped spec load",
			err:  fmt.Errorf("install failed: %w", &SpecLoadError{Source: "/tmp/spec.yaml", Err: errors.New("file not found")}),
			want: batch3SpecLoadText,
		},
		{
			name: "parameter type mismatch keeps generic rendering",
			err:  &request.ParameterValidationError{Parameter: "limit", Reason: "type mismatch: got string, expected one of [integer]"},
			want: batch3TypeMismatchText,
		},
		{
			name: "missing body field keeps generic rendering",
			err:  &request.MissingParameterError{Name: "name", BodyField: true},
			want: batch3BodyMissingText,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := formatter.FormatError(tt.err); got != tt.want {
				t.Errorf("FormatError() =\n%q\nwant:\n%q", got, tt.want)
			}
		})
	}
}

// TestFormatErrorWithContextStillHandlesLegacyPlainErrors makes sure unrelated
// (non-classified) errors fall through to the generic rendering.
func TestFormatErrorWithContextStillHandlesLegacyPlainErrors(t *testing.T) {
	formatter := NewErrorFormatter()
	got := formatter.FormatError(errors.New("totally unrelated failure"))
	if want := "Error: totally unrelated failure"; got != want {
		t.Errorf("FormatError() = %q, want %q", got, want)
	}
}

// TestPrintedErrorSemantics pins PrintedError: it must expose the underlying
// message (so logs/tests see the real cause) while IsPrintedError still tells
// main() not to print it a second time.
func TestPrintedErrorSemantics(t *testing.T) {
	inner := errors.New("failed to load spec: boom")
	printed := &PrintedError{Err: inner}

	if got, want := printed.Error(), inner.Error(); got != want {
		t.Errorf("PrintedError.Error() = %q, want %q", got, want)
	}
	if !errors.Is(printed, inner) {
		t.Error("PrintedError must unwrap to the underlying error")
	}
	if !IsPrintedError(printed) {
		t.Error("IsPrintedError() = false, want true")
	}
	if !IsPrintedError(fmt.Errorf("wrapped: %w", printed)) {
		t.Error("IsPrintedError() must find a wrapped PrintedError")
	}

	// Emulate cmd/ob/main.go: printed errors are not printed again.
	shouldPrint := !IsPrintedError(printed) && printed.Error() != ""
	if shouldPrint {
		t.Error("PrintedError would be double printed by main()")
	}

	// A nil cause must not panic and must stay silent for main().
	if got := (&PrintedError{}).Error(); got != "" {
		t.Errorf("(&PrintedError{}).Error() = %q, want empty string", got)
	}
}
