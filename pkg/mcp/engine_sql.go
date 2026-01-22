package mcp

import (
	"database/sql"
	"fmt"
	"strings"
	"sync"

	_ "modernc.org/sqlite" // Pure Go SQLite driver
)

// SQLSearchEngine implements ToolSearchEngine using SQLite FTS5 for full-text search.
// It provides efficient keyword-based search with support for:
// - Boolean operators (AND, OR, NOT)
// - Phrase matching ("exact phrase")
// - Prefix matching (prefix*)
// - Column-specific search (column:term)
type SQLSearchEngine struct {
	db *sql.DB
	mu sync.RWMutex
}

// NewSQLSearchEngine creates a new SQLite-based search engine.
// It uses an in-memory database for fast access.
func NewSQLSearchEngine() (*SQLSearchEngine, error) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		return nil, fmt.Errorf("failed to open SQLite database: %w", err)
	}

	// Create FTS5 virtual table for full-text search
	_, err = db.Exec(`
		CREATE VIRTUAL TABLE IF NOT EXISTS tools USING fts5(
			id,
			name,
			description,
			method,
			path,
			tags,
			tokenize='porter unicode61'
		);
	`)
	if err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("failed to create FTS5 table: %w", err)
	}

	return &SQLSearchEngine{db: db}, nil
}

// Search finds tools matching the given query using FTS5.
// Query syntax supports:
// - Simple terms: "user" matches any tool with "user" in any field
// - Phrases: '"create user"' matches exact phrase
// - Boolean: "user AND admin" or "user OR admin"
// - Prefix: "get*" matches "get", "getUser", etc.
// - Column filter: "method:GET" or "path:/users"
// - Negation: "NOT delete" or "-delete"
func (e *SQLSearchEngine) Search(query string) ([]ToolMetadata, error) {
	e.mu.RLock()
	defer e.mu.RUnlock()

	if query == "" {
		return e.getAllTools()
	}

	// Use FTS5 MATCH query
	rows, err := e.db.Query(`
		SELECT id, name, description, method, path, tags
		FROM tools
		WHERE tools MATCH ?
		ORDER BY rank
		LIMIT 50
	`, query)
	if err != nil {
		return nil, fmt.Errorf("search query failed: %w", err)
	}
	defer func() { _ = rows.Close() }()

	return e.scanToolRows(rows)
}

// getAllTools returns all indexed tools (used when query is empty).
func (e *SQLSearchEngine) getAllTools() ([]ToolMetadata, error) {
	rows, err := e.db.Query(`
		SELECT id, name, description, method, path, tags
		FROM tools
		LIMIT 100
	`)
	if err != nil {
		return nil, fmt.Errorf("failed to get all tools: %w", err)
	}
	defer func() { _ = rows.Close() }()

	return e.scanToolRows(rows)
}

// scanToolRows converts SQL rows to ToolMetadata slice.
func (e *SQLSearchEngine) scanToolRows(rows *sql.Rows) ([]ToolMetadata, error) {
	var results []ToolMetadata
	for rows.Next() {
		var tool ToolMetadata
		var tagsStr string
		if err := rows.Scan(&tool.ID, &tool.Name, &tool.Description, &tool.Method, &tool.Path, &tagsStr); err != nil {
			return nil, fmt.Errorf("failed to scan row: %w", err)
		}
		if tagsStr != "" {
			tool.Tags = strings.Split(tagsStr, ",")
		}
		results = append(results, tool)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("row iteration error: %w", err)
	}
	return results, nil
}

// GetDescription returns a description of the SQL search engine.
func (e *SQLSearchEngine) GetDescription() string {
	return `SQLite FTS5 full-text search engine. Supports:
- Simple keyword search: "user" matches tools with "user" in any field
- Phrase matching: "\"create user\"" matches exact phrase
- Boolean operators: "user AND admin", "user OR admin", "NOT delete"
- Prefix matching: "get*" matches "get", "getUser", "getPets", etc.
- Column-specific: "method:GET", "path:/users", "tags:pet"
- Combined queries: "method:POST AND user*"`
}

// GetQueryExample returns an example query.
func (e *SQLSearchEngine) GetQueryExample() string {
	return `"method:GET AND user*" or "create OR update"`
}

// Index adds or updates tools in the search index.
func (e *SQLSearchEngine) Index(tools []ToolMetadata) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	// Clear existing data
	_, err := e.db.Exec("DELETE FROM tools")
	if err != nil {
		return fmt.Errorf("failed to clear existing tools: %w", err)
	}

	// Prepare insert statement
	stmt, err := e.db.Prepare(`
		INSERT INTO tools (id, name, description, method, path, tags)
		VALUES (?, ?, ?, ?, ?, ?)
	`)
	if err != nil {
		return fmt.Errorf("failed to prepare insert statement: %w", err)
	}
	defer func() { _ = stmt.Close() }()

	// Insert all tools
	for _, tool := range tools {
		tagsStr := strings.Join(tool.Tags, ",")
		_, err = stmt.Exec(tool.ID, tool.Name, tool.Description, tool.Method, tool.Path, tagsStr)
		if err != nil {
			return fmt.Errorf("failed to insert tool %s: %w", tool.ID, err)
		}
	}

	return nil
}

// Close releases database resources.
func (e *SQLSearchEngine) Close() error {
	return e.db.Close()
}
