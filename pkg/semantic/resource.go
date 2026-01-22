// Package semantic provides semantic mapping from OpenAPI operations to CLI commands.
// This file contains resource extraction logic.
package semantic

import (
	"regexp"
	"slices"
	"strings"
	"unicode"

	"github.com/getkin/kin-openapi/openapi3"
)

// ResourceExtractor handles resource name extraction from API paths.
type ResourceExtractor struct {
	// IgnoredPrefixes are path segments to skip when extracting resources.
	IgnoredPrefixes []string

	// VersionPattern matches version segments like v1, v2, v2.1.
	VersionPattern *regexp.Regexp

	// PathParamPattern matches path parameters like {id}, {userId}.
	PathParamPattern *regexp.Regexp
}

// NewResourceExtractor creates a new resource extractor with default settings.
func NewResourceExtractor() *ResourceExtractor {
	return &ResourceExtractor{
		IgnoredPrefixes: []string{
			"api", "apis", "rest", "v1", "v2", "v3",
			"internal", "external", "public", "private",
			"admin", "management", "manage",
		},
		VersionPattern:   regexp.MustCompile(`^v\d+(\.\d+)?$`),
		PathParamPattern: regexp.MustCompile(`^\{[^}]+\}$`),
	}
}

// ExtractResult contains the result of resource extraction.
type ExtractResult struct {
	// Resource is the extracted resource name.
	Resource string

	// ParentResource is the parent resource name (for nested resources).
	ParentResource string

	// IsNested indicates if this is a nested resource.
	IsNested bool

	// PathParams contains the path parameter names.
	PathParams []string

	// OriginalSegment is the original path segment the resource was extracted from.
	OriginalSegment string

	// ActionHint contains the action segment if detected (e.g., findByStatus).
	// This can be used by verb mapping to infer the verb.
	ActionHint string
}

// extractFromExtension attempts to extract resource info from x-cli-resource extension.
// Returns true if extraction was successful.
func (e *ResourceExtractor) extractFromExtension(operation *openapi3.Operation, result *ExtractResult) bool {
	if operation == nil {
		return false
	}

	ext, ok := operation.Extensions["x-cli-resource"]
	if !ok {
		return false
	}

	if resource, ok := ext.(string); ok {
		result.Resource = resource
		return true
	}

	if m, ok := ext.(map[string]any); ok {
		if name, ok := m["name"].(string); ok {
			result.Resource = name
		}
		if parent, ok := m["parent"].(string); ok {
			result.ParentResource = parent
			result.IsNested = true
		}
		return result.Resource != ""
	}

	return false
}

// extractResourceFromSegments extracts resource info from path segments.
func (e *ResourceExtractor) extractResourceFromSegments(resourceSegments []string, result *ExtractResult) {
	if len(resourceSegments) == 0 {
		result.Resource = "resource"
		return
	}

	lastIdx := len(resourceSegments) - 1
	lastSegment := resourceSegments[lastIdx]

	if len(resourceSegments) > 1 && e.isActionSegment(lastSegment) {
		result.Resource = NormalizeName(resourceSegments[lastIdx-1])
		result.OriginalSegment = resourceSegments[lastIdx-1]
		result.ActionHint = lastSegment
		if len(resourceSegments) > 2 {
			result.ParentResource = NormalizeName(resourceSegments[lastIdx-2])
			result.IsNested = true
		}
	} else {
		result.Resource = NormalizeName(lastSegment)
		result.OriginalSegment = lastSegment
		if len(resourceSegments) > 1 {
			result.ParentResource = NormalizeName(resourceSegments[lastIdx-1])
			result.IsNested = true
		}
	}
}

// Extract extracts resource information from an API path and operation.
func (e *ResourceExtractor) Extract(path string, operation *openapi3.Operation) *ExtractResult {
	result := &ExtractResult{}

	if e.extractFromExtension(operation, result) {
		return result
	}

	segments := e.parsePathSegments(path)
	result.PathParams = e.extractPathParams(segments)
	resourceSegments := e.findResourceSegments(segments)

	e.extractResourceFromSegments(resourceSegments, result)

	return result
}

// isActionSegment checks if a path segment represents an action rather than a resource.
// Common patterns: findByX, searchByX, listX, getX, createX, updateX, deleteX, etc.
func (e *ResourceExtractor) isActionSegment(segment string) bool {
	lower := strings.ToLower(segment)

	// Action verb prefixes
	actionPrefixes := []string{
		"find", "search", "query", "filter",
		"list", "get", "fetch", "load",
		"create", "add", "new",
		"update", "modify", "edit", "patch",
		"delete", "remove", "clear",
		"check", "verify", "validate",
		"enable", "disable", "activate", "deactivate",
		"start", "stop", "restart", "pause", "resume",
		"export", "import", "download", "upload",
		"sync", "refresh", "reset",
		"approve", "reject", "cancel",
		"publish", "unpublish", "archive", "unarchive",
		"lock", "unlock",
		"send", "resend",
		"clone", "copy", "move", "rename",
		"batch", "bulk",
	}

	for _, prefix := range actionPrefixes {
		if strings.HasPrefix(lower, prefix) && len(segment) > len(prefix) {
			// Make sure it's followed by another word (camelCase or separator)
			nextChar := segment[len(prefix)]
			if nextChar >= 'A' && nextChar <= 'Z' || nextChar == '_' || nextChar == '-' {
				return true
			}
		}
	}

	return false
}

// parsePathSegments splits a path into segments and cleans them.
func (e *ResourceExtractor) parsePathSegments(path string) []string {
	parts := strings.Split(path, "/")
	var segments []string
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			segments = append(segments, p)
		}
	}
	return segments
}

// extractPathParams extracts path parameter names from segments.
func (e *ResourceExtractor) extractPathParams(segments []string) []string {
	var params []string
	for _, seg := range segments {
		if e.isPathParam(seg) {
			// Extract param name without braces
			paramName := strings.TrimPrefix(strings.TrimSuffix(seg, "}"), "{")
			params = append(params, paramName)
		}
	}
	return params
}

// findResourceSegments finds segments that represent resources.
func (e *ResourceExtractor) findResourceSegments(segments []string) []string {
	var resources []string
	for _, seg := range segments {
		if e.isResourceSegment(seg) {
			resources = append(resources, seg)
		}
	}
	return resources
}

// isPathParam checks if a segment is a path parameter.
func (e *ResourceExtractor) isPathParam(segment string) bool {
	return e.PathParamPattern.MatchString(segment)
}

// isVersionSegment checks if a segment is a version identifier.
func (e *ResourceExtractor) isVersionSegment(segment string) bool {
	return e.VersionPattern.MatchString(strings.ToLower(segment))
}

// isIgnoredPrefix checks if a segment should be ignored.
func (e *ResourceExtractor) isIgnoredPrefix(segment string) bool {
	lower := strings.ToLower(segment)
	return slices.Contains(e.IgnoredPrefixes, lower)
}

// isResourceSegment checks if a segment represents a resource.
func (e *ResourceExtractor) isResourceSegment(segment string) bool {
	// Not a path parameter
	if e.isPathParam(segment) {
		return false
	}
	// Not a version segment
	if e.isVersionSegment(segment) {
		return false
	}
	// Not an ignored prefix
	if e.isIgnoredPrefix(segment) {
		return false
	}
	return true
}

// ExtractResourceName is a convenience function for simple resource extraction.
func ExtractResourceName(path string, operation *openapi3.Operation) string {
	extractor := NewResourceExtractor()
	result := extractor.Extract(path, operation)
	return result.Resource
}

// NormalizeName normalizes a resource name.
func NormalizeName(name string) string {
	// Convert to lowercase
	name = strings.ToLower(name)

	// Replace common separators with nothing
	name = strings.ReplaceAll(name, "-", "")
	name = strings.ReplaceAll(name, "_", "")

	// Keep the name as-is (with separators removed) for CLI
	return normalizeForCLI(strings.ToLower(strings.ReplaceAll(strings.ReplaceAll(name, "-", ""), "_", "")))
}

// normalizeForCLI normalizes a name for CLI use.
func normalizeForCLI(name string) string {
	// Convert camelCase/PascalCase to lowercase with no separators
	var result strings.Builder
	for _, r := range name {
		result.WriteRune(unicode.ToLower(r))
	}
	return result.String()
}

// NormalizeResourceName normalizes a resource name with optional pluralization handling.
func NormalizeResourceName(name string, preservePlural bool) string {
	normalized := strings.ToLower(name)

	// Remove common separators
	normalized = strings.ReplaceAll(normalized, "-", "")
	normalized = strings.ReplaceAll(normalized, "_", "")

	if !preservePlural {
		normalized = Singularize(normalized)
	}

	return normalized
}

// Singularize attempts to convert a plural word to singular.
// This is a simple implementation - a full implementation would use a library.
func Singularize(word string) string {
	if word == "" {
		return word
	}

	// Common irregular plurals
	irregulars := map[string]string{
		"people":   "person",
		"children": "child",
		"men":      "man",
		"women":    "woman",
		"feet":     "foot",
		"teeth":    "tooth",
		"geese":    "goose",
		"mice":     "mouse",
		"leaves":   "leaf",
		"lives":    "life",
		"knives":   "knife",
		"wives":    "wife",
		"halves":   "half",
		"selves":   "self",
		"indices":  "index",
		"matrices": "matrix",
		"vertices": "vertex",
		"oxen":     "ox",
		"criteria": "criterion",
		"data":     "datum",
		"media":    "medium",
	}

	if singular, ok := irregulars[word]; ok {
		return singular
	}

	// Common patterns
	suffixMappings := []struct {
		suffix      string
		replacement string
	}{
		{"ies", "y"},   // categories -> category
		{"ves", "f"},   // wolves -> wolf
		{"oes", "o"},   // heroes -> hero
		{"ses", "s"},   // buses -> bus
		{"xes", "x"},   // boxes -> box
		{"ches", "ch"}, // matches -> match
		{"shes", "sh"}, // dishes -> dish
		{"s", ""},      // users -> user
	}

	for _, mapping := range suffixMappings {
		if before, ok := strings.CutSuffix(word, mapping.suffix); ok {
			return before + mapping.replacement
		}
	}

	return word
}

// Pluralize attempts to convert a singular word to plural.
func Pluralize(word string) string {
	if word == "" {
		return word
	}

	// Common irregular plurals
	irregulars := map[string]string{
		"person":    "people",
		"child":     "children",
		"man":       "men",
		"woman":     "women",
		"foot":      "feet",
		"tooth":     "teeth",
		"goose":     "geese",
		"mouse":     "mice",
		"leaf":      "leaves",
		"life":      "lives",
		"knife":     "knives",
		"wife":      "wives",
		"half":      "halves",
		"self":      "selves",
		"index":     "indices",
		"matrix":    "matrices",
		"vertex":    "vertices",
		"ox":        "oxen",
		"criterion": "criteria",
		"datum":     "data",
		"medium":    "media",
	}

	if plural, ok := irregulars[word]; ok {
		return plural
	}

	// Common patterns
	switch {
	case strings.HasSuffix(word, "y") && !isVowel(rune(word[len(word)-2])):
		return strings.TrimSuffix(word, "y") + "ies"
	case strings.HasSuffix(word, "f"):
		return strings.TrimSuffix(word, "f") + "ves"
	case strings.HasSuffix(word, "fe"):
		return strings.TrimSuffix(word, "fe") + "ves"
	case strings.HasSuffix(word, "o"):
		return word + "es"
	case strings.HasSuffix(word, "s") ||
		strings.HasSuffix(word, "x") ||
		strings.HasSuffix(word, "z") ||
		strings.HasSuffix(word, "ch") ||
		strings.HasSuffix(word, "sh"):
		return word + "es"
	default:
		return word + "s"
	}
}

func isVowel(r rune) bool {
	return r == 'a' || r == 'e' || r == 'i' || r == 'o' || r == 'u'
}

// PathAnalysis contains detailed analysis of an API path.
type PathAnalysis struct {
	// Path is the original path.
	Path string

	// Segments is the list of path segments.
	Segments []string

	// ResourceSegments are segments that represent resources.
	ResourceSegments []string

	// ParameterSegments are path parameter segments.
	ParameterSegments []string

	// ParameterNames are the parameter names (without braces).
	ParameterNames []string

	// IsCollection indicates if the path targets a collection.
	IsCollection bool

	// IsSingleItem indicates if the path targets a single item.
	IsSingleItem bool

	// IsNested indicates if this is a nested resource path.
	IsNested bool

	// NestingLevel is how many levels deep the resource is nested.
	NestingLevel int
}

// AnalyzePath performs detailed analysis of an API path.
func AnalyzePath(path string) *PathAnalysis {
	extractor := NewResourceExtractor()
	segments := extractor.parsePathSegments(path)

	analysis := &PathAnalysis{
		Path:     path,
		Segments: segments,
	}

	for _, seg := range segments {
		if extractor.isPathParam(seg) {
			analysis.ParameterSegments = append(analysis.ParameterSegments, seg)
			paramName := strings.TrimPrefix(strings.TrimSuffix(seg, "}"), "{")
			analysis.ParameterNames = append(analysis.ParameterNames, paramName)
		} else if extractor.isResourceSegment(seg) {
			analysis.ResourceSegments = append(analysis.ResourceSegments, seg)
		}
	}

	// Determine if collection or single item
	if len(segments) > 0 {
		lastSeg := segments[len(segments)-1]
		analysis.IsSingleItem = extractor.isPathParam(lastSeg)
		analysis.IsCollection = !analysis.IsSingleItem
	}

	// Determine nesting level
	analysis.NestingLevel = max(len(analysis.ResourceSegments)-1, 0)
	analysis.IsNested = analysis.NestingLevel > 0

	return analysis
}

// InferResourceFromOperationID attempts to extract resource from operationId.
func InferResourceFromOperationID(operationID string) string {
	if operationID == "" {
		return ""
	}

	// Common patterns:
	// listUsers -> users
	// getUser -> user
	// createOrder -> order
	// deleteOrderItem -> orderitem

	// Remove common verb prefixes
	verbPrefixes := []string{
		"list", "get", "create", "update", "delete", "patch",
		"find", "search", "query", "fetch", "add", "remove",
		"post", "put",
	}

	lower := strings.ToLower(operationID)
	for _, prefix := range verbPrefixes {
		if strings.HasPrefix(lower, prefix) {
			resource := operationID[len(prefix):]
			if resource != "" {
				return NormalizeName(resource)
			}
		}
	}

	// If no verb prefix found, use the whole operationId
	return NormalizeName(operationID)
}

// BuildResourcePath builds a CLI resource path from a resource hierarchy.
func BuildResourcePath(resources ...string) string {
	var parts []string
	for _, r := range resources {
		if r != "" {
			parts = append(parts, NormalizeName(r))
		}
	}
	return strings.Join(parts, " ")
}
