package mcp

import (
	"fmt"
	"strings"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/nomagicln/open-bridge/pkg/semantic"
)

// GenerateToolName generates a clean, concise MCP tool name from an OpenAPI operation.
//
// The generated name follows the pattern: {resource}_{verb}
// This matches the CLI command structure: {app} {resource} {verb}
//
// Examples:
//   - list_todos_todos_get → todos_list
//   - create_todo_todos_post → todos_create
//   - get_todo_todos__todo_id__get → todos_get
//   - update_todo_todos__todo_id__put → todos_update
//   - batch_create_todos_todos_batch_post → todos_batch_create
func GenerateToolName(method, path string, op *openapi3.Operation) string {
	// If operationId is clean and doesn't have redundancy, use it directly
	if op.OperationID != "" && !hasRedundancy(op.OperationID) {
		return op.OperationID
	}

	// Try to extract a better name from operationId if it has FastAPI pattern
	if op.OperationID != "" && isFastAPIPattern(op.OperationID) {
		name := extractMeaningfulName(op.OperationID, path)
		if name != "" {
			return name
		}
	}

	// Use semantic mapper to extract resource and verb
	mapper := semantic.NewMapper()
	resource := mapper.ExtractResource(path, op)
	verb := mapper.MapVerb(method, path, op)

	// Build tool name: {resource}_{verb}
	return fmt.Sprintf("%s_%s", resource, verb)
}

// hasRedundancy checks if an operationId contains redundant information.
// It detects patterns like "list_todos_todos_get" where the resource name appears multiple times.
func hasRedundancy(operationID string) bool {
	// Convert to lowercase for case-insensitive matching
	lower := strings.ToLower(operationID)

	// Split by underscore
	parts := strings.Split(lower, "_")
	if len(parts) < 3 {
		return false
	}

	// Check for repeated resource names in different positions
	// Example: "list_todos_todos_get" has "todos" appearing twice
	seen := make(map[string]int)
	for _, part := range parts {
		// Skip common HTTP verbs and methods
		if isHTTPVerbOrMethod(part) {
			continue
		}
		// Skip single-letter parts or very short parts
		if len(part) <= 1 {
			continue
		}
		seen[part]++
		if seen[part] > 1 {
			return true
		}
	}

	// Check if it follows the FastAPI pattern: {function}_{path}_{method}
	// where function and path contain the same resource name
	if isFastAPIPattern(operationID) {
		return true
	}

	return false
}

// isFastAPIPattern detects if an operationId follows FastAPI's auto-generated pattern.
// FastAPI pattern: {function_name}_{path_segments}_{http_method}
// Examples:
//   - list_todos_todos_get
//   - create_todo_todos_post
//   - get_todo_todos__todo_id__get
func isFastAPIPattern(operationID string) bool {
	// FastAPI pattern typically ends with HTTP method
	lower := strings.ToLower(operationID)
	httpMethods := []string{"_get", "_post", "_put", "_patch", "_delete"}

	for _, method := range httpMethods {
		if strings.HasSuffix(lower, method) {
			return true
		}
	}

	return false
}

// isHTTPVerbOrMethod checks if a string is an HTTP verb or common REST method name.
func isHTTPVerbOrMethod(s string) bool {
	methods := map[string]bool{
		"get":     true,
		"post":    true,
		"put":     true,
		"patch":   true,
		"delete":  true,
		"head":    true,
		"options": true,
		"trace":   true,
		"id":      true, // Path parameters often use "id"
	}
	return methods[strings.ToLower(s)]
}

// extractMeaningfulName extracts a meaningful tool name from FastAPI-style operationId.
// It removes redundant path repetition and HTTP method suffix.
//
// FastAPI pattern: {function_name}_{path_parts}_{http_method}
// Examples:
//   - list_todos_todos_get → todos_list
//   - create_todo_todos_post → todos_create
//   - batch_create_todos_todos_batch_post → todos_batch_create
//   - get_stats_todos_stats_get → todos_stats
func extractMeaningfulName(operationID, path string) string {
	lower := removeHTTPMethodSuffix(strings.ToLower(operationID))

	pathParts := extractPathSegments(path)
	if len(pathParts) == 0 {
		return ""
	}
	mainResource := pathParts[0]
	matchedVerb := extractVerbFromOperationID(lower, mainResource)
	if matchedVerb == "" {
		return ""
	}

	return buildToolName(mainResource, matchedVerb, pathParts)
}

// removeHTTPMethodSuffix removes HTTP method suffix from operationId.
func removeHTTPMethodSuffix(operationID string) string {
	for _, method := range []string{"_get", "_post", "_put", "_patch", "_delete"} {
		if before, ok := strings.CutSuffix(operationID, method); ok {
			return before
		}
	}
	return operationID
}

// extractVerbFromOperationID extracts the verb/action from operationId.
func extractVerbFromOperationID(operationID, mainResource string) string {
	// Common verb prefixes in FastAPI operationIds
	verbPrefixes := []string{
		"batch_create", "batch_delete", "batch_update", // Compound verbs first
		"list", "get", "create", "update", "delete", "patch",
		"post", "put", "find", "search", "query",
	}

	for _, verb := range verbPrefixes {
		if strings.HasPrefix(operationID, verb+"_") {
			return verb
		}
	}

	// Try to find resource in operationId to infer verb
	parts := strings.Split(operationID, "_")
	for i, part := range parts {
		if part == mainResource {
			if i > 0 {
				return strings.Join(parts[:i], "_")
			}
			return strings.Join(parts[i+1:], "_")
		}
	}

	return ""
}

// buildToolName constructs the final tool name from resource, verb, and path parts.
func buildToolName(mainResource, matchedVerb string, pathParts []string) string {
	// Check for subresource operations (e.g., /todos/batch or /todos/stats)
	if len(pathParts) > 1 {
		subResource := pathParts[1]
		if strings.Contains(matchedVerb, subResource) {
			return fmt.Sprintf("%s_%s", mainResource, matchedVerb)
		}
		return fmt.Sprintf("%s_%s", mainResource, subResource)
	}

	// Standard CRUD operation: remove redundant resource name from verb
	cleanVerb := strings.TrimSuffix(matchedVerb, "_"+mainResource)
	cleanVerb = strings.TrimSuffix(cleanVerb, "_"+strings.TrimSuffix(mainResource, "s"))
	return fmt.Sprintf("%s_%s", mainResource, cleanVerb)
}

// extractPathSegments extracts meaningful path segments (excluding parameters and common prefixes).
func extractPathSegments(path string) []string {
	segments := strings.Split(strings.Trim(path, "/"), "/")
	var result []string

	for _, seg := range segments {
		// Skip empty, parameters, version prefixes, and "api" prefix
		if seg == "" || strings.HasPrefix(seg, "{") ||
			strings.HasPrefix(seg, "v") && len(seg) <= 3 ||
			seg == "api" {
			continue
		}
		result = append(result, seg)
	}

	return result
}

// NormalizeOperationID normalizes an operationId by removing redundant parts.
// This is used as a fallback when semantic mapping isn't applicable.
func NormalizeOperationID(operationID string) string {
	if operationID == "" {
		return ""
	}

	// If no redundancy detected, return as-is
	if !hasRedundancy(operationID) {
		return operationID
	}

	// Remove FastAPI pattern suffix: method at end + redundant path parts
	// Example: list_todos_todos_get → list_todos
	// Example: get_todo_todos__id__get → get_todo
	// Match pattern: (prefix)_(resource/path parts)_(http_method)
	// We want to keep (prefix)_(first occurrence of resource)

	lower := strings.ToLower(operationID)

	// Remove HTTP method suffix first
	for _, method := range []string{"_get", "_post", "_put", "_patch", "_delete"} {
		if before, ok := strings.CutSuffix(lower, method); ok {
			lower = before
			break
		}
	}

	// Now find the first occurrence of repeated resource name
	// Split by underscore and find duplicates
	parts := strings.Split(lower, "_")
	seen := make(map[string]bool)
	var result []string

	for _, part := range parts {
		// Skip empty parts (from double underscores like __id__)
		if part == "" {
			continue
		}
		// Skip HTTP verbs/methods (they're not part of the meaningful name)
		if isHTTPVerbOrMethod(part) {
			continue
		}
		// Keep first occurrence, skip duplicates
		if !seen[part] {
			result = append(result, part)
			seen[part] = true
		}
		// Skip subsequent occurrences (duplicates)
	}

	if len(result) > 0 {
		return strings.Join(result, "_")
	}

	return operationID
}
