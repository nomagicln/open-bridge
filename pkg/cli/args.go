package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/getkin/kin-openapi/openapi3"
	"gopkg.in/yaml.v3"
)

// RequestParams represents classified request parameters by location.
// Parameters are separated into Header, Query, Path, and Body based on their intended use.
type RequestParams struct {
	Headers map[string]string
	Query   map[string]string
	Path    map[string]string
	Body    map[string]any
}

// SplitArgs separates CLI arguments from request parameters using '--' as delimiter.
// Returns (cliArgs, requestArgs, hasDelimiter).
// - If '--' is found: cliArgs are before '--', requestArgs are after '--'
// - If '--' is not found: all args are returned as cliArgs, requestArgs is empty
// - hasDelimiter indicates whether '--' separator was found
//
// Example:
//
//	Input:  ["--generate", "docs", "--", "--header:Auth=Bearer xxx", "--query:page=1"]
//	Output: (["--generate", "docs"], ["--header:Auth=Bearer xxx", "--query:page=1"], true)
//
//	Input:  ["--name", "John", "--query:page=1"]
//	Output: (["--name", "John", "--query:page=1"], nil, false)
func SplitArgs(args []string) (cliArgs []string, requestArgs []string, hasDelimiter bool) {
	for i, arg := range args {
		if arg == "--" {
			// Found delimiter
			cliArgs = args[:i]
			if i+1 < len(args) {
				requestArgs = args[i+1:]
			}
			return cliArgs, requestArgs, true
		}
	}
	// No delimiter found
	return args, nil, false
}

// ParseRequestParams parses request parameters from command arguments.
// Depending on whether hasDelimiter is true, uses different classification strategies:
//
// Mode A (hasDelimiter=true): Prefix-based classification
//
//	Parameters must have prefixes: --header:key=value, --query:key=value, --path:key=value, --body:key=value
//	Example: ["--header:Authorization=Bearer xxx", "--body:tags=dog"]
//
// Mode B (hasDelimiter=false): Auto-classification based on OpenAPI spec
//
//	Parameters use --key=value format, classified by opSpec.Parameters definitions
//	Remaining parameters are treated as Body
//	Example: ["--petId=1", "--name=Fluffy", "--tags=dog"] (auto-classified)
//
// For Body parameters with nested structures, use JSONPath notation:
//
//	--body:user.name=John -> {"user": {"name": "John"}}
//	--body:$.user.name=John -> same structure with explicit $ prefix
//
// Multi-value parameters (appearing multiple times):
//
//	If the same parameter appears multiple times:
//	- If its Schema.Type == "array": collect values into a slice
//	- Otherwise: return error with expected type information
//
// Value parsing supports three formats:
//  1. Quoted strings: "hello world" -> hello world (removes quotes)
//  2. File references: @/path/to/file.json -> read and JSON parse
//     - Absolute paths: /unix/path, C:\windows\path
//     - Relative paths: ./relative/path (resolved from current working directory)
//  3. Direct values: 123, true, $.foo.bar -> parsed based on context
//
// Returns:
//   - *RequestParams: classified parameters
//   - error: if parsing, classification, or validation fails
func ParseRequestParams(args []string, opSpec *openapi3.Operation, hasDelimiter bool) (*RequestParams, error) {
	params := &RequestParams{
		Headers: make(map[string]string),
		Query:   make(map[string]string),
		Path:    make(map[string]string),
		Body:    make(map[string]any),
	}

	if len(args) == 0 {
		return params, nil
	}

	if hasDelimiter {
		// Mode A: Prefix-based classification
		return parseRequestParamsWithPrefix(args, params, opSpec)
	}

	// Mode B: Auto-classification based on OpenAPI spec
	return parseRequestParamsAuto(args, params, opSpec)
}

// extractPrefixAndKey parses "--prefix:key=value" format
// Returns (prefix, key, value, error)
func extractPrefixAndKey(arg string) (string, string, string, error) {
	if !strings.HasPrefix(arg, "--") {
		return "", "", "", nil // Not a prefixed argument
	}

	rest := arg[2:]

	// Extract prefix
	var prefix string
	prefixes := map[string]int{"header": 7, "query": 6, "path": 5, "body": 5}
	for p, len := range prefixes {
		if strings.HasPrefix(rest, p+":") {
			prefix = p
			rest = rest[len:]
			break
		}
	}

	if prefix == "" {
		return "", "", "", fmt.Errorf("unknown parameter prefix in '--%s' (supported: --header:, --query:, --path:, --body:)", rest)
	}

	// Parse key=value
	parts := strings.SplitN(rest, "=", 2)
	if len(parts) != 2 {
		return "", "", "", fmt.Errorf("invalid parameter format '--%s:%s', expected --%s:key=value", prefix, rest, prefix)
	}

	return prefix, parts[0], parts[1], nil
}

// validateMultiValueParam checks if a parameter can accept multiple values
func validateMultiValueParam(opSpec *openapi3.Operation, paramName string) bool {
	// Check query/header parameters first
	if param, found := getParamFromSpec(opSpec, paramName, "query"); found && isArrayType(param) {
		return true
	}
	if param, found := getParamFromSpec(opSpec, paramName, "header"); found && isArrayType(param) {
		return true
	}

	// Check body schema
	if isBodyParamArray(opSpec, paramName) {
		return true
	}

	return false
}

// isArrayType checks if a parameter's schema type is array
func isArrayType(param *openapi3.Parameter) bool {
	return param != nil && param.Schema != nil && param.Schema.Value != nil &&
		param.Schema.Value.Type != nil && param.Schema.Value.Type.Is("array")
}

// isBodyParamArray checks if a body parameter is an array type
func isBodyParamArray(opSpec *openapi3.Operation, paramName string) bool {
	if opSpec == nil || opSpec.RequestBody == nil || opSpec.RequestBody.Value == nil {
		return false
	}

	content := opSpec.RequestBody.Value.Content["application/json"]
	if content == nil || content.Schema == nil || content.Schema.Value == nil {
		return false
	}

	bodySchema := content.Schema.Value
	if bodySchema.Properties == nil {
		return false
	}

	prop := bodySchema.Properties[paramName]
	return prop != nil && prop.Value != nil && prop.Value.Type != nil && prop.Value.Type.Is("array")
}

// processBodyMultiValues processes and validates multi-value body parameters
func processBodyMultiValues(multiValueParams map[string][]any, params *RequestParams, opSpec *openapi3.Operation) error {
	for key, values := range multiValueParams {
		if len(values) > 1 {
			if !validateMultiValueParam(opSpec, key) {
				return fmt.Errorf("parameter '%s' appears multiple times but is not an array type", key)
			}
			params.Body[key] = values
		} else {
			params.Body[key] = values[0]
		}
	}
	return nil
}

// parseRequestParamsWithPrefix handles prefix-based parameter classification (Mode A)
func parseRequestParamsWithPrefix(args []string, params *RequestParams, opSpec *openapi3.Operation) (*RequestParams, error) {
	multiValueParams := make(map[string][]any)

	for _, arg := range args {
		if err := processPrefixedArg(arg, params, opSpec, multiValueParams); err != nil {
			return nil, err
		}
	}

	if err := processBodyMultiValues(multiValueParams, params, opSpec); err != nil {
		return nil, err
	}

	if err := buildAndSetNestedBody(params); err != nil {
		return nil, err
	}

	return params, nil
}

// processPrefixedArg handles a single prefixed argument
func processPrefixedArg(arg string, params *RequestParams, opSpec *openapi3.Operation, multiValueParams map[string][]any) error {
	prefix, key, valueStr, err := extractPrefixAndKey(arg)
	if err != nil {
		return err
	}
	if prefix == "" {
		return nil
	}

	parsed, err := ParseValue(valueStr)
	if err != nil {
		return fmt.Errorf("failed to parse --%s:%s: %w", prefix, key, err)
	}

	switch prefix {
	case "header":
		params.Headers[key] = fmt.Sprintf("%v", parsed)
	case "query":
		params.Query[key] = fmt.Sprintf("%v", parsed)
	case "path":
		params.Path[key] = fmt.Sprintf("%v", parsed)
	case "body":
		if err := validateBodyParam(opSpec, key); err != nil {
			return err
		}
		converted := convertBodyParamToSchemaType(opSpec, key, parsed)
		if _, exists := params.Body[key]; exists {
			multiValueParams[key] = append(multiValueParams[key], converted)
		} else {
			multiValueParams[key] = []any{converted}
		}
	default:
		return fmt.Errorf("unknown parameter location: %s", prefix)
	}
	return nil
}

// buildAndSetNestedBody builds nested body from flat body parameters
func buildAndSetNestedBody(params *RequestParams) error {
	if len(params.Body) > 0 {
		nested, err := BuildNestedBody(params.Body)
		if err != nil {
			return fmt.Errorf("failed to build nested body: %w", err)
		}
		params.Body = nested
	}
	return nil
}

// classifyParamInSpec tries to find and classify a parameter in the OpenAPI spec
// Returns (location, value, error) if found, or ("", nil, nil) if not found
func classifyParamInSpec(opSpec *openapi3.Operation, key string, valueStr string) (string, any, error) {
	for _, location := range []string{"path", "query", "header"} {
		if param, exists := getParamFromSpec(opSpec, key, location); exists {
			var val any
			var err error
			if param.Schema != nil && param.Schema.Value != nil {
				val, err = parseValueWithContext(valueStr, param.Schema.Value)
				if err != nil {
					return "", nil, fmt.Errorf("failed to parse --%s: %w", key, err)
				}
			} else {
				parsed, err := ParseValue(valueStr)
				if err != nil {
					return "", nil, err
				}
				val = parsed
			}
			return location, val, nil
		}
	}
	return "", nil, nil
}

// storeParamValue stores a parameter value in the appropriate location based on its type
func storeParamValue(params *RequestParams, location string, key string, value any) error {
	valStr := fmt.Sprintf("%v", value)
	switch location {
	case "path":
		params.Path[key] = valStr
	case "query":
		params.Query[key] = valStr
	case "header":
		params.Headers[key] = valStr
	default:
		return fmt.Errorf("unknown parameter location: %s", location)
	}
	return nil
}

// parseRequestParamsAuto handles auto-classification based on OpenAPI spec (Mode B)
// nolint:gocognit,gocyclo,funlen // Complex logic for parameter classification is necessary
func parseRequestParamsAuto(args []string, params *RequestParams, opSpec *openapi3.Operation) (*RequestParams, error) {
	multiValueParams := make(map[string][]any)
	multiValueLocations := make(map[string]string)
	bodyParams := make(map[string]any)

	for i := 0; i < len(args); i++ {
		arg := args[i]
		if !strings.HasPrefix(arg, "--") {
			continue
		}

		rest := arg[2:]
		var key, valueStr string

		// Check for = separator (--key=value format)
		if before, after, ok := strings.Cut(rest, "="); ok {
			key = before
			valueStr = after
		} else {
			// --key value format: get value from next argument
			key = rest
			if i+1 >= len(args) || strings.HasPrefix(args[i+1], "--") {
				// No value or next arg is another flag - treat as boolean
				valueStr = "true"
			} else {
				// Get value from next argument
				valueStr = args[i+1]
				i++
			}
		}

		// Try to classify in spec
		location, val, err := classifyParamInSpec(opSpec, key, valueStr)
		if err != nil {
			return nil, err
		}

		if location != "" {
			// Found in spec
			multiValueParams[key] = append(multiValueParams[key], val)
			multiValueLocations[key] = location
			if len(multiValueParams[key]) == 1 {
				_ = storeParamValue(params, location, key, val)
			}
		} else {
			// Not found in spec, treat as body parameter
			parsed, err := ParseValue(valueStr)
			if err != nil {
				return nil, fmt.Errorf("failed to parse --%s: %w", key, err)
			}
			bodyParams[key] = parsed
		}
	}

	// Validate and store multi-value parameters
	for key, values := range multiValueParams {
		if len(values) <= 1 {
			continue
		}

		location := multiValueLocations[key]
		if param, exists := getParamFromSpec(opSpec, key, location); exists && param.Schema != nil && param.Schema.Value != nil {
			if param.Schema.Value.Type == nil || !param.Schema.Value.Type.Is("array") {
				return nil, fmt.Errorf("parameter '%s' appears multiple times but is not an array type", key)
			}
			_ = storeParamValue(params, location, key, values)
		}
	}

	params.Body = bodyParams

	// Build nested body structure
	if len(params.Body) > 0 {
		nested, err := BuildNestedBody(params.Body)
		if err != nil {
			return nil, fmt.Errorf("failed to build nested body: %w", err)
		}
		params.Body = nested
	}

	return params, nil
}

// ParseValue parses a parameter value supporting three formats:
//  1. Quoted string: "hello world" -> removes quotes, returns string
//  2. File reference: @/path/to/file.json -> reads file, JSON parses if .json/.yaml, else string
//     - Detects absolute vs relative paths (/ or drive letter for Unix/Windows)
//  3. Direct value: 123, true, null -> returns as-is or parsed to appropriate type
//
// Returns the parsed value (string, any, []any, etc.) and error if parsing fails.
// File-related errors include: file not found, read permission denied, invalid JSON/YAML.
func ParseValue(value string) (any, error) {
	// Handle quoted strings
	if len(value) >= 2 && (value[0] == '"' && value[len(value)-1] == '"' || value[0] == '\'' && value[len(value)-1] == '\'') {
		// Remove quotes
		unquoted := value[1 : len(value)-1]
		// Handle escape sequences
		unquoted = strings.ReplaceAll(unquoted, "\\n", "\n")
		unquoted = strings.ReplaceAll(unquoted, "\\t", "\t")
		unquoted = strings.ReplaceAll(unquoted, "\\\"", "\"")
		unquoted = strings.ReplaceAll(unquoted, "\\'", "'")
		unquoted = strings.ReplaceAll(unquoted, "\\\\", "\\")
		return unquoted, nil
	}

	// Handle file references
	if strings.HasPrefix(value, "@") {
		filePath := value[1:]
		absPath, err := resolveFilePath(filePath)
		if err != nil {
			return nil, err
		}
		return readFileContent(absPath)
	}

	// Handle direct values - try to parse as specific types
	// Try boolean
	if value == "true" || value == "false" {
		return value == "true", nil
	}

	// Try null
	if value == "null" {
		return nil, nil
	}

	// Try integer
	if intVal, err := strconv.ParseInt(value, 10, 64); err == nil {
		return intVal, nil
	}

	// Try float
	if floatVal, err := strconv.ParseFloat(value, 64); err == nil {
		return floatVal, nil
	}

	// Try JSON object/array
	if (strings.HasPrefix(value, "{") && strings.HasSuffix(value, "}")) ||
		(strings.HasPrefix(value, "[") && strings.HasSuffix(value, "]")) {
		var result any
		if err := json.Unmarshal([]byte(value), &result); err == nil {
			return result, nil
		}
	}

	// Return as string
	return value, nil
}

// BuildNestedBody constructs nested JSON objects from flat Body parameters using JSONPath.
// Input map may contain keys like:
//   - "user.name" -> {"user": {"name": ...}}
//   - "user.address.zip" -> {"user": {"address": {"zip": ...}}}
//   - "items" -> {"items": ...} (no nesting)
//
// Returns:
//   - map[string]any: nested structure
//   - error: if path construction fails (e.g., conflicting value types)
func BuildNestedBody(flatBodyParams map[string]any) (map[string]any, error) {
	result := make(map[string]any)

	for key, value := range flatBodyParams {
		// Skip $ prefix if present (JSONPath notation)
		pathKey := key
		if strings.HasPrefix(pathKey, "$.") {
			pathKey = pathKey[2:]
		} else if strings.HasPrefix(pathKey, "$") {
			pathKey = pathKey[1:]
		}

		// Handle direct values (no nesting)
		if !strings.Contains(pathKey, ".") {
			result[pathKey] = value
			continue
		}

		// Handle nested paths
		if err := setNestedValue(result, pathKey, value); err != nil {
			return nil, err
		}
	}

	return result, nil
}

// parseValueWithContext is an internal helper for parsing values in context of a parameter's schema.
// Supports type-aware parsing for proper JSON serialization.
// If schema is nil, performs generic parsing.
// nolint:gocognit,funlen // Type-aware parsing requires handling multiple cases
func parseValueWithContext(value string, schema *openapi3.Schema) (any, error) {
	// First try generic parsing
	parsed, err := ParseValue(value)
	if err != nil {
		return nil, err
	}

	// If no schema, return as-is
	if schema == nil {
		return parsed, nil
	}

	// Type-aware parsing based on schema
	var schemaType string
	if schema.Type != nil && len(schema.Type.Slice()) > 0 {
		schemaType = schema.Type.Slice()[0]
	}

	switch schemaType {
	case "string":
		// Ensure string type
		if strVal, ok := parsed.(string); ok {
			return strVal, nil
		}
		return fmt.Sprintf("%v", parsed), nil

	case "integer":
		// Try to convert to integer
		switch v := parsed.(type) {
		case int64:
			return v, nil
		case float64:
			return int64(v), nil
		case string:
			intVal, err := strconv.ParseInt(v, 10, 64)
			if err != nil {
				return nil, fmt.Errorf("cannot convert '%s' to integer: %w", v, err)
			}
			return intVal, nil
		default:
			return nil, fmt.Errorf("cannot convert %T to integer", v)
		}

	case "number":
		// Ensure number type
		switch v := parsed.(type) {
		case float64:
			return v, nil
		case int64:
			return float64(v), nil
		case string:
			floatVal, err := strconv.ParseFloat(v, 64)
			if err != nil {
				return nil, fmt.Errorf("cannot convert '%s' to number: %w", v, err)
			}
			return floatVal, nil
		default:
			return nil, fmt.Errorf("cannot convert %T to number", v)
		}

	case "boolean":
		// Try to convert to boolean
		switch v := parsed.(type) {
		case bool:
			return v, nil
		case string:
			boolVal, err := strconv.ParseBool(v)
			if err != nil {
				return nil, fmt.Errorf("cannot convert '%s' to boolean: %w", v, err)
			}
			return boolVal, nil
		default:
			return nil, fmt.Errorf("cannot convert %T to boolean", v)
		}

	case "object":
		// Ensure object type (map)
		if _, ok := parsed.(map[string]any); ok {
			return parsed, nil
		}
		return nil, fmt.Errorf("expected object type, got %T", parsed)

	case "array":
		// Ensure array type
		if _, ok := parsed.([]any); ok {
			return parsed, nil
		}
		return nil, fmt.Errorf("expected array type, got %T", parsed)
	default:
		// Unknown schema type, return parsed value as-is
		return parsed, nil
	}
}

// validateBodyParam checks if a body parameter is defined in the operation's request body schema.
// Returns error if parameter is not found in the schema.
func validateBodyParam(opSpec *openapi3.Operation, paramName string) error {
	if opSpec == nil {
		return nil
	}

	// If no request body is defined, allow any body parameters
	if opSpec.RequestBody == nil || opSpec.RequestBody.Value == nil {
		return nil
	}

	rb := opSpec.RequestBody.Value
	if rb.Content == nil {
		return nil
	}

	// Check for JSON content schema
	jsonContent, ok := rb.Content["application/json"]
	if !ok {
		return nil
	}

	if jsonContent.Schema == nil || jsonContent.Schema.Value == nil {
		return nil
	}

	schema := jsonContent.Schema.Value

	// Extract top-level property name (before any dots for nested paths)
	topLevelKey := paramName
	if before, _, ok0 := strings.Cut(paramName, "."); ok0 {
		topLevelKey = before
	}

	// Check if the property exists in the schema
	if schema.Properties != nil {
		if _, exists := schema.Properties[topLevelKey]; exists {
			return nil
		}
	}

	// If required properties are defined, check if parameter is there
	if len(schema.Properties) > 0 {
		// Property not found in schema
		var validProps []string
		for prop := range schema.Properties {
			validProps = append(validProps, prop)
		}
		return fmt.Errorf("body parameter '%s' is not defined in the request schema. Valid parameters: %v", paramName, validProps)
	}

	// No schema properties defined, allow any parameters
	return nil
}

// convertBodyParamToSchemaType converts a parsed value to match the schema type of the body parameter.
// If the schema defines the parameter as a string, the value will be converted to string.
// If the schema defines it as an integer or other type, it will be converted accordingly.
func convertBodyParamToSchemaType(opSpec *openapi3.Operation, paramName string, parsed any) any {
	schemaValue := getBodyParamSchema(opSpec, paramName)
	if schemaValue == nil {
		return parsed
	}

	return convertToSchemaType(schemaValue, parsed)
}

// getBodyParamSchema retrieves the schema for a body parameter
func getBodyParamSchema(opSpec *openapi3.Operation, paramName string) *openapi3.Schema {
	if opSpec == nil || opSpec.RequestBody == nil || opSpec.RequestBody.Value == nil {
		return nil
	}

	rb := opSpec.RequestBody.Value
	if rb.Content == nil {
		return nil
	}

	jsonContent, ok := rb.Content["application/json"]
	if !ok || jsonContent.Schema == nil || jsonContent.Schema.Value == nil {
		return nil
	}

	schema := jsonContent.Schema.Value
	if schema.Properties == nil {
		return nil
	}

	topLevelKey := paramName
	if before, _, ok0 := strings.Cut(paramName, "."); ok0 {
		topLevelKey = before
	}

	propSchema, ok := schema.Properties[topLevelKey]
	if !ok || propSchema == nil || propSchema.Value == nil {
		return nil
	}

	return propSchema.Value
}

// convertToSchemaType converts a value to match the schema type
func convertToSchemaType(schemaValue *openapi3.Schema, parsed any) any {
	if schemaValue.Type == nil {
		return parsed
	}

	switch {
	case schemaValue.Type.Is("string"):
		return convertToString(parsed)
	case schemaValue.Type.Is("integer"):
		return convertToInteger(parsed)
	case schemaValue.Type.Is("number"):
		return convertToNumber(parsed)
	case schemaValue.Type.Is("boolean"):
		return convertToBoolean(parsed)
	default:
		return parsed
	}
}

func convertToString(parsed any) any {
	return fmt.Sprintf("%v", parsed)
}

func convertToInteger(parsed any) any {
	if i, ok := parsed.(float64); ok {
		return int64(i)
	}
	if i, ok := parsed.(int64); ok {
		return i
	}
	if s, ok := parsed.(string); ok {
		if v, err := strconv.ParseInt(s, 10, 64); err == nil {
			return v
		}
	}
	return parsed
}

func convertToNumber(parsed any) any {
	if f, ok := parsed.(float64); ok {
		return f
	}
	if i, ok := parsed.(int64); ok {
		return float64(i)
	}
	if s, ok := parsed.(string); ok {
		if v, err := strconv.ParseFloat(s, 64); err == nil {
			return v
		}
	}
	return parsed
}

func convertToBoolean(parsed any) any {
	if b, ok := parsed.(bool); ok {
		return b
	}
	if s, ok := parsed.(string); ok {
		s = strings.ToLower(s)
		if s == "true" || s == "1" || s == "yes" {
			return true
		}
		if s == "false" || s == "0" || s == "no" {
			return false
		}
	}
	return parsed
}

// getParamFromSpec retrieves a parameter definition from operation spec by name and location.
// Returns (param, found) tuple.
func getParamFromSpec(opSpec *openapi3.Operation, name string, in string) (*openapi3.Parameter, bool) {
	if opSpec == nil || opSpec.Parameters == nil {
		return nil, false
	}

	for _, paramRef := range opSpec.Parameters {
		if paramRef == nil || paramRef.Value == nil {
			continue
		}
		param := paramRef.Value
		if param.Name == name && param.In == in {
			return param, true
		}
	}
	return nil, false
}

// isPathAbsolute detects if a path string is absolute (works cross-platform).
// Recognizes Unix paths (/...) and Windows paths (C:\..., c:/, etc.)
func isPathAbsolute(path string) bool {
	return filepath.IsAbs(path)
}

// resolveFilePath converts a path string to absolute path, handling cross-platform path formats.
// Returns absolute path or error if file doesn't exist or can't be read.
func resolveFilePath(path string) (string, error) {
	var absPath string
	var err error

	if isPathAbsolute(path) {
		absPath = path
	} else {
		// Relative path - resolve from current working directory
		cwd, err := os.Getwd()
		if err != nil {
			return "", fmt.Errorf("failed to get current working directory: %w", err)
		}
		absPath = filepath.Join(cwd, path)
	}

	// Clean the path
	absPath = filepath.Clean(absPath)

	// Check if file exists
	_, err = os.Stat(absPath)
	if err != nil {
		if os.IsNotExist(err) {
			return "", fmt.Errorf("file not found: %s", absPath)
		}
		return "", fmt.Errorf("failed to access file: %w", err)
	}

	return absPath, nil
}

// readFileContent reads file content, returns string for .txt and raw formats, parses JSON/YAML for structured formats.
func readFileContent(filePath string) (any, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read file: %w", err)
	}

	// Determine file format by extension
	ext := strings.ToLower(filepath.Ext(filePath))

	switch ext {
	case ".json":
		var result any
		if err := json.Unmarshal(data, &result); err != nil {
			return nil, fmt.Errorf("failed to parse JSON: %w", err)
		}
		return result, nil
	case ".yaml", ".yml":
		var result any
		if err := yaml.Unmarshal(data, &result); err != nil {
			return nil, fmt.Errorf("failed to parse YAML: %w", err)
		}
		return result, nil
	default:
		// Return as string for other formats
		return string(data), nil
	}
}

// setNestedValue sets a value in a nested map using dot-notation paths.
// Creates intermediate maps as needed.
// Example: setNestedValue(obj, "user.address.zip", 12345)
func setNestedValue(obj map[string]any, path string, value any) error {
	parts := strings.Split(path, ".")
	if len(parts) == 0 {
		return fmt.Errorf("invalid path: empty")
	}

	// Navigate/create nested structure
	current := obj
	for i := 0; i < len(parts)-1; i++ {
		key := parts[i]
		if key == "" {
			return fmt.Errorf("invalid path: empty key component")
		}

		if _, exists := current[key]; !exists {
			// Create intermediate map
			current[key] = make(map[string]any)
		}

		// Move to next level
		next, ok := current[key].(map[string]any)
		if !ok {
			// Type conflict: trying to nest into non-map value
			return fmt.Errorf("conflicting types at path '%s': expected map, got %T", strings.Join(parts[:i+1], "."), current[key])
		}
		current = next
	}

	// Set final value
	finalKey := parts[len(parts)-1]
	if finalKey == "" {
		return fmt.Errorf("invalid path: empty final key")
	}
	current[finalKey] = value
	return nil
}
