//nolint:dupl // Test code duplication is acceptable for readability and isolation.
package mcp

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewSQLSearchEngine(t *testing.T) {
	engine, err := NewSQLSearchEngine()
	require.NoError(t, err)
	require.NotNil(t, engine)
	defer func() { _ = engine.Close() }()

	assert.NotNil(t, engine.db)
}

func TestSQLSearchEngine_Index(t *testing.T) {
	engine, err := NewSQLSearchEngine()
	require.NoError(t, err)
	defer func() { _ = engine.Close() }()

	tools := []ToolMetadata{
		{
			ID:          "listPets",
			Name:        "listPets",
			Description: "List all pets in the store",
			Method:      "GET",
			Path:        "/pets",
			Tags:        []string{"pets", "store"},
		},
		{
			ID:          "createPet",
			Name:        "createPet",
			Description: "Create a new pet",
			Method:      "POST",
			Path:        "/pets",
			Tags:        []string{"pets"},
		},
		{
			ID:          "getPetById",
			Name:        "getPetById",
			Description: "Get a pet by its ID",
			Method:      "GET",
			Path:        "/pets/{petId}",
			Tags:        []string{"pets"},
		},
		{
			ID:          "deletePet",
			Name:        "deletePet",
			Description: "Delete a pet from the store",
			Method:      "DELETE",
			Path:        "/pets/{petId}",
			Tags:        []string{"pets", "admin"},
		},
		{
			ID:          "listUsers",
			Name:        "listUsers",
			Description: "List all users",
			Method:      "GET",
			Path:        "/users",
			Tags:        []string{"users"},
		},
	}

	err = engine.Index(tools)
	require.NoError(t, err)
}

func TestSQLSearchEngine_Search_EmptyQuery(t *testing.T) {
	engine, err := NewSQLSearchEngine()
	require.NoError(t, err)
	defer func() { _ = engine.Close() }()

	tools := createTestTools()
	err = engine.Index(tools)
	require.NoError(t, err)

	results, err := engine.Search("")
	require.NoError(t, err)
	assert.Len(t, results, 5)
}

func TestSQLSearchEngine_Search_SimpleKeyword(t *testing.T) {
	engine, err := NewSQLSearchEngine()
	require.NoError(t, err)
	defer func() { _ = engine.Close() }()

	tools := createTestTools()
	err = engine.Index(tools)
	require.NoError(t, err)

	// Search for "pet"
	results, err := engine.Search("pet")
	require.NoError(t, err)
	assert.NotEmpty(t, results)

	// All results should be pet-related
	for _, r := range results {
		assert.Contains(t, r.Path, "/pets")
	}
}

func TestSQLSearchEngine_Search_MethodFilter(t *testing.T) {
	engine, err := NewSQLSearchEngine()
	require.NoError(t, err)
	defer func() { _ = engine.Close() }()

	tools := createTestTools()
	err = engine.Index(tools)
	require.NoError(t, err)

	// Search for GET methods
	results, err := engine.Search("method:GET")
	require.NoError(t, err)
	assert.NotEmpty(t, results)

	for _, r := range results {
		assert.Equal(t, "GET", r.Method)
	}
}

func TestSQLSearchEngine_Search_BooleanOperators(t *testing.T) {
	engine, err := NewSQLSearchEngine()
	require.NoError(t, err)
	defer func() { _ = engine.Close() }()

	tools := createTestTools()
	err = engine.Index(tools)
	require.NoError(t, err)

	// Search with AND
	results, err := engine.Search("pet AND delete")
	require.NoError(t, err)
	assert.Len(t, results, 1)
	assert.Equal(t, "deletePet", results[0].ID)
}

func TestSQLSearchEngine_Search_PrefixMatch(t *testing.T) {
	engine, err := NewSQLSearchEngine()
	require.NoError(t, err)
	defer func() { _ = engine.Close() }()

	tools := createTestTools()
	err = engine.Index(tools)
	require.NoError(t, err)

	// Search with prefix
	results, err := engine.Search("list*")
	require.NoError(t, err)
	assert.Len(t, results, 2) // listPets and listUsers
}

func TestSQLSearchEngine_GetDescription(t *testing.T) {
	engine, err := NewSQLSearchEngine()
	require.NoError(t, err)
	defer func() { _ = engine.Close() }()

	desc := engine.GetDescription()
	assert.Contains(t, desc, "SQLite FTS5")
	assert.Contains(t, desc, "Boolean")
}

func TestSQLSearchEngine_GetQueryExample(t *testing.T) {
	engine, err := NewSQLSearchEngine()
	require.NoError(t, err)
	defer func() { _ = engine.Close() }()

	example := engine.GetQueryExample()
	assert.NotEmpty(t, example)
}

func TestSQLSearchEngine_ReIndex(t *testing.T) {
	engine, err := NewSQLSearchEngine()
	require.NoError(t, err)
	defer func() { _ = engine.Close() }()

	// Initial index
	tools := createTestTools()
	err = engine.Index(tools)
	require.NoError(t, err)

	results, err := engine.Search("")
	require.NoError(t, err)
	assert.Len(t, results, 5)

	// Re-index with fewer tools
	newTools := []ToolMetadata{
		{ID: "newTool", Name: "newTool", Description: "A new tool", Method: "GET", Path: "/new"},
	}
	err = engine.Index(newTools)
	require.NoError(t, err)

	results, err = engine.Search("")
	require.NoError(t, err)
	assert.Len(t, results, 1)
	assert.Equal(t, "newTool", results[0].ID)
}
