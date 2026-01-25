// Package request provides HTTP request building from OpenAPI operations.
package request

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"slices"
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
	fullURL := b.buildFullURL(baseURL, finalPath, queryString)

	// Build request body for non-GET methods
	bodyReader, err := b.createBodyReader(method, params, opParams, requestBody)
	if err != nil {
		return nil, fmt.Errorf("failed to build request body: %w", err)
	}

	// Create request
	req, err := b.createHTTPRequest(method, fullURL, bodyReader)
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

// buildFullURL constructs the full URL from base URL, path, and query string.
func (b *Builder) buildFullURL(baseURL, path, queryString string) string {
	fullURL := baseURL + path
	if queryString != "" {
		fullURL += "?" + queryString
	}
	return fullURL
}

// createBodyReader creates a body reader for the request.
func (b *Builder) createBodyReader(method string, params map[string]any, opParams openapi3.Parameters, requestBody *openapi3.RequestBody) (*bytes.Reader, error) {
	if method == http.MethodGet || method == http.MethodHead {
		return nil, nil
	}

	bodyData, err := b.buildRequestBody(params, opParams, requestBody)
	if err != nil {
		return nil, err
	}
	if bodyData == nil {
		return nil, nil
	}

	reader := bytes.NewReader(bodyData)
	return reader, nil
}

// createHTTPRequest creates an HTTP request with or without a body.
func (b *Builder) createHTTPRequest(method, urlStr string, bodyReader *bytes.Reader) (*http.Request, error) {
	if bodyReader != nil {
		return http.NewRequest(method, urlStr, bodyReader)
	}
	return http.NewRequest(method, urlStr, nil)
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

	return b.injectAuthCredentials(req, authConfig, cred)
}

// injectAuthCredentials injects the actual credentials based on auth type.
func (b *Builder) injectAuthCredentials(req *http.Request, authConfig *config.AuthConfig, cred *credential.Credential) error {
	switch authConfig.Type {
	case "bearer":
		return injectBearerToken(req, cred.Token)
	case "api_key":
		return injectAPIKey(req, authConfig, cred.Token)
	case "basic":
		return injectBasicAuth(req, cred.Username, cred.Password)
	default:
		return nil
	}
}

// injectBearerToken adds Bearer token to Authorization header.
func injectBearerToken(req *http.Request, token string) error {
	req.Header.Set("Authorization", "Bearer "+token)
	return nil
}

// injectAPIKey adds API key to header or query parameter.
func injectAPIKey(req *http.Request, authConfig *config.AuthConfig, token string) error {
	if authConfig.Location == "query" {
		q := req.URL.Query()
		q.Set(authConfig.KeyName, token)
		req.URL.RawQuery = q.Encode()
	} else {
		req.Header.Set(authConfig.KeyName, token)
	}
	return nil
}

// injectBasicAuth adds Basic authentication to the request.
func injectBasicAuth(req *http.Request, username, password string) error {
	req.SetBasicAuth(username, password)
	return nil
}

// substitutePathParams replaces path parameters with actual values.
func (b *Builder) substitutePathParams(path string, params map[string]any, opParams openapi3.Parameters) string {
	result, _ := url.PathUnescape(path)
	for _, paramRef := range opParams {
		if paramRef == nil || paramRef.Value == nil {
			continue
		}
		param := paramRef.Value
		if param.In != "path" {
			continue
		}
		if val, ok := params[param.Name]; ok {
			placeholder := "{" + param.Name + "}"
			// Decode URL-encoded values
			paramValue := fmt.Sprintf("%v", val)
			if decoded, err := url.QueryUnescape(paramValue); err == nil {
				paramValue = decoded
			}
			result = strings.Replace(result, placeholder, paramValue, 1)
		}
	}
	return result
}

// buildQueryString builds a query string from parameters.
func (b *Builder) buildQueryString(params map[string]any, opParams openapi3.Parameters) string {
	values := url.Values{}
	for _, paramRef := range opParams {
		if paramRef == nil || paramRef.Value == nil {
			continue
		}
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
		if paramRef == nil || paramRef.Value == nil {
			continue
		}
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
		if after, ok := strings.CutPrefix(v, "@"); ok {
			filename := after
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
		if paramRef == nil || paramRef.Value == nil {
			continue
		}
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
	if schema == nil || schema.Type == nil {
		return params
	}

	switch {
	case schema.Type.Is("object"):
		return b.constructObject(params, schema)
	case schema.Type.Is("array"):
		return b.constructArray(params, schema)
	default:
		return params
	}
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
// parseCommaSeparatedValues parses comma-separated string into array.
func parseCommaSeparatedValues(v string) []any {
	parts := strings.Split(v, ",")
	result := make([]any, len(parts))
	for i, part := range parts {
		result[i] = strings.TrimSpace(part)
	}
	return result
}

// convertArrayWithSchema converts array items according to schema.
func (b *Builder) convertArrayWithSchema(rawItems []any, schema *openapi3.Schema) []any {
	if schema == nil || schema.Items == nil || schema.Items.Value == nil {
		return rawItems
	}
	itemSchema := schema.Items.Value
	result := make([]any, len(rawItems))
	for i, item := range rawItems {
		result[i] = b.convertToSchemaType(item, itemSchema)
	}
	return result
}

// constructArrayValue converts a value to an array.
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
		if strings.Contains(v, ",") {
			rawItems = parseCommaSeparatedValues(v)
		} else {
			rawItems = []any{v}
		}
	default:
		rawItems = []any{val}
	}

	return b.convertArrayWithSchema(rawItems, schema)
}

// convertToSchemaType converts a value to match the schema type.
func (b *Builder) convertToSchemaType(val any, schema *openapi3.Schema) any {
	if schema == nil || schema.Type == nil {
		return val
	}

	// Handle non-string values
	if !isStringValue(val) {
		return handleNonStringValue(val, schema)
	}

	// Convert string values based on schema type
	return convertStringValue(val.(string), schema, b)
}

// isStringValue checks if the value is a string.
func isStringValue(val any) bool {
	_, ok := val.(string)
	return ok
}

// handleNonStringValue handles non-string values.
func handleNonStringValue(val any, schema *openapi3.Schema) any {
	if schema.Type.Is("array") {
		return val
	}
	return val
}

// convertStringValue converts a string value according to schema type.
func convertStringValue(strVal string, schema *openapi3.Schema, builder *Builder) any {
	switch {
	case schema.Type.Is("integer"):
		return convertToInt(strVal)
	case schema.Type.Is("number"):
		return convertToFloat(strVal)
	case schema.Type.Is("boolean"):
		return convertToBool(strVal)
	case schema.Type.Is("object"):
		return convertToObject(strVal)
	case schema.Type.Is("array"):
		return builder.constructArrayValue(strVal, schema)
	default:
		return strVal
	}
}

// convertToInt converts a string to integer.
func convertToInt(strVal string) any {
	v, err := strconv.Atoi(strVal)
	if err == nil {
		return v
	}
	return strVal
}

// convertToFloat converts a string to float.
func convertToFloat(strVal string) any {
	v, err := strconv.ParseFloat(strVal, 64)
	if err == nil {
		return v
	}
	return strVal
}

// convertToBool converts a string to boolean.
func convertToBool(strVal string) any {
	v, err := strconv.ParseBool(strVal)
	if err == nil {
		return v
	}
	return strVal
}

// convertToObject converts a string to object/map.
func convertToObject(strVal string) any {
	var m map[string]any
	if err := json.Unmarshal([]byte(strVal), &m); err == nil {
		return m
	}
	return strVal
}

// checkRequiredParams validates that all required parameters are present.
func (b *Builder) checkRequiredParams(params map[string]any, opParams openapi3.Parameters) error {
	for _, paramRef := range opParams {
		if paramRef == nil || paramRef.Value == nil {
			continue
		}
		param := paramRef.Value

		val, exists := params[param.Name]
		if !exists {
			if param.Required {
				return fmt.Errorf("required parameter '%s' is missing", param.Name)
			}
			continue
		}

		if param.Schema != nil && param.Schema.Value != nil {
			convertedVal := b.convertToSchemaType(val, param.Schema.Value)
			params[param.Name] = convertedVal
			val = convertedVal
		}

		if err := b.validateParameterType(param.Name, val, param.Schema); err != nil {
			return err
		}
	}
	return nil
}

// hasRequiredBody checks if required request body is present.
func (b *Builder) hasRequiredBody(params map[string]any, opParams openapi3.Parameters, requestBody *openapi3.RequestBody) bool {
	if requestBody == nil || !requestBody.Required {
		return true
	}

	if _, ok := params["body"]; ok {
		return true
	}

	bodyParams := b.extractBodyParams(params, opParams)
	return len(bodyParams) > 0
}

// checkRequiredBodyFields validates that all required body fields are present.
func (b *Builder) checkRequiredBodyFields(params map[string]any, opParams openapi3.Parameters, requestBody *openapi3.RequestBody) error {
	if requestBody == nil || requestBody.Content == nil {
		return nil
	}

	// Get JSON content schema
	jsonContent, ok := requestBody.Content["application/json"]
	if !ok || jsonContent.Schema == nil || jsonContent.Schema.Value == nil {
		return nil
	}

	schema := jsonContent.Schema.Value
	if schema.Properties == nil || len(schema.Required) == 0 {
		return nil
	}

	// Extract body parameters
	bodyParams := b.extractBodyParams(params, opParams)

	// Check if all required fields are present
	for _, requiredField := range schema.Required {
		if _, exists := bodyParams[requiredField]; !exists {
			return fmt.Errorf("required body parameter '%s' is missing", requiredField)
		}
	}

	return nil
}

// ValidateParams validates parameters against the operation schema.
func (b *Builder) ValidateParams(params map[string]any, opParams openapi3.Parameters, requestBody *openapi3.RequestBody) error {
	if err := b.checkRequiredParams(params, opParams); err != nil {
		return err
	}

	if !b.hasRequiredBody(params, opParams, requestBody) {
		return fmt.Errorf("request body is required for this operation")
	}

	return b.checkRequiredBodyFields(params, opParams, requestBody)
}

// typeMatchesAny checks if the actual type matches any of the expected schema types.
// This implements OpenAPI 3.1 union type semantics where a value is valid if it
// matches ANY type in the type array.
func typeMatchesAny(actualType string, expectedTypes []string) bool {
	for _, expectedType := range expectedTypes {
		if typeMatches(actualType, expectedType) {
			return true
		}
	}
	return false
}

// typeMatches checks if the actual type matches the expected schema type.
func typeMatches(actualType, expectedType string) bool {
	switch expectedType {
	case "string":
		return actualType == "string"
	case "integer", "number":
		// Allow both integer and number for numeric schemas
		// (numbers without decimals can match integer schema)
		return actualType == "integer" || actualType == "number"
	case "boolean":
		return actualType == "boolean"
	case "array":
		return actualType == "array"
	case "object":
		return actualType == "object"
	default:
		// Unknown type - allow it (permissive for forward compatibility)
		return true
	}
}

// validateEnumValue validates that the value is one of the allowed enum values.
func validateEnumValue(name string, value any, enums []any) error {
	if slices.Contains(enums, value) {
		return nil
	}
	return fmt.Errorf("parameter '%s' must be one of %v, got %v", name, enums, value)
}

// validateParameterType validates a parameter value against its schema type.
// For OpenAPI 3.1 union types (e.g., ["string", "integer"]), the value is valid
// if it matches ANY of the types in the array.
func (b *Builder) validateParameterType(name string, value any, schemaRef *openapi3.SchemaRef) error {
	if schemaRef == nil || schemaRef.Value == nil {
		return nil
	}

	schema := schemaRef.Value
	if schema.Type == nil {
		return nil
	}

	actualType := b.getValueType(value)
	schemaTypes := schema.Type.Slice()

	// For union types, value is valid if it matches ANY type in the array
	if !typeMatchesAny(actualType, schemaTypes) {
		return fmt.Errorf("parameter '%s' type mismatch: got %s, expected one of %v",
			name, actualType, schemaTypes)
	}

	// Validate enum values if present
	if len(schema.Enum) > 0 {
		return validateEnumValue(name, value, schema.Enum)
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
