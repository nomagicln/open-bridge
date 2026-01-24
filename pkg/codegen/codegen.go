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

	// Mask Authorization header values
	if strings.HasPrefix(value, "Bearer ") {
		return "Bearer <YOUR_API_KEY>"
	}
	if strings.HasPrefix(value, "Basic ") {
		return "Basic <YOUR_CREDENTIALS>"
	}

	// For other values that look like tokens/keys, mask them:
	// - No spaces (valid credential constraint)
	// - Longer than typical words (minimum 15 chars is conservative for real tokens)
	// - Contains underscores, hyphens, or alphanumeric-only patterns (typical for API keys)
	if len(value) > 14 && !strings.Contains(value, " ") {
		hasSpecialChars := strings.ContainsAny(value, "-_")
		// Also mask if it's a hex-like string (common for tokens)
		allAlphaNum := true
		for _, ch := range value {
			isAlphaNum := (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || (ch >= '0' && ch <= '9')
			if !isAlphaNum && ch != '-' && ch != '_' {
				allAlphaNum = false
				break
			}
		}

		if hasSpecialChars || allAlphaNum {
			return "<YOUR_API_KEY>"
		}
	}

	return value
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

	// Sensitive field name patterns (case-insensitive matching in regex)
	sensitivePatterns := []string{
		"api_key", "apikey", "api-key",
		"token", "access_token", "accesstoken",
		"secret", "password",
		"authorization", "auth_header", "authheader",
		"api_secret", "apisecret",
		"bearer", "x-api-key", "x-auth-token",
	}

	// Build regex patterns for JSON format: "field": "value" or "field": value
	for _, pattern := range sensitivePatterns {
		// Match JSON format: "fieldName": "stringValue" or "fieldName": 123
		// Case-insensitive field matching
		jsonPattern := fmt.Sprintf(`(?i)"%s"\s*:\s*"[^"]*"`, pattern)
		re := regexp.MustCompile(jsonPattern)
		content = re.ReplaceAllStringFunc(content, func(match string) string {
			// Replace the value part while keeping the field name
			before, _, ok := strings.Cut(match, ":")
			if !ok {
				return match
			}
			return before + `: "<MASKED>"`
		})

		// Also match numeric/boolean values: "fieldName": 123 or "fieldName": true
		jsonNumPattern := fmt.Sprintf(`(?i)"%s"\s*:\s*(?:[0-9]+|true|false|null)`, pattern)
		re = regexp.MustCompile(jsonNumPattern)
		content = re.ReplaceAllString(content, fmt.Sprintf(`"%s": "<MASKED>"`, pattern))
	}

	// Build regex patterns for URL-encoded form data: field=value&
	for _, pattern := range sensitivePatterns {
		// Match form data: fieldName=value& or fieldName=value (end of string)
		// Case-insensitive field matching
		formPattern := fmt.Sprintf(`(?i)%s=[^&]*`, pattern)
		re := regexp.MustCompile(formPattern)
		content = re.ReplaceAllString(content, fmt.Sprintf(`%s=<MASKED>`, pattern))
	}

	return content
}
