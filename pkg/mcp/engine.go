// Package mcp provides MCP (Model Context Protocol) server functionality.
package mcp

import (
	"fmt"
)

// SearchEngineType represents the type of search engine to use for tool discovery.
type SearchEngineType string

const (
	// SearchEngineSQL uses SQLite FTS5 for full-text search.
	SearchEngineSQL SearchEngineType = "sql"
	// SearchEnginePredicate uses vulcand-predicate for expression matching.
	SearchEnginePredicate SearchEngineType = "predicate"
	// SearchEngineVector uses vector similarity for semantic search.
	SearchEngineVector SearchEngineType = "vector"
	// SearchEngineHybrid combines SQL FTS5 and vector search with RRF fusion.
	SearchEngineHybrid SearchEngineType = "hybrid"
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
	case SearchEngineSQL:
		return NewSQLSearchEngine()
	case SearchEnginePredicate:
		return NewPredicateSearchEngine()
	case SearchEngineVector:
		return NewVectorSearchEngine()
	case SearchEngineHybrid:
		return NewHybridSearchEngine(DefaultHybridSearchConfig())
	default:
		return nil, fmt.Errorf("unknown search engine type: %s", engineType)
	}
}

// NewSearchEngineWithConfig creates a new search engine with custom configuration.
// This is primarily useful for hybrid search which has additional configuration options.
func NewSearchEngineWithConfig(engineType SearchEngineType, hybridCfg *HybridSearchConfig) (ToolSearchEngine, error) {
	switch engineType {
	case SearchEngineSQL:
		return NewSQLSearchEngine()
	case SearchEnginePredicate:
		return NewPredicateSearchEngine()
	case SearchEngineVector:
		return NewVectorSearchEngine()
	case SearchEngineHybrid:
		cfg := DefaultHybridSearchConfig()
		if hybridCfg != nil {
			cfg = *hybridCfg
		}
		return NewHybridSearchEngine(cfg)
	default:
		return nil, fmt.Errorf("unknown search engine type: %s", engineType)
	}
}

// ParseSearchEngineType parses a string into a SearchEngineType.
func ParseSearchEngineType(s string) (SearchEngineType, error) {
	switch s {
	case "sql", "sqlite":
		return SearchEngineSQL, nil
	case "predicate":
		return SearchEnginePredicate, nil
	case "vector":
		return SearchEngineVector, nil
	case "hybrid":
		return SearchEngineHybrid, nil
	default:
		return "", fmt.Errorf("invalid search engine type: %s (valid: sql, predicate, vector, hybrid)", s)
	}
}
