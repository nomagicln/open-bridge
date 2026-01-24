// Package mcp provides MCP (Model Context Protocol) server functionality.
package mcp

import (
	"fmt"
)

// SearchEngineType represents the type of search engine to use for tool discovery.
type SearchEngineType string

const (
	// SearchEnginePredicate uses vulcand-predicate for expression matching.
	SearchEnginePredicate SearchEngineType = "predicate"
)

// ToolMetadata contains summary information about a tool for search results.
// It provides enough context for the AI to decide whether to load the full definition.
type ToolMetadata struct {
	// ID is the unique identifier for the tool (usually operationId or method_path).
	ID string `json:"id"`

	// Name is the human-readable name of the tool.
	Name string `json:"name"`

	// Description is a brief summary of what the tool does.
	Description string `json:"description"`

	// Method is the HTTP method (GET, POST, PUT, DELETE, PATCH).
	Method string `json:"method"`

	// Path is the API endpoint path.
	Path string `json:"path"`

	// Tags are categories/groups the tool belongs to (from OpenAPI tags).
	Tags []string `json:"tags,omitempty"`
}

// ToolSearchEngine defines the interface for searching tools.
// Different implementations provide different search capabilities.
type ToolSearchEngine interface {
	// Search finds tools matching the given query.
	// The query format depends on the engine type:
	// - SQL: Full-text search query (supports FTS5 syntax)
	// - Predicate: vulcand-predicate expression
	// - Vector: Natural language description for semantic matching
	Search(query string) ([]ToolMetadata, error)

	// GetDescription returns a human-readable description of the engine
	// and its query syntax for use in tool descriptions.
	GetDescription() string

	// GetQueryExample returns an example query for the engine type.
	GetQueryExample() string

	// Index adds or updates tools in the search index.
	Index(tools []ToolMetadata) error

	// Close releases any resources held by the engine.
	Close() error
}

// NewSearchEngine creates a new search engine of the specified type.
func NewSearchEngine(engineType SearchEngineType) (ToolSearchEngine, error) {
	switch engineType {
	case SearchEnginePredicate:
		return NewPredicateSearchEngine()
	default:
		return nil, fmt.Errorf("unknown search engine type: %s", engineType)
	}
}

// NewSearchEngineWithConfig creates a new search engine with custom configuration.
// Deprecated: This function is kept for backward compatibility but is no longer needed
// since only predicate engine is supported.
func NewSearchEngineWithConfig(engineType SearchEngineType, _ any) (ToolSearchEngine, error) {
	return NewSearchEngine(engineType)
}

// ParseSearchEngineType parses a string into a SearchEngineType.
func ParseSearchEngineType(s string) (SearchEngineType, error) {
	switch s {
	case "predicate":
		return SearchEnginePredicate, nil
	default:
		return "", fmt.Errorf("invalid search engine type: %s (valid: predicate)", s)
	}
}
