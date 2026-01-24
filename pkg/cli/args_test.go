package cli

import (
	"os"
	"path/filepath"
	"testing"
)

// TestSplitArgs_NoDelimiter tests splitting args without '--' delimiter
func TestSplitArgs_NoDelimiter(t *testing.T) {
	args := []string{"--name", "John", "--query:page=1"}
	cliArgs, requestArgs, hasDelimiter := SplitArgs(args)

	if len(cliArgs) != 3 || cliArgs[0] != "--name" {
		t.Errorf("expected cliArgs to be all args, got %v", cliArgs)
	}
	if requestArgs != nil {
		t.Errorf("expected requestArgs to be nil, got %v", requestArgs)
	}
	if hasDelimiter {
		t.Errorf("expected hasDelimiter to be false")
	}
}

// TestSplitArgs_WithDelimiter tests splitting args with '--' delimiter
func TestSplitArgs_WithDelimiter(t *testing.T) {
	args := []string{"--generate", "docs", "--", "--header:Auth=Bearer xxx", "--query:page=1"}
	cliArgs, requestArgs, hasDelimiter := SplitArgs(args)

	if len(cliArgs) != 2 || cliArgs[0] != "--generate" {
		t.Errorf("expected cliArgs to have 2 elements, got %d", len(cliArgs))
	}
	if len(requestArgs) != 2 || requestArgs[0] != "--header:Auth=Bearer xxx" {
		t.Errorf("expected requestArgs to have 2 elements, got %d", len(requestArgs))
	}
	if !hasDelimiter {
		t.Errorf("expected hasDelimiter to be true")
	}
}

// TestSplitArgs_MultipleDelimiters tests handling multiple '--' (only first one counts)
func TestSplitArgs_MultipleDelimiters(t *testing.T) {
	args := []string{"--cli", "--", "--req1", "--", "--req2"}
	cliArgs, requestArgs, hasDelimiter := SplitArgs(args)

	if len(cliArgs) != 1 || cliArgs[0] != "--cli" {
		t.Errorf("expected cliArgs to have 1 element, got %d", len(cliArgs))
	}
	if len(requestArgs) != 3 {
		t.Errorf("expected requestArgs to have 3 elements (including second --), got %d", len(requestArgs))
	}
	if !hasDelimiter {
		t.Errorf("expected hasDelimiter to be true")
	}
}

// TestSplitArgs_DelimiterAtStart tests '--' at the beginning
func TestSplitArgs_DelimiterAtStart(t *testing.T) {
	args := []string{"--", "--header:Auth=Bearer xxx"}
	cliArgs, requestArgs, hasDelimiter := SplitArgs(args)

	if len(cliArgs) != 0 {
		t.Errorf("expected cliArgs to be empty, got %d", len(cliArgs))
	}
	if len(requestArgs) != 1 {
		t.Errorf("expected requestArgs to have 1 element, got %d", len(requestArgs))
	}
	if !hasDelimiter {
		t.Errorf("expected hasDelimiter to be true")
	}
}

// TestSplitArgs_DelimiterAtEnd tests '--' at the end
func TestSplitArgs_DelimiterAtEnd(t *testing.T) {
	args := []string{"--cli", "arg", "--"}
	cliArgs, requestArgs, hasDelimiter := SplitArgs(args)

	if len(cliArgs) != 2 {
		t.Errorf("expected cliArgs to have 2 elements, got %d", len(cliArgs))
	}
	if len(requestArgs) > 0 {
		t.Errorf("expected requestArgs to be empty, got %d", len(requestArgs))
	}
	if !hasDelimiter {
		t.Errorf("expected hasDelimiter to be true")
	}
}

// TestSplitArgs_Empty tests empty args
func TestSplitArgs_Empty(t *testing.T) {
	args := []string{}
	cliArgs, requestArgs, hasDelimiter := SplitArgs(args)

	if len(cliArgs) != 0 {
		t.Errorf("expected cliArgs to be empty, got %d", len(cliArgs))
	}
	if requestArgs != nil {
		t.Errorf("expected requestArgs to be nil, got %v", requestArgs)
	}
	if hasDelimiter {
		t.Errorf("expected hasDelimiter to be false")
	}
}

// TestParseValue_QuotedString tests parsing quoted strings
func TestParseValue_QuotedString(t *testing.T) {
	val, err := ParseValue(`"hello world"`)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if val != "hello world" {
		t.Errorf("expected 'hello world', got %q", val)
	}
}

// TestParseValue_UnquotedString tests parsing unquoted strings
func TestParseValue_UnquotedString(t *testing.T) {
	val, err := ParseValue("plaintext")
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if val != "plaintext" {
		t.Errorf("expected 'plaintext', got %q", val)
	}
}

// TestParseValue_IntegerValue tests parsing integer values
func TestParseValue_IntegerValue(t *testing.T) {
	val, err := ParseValue("123")
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	intVal, ok := val.(int64)
	if !ok || intVal != 123 {
		t.Errorf("expected int64(123), got %T %v", val, val)
	}
}

// TestParseValue_BooleanValue tests parsing boolean values
func TestParseValue_BooleanValue(t *testing.T) {
	val, err := ParseValue("true")
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if val != true {
		t.Errorf("expected true, got %v", val)
	}
}

// TestParseValue_FileReference_Absolute tests file references with absolute paths
func TestParseValue_FileReference_Absolute(t *testing.T) {
	// Create a temporary file
	tmpfile, err := os.CreateTemp("", "test_*.txt")
	if err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}
	defer func() {
		_ = os.Remove(tmpfile.Name())
	}()

	_, _ = tmpfile.WriteString("test content")
	_ = tmpfile.Close()

	val, err := ParseValue("@" + tmpfile.Name())
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if val != "test content" {
		t.Errorf("expected 'test content', got %q", val)
	}
}

// TestParseValue_FileReference_Relative tests file references with relative paths
func TestParseValue_FileReference_Relative(t *testing.T) {
	// Create a temporary file in current directory
	tmpfile, err := os.CreateTemp(".", "test_*.txt")
	if err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}
	defer func() {
		_ = os.Remove(tmpfile.Name())
	}()

	_, _ = tmpfile.WriteString("relative content")
	_ = tmpfile.Close()

	filename := filepath.Base(tmpfile.Name())
	val, err := ParseValue("@" + filename)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if val != "relative content" {
		t.Errorf("expected 'relative content', got %q", val)
	}
}

// TestParseValue_FileReference_JSON tests parsing JSON files
// nolint:dupl // Test structure similar to YAML variant is intentional
func TestParseValue_FileReference_JSON(t *testing.T) {
	tmpfile, err := os.CreateTemp("", "test_*.json")
	if err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}
	defer func() {
		_ = os.Remove(tmpfile.Name())
	}()

	_, _ = tmpfile.WriteString(`{"key": "value"}`)
	_ = tmpfile.Close()

	val, err := ParseValue("@" + tmpfile.Name())
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	mapVal, ok := val.(map[string]any)
	if !ok {
		t.Errorf("expected map[string]interface{}, got %T", val)
	}
	if mapVal["key"] != "value" {
		t.Errorf("expected key='value', got %v", mapVal["key"])
	}
}

// TestParseValue_FileReference_YAML tests parsing YAML files
// nolint:dupl // Test structure similar to JSON variant is intentional
func TestParseValue_FileReference_YAML(t *testing.T) {
	tmpfile, err := os.CreateTemp("", "test_*.yaml")
	if err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}
	defer func() {
		_ = os.Remove(tmpfile.Name())
	}()

	_, _ = tmpfile.WriteString(`key: value`)
	_ = tmpfile.Close()

	val, err := ParseValue("@" + tmpfile.Name())
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	mapVal, ok := val.(map[string]any)
	if !ok {
		t.Errorf("expected map[string]interface{}, got %T", val)
	}
	if mapVal["key"] != "value" {
		t.Errorf("expected key='value', got %v", mapVal["key"])
	}
}

// TestParseValue_FileReference_NotFound tests error when file not found
func TestParseValue_FileReference_NotFound(t *testing.T) {
	val, err := ParseValue("@/nonexistent/file/path.json")
	if err == nil {
		t.Errorf("expected error for nonexistent file, got %v", val)
	}
}

// TestParseValue_InvalidJSON tests error when JSON is invalid
func TestParseValue_InvalidJSON(t *testing.T) {
	tmpfile, err := os.CreateTemp("", "test_*.json")
	if err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}
	defer func() {
		_ = os.Remove(tmpfile.Name())
	}()

	_, _ = tmpfile.WriteString(`{invalid json}`)
	_ = tmpfile.Close()

	_, err = ParseValue("@" + tmpfile.Name())
	if err == nil {
		t.Errorf("expected error for invalid JSON")
	}
}

// TestParseValue_QuotedStringWithEscapes tests quoted strings with escape sequences
func TestParseValue_QuotedStringWithEscapes(t *testing.T) {
	val, err := ParseValue(`"hello\nworld"`)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	expected := "hello\nworld"
	if val != expected {
		t.Errorf("expected %q, got %q", expected, val)
	}
}

// TestParseValue_JSONPath_Dollar tests JSONPath starting with $.
func TestParseValue_JSONPath_Dollar(t *testing.T) {
	val, err := ParseValue("$.foo.bar")
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	// Should be returned as string since it doesn't match JSON syntax
	if val != "$.foo.bar" {
		t.Errorf("expected '$.foo.bar', got %q", val)
	}
}

// TestSetNestedValue_SimpleKey tests setting a simple non-nested key
func TestSetNestedValue_SimpleKey(t *testing.T) {
	obj := make(map[string]any)
	err := setNestedValue(obj, "key", "value")
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if obj["key"] != "value" {
		t.Errorf("expected key='value', got %v", obj["key"])
	}
}

// TestSetNestedValue_DottedPath tests setting nested key via dot notation
func TestSetNestedValue_DottedPath(t *testing.T) {
	obj := make(map[string]any)
	err := setNestedValue(obj, "user.name", "John")
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	user, ok := obj["user"].(map[string]any)
	if !ok {
		t.Errorf("expected user to be map, got %T", obj["user"])
	}
	if user["name"] != "John" {
		t.Errorf("expected user.name='John', got %v", user["name"])
	}
}

// TestSetNestedValue_OverwriteExisting tests overwriting existing nested value
func TestSetNestedValue_OverwriteExisting(t *testing.T) {
	obj := make(map[string]any)
	_ = setNestedValue(obj, "user.name", "John")
	err := setNestedValue(obj, "user.name", "Jane")
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	user := obj["user"].(map[string]any)
	if user["name"] != "Jane" {
		t.Errorf("expected user.name='Jane', got %v", user["name"])
	}
}

// TestSetNestedValue_MultipleNested tests setting multiple nested values
func TestSetNestedValue_MultipleNested(t *testing.T) {
	obj := make(map[string]any)
	_ = setNestedValue(obj, "user.address.zip", 12345)
	_ = setNestedValue(obj, "user.address.street", "Main St")

	user := obj["user"].(map[string]any)
	addr := user["address"].(map[string]any)
	if addr["zip"] != 12345 || addr["street"] != "Main St" {
		t.Errorf("failed to set multiple nested values")
	}
}

// TestBuildNestedBody_FlatKeys tests building nested structure from flat keys
func TestBuildNestedBody_FlatKeys(t *testing.T) {
	flatParams := map[string]any{
		"name":  "John",
		"email": "john@example.com",
	}
	result, err := BuildNestedBody(flatParams)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if result["name"] != "John" || result["email"] != "john@example.com" {
		t.Errorf("failed to build flat body")
	}
}

// TestBuildNestedBody_DottedPath tests nested path with dots
func TestBuildNestedBody_DottedPath(t *testing.T) {
	flatParams := map[string]any{
		"user.name": "John",
	}
	result, err := BuildNestedBody(flatParams)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	user, ok := result["user"].(map[string]any)
	if !ok {
		t.Errorf("expected user to be map, got %T", result["user"])
	}
	if user["name"] != "John" {
		t.Errorf("expected user.name='John', got %v", user["name"])
	}
}

// TestBuildNestedBody_MultipleNesting tests multiple nested paths
func TestBuildNestedBody_MultipleNesting(t *testing.T) {
	flatParams := map[string]any{
		"user.name":  "John",
		"user.email": "john@example.com",
	}
	result, err := BuildNestedBody(flatParams)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	user := result["user"].(map[string]any)
	if user["name"] != "John" || user["email"] != "john@example.com" {
		t.Errorf("failed to build multiple nested values")
	}
}

// TestBuildNestedBody_DeepNesting tests deeply nested structures
func TestBuildNestedBody_DeepNesting(t *testing.T) {
	flatParams := map[string]any{
		"user.address.zip": 12345,
	}
	result, err := BuildNestedBody(flatParams)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	user := result["user"].(map[string]any)
	addr := user["address"].(map[string]any)
	if addr["zip"] != 12345 {
		t.Errorf("expected zip=12345, got %v", addr["zip"])
	}
}

// TestBuildNestedBody_ConflictingTypes tests error when paths create type conflicts
func TestBuildNestedBody_ConflictingTypes(t *testing.T) {
	flatParams := map[string]any{
		"user":      "string value",
		"user.name": "John",
	}
	_, err := BuildNestedBody(flatParams)
	if err == nil {
		t.Errorf("expected error for conflicting types")
	}
}

// TestParseRequestParams_EmptyArgs tests empty arguments
func TestParseRequestParams_EmptyArgs(t *testing.T) {
	result, err := ParseRequestParams([]string{}, nil, false)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if len(result.Body) != 0 || len(result.Headers) != 0 {
		t.Errorf("expected empty params")
	}
}
