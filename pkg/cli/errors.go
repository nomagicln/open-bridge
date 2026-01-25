// Package cli provides CLI command handling for OpenBridge.
package cli

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/nomagicln/open-bridge/pkg/config"
)

// PrintedError wraps an error that has already been printed to stderr.
// This prevents double printing when the error bubbles up to main().
type PrintedError struct {
	Err error
}

func (e *PrintedError) Error() string {
	// Return empty string since the error has already been printed.
	// This prevents double printing when the error bubbles up.
	return ""
}

func (e *PrintedError) Unwrap() error {
	return e.Err
}

// IsPrintedError checks if the error is a PrintedError.
func IsPrintedError(err error) bool {
	var pe *PrintedError
	return errors.As(err, &pe)
}

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
	{
		var e *config.AppNotFoundError
		var e1 *url.Error
		var e2 *net.OpError
		switch {
		case errors.As(err, &e):
			return f.formatAppNotFoundError(e, installedApps)
		case errors.As(err, &e1):
			return f.formatNetworkError(e1)
		case errors.As(err, &e2):
			return f.formatNetworkError(e2)
		default:
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
}

// formatAppNotFoundError formats app not found errors with suggestions.
func (f *ErrorFormatter) formatAppNotFoundError(err *config.AppNotFoundError, installedApps []string) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "Error: App '%s' is not installed.\n\n", err.AppName)

	// Suggest similar app names if available
	if len(installedApps) > 0 {
		suggestions := f.SuggestSimilarApps(err.AppName, installedApps)
		if len(suggestions) > 0 {
			sb.WriteString("Did you mean:\n")
			for _, suggestion := range suggestions {
				fmt.Fprintf(&sb, "  %s\n", suggestion)
			}
			sb.WriteString("\n")
		}
	}

	sb.WriteString("To install an app, use:\n")
	fmt.Fprintf(&sb, "  ob install %s --spec <path-or-url>\n\n", err.AppName)
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
	{
		var e *url.Error
		var e1 *net.OpError
		switch {
		case errors.As(err, &e):
			targetURL = e.URL
			fmt.Fprintf(&sb, "Failed to connect to: %s\n", targetURL)
			if e.Timeout() {
				sb.WriteString("Reason: Connection timeout\n\n")
			} else if e.Temporary() {
				sb.WriteString("Reason: Temporary network error\n\n")
			} else {
				fmt.Fprintf(&sb, "Reason: %v\n\n", e.Err)
			}
		case errors.As(err, &e1):
			if e1.Addr != nil {
				targetURL = e1.Addr.String()
				fmt.Fprintf(&sb, "Failed to connect to: %s\n", targetURL)
			}
			fmt.Fprintf(&sb, "Reason: %v\n\n", e1.Err)
		default:
			fmt.Fprintf(&sb, "Details: %v\n\n", err)
		}
	}

	sb.WriteString("Troubleshooting:\n")
	sb.WriteString("  - Check your internet connection\n")
	sb.WriteString("  - Verify the API endpoint is accessible\n")
	sb.WriteString("  - Check if a proxy or firewall is blocking the connection\n")
	if targetURL != "" {
		fmt.Fprintf(&sb, "  - Try accessing %s in a browser\n", targetURL)
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
	fmt.Fprintf(&sb, "Error: Unknown resource '%s'.\n\n", resourceName)
	sb.WriteString("To see available resources, use:\n")
	sb.WriteString("  <app> --help\n\n")
	sb.WriteString("Or list all operations:\n")
	sb.WriteString("  <app> list operations")

	return sb.String()
}

// formatUnknownVerbError formats unknown verb errors.
func (f *ErrorFormatter) formatUnknownVerbError(errMsg string) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "Error: %s\n\n", errMsg)
	sb.WriteString("Common verbs include:\n")
	sb.WriteString("  - create: Create a new resource\n")
	sb.WriteString("  - list: List all resources\n")
	sb.WriteString("  - get: Get a specific resource\n")
	sb.WriteString("  - update: Update a resource\n")
	sb.WriteString("  - delete: Delete a resource\n\n")
	sb.WriteString("\nTo see available verbs for a resource, use:\n")
	sb.WriteString("  <app> <resource> --help")

	return sb.String()
}

// formatMissingParameterError formats missing parameter errors.
func (f *ErrorFormatter) formatMissingParameterError(errMsg string) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "Error: %s\n\n", errMsg)
	sb.WriteString("Usage:\n")
	sb.WriteString("  <app> <resource> <verb> --<param-name> <value>\n\n")
	sb.WriteString("To see all required parameters for this operation, use:\n")
	sb.WriteString("  <app> <resource> <verb> --help")

	return sb.String()
}

func (f *ErrorFormatter) formatErrorWithList(errMsg, listTitle string, listItems []string, footerTitle, footerCommand string) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "Error: %s\n\n", errMsg)

	if listTitle != "" {
		sb.WriteString(listTitle)
		sb.WriteString("\n")
		for _, item := range listItems {
			sb.WriteString("  - ")
			sb.WriteString(item)
			sb.WriteString("\n")
		}
		sb.WriteString("\n")
	}

	if footerTitle != "" {
		sb.WriteString(footerTitle)
		sb.WriteString("\n")
	}
	if footerCommand != "" {
		sb.WriteString("  ")
		sb.WriteString(footerCommand)
	}

	return sb.String()
}

// formatParameterValidationError formats parameter validation errors.
func (f *ErrorFormatter) formatParameterValidationError(errMsg string) string {
	return f.formatErrorWithList(
		errMsg,
		"Please check:",
		[]string{
			"Parameter names are spelled correctly",
			"Parameter values match the expected type",
			"Required parameters are provided",
		},
		"To see parameter details, use:",
		"<app> <resource> <verb> --help",
	)
}

// formatSpecLoadError formats spec loading errors.
func (f *ErrorFormatter) formatSpecLoadError(errMsg string) string {
	return f.formatErrorWithList(
		errMsg,
		"Possible causes:",
		[]string{
			"The OpenAPI specification file is not accessible",
			"The specification URL is incorrect or unreachable",
			"The specification file is malformed or invalid",
		},
		"To update the spec source, use:",
		"ob install <app> --spec <new-path-or-url>",
	)
}

// writeStatusExplanation writes user-friendly explanation for HTTP status code.
func writeStatusExplanation(sb *strings.Builder, statusCode int) {
	switch statusCode {
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
	default:
		sb.WriteString("An unexpected error occurred.\n")
	}
}

// writeResponseBody writes the response body to the error message.
func writeResponseBody(sb *strings.Builder, body []byte) {
	if len(body) == 0 {
		return
	}
	sb.WriteString("\nResponse:\n")
	sb.WriteString(string(body))
}

// FormatHTTPError formats HTTP error responses with status codes and details.
func (f *ErrorFormatter) FormatHTTPError(resp *http.Response, body []byte) string {
	var sb strings.Builder

	fmt.Fprintf(&sb, "Error: HTTP %d %s\n\n", resp.StatusCode, resp.Status)
	writeStatusExplanation(&sb, resp.StatusCode)
	writeResponseBody(&sb, body)

	return sb.String()
}

// FormatUsageHelp formats usage help for a command.
func (f *ErrorFormatter) FormatUsageHelp(appName, resource, verb string, operation *openapi3.Operation, opParams openapi3.Parameters) string {
	return f.FormatUsageHelpWithBody(appName, resource, verb, operation, opParams, nil)
}

// extractBodySchema extracts body properties and required fields from request body.
func extractBodySchema(requestBody *openapi3.RequestBody) (map[string]*openapi3.SchemaRef, []string) {
	if requestBody == nil {
		return nil, nil
	}
	for _, mediaType := range requestBody.Content {
		if mediaType.Schema != nil && mediaType.Schema.Value != nil {
			schema := mediaType.Schema.Value
			return schema.Properties, schema.Required
		}
	}
	return nil, nil
}

// getOptionalBodyProps returns body properties that are not required.
func getOptionalBodyProps(bodyProperties map[string]*openapi3.SchemaRef, requiredBodyProps []string) []string {
	requiredSet := make(map[string]bool, len(requiredBodyProps))
	for _, req := range requiredBodyProps {
		requiredSet[req] = true
	}

	var optionalProps []string
	for propName := range bodyProperties {
		if !requiredSet[propName] {
			optionalProps = append(optionalProps, propName)
		}
	}
	return optionalProps
}

// writeURLParameters writes URL parameters section to builder.
func (f *ErrorFormatter) writeURLParameters(sb *strings.Builder, opParams openapi3.Parameters, required bool) {
	for _, paramRef := range opParams {
		param := paramRef.Value
		if param.Required == required {
			sb.WriteString(f.formatParameter(param))
		}
	}
}

// writeBodyParameters writes body parameters section to builder.
func (f *ErrorFormatter) writeBodyParameters(sb *strings.Builder, props []string, bodyProperties map[string]*openapi3.SchemaRef, required bool) {
	for _, propName := range props {
		if propSchema, ok := bodyProperties[propName]; ok {
			sb.WriteString(f.formatBodyProperty(propName, propSchema, required))
		}
	}
}

// checkParamTypes returns whether there are required and optional URL parameters.
func checkParamTypes(opParams openapi3.Parameters) (hasRequired, hasOptional bool) {
	for _, paramRef := range opParams {
		if paramRef.Value.Required {
			hasRequired = true
		} else {
			hasOptional = true
		}
	}
	return hasRequired, hasOptional
}

// writeOutputFlags writes the standard output flags section.
func writeOutputFlags(sb *strings.Builder) {
	sb.WriteString("Output Flags:\n")
	sb.WriteString("  --json       Output in JSON format\n")
	sb.WriteString("  --yaml       Output in YAML format (default)\n")
	sb.WriteString("  --output     Output format: json, yaml (default: yaml)\n\n")
}

// FormatUsageHelpWithBody formats usage help for a command including request body parameters.
func (f *ErrorFormatter) FormatUsageHelpWithBody(appName, resource, verb string, operation *openapi3.Operation, opParams openapi3.Parameters, requestBody *openapi3.RequestBody) string {
	var sb strings.Builder

	fmt.Fprintf(&sb, "Usage: %s %s %s [flags]\n\n", appName, resource, verb)

	// Description
	if operation.Summary != "" {
		fmt.Fprintf(&sb, "Description: %s\n\n", operation.Summary)
	} else if operation.Description != "" {
		fmt.Fprintf(&sb, "Description: %s\n\n", operation.Description)
	}

	hasRequiredParams, hasOptionalParams := checkParamTypes(opParams)
	bodyProperties, requiredBodyProps := extractBodySchema(requestBody)

	f.writeParamSections(&sb, opParams, bodyProperties, requiredBodyProps, hasRequiredParams, hasOptionalParams)

	writeOutputFlags(&sb)

	sb.WriteString("Example:\n")
	sb.WriteString(f.formatExampleWithBody(appName, resource, verb, opParams, bodyProperties, requiredBodyProps))

	return sb.String()
}

// writeParamSections writes all parameter sections (required/optional URL and body params).
func (f *ErrorFormatter) writeParamSections(sb *strings.Builder, opParams openapi3.Parameters, bodyProperties map[string]*openapi3.SchemaRef, requiredBodyProps []string, hasRequiredParams, hasOptionalParams bool) {
	if hasRequiredParams {
		sb.WriteString("Required Parameters:\n")
		f.writeURLParameters(sb, opParams, true)
		sb.WriteString("\n")
	}

	if len(requiredBodyProps) > 0 && bodyProperties != nil {
		sb.WriteString("Required Body Parameters:\n")
		f.writeBodyParameters(sb, requiredBodyProps, bodyProperties, true)
		sb.WriteString("\n")
	}

	if hasOptionalParams {
		sb.WriteString("Optional Parameters:\n")
		f.writeURLParameters(sb, opParams, false)
		sb.WriteString("\n")
	}

	if bodyProperties != nil {
		optionalBodyProps := getOptionalBodyProps(bodyProperties, requiredBodyProps)
		if len(optionalBodyProps) > 0 {
			sb.WriteString("Optional Body Parameters:\n")
			f.writeBodyParameters(sb, optionalBodyProps, bodyProperties, false)
			sb.WriteString("\n")
		}
	}
}

// formatBodyProperty formats a body property for help output.
func (f *ErrorFormatter) formatBodyProperty(name string, schemaRef *openapi3.SchemaRef, required bool) string {
	var sb strings.Builder

	fmt.Fprintf(&sb, "  --%s", name)

	// Add type information
	if schemaRef != nil && schemaRef.Value != nil {
		schema := schemaRef.Value
		if schema.Type != nil {
			fmt.Fprintf(&sb, " <%s>", schema.Type.Slice()[0])
		}

		// Add description
		if schema.Description != "" {
			fmt.Fprintf(&sb, "\n      %s", schema.Description)
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
			fmt.Fprintf(&sb, "\n      Example: %v", schema.Example)
		}
	}

	if required {
		sb.WriteString(" (required)")
	}

	sb.WriteString("\n")
	return sb.String()
}

// formatExampleWithBody formats an example command including body parameters.
func (f *ErrorFormatter) formatExampleWithBody(appName, resource, verb string, opParams openapi3.Parameters, bodyProps map[string]*openapi3.SchemaRef, requiredBodyProps []string) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "  %s %s %s", appName, resource, verb)

	// Add example values for required URL parameters
	for _, paramRef := range opParams {
		param := paramRef.Value
		if param.Required {
			exampleValue := f.getExampleValue(param)
			fmt.Fprintf(&sb, " --%s %s", param.Name, exampleValue)
		}
	}

	// Add example values for required body parameters
	for _, propName := range requiredBodyProps {
		if propSchema, ok := bodyProps[propName]; ok {
			exampleValue := f.getExampleValueForSchema(propName, propSchema)
			fmt.Fprintf(&sb, " --%s %s", propName, exampleValue)
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

	fmt.Fprintf(&sb, "  --%s", param.Name)

	// Add type information
	if param.Schema != nil && param.Schema.Value != nil {
		schema := param.Schema.Value
		if schema.Type != nil {
			fmt.Fprintf(&sb, " <%s>", schema.Type.Slice()[0])
		}
	}

	// Add description
	if param.Description != "" {
		fmt.Fprintf(&sb, "\n      %s", param.Description)
	}

	// Add location
	fmt.Fprintf(&sb, " (in: %s)", param.In)

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
			matrix[i][j] = minOfThree(
				matrix[i-1][j]+1,      // deletion
				matrix[i][j-1]+1,      // insertion
				matrix[i-1][j-1]+cost, // substitution
			)
		}
	}

	return matrix[len(s1)][len(s2)]
}

// minOfThree returns the minimum of three integers.
func minOfThree(a, b, c int) int {
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
