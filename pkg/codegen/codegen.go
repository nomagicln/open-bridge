// Package codegen provides code generation capabilities for API requests.
// It supports generating code in multiple languages (curl, nodejs, go, python)
// from http.Request objects, with optional sensitive information masking.
package codegen

import (
	"fmt"
	"net/http"
	"regexp"
	"strings"
)

// OutputFormat represents the target language/tool for code generation.
type OutputFormat string

const (
	FormatCurl   OutputFormat = "curl"
	FormatNodeJS OutputFormat = "nodejs"
	FormatGo     OutputFormat = "go"
	FormatPython OutputFormat = "python"
)

// Options contains configuration for code generation.
type Options struct {
	// MaskSecrets controls whether sensitive information (API keys, tokens, etc.)
	// should be replaced with placeholders like <YOUR_API_KEY>.
	MaskSecrets bool
}

// Generator defines the interface for code generation from HTTP requests.
type Generator interface {
	// Generate produces code from an HTTP request.
	Generate(req *http.Request) (string, error)
}

// GeneratorFactory is a function type that creates a new Generator instance.
type GeneratorFactory func(opts Options) Generator

// registry maps output formats to their corresponding generator factories.
var registry = make(map[OutputFormat]GeneratorFactory)

// init registers all built-in code generators.
func init() {
	register(FormatCurl, func(opts Options) Generator {
		return NewCurlGenerator(opts)
	})
	register(FormatNodeJS, func(opts Options) Generator {
		return NewNodeJSGenerator(opts)
	})
	register(FormatGo, func(opts Options) Generator {
		return NewGoGenerator(opts)
	})
	register(FormatPython, func(opts Options) Generator {
		return NewPythonGenerator(opts)
	})
}

// register registers a new code generator factory for the specified format.
// This function is used during initialization and can be called by extensions
// to register custom generators.
func register(format OutputFormat, factory GeneratorFactory) {
	if factory == nil {
		panic(fmt.Sprintf("generator factory for format %s cannot be nil", format))
	}
	registry[format] = factory
}

// NewGenerator creates a new code generator for the specified format.
// It looks up the factory in the registry and instantiates a new generator.
func NewGenerator(format OutputFormat, opts Options) (Generator, error) {
	factory, ok := registry[format]
	if !ok {
		return nil, fmt.Errorf("unsupported output format: %s", format)
	}
	return factory(opts), nil
}

// ValidateFormat checks if the given format is valid.
func ValidateFormat(format string) bool {
	_, ok := registry[OutputFormat(format)]
	return ok
}

// ListFormats returns all registered output formats.
func ListFormats() []string {
	formats := make([]string, 0, len(registry))
	for format := range registry {
		formats = append(formats, string(format))
	}
	return formats
}

// maskSensitiveValue masks a potentially sensitive value if masking is enabled.
// Common patterns: "Bearer <token>", API keys, OAuth tokens, etc.
func maskSensitiveValue(value string, maskSecrets bool) string {
	if !maskSecrets {
		return value
	}

	if isBearerToken(value) {
		return "Bearer <YOUR_API_KEY>"
	}

	if isBasicAuth(value) {
		return "Basic <YOUR_CREDENTIALS>"
	}

	if looksLikeToken(value) {
		return "<YOUR_API_KEY>"
	}

	return value
}

// isBearerToken checks if the value is a Bearer token.
func isBearerToken(value string) bool {
	return strings.HasPrefix(value, "Bearer ")
}

// isBasicAuth checks if the value is Basic authentication.
func isBasicAuth(value string) bool {
	return strings.HasPrefix(value, "Basic ")
}

// looksLikeToken checks if a value looks like a token/API key.
func looksLikeToken(value string) bool {
	if len(value) <= 14 || strings.Contains(value, " ") {
		return false
	}

	hasSpecialChars := strings.ContainsAny(value, "-_")

	allAlphaNum := true
	for _, ch := range value {
		isAlphaNum := (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || (ch >= '0' && ch <= '9')
		if !isAlphaNum && ch != '-' && ch != '_' {
			allAlphaNum = false
			break
		}
	}

	return hasSpecialChars || allAlphaNum
}

// maskRequestHeaders returns a copy of the headers with sensitive values masked if needed.
func maskRequestHeaders(headers http.Header, maskSecrets bool) http.Header {
	if !maskSecrets {
		return headers.Clone()
	}

	masked := headers.Clone()
	sensitivePrefixes := []string{
		"Authorization",
		"X-API-Key",
		"X-API-Secret",
		"X-Access-Token",
		"X-Auth-Token",
		"API-Key",
		"Access-Token",
		"Secret",
		"Token",
		"Apikey",
		"X-Secret",
	}

	for _, prefix := range sensitivePrefixes {
		// Case-insensitive header matching
		for headerKey := range masked {
			if strings.EqualFold(headerKey, prefix) {
				maskedValue := maskSensitiveValue(masked.Get(headerKey), true)
				masked.Set(headerKey, maskedValue)
			}
		}
	}

	return masked
}

// maskBody masks sensitive information in the request body.
// Supports JSON and URL-encoded form data formats.
// For JSON: matches "field": "value" and replaces value with <MASKED>.
// For form data: matches field=value and replaces value with <MASKED>.
func maskBody(body []byte, maskSecrets bool) string {
	if !maskSecrets {
		return string(body)
	}

	content := string(body)

	sensitivePatterns := getSensitivePatterns()

	content = maskJSONFields(content, sensitivePatterns)
	content = maskFormFields(content, sensitivePatterns)

	return content
}

// getSensitivePatterns returns the list of sensitive field name patterns.
func getSensitivePatterns() []string {
	return []string{
		"api_key", "apikey", "api-key",
		"token", "access_token", "accesstoken",
		"secret", "password",
		"authorization", "auth_header", "authheader",
		"api_secret", "apisecret",
		"bearer", "x-api-key", "x-auth-token",
	}
}

// maskJSONFields masks sensitive fields in JSON-formatted content.
func maskJSONFields(content string, patterns []string) string {
	for _, pattern := range patterns {
		content = maskJSONStringValue(content, pattern)
		content = maskJSONNumericValue(content, pattern)
	}
	return content
}

// maskJSONStringValue masks JSON string values for a specific field pattern.
func maskJSONStringValue(content, pattern string) string {
	jsonPattern := fmt.Sprintf(`(?i)"%s"\s*:\s*"[^"]*"`, pattern)
	re := regexp.MustCompile(jsonPattern)
	return re.ReplaceAllStringFunc(content, func(match string) string {
		before, _, ok := strings.Cut(match, ":")
		if !ok {
			return match
		}
		return before + `: "<MASKED>"`
	})
}

// maskJSONNumericValue masks JSON numeric/boolean values for a specific field pattern.
func maskJSONNumericValue(content, pattern string) string {
	jsonNumPattern := fmt.Sprintf(`(?i)"%s"\s*:\s*(?:[0-9]+|true|false|null)`, pattern)
	re := regexp.MustCompile(jsonNumPattern)
	return re.ReplaceAllString(content, fmt.Sprintf(`"%s": "<MASKED>"`, pattern))
}

// maskFormFields masks sensitive fields in URL-encoded form data.
func maskFormFields(content string, patterns []string) string {
	for _, pattern := range patterns {
		formPattern := fmt.Sprintf(`(?i)%s=[^&]*`, pattern)
		re := regexp.MustCompile(formPattern)
		content = re.ReplaceAllString(content, fmt.Sprintf(`%s=<MASKED>`, pattern))
	}
	return content
}
