// Package cli provides CLI command handling for OpenBridge.
package cli

import (
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/nomagicln/open-bridge/pkg/config"
)

// ErrorFormatter provides user-friendly error messages.
type ErrorFormatter struct{}

// NewErrorFormatter creates a new error formatter.
func NewErrorFormatter() *ErrorFormatter {
	return &ErrorFormatter{}
}

// FormatError formats an error into a user-friendly message.
func (f *ErrorFormatter) FormatError(err error) string {
	return f.FormatErrorWithContext(err, nil)
}

// FormatErrorWithContext formats an error with additional context like installed apps.
func (f *ErrorFormatter) FormatErrorWithContext(err error, installedApps []string) string {
	if err == nil {
		return ""
	}

	// Check for specific error types
	switch e := err.(type) {
	case *config.AppNotFoundError:
		return f.formatAppNotFoundError(e, installedApps)
	case *url.Error:
		return f.formatNetworkError(e)
	case *net.OpError:
		return f.formatNetworkError(e)
	default:
		// Check error message patterns
		errMsg := err.Error()
		if strings.Contains(errMsg, "unknown resource") {
			return f.formatUnknownResourceError(errMsg)
		}
		if strings.Contains(errMsg, "unknown verb") {
			return f.formatUnknownVerbError(errMsg)
		}
		if strings.Contains(errMsg, "required parameter") && strings.Contains(errMsg, "missing") {
			return f.formatMissingParameterError(errMsg)
		}
		if strings.Contains(errMsg, "parameter validation failed") {
			return f.formatParameterValidationError(errMsg)
		}
		if strings.Contains(errMsg, "failed to load spec") {
			return f.formatSpecLoadError(errMsg)
		}
		return fmt.Sprintf("Error: %s", errMsg)
	}
}

// formatAppNotFoundError formats app not found errors with suggestions.
func (f *ErrorFormatter) formatAppNotFoundError(err *config.AppNotFoundError, installedApps []string) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Error: App '%s' is not installed.\n\n", err.AppName))

	// Suggest similar app names if available
	if len(installedApps) > 0 {
		suggestions := f.SuggestSimilarApps(err.AppName, installedApps)
		if len(suggestions) > 0 {
			sb.WriteString("Did you mean:\n")
			for _, suggestion := range suggestions {
				sb.WriteString(fmt.Sprintf("  %s\n", suggestion))
			}
			sb.WriteString("\n")
		}
	}

	sb.WriteString("To install an app, use:\n")
	sb.WriteString(fmt.Sprintf("  ob install %s --spec <path-or-url>\n\n", err.AppName))
	sb.WriteString("To see all installed apps, use:\n")
	sb.WriteString("  ob list")
	return sb.String()
}

// formatNetworkError formats network connectivity errors.
func (f *ErrorFormatter) formatNetworkError(err error) string {
	var sb strings.Builder
	sb.WriteString("Error: Network connectivity issue.\n\n")

	// Extract URL if available
	var targetURL string
	switch e := err.(type) {
	case *url.Error:
		targetURL = e.URL
		sb.WriteString(fmt.Sprintf("Failed to connect to: %s\n", targetURL))
		if e.Timeout() {
			sb.WriteString("Reason: Connection timeout\n\n")
		} else if e.Temporary() {
			sb.WriteString("Reason: Temporary network error\n\n")
		} else {
			sb.WriteString(fmt.Sprintf("Reason: %v\n\n", e.Err))
		}
	case *net.OpError:
		if e.Addr != nil {
			targetURL = e.Addr.String()
			sb.WriteString(fmt.Sprintf("Failed to connect to: %s\n", targetURL))
		}
		sb.WriteString(fmt.Sprintf("Reason: %v\n\n", e.Err))
	default:
		sb.WriteString(fmt.Sprintf("Details: %v\n\n", err))
	}

	sb.WriteString("Troubleshooting:\n")
	sb.WriteString("  - Check your internet connection\n")
	sb.WriteString("  - Verify the API endpoint is accessible\n")
	sb.WriteString("  - Check if a proxy or firewall is blocking the connection\n")
	if targetURL != "" {
		sb.WriteString(fmt.Sprintf("  - Try accessing %s in a browser\n", targetURL))
	}

	return sb.String()
}

// formatUnknownResourceError formats unknown resource errors.
func (f *ErrorFormatter) formatUnknownResourceError(errMsg string) string {
	// Extract resource name from error message
	parts := strings.Split(errMsg, ":")
	resourceName := ""
	if len(parts) > 1 {
		resourceName = strings.TrimSpace(parts[1])
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Error: Unknown resource '%s'.\n\n", resourceName))
	sb.WriteString("To see available resources, use:\n")
	sb.WriteString("  <app> --help\n\n")
	sb.WriteString("Or list all operations:\n")
	sb.WriteString("  <app> list operations")

	return sb.String()
}

// formatUnknownVerbError formats unknown verb errors.
func (f *ErrorFormatter) formatUnknownVerbError(errMsg string) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Error: %s\n\n", errMsg))
	sb.WriteString("Common verbs include:\n")
	sb.WriteString("  - create: Create a new resource\n")
	sb.WriteString("  - list: List all resources\n")
	sb.WriteString("  - get: Get a specific resource\n")
	sb.WriteString("  - update: Update a resource\n")
	sb.WriteString("  - delete: Delete a resource\n\n")
	sb.WriteString("To see available verbs for a resource, use:\n")
	sb.WriteString("  <app> <resource> --help")

	return sb.String()
}

// formatMissingParameterError formats missing parameter errors.
func (f *ErrorFormatter) formatMissingParameterError(errMsg string) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Error: %s\n\n", errMsg))
	sb.WriteString("Usage:\n")
	sb.WriteString("  <app> <verb> <resource> --<param-name> <value>\n\n")
	sb.WriteString("To see all required parameters for this operation, use:\n")
	sb.WriteString("  <app> <verb> <resource> --help")

	return sb.String()
}

// formatParameterValidationError formats parameter validation errors.
func (f *ErrorFormatter) formatParameterValidationError(errMsg string) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Error: %s\n\n", errMsg))
	sb.WriteString("Please check:\n")
	sb.WriteString("  - Parameter names are spelled correctly\n")
	sb.WriteString("  - Parameter values match the expected type\n")
	sb.WriteString("  - Required parameters are provided\n\n")
	sb.WriteString("To see parameter details, use:\n")
	sb.WriteString("  <app> <verb> <resource> --help")

	return sb.String()
}

// formatSpecLoadError formats spec loading errors.
func (f *ErrorFormatter) formatSpecLoadError(errMsg string) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Error: %s\n\n", errMsg))
	sb.WriteString("Possible causes:\n")
	sb.WriteString("  - The OpenAPI specification file is not accessible\n")
	sb.WriteString("  - The specification URL is incorrect or unreachable\n")
	sb.WriteString("  - The specification file is malformed or invalid\n\n")
	sb.WriteString("To update the spec source, use:\n")
	sb.WriteString("  ob install <app> --spec <new-path-or-url>")

	return sb.String()
}

// FormatHTTPError formats HTTP error responses with status codes and details.
func (f *ErrorFormatter) FormatHTTPError(resp *http.Response, body []byte) string {
	var sb strings.Builder

	// Format status line
	sb.WriteString(fmt.Sprintf("Error: HTTP %d %s\n\n", resp.StatusCode, resp.Status))

	// Add user-friendly explanation based on status code
	switch resp.StatusCode {
	case http.StatusBadRequest:
		sb.WriteString("The request was invalid or malformed.\n")
	case http.StatusUnauthorized:
		sb.WriteString("Authentication is required or has failed.\n")
		sb.WriteString("Check your credentials with: <app> config auth\n")
	case http.StatusForbidden:
		sb.WriteString("You don't have permission to access this resource.\n")
	case http.StatusNotFound:
		sb.WriteString("The requested resource was not found.\n")
	case http.StatusMethodNotAllowed:
		sb.WriteString("The HTTP method is not allowed for this resource.\n")
	case http.StatusConflict:
		sb.WriteString("The request conflicts with the current state of the resource.\n")
	case http.StatusUnprocessableEntity:
		sb.WriteString("The request was well-formed but contains semantic errors.\n")
	case http.StatusTooManyRequests:
		sb.WriteString("Too many requests. Please wait before trying again.\n")
	case http.StatusInternalServerError:
		sb.WriteString("The server encountered an internal error.\n")
	case http.StatusBadGateway:
		sb.WriteString("The server received an invalid response from an upstream server.\n")
	case http.StatusServiceUnavailable:
		sb.WriteString("The service is temporarily unavailable.\n")
	case http.StatusGatewayTimeout:
		sb.WriteString("The server did not receive a timely response from an upstream server.\n")
	}

	// Add response body if available
	if len(body) > 0 {
		sb.WriteString("\nResponse:\n")
		// Try to format as JSON
		var prettyBody string
		if strings.HasPrefix(strings.TrimSpace(string(body)), "{") || strings.HasPrefix(strings.TrimSpace(string(body)), "[") {
			prettyBody = string(body)
		} else {
			prettyBody = string(body)
		}
		sb.WriteString(prettyBody)
	}

	return sb.String()
}

// FormatUsageHelp formats usage help for a command.
func (f *ErrorFormatter) FormatUsageHelp(appName, verb, resource string, operation *openapi3.Operation, opParams openapi3.Parameters) string {
	return f.FormatUsageHelpWithBody(appName, verb, resource, operation, opParams, nil)
}

// FormatUsageHelpWithBody formats usage help for a command including request body parameters.
func (f *ErrorFormatter) FormatUsageHelpWithBody(appName, verb, resource string, operation *openapi3.Operation, opParams openapi3.Parameters, requestBody *openapi3.RequestBody) string {
	var sb strings.Builder

	// Command syntax
	sb.WriteString(fmt.Sprintf("Usage: %s %s %s [flags]\n\n", appName, verb, resource))

	// Description
	if operation.Summary != "" {
		sb.WriteString(fmt.Sprintf("Description: %s\n\n", operation.Summary))
	} else if operation.Description != "" {
		sb.WriteString(fmt.Sprintf("Description: %s\n\n", operation.Description))
	}

	// Collect required and optional parameters from URL params
	var requiredParams []string
	var optionalParams []string

	for _, paramRef := range opParams {
		param := paramRef.Value
		if param.Required {
			requiredParams = append(requiredParams, param.Name)
		} else {
			optionalParams = append(optionalParams, param.Name)
		}
	}

	// Extract request body parameters
	var bodyProperties map[string]*openapi3.SchemaRef
	var requiredBodyProps []string
	if requestBody != nil {
		for _, mediaType := range requestBody.Content {
			if mediaType.Schema != nil && mediaType.Schema.Value != nil {
				schema := mediaType.Schema.Value
				bodyProperties = schema.Properties
				requiredBodyProps = schema.Required
				break
			}
		}
	}

	// Display required URL parameters
	if len(requiredParams) > 0 {
		sb.WriteString("Required Parameters:\n")
		for _, paramRef := range opParams {
			param := paramRef.Value
			if param.Required {
				sb.WriteString(f.formatParameter(param))
			}
		}
		sb.WriteString("\n")
	}

	// Display required body parameters
	if len(requiredBodyProps) > 0 && bodyProperties != nil {
		sb.WriteString("Required Body Parameters:\n")
		for _, propName := range requiredBodyProps {
			if propSchema, ok := bodyProperties[propName]; ok {
				sb.WriteString(f.formatBodyProperty(propName, propSchema, true))
			}
		}
		sb.WriteString("\n")
	}

	// Display optional URL parameters
	if len(optionalParams) > 0 {
		sb.WriteString("Optional Parameters:\n")
		for _, paramRef := range opParams {
			param := paramRef.Value
			if !param.Required {
				sb.WriteString(f.formatParameter(param))
			}
		}
		sb.WriteString("\n")
	}

	// Display optional body parameters
	if bodyProperties != nil {
		var optionalBodyProps []string
		for propName := range bodyProperties {
			isRequired := false
			for _, req := range requiredBodyProps {
				if req == propName {
					isRequired = true
					break
				}
			}
			if !isRequired {
				optionalBodyProps = append(optionalBodyProps, propName)
			}
		}
		if len(optionalBodyProps) > 0 {
			sb.WriteString("Optional Body Parameters:\n")
			for _, propName := range optionalBodyProps {
				if propSchema, ok := bodyProperties[propName]; ok {
					sb.WriteString(f.formatBodyProperty(propName, propSchema, false))
				}
			}
			sb.WriteString("\n")
		}
	}

	// Output format flags
	sb.WriteString("Output Flags:\n")
	sb.WriteString("  --json       Output in JSON format\n")
	sb.WriteString("  --yaml       Output in YAML format\n")
	sb.WriteString("  --output     Output format: table, json, yaml (default: table)\n\n")

	// Example
	sb.WriteString("Example:\n")
	sb.WriteString(f.formatExampleWithBody(appName, verb, resource, opParams, bodyProperties, requiredBodyProps))

	return sb.String()
}

// formatBodyProperty formats a body property for help output.
func (f *ErrorFormatter) formatBodyProperty(name string, schemaRef *openapi3.SchemaRef, required bool) string {
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("  --%s", name))

	// Add type information
	if schemaRef != nil && schemaRef.Value != nil {
		schema := schemaRef.Value
		if schema.Type != nil {
			sb.WriteString(fmt.Sprintf(" <%s>", schema.Type.Slice()[0]))
		}

		// Add description
		if schema.Description != "" {
			sb.WriteString(fmt.Sprintf("\n      %s", schema.Description))
		}

		// Add enum values if present
		if len(schema.Enum) > 0 {
			sb.WriteString("\n      Allowed values: ")
			var values []string
			for _, v := range schema.Enum {
				values = append(values, fmt.Sprintf("%v", v))
			}
			sb.WriteString(strings.Join(values, ", "))
		}

		// Add example if present
		if schema.Example != nil {
			sb.WriteString(fmt.Sprintf("\n      Example: %v", schema.Example))
		}
	}

	if required {
		sb.WriteString(" (required)")
	}

	sb.WriteString("\n")
	return sb.String()
}

// formatExampleWithBody formats an example command including body parameters.
func (f *ErrorFormatter) formatExampleWithBody(appName, verb, resource string, opParams openapi3.Parameters, bodyProps map[string]*openapi3.SchemaRef, requiredBodyProps []string) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("  %s %s %s", appName, verb, resource))

	// Add example values for required URL parameters
	for _, paramRef := range opParams {
		param := paramRef.Value
		if param.Required {
			exampleValue := f.getExampleValue(param)
			sb.WriteString(fmt.Sprintf(" --%s %s", param.Name, exampleValue))
		}
	}

	// Add example values for required body parameters
	for _, propName := range requiredBodyProps {
		if propSchema, ok := bodyProps[propName]; ok {
			exampleValue := f.getExampleValueForSchema(propName, propSchema)
			sb.WriteString(fmt.Sprintf(" --%s %s", propName, exampleValue))
		}
	}

	sb.WriteString("\n")
	return sb.String()
}

// getExampleValueForSchema returns an example value for a schema property.
func (f *ErrorFormatter) getExampleValueForSchema(name string, schemaRef *openapi3.SchemaRef) string {
	if schemaRef == nil || schemaRef.Value == nil {
		return "\"value\""
	}

	schema := schemaRef.Value

	// Check for example in schema
	if schema.Example != nil {
		return fmt.Sprintf("\"%v\"", schema.Example)
	}

	// Use enum value if available
	if len(schema.Enum) > 0 {
		return fmt.Sprintf("\"%v\"", schema.Enum[0])
	}

	// Generate example based on type
	if schema.Type != nil {
		switch {
		case schema.Type.Is("string"):
			return fmt.Sprintf("\"%s-value\"", name)
		case schema.Type.Is("integer"):
			return "123"
		case schema.Type.Is("number"):
			return "123.45"
		case schema.Type.Is("boolean"):
			return "true"
		case schema.Type.Is("array"):
			return "\"value1,value2\""
		case schema.Type.Is("object"):
			return "\"{}\""
		}
	}

	return "\"value\""
}

// formatParameter formats a single parameter for help output.
func (f *ErrorFormatter) formatParameter(param *openapi3.Parameter) string {
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("  --%s", param.Name))

	// Add type information
	if param.Schema != nil && param.Schema.Value != nil {
		schema := param.Schema.Value
		if schema.Type != nil {
			sb.WriteString(fmt.Sprintf(" <%s>", schema.Type.Slice()[0]))
		}
	}

	// Add description
	if param.Description != "" {
		sb.WriteString(fmt.Sprintf("\n      %s", param.Description))
	}

	// Add location
	sb.WriteString(fmt.Sprintf(" (in: %s)", param.In))

	// Add enum values if present
	if param.Schema != nil && param.Schema.Value != nil && len(param.Schema.Value.Enum) > 0 {
		sb.WriteString("\n      Allowed values: ")
		var values []string
		for _, v := range param.Schema.Value.Enum {
			values = append(values, fmt.Sprintf("%v", v))
		}
		sb.WriteString(strings.Join(values, ", "))
	}

	sb.WriteString("\n")
	return sb.String()
}

// formatExample formats an example command.
func (f *ErrorFormatter) formatExample(appName, verb, resource string, opParams openapi3.Parameters) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("  %s %s %s", appName, verb, resource))

	// Add example values for required parameters
	for _, paramRef := range opParams {
		param := paramRef.Value
		if param.Required {
			exampleValue := f.getExampleValue(param)
			sb.WriteString(fmt.Sprintf(" --%s %s", param.Name, exampleValue))
		}
	}

	sb.WriteString("\n")
	return sb.String()
}

// getExampleValue returns an example value for a parameter.
func (f *ErrorFormatter) getExampleValue(param *openapi3.Parameter) string {
	// Check for example in parameter
	if param.Example != nil {
		return fmt.Sprintf("%v", param.Example)
	}

	// Check for example in schema
	if param.Schema != nil && param.Schema.Value != nil {
		schema := param.Schema.Value
		if schema.Example != nil {
			return fmt.Sprintf("%v", schema.Example)
		}

		// Use enum value if available
		if len(schema.Enum) > 0 {
			return fmt.Sprintf("%v", schema.Enum[0])
		}

		// Generate example based on type
		if schema.Type != nil {
			switch {
			case schema.Type.Is("string"):
				return fmt.Sprintf("\"%s-value\"", param.Name)
			case schema.Type.Is("integer"):
				return "123"
			case schema.Type.Is("number"):
				return "123.45"
			case schema.Type.Is("boolean"):
				return "true"
			case schema.Type.Is("array"):
				return "\"value1,value2\""
			}
		}
	}

	return "\"value\""
}

// SuggestSimilarApps suggests similar app names for typos.
func (f *ErrorFormatter) SuggestSimilarApps(appName string, installedApps []string) []string {
	if len(installedApps) == 0 {
		return nil
	}

	var suggestions []string
	appNameLower := strings.ToLower(appName)

	for _, installed := range installedApps {
		installedLower := strings.ToLower(installed)

		// Exact match (case-insensitive)
		if appNameLower == installedLower {
			return []string{installed}
		}

		// Prefix match
		if strings.HasPrefix(installedLower, appNameLower) {
			suggestions = append(suggestions, installed)
			continue
		}

		// Contains match
		if strings.Contains(installedLower, appNameLower) {
			suggestions = append(suggestions, installed)
			continue
		}

		// Levenshtein distance (simple implementation)
		if f.levenshteinDistance(appNameLower, installedLower) <= 2 {
			suggestions = append(suggestions, installed)
		}
	}

	return suggestions
}

// levenshteinDistance calculates the Levenshtein distance between two strings.
func (f *ErrorFormatter) levenshteinDistance(s1, s2 string) int {
	if len(s1) == 0 {
		return len(s2)
	}
	if len(s2) == 0 {
		return len(s1)
	}

	// Create matrix
	matrix := make([][]int, len(s1)+1)
	for i := range matrix {
		matrix[i] = make([]int, len(s2)+1)
		matrix[i][0] = i
	}
	for j := range matrix[0] {
		matrix[0][j] = j
	}

	// Fill matrix
	for i := 1; i <= len(s1); i++ {
		for j := 1; j <= len(s2); j++ {
			cost := 0
			if s1[i-1] != s2[j-1] {
				cost = 1
			}
			matrix[i][j] = min(
				matrix[i-1][j]+1,      // deletion
				matrix[i][j-1]+1,      // insertion
				matrix[i-1][j-1]+cost, // substitution
			)
		}
	}

	return matrix[len(s1)][len(s2)]
}

// min returns the minimum of three integers.
func min(a, b, c int) int {
	if a < b {
		if a < c {
			return a
		}
		return c
	}
	if b < c {
		return b
	}
	return c
}
