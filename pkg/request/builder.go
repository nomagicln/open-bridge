// Package request provides HTTP request building from OpenAPI operations.
package request

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/nomagicln/open-bridge/pkg/config"
	"github.com/nomagicln/open-bridge/pkg/credential"
)

// Builder constructs HTTP requests from OpenAPI operations and parameters.
type Builder struct {
	credMgr *credential.Manager
}

// NewBuilder creates a new request builder.
func NewBuilder(credMgr *credential.Manager) *Builder {
	return &Builder{
		credMgr: credMgr,
	}
}

// BuildRequest constructs an HTTP request from an operation and parameters.
func (b *Builder) BuildRequest(
	method, path, baseURL string,
	params map[string]any,
	opParams openapi3.Parameters,
	requestBody *openapi3.RequestBody,
) (*http.Request, error) {
	// Substitute path parameters
	finalPath := b.substitutePathParams(path, params, opParams)

	// Build query string
	queryString := b.buildQueryString(params, opParams)

	// Construct full URL
	fullURL := baseURL + finalPath
	if queryString != "" {
		fullURL += "?" + queryString
	}

	// Build request body for non-GET methods
	var bodyReader *bytes.Reader
	if method != http.MethodGet && method != http.MethodHead {
		bodyData, err := b.buildRequestBody(params, opParams, requestBody)
		if err != nil {
			return nil, err
		}
		if bodyData != nil {
			bodyReader = bytes.NewReader(bodyData)
		}
	}

	// Create request
	var req *http.Request
	var err error
	if bodyReader != nil {
		req, err = http.NewRequest(method, fullURL, bodyReader)
	} else {
		req, err = http.NewRequest(method, fullURL, nil)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// Set content type if we have a body
	if bodyReader != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	// Add header parameters
	b.addHeaderParams(req, params, opParams)

	return req, nil
}

// InjectAuth adds authentication to a request based on configuration.
func (b *Builder) InjectAuth(req *http.Request, appName, profileName string, authConfig *config.AuthConfig) error {
	if b.credMgr == nil {
		return nil
	}

	cred, err := b.credMgr.GetCredential(appName, profileName)
	if err != nil {
		return nil // No credential, skip auth
	}

	switch authConfig.Type {
	case "bearer":
		req.Header.Set("Authorization", "Bearer "+cred.Token)
	case "api_key":
		switch authConfig.Location {
		case "header":
			req.Header.Set(authConfig.KeyName, cred.Token)
		case "query":
			q := req.URL.Query()
			q.Set(authConfig.KeyName, cred.Token)
			req.URL.RawQuery = q.Encode()
		}
	case "basic":
		req.SetBasicAuth(cred.Username, cred.Password)
	}

	return nil
}

// substitutePathParams replaces path parameters with actual values.
func (b *Builder) substitutePathParams(path string, params map[string]any, opParams openapi3.Parameters) string {
	result := path
	for _, paramRef := range opParams {
		param := paramRef.Value
		if param.In != "path" {
			continue
		}
		if val, ok := params[param.Name]; ok {
			placeholder := "{" + param.Name + "}"
			result = strings.Replace(result, placeholder, fmt.Sprintf("%v", val), 1)
		}
	}
	return result
}

// buildQueryString builds a query string from parameters.
func (b *Builder) buildQueryString(params map[string]any, opParams openapi3.Parameters) string {
	values := url.Values{}
	for _, paramRef := range opParams {
		param := paramRef.Value
		if param.In != "query" {
			continue
		}
		if val, ok := params[param.Name]; ok {
			values.Add(param.Name, fmt.Sprintf("%v", val))
		}
	}
	return values.Encode()
}

// addHeaderParams adds header parameters to the request.
func (b *Builder) addHeaderParams(req *http.Request, params map[string]any, opParams openapi3.Parameters) {
	for _, paramRef := range opParams {
		param := paramRef.Value
		if param.In != "header" {
			continue
		}
		if val, ok := params[param.Name]; ok {
			req.Header.Set(param.Name, fmt.Sprintf("%v", val))
		}
	}
}

// buildRequestBody builds the request body from parameters.
func (b *Builder) buildRequestBody(params map[string]any, opParams openapi3.Parameters, requestBody *openapi3.RequestBody) ([]byte, error) {
	// Check for direct body input via --body flag
	if body, ok := params["body"]; ok {
		return b.handleBodyFlag(body)
	}

	// Filter out path, query, and header params - remaining are body params
	bodyParams := b.extractBodyParams(params, opParams)

	// If we have a request body schema, construct body from schema
	if requestBody != nil && requestBody.Content != nil {
		if jsonContent, ok := requestBody.Content["application/json"]; ok && jsonContent.Schema != nil {
			return b.buildBodyFromSchema(bodyParams, jsonContent.Schema.Value)
		}
	}

	// Fallback: marshal all body params as-is
	if len(bodyParams) == 0 {
		return nil, nil
	}

	return json.Marshal(bodyParams)
}

// handleBodyFlag processes the --body flag value.
// Supports:
// - Direct JSON: --body '{"key":"value"}'
// - File input: --body @file.json
func (b *Builder) handleBodyFlag(body any) ([]byte, error) {
	switch v := body.(type) {
	case string:
		// Check for file input: @filename
		if strings.HasPrefix(v, "@") {
			filename := strings.TrimPrefix(v, "@")
			return b.readBodyFromFile(filename)
		}
		// Check if it's a JSON string
		if strings.HasPrefix(strings.TrimSpace(v), "{") || strings.HasPrefix(strings.TrimSpace(v), "[") {
			// Validate JSON
			var temp any
			if err := json.Unmarshal([]byte(v), &temp); err != nil {
				return nil, fmt.Errorf("invalid JSON in --body flag: %w", err)
			}
			return []byte(v), nil
		}
		return []byte(v), nil
	case map[string]any:
		return json.Marshal(v)
	default:
		return json.Marshal(v)
	}
}

// readBodyFromFile reads request body from a file.
func (b *Builder) readBodyFromFile(filename string) ([]byte, error) {
	data, err := os.ReadFile(filename)
	if err != nil {
		return nil, fmt.Errorf("failed to read body from file %s: %w", filename, err)
	}

	// Validate that it's valid JSON
	var temp any
	if err := json.Unmarshal(data, &temp); err != nil {
		return nil, fmt.Errorf("file %s does not contain valid JSON: %w", filename, err)
	}

	return data, nil
}

// extractBodyParams filters out path, query, and header params.
func (b *Builder) extractBodyParams(params map[string]any, opParams openapi3.Parameters) map[string]any {
	bodyParams := make(map[string]any)

	paramNames := make(map[string]bool)
	for _, paramRef := range opParams {
		paramNames[paramRef.Value.Name] = true
	}

	for key, val := range params {
		// Skip known parameter names and special flags
		if !paramNames[key] && key != "body" && key != "output" && key != "json" && key != "yaml" {
			bodyParams[key] = val
		}
	}

	return bodyParams
}

// buildBodyFromSchema constructs request body based on OpenAPI schema.
// Handles nested objects and arrays according to schema definition.
func (b *Builder) buildBodyFromSchema(params map[string]any, schema *openapi3.Schema) ([]byte, error) {
	if schema == nil {
		return json.Marshal(params)
	}

	// Build body according to schema type
	body := b.constructFromSchema(params, schema)

	return json.Marshal(body)
}

// constructFromSchema recursively constructs data structure from schema.
func (b *Builder) constructFromSchema(params map[string]any, schema *openapi3.Schema) any {
	if schema == nil {
		return params
	}

	if schema.Type == nil {
		return params
	}

	if schema.Type.Is("object") {
		return b.constructObject(params, schema)
	} else if schema.Type.Is("array") {
		// For array type at top level, params might be the array itself
		return b.constructArray(params, schema)
	}

	// For primitive types, return params as-is
	return params
}

// constructObject constructs an object from parameters according to schema.
func (b *Builder) constructObject(params map[string]any, schema *openapi3.Schema) map[string]any {
	result := make(map[string]any)

	// Process each property defined in schema
	for propName, propSchemaRef := range schema.Properties {
		if propSchemaRef == nil || propSchemaRef.Value == nil {
			continue
		}

		propSchema := propSchemaRef.Value

		// Check if we have a value for this property
		if val, ok := params[propName]; ok {
			result[propName] = b.convertToSchemaType(val, propSchema)
		}
	}

	// Include any additional properties not in schema
	for key, val := range params {
		if _, exists := result[key]; !exists {
			result[key] = val
		}
	}

	return result
}

// constructArray constructs an array from parameters according to schema.
// This is called when the top-level schema is an array type.
func (b *Builder) constructArray(params any, schema *openapi3.Schema) []any {
	return b.constructArrayValue(params, schema)
}

// constructArrayValue handles array values from flags.
// Supports:
// - Repeated flags: --tags "admin" --tags "developer"
// - Comma-separated: --tags "admin,developer"
// - Array values: ["admin", "developer"]
func (b *Builder) constructArrayValue(val any, schema *openapi3.Schema) []any {
	var rawItems []any
	switch v := val.(type) {
	case []any:
		rawItems = v
	case []string:
		rawItems = make([]any, len(v))
		for i, s := range v {
			rawItems[i] = s
		}
	case string:
		// Check for comma-separated values
		if strings.Contains(v, ",") {
			parts := strings.Split(v, ",")
			rawItems = make([]any, len(parts))
			for i, part := range parts {
				rawItems[i] = strings.TrimSpace(part)
			}
		} else {
			// Single value
			rawItems = []any{v}
		}
	default:
		// Wrap in array
		rawItems = []any{val}
	}

	// If we have a schema for items, convert each item
	if schema != nil && schema.Items != nil && schema.Items.Value != nil {
		itemSchema := schema.Items.Value
		result := make([]any, len(rawItems))
		for i, item := range rawItems {
			result[i] = b.convertToSchemaType(item, itemSchema)
		}
		return result
	}

	return rawItems
}

// convertToSchemaType converts a value to match the schema type.
func (b *Builder) convertToSchemaType(val any, schema *openapi3.Schema) any {
	if schema == nil || schema.Type == nil {
		return val
	}

	// We primarily handle string conversion here because CLI args are strings
	strVal, isString := val.(string)
	if !isString {
		// If it's not a string, we might still need handling for arrays that came in as string slices
		if schema.Type.Is("array") {
			// If it's not already an array, try to convert
			// But for now, array inputs are handled by constructArrayValue or slices
			return val
		}
		// Return as is for other types, assuming they are already correct
		return val
	}

	if schema.Type.Is("integer") {
		if v, err := strconv.Atoi(strVal); err == nil {
			return v
		}
		// If conversions fail, return original value and let validation catch it
	} else if schema.Type.Is("number") {
		if v, err := strconv.ParseFloat(strVal, 64); err == nil {
			return v
		}
	} else if schema.Type.Is("boolean") {
		if v, err := strconv.ParseBool(strVal); err == nil {
			return v
		}
	} else if schema.Type.Is("object") {
		// Try to parse JSON string to map
		var m map[string]any
		if err := json.Unmarshal([]byte(strVal), &m); err == nil {
			return m
		}
	} else if schema.Type.Is("array") {
		// This case might not be hit if flags are already parsed as slices,
		// but if a single string was passed to an array type (e.g. env var or single flag)
		// we treat it as a single-item array or comma-separated
		return b.constructArrayValue(strVal, schema)
	}

	return val
}

// ValidateParams validates parameters against the operation schema.
func (b *Builder) ValidateParams(params map[string]any, opParams openapi3.Parameters, requestBody *openapi3.RequestBody) error {
	// Check required parameters
	// Check required parameters
	for _, paramRef := range opParams {
		param := paramRef.Value

		// Get parameter value
		val, exists := params[param.Name]
		if !exists {
			if param.Required {
				return fmt.Errorf("required parameter '%s' is missing", param.Name)
			}
			continue
		}

		// Convert value if needed
		if param.Schema != nil && param.Schema.Value != nil {
			convertedVal := b.convertToSchemaType(val, param.Schema.Value)
			params[param.Name] = convertedVal // Update with converted value
			val = convertedVal
		}

		// Validate parameter type
		if err := b.validateParameterType(param.Name, val, param.Schema); err != nil {
			return err
		}
	}

	// Validate request body required properties if present
	if requestBody != nil && requestBody.Required {
		// Check if we have body content
		hasBody := false
		if _, ok := params["body"]; ok {
			hasBody = true
		} else {
			// Check if we have any body parameters (non-path/query/header params)
			bodyParams := b.extractBodyParams(params, opParams)
			hasBody = len(bodyParams) > 0
		}

		if !hasBody {
			return fmt.Errorf("request body is required for this operation")
		}
	}

	return nil
}

// validateParameterType validates a parameter value against its schema type.
func (b *Builder) validateParameterType(name string, value any, schemaRef *openapi3.SchemaRef) error {
	if schemaRef == nil || schemaRef.Value == nil {
		return nil // No schema to validate against
	}

	schema := schemaRef.Value
	if schema.Type == nil {
		return nil // No type constraint
	}

	// Get the actual type of the value
	actualType := b.getValueType(value)

	// Check if the value type matches the schema type
	if schema.Type.Is("string") {
		if actualType != "string" {
			return fmt.Errorf("parameter '%s' must be a string, got %s", name, actualType)
		}
	} else if schema.Type.Is("integer") {
		if actualType != "integer" && actualType != "number" {
			return fmt.Errorf("parameter '%s' must be an integer, got %s", name, actualType)
		}
	} else if schema.Type.Is("number") {
		if actualType != "integer" && actualType != "number" {
			return fmt.Errorf("parameter '%s' must be a number, got %s", name, actualType)
		}
	} else if schema.Type.Is("boolean") {
		if actualType != "boolean" {
			return fmt.Errorf("parameter '%s' must be a boolean, got %s", name, actualType)
		}
	} else if schema.Type.Is("array") {
		if actualType != "array" {
			return fmt.Errorf("parameter '%s' must be an array, got %s", name, actualType)
		}
	} else if schema.Type.Is("object") {
		if actualType != "object" {
			return fmt.Errorf("parameter '%s' must be an object, got %s", name, actualType)
		}
	}

	// Validate enum values if present
	if len(schema.Enum) > 0 {
		found := false
		for _, enumVal := range schema.Enum {
			if value == enumVal {
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("parameter '%s' must be one of %v, got %v", name, schema.Enum, value)
		}
	}

	return nil
}

// getValueType returns the type name of a value.
func (b *Builder) getValueType(value any) string {
	switch v := value.(type) {
	case string:
		return "string"
	case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64:
		return "integer"
	case float32, float64:
		return "number"
	case bool:
		return "boolean"
	case []any, []string, []int, []float64:
		return "array"
	case map[string]any:
		return "object"
	default:
		return fmt.Sprintf("%T", v)
	}
}
