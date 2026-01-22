//nolint:dupl // Test code duplication is acceptable for readability and isolation.
package mcp

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewVectorSearchEngine(t *testing.T) {
	engine, err := NewVectorSearchEngine()
	require.NoError(t, err)
	require.NotNil(t, engine)
	defer func() { _ = engine.Close() }()
}

func TestVectorSearchEngine_Index(t *testing.T) {
	engine, err := NewVectorSearchEngine()
	require.NoError(t, err)
	defer func() { _ = engine.Close() }()

	tools := createTestTools()
	err = engine.Index(tools)
	require.NoError(t, err)

	assert.Len(t, engine.tools, 5)
	assert.Len(t, engine.vectors, 5)
	assert.NotEmpty(t, engine.vocabulary)
	assert.NotEmpty(t, engine.idf)
}

func TestVectorSearchEngine_Search_EmptyQuery(t *testing.T) {
	engine, err := NewVectorSearchEngine()
	require.NoError(t, err)
	defer func() { _ = engine.Close() }()

	tools := createTestTools()
	err = engine.Index(tools)
	require.NoError(t, err)

	results, err := engine.Search("")
	require.NoError(t, err)
	assert.Len(t, results, 5)
}

func TestVectorSearchEngine_Search_SemanticQuery(t *testing.T) {
	engine, err := NewVectorSearchEngine()
	require.NoError(t, err)
	defer func() { _ = engine.Close() }()

	tools := createTestTools()
	err = engine.Index(tools)
	require.NoError(t, err)

	// Search for pet-related
	results, err := engine.Search("list all pets in the store")
	require.NoError(t, err)
	assert.NotEmpty(t, results)

	// The first result should be the most relevant
	assert.Equal(t, "listPets", results[0].ID)
}

func TestVectorSearchEngine_Search_CreateQuery(t *testing.T) {
	engine, err := NewVectorSearchEngine()
	require.NoError(t, err)
	defer func() { _ = engine.Close() }()

	tools := createTestTools()
	err = engine.Index(tools)
	require.NoError(t, err)

	results, err := engine.Search("create a new pet")
	require.NoError(t, err)
	assert.NotEmpty(t, results)

	// createPet should be highly ranked
	found := false
	for i, r := range results {
		if r.ID == "createPet" {
			found = true
			assert.Less(t, i, 3, "createPet should be in top 3 results")
			break
		}
	}
	assert.True(t, found, "createPet should be in results")
}

func TestVectorSearchEngine_Search_DeleteQuery(t *testing.T) {
	engine, err := NewVectorSearchEngine()
	require.NoError(t, err)
	defer func() { _ = engine.Close() }()

	tools := createTestTools()
	err = engine.Index(tools)
	require.NoError(t, err)

	results, err := engine.Search("delete pet from store")
	require.NoError(t, err)
	assert.NotEmpty(t, results)

	// deletePet should be in results
	found := false
	for _, r := range results {
		if r.ID == "deletePet" {
			found = true
			break
		}
	}
	assert.True(t, found, "deletePet should be in results")
}

func TestVectorSearchEngine_Search_UserQuery(t *testing.T) {
	engine, err := NewVectorSearchEngine()
	require.NoError(t, err)
	defer func() { _ = engine.Close() }()

	tools := createTestTools()
	err = engine.Index(tools)
	require.NoError(t, err)

	results, err := engine.Search("list users")
	require.NoError(t, err)
	assert.NotEmpty(t, results)

	// listUsers should be highly ranked
	found := false
	for i, r := range results {
		if r.ID == "listUsers" {
			found = true
			assert.Less(t, i, 3, "listUsers should be in top 3 results")
			break
		}
	}
	assert.True(t, found, "listUsers should be in results")
}

func TestVectorSearchEngine_Search_NoMatch(t *testing.T) {
	engine, err := NewVectorSearchEngine()
	require.NoError(t, err)
	defer func() { _ = engine.Close() }()

	tools := createTestTools()
	err = engine.Index(tools)
	require.NoError(t, err)

	// Search for completely unrelated term
	results, err := engine.Search("xyz123 qwerty asdfg")
	require.NoError(t, err)
	// May return empty or very low scored results
	// This is expected behavior for semantic search
	_ = results // results may be empty, which is valid
}

func TestVectorSearchEngine_GetDescription(t *testing.T) {
	engine, err := NewVectorSearchEngine()
	require.NoError(t, err)
	defer func() { _ = engine.Close() }()

	desc := engine.GetDescription()
	assert.Contains(t, desc, "Semantic")
	assert.Contains(t, desc, "natural language")
}

func TestVectorSearchEngine_GetQueryExample(t *testing.T) {
	engine, err := NewVectorSearchEngine()
	require.NoError(t, err)
	defer func() { _ = engine.Close() }()

	example := engine.GetQueryExample()
	assert.NotEmpty(t, example)
}

func TestVectorSearchEngine_Close(t *testing.T) {
	engine, err := NewVectorSearchEngine()
	require.NoError(t, err)

	err = engine.Close()
	assert.NoError(t, err)
}

func TestVectorSearchEngine_Tokenize(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected []string
	}{
		{
			name:     "simple words",
			input:    "hello world",
			expected: []string{"hello", "world"},
		},
		{
			name:     "mixed case",
			input:    "Hello World",
			expected: []string{"hello", "world"},
		},
		{
			name:     "with punctuation",
			input:    "hello, world!",
			expected: []string{"hello", "world"},
		},
		{
			name:     "path like",
			input:    "/pets/{petId}",
			expected: []string{"pets", "petid"},
		},
		{
			name:     "camelCase",
			input:    "getPetById",
			expected: []string{"getpetbyid"},
		},
		{
			name:     "numbers",
			input:    "v1 api",
			expected: []string{"v1", "api"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tokenize(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestVectorSearchEngine_CosineSimilarity(t *testing.T) {
	tests := []struct {
		name     string
		a        []float64
		b        []float64
		expected float64
	}{
		{
			name:     "identical vectors",
			a:        []float64{1, 0, 0},
			b:        []float64{1, 0, 0},
			expected: 1.0,
		},
		{
			name:     "orthogonal vectors",
			a:        []float64{1, 0, 0},
			b:        []float64{0, 1, 0},
			expected: 0.0,
		},
		{
			name:     "zero vector",
			a:        []float64{0, 0, 0},
			b:        []float64{1, 1, 1},
			expected: 0.0,
		},
		{
			name:     "different lengths",
			a:        []float64{1, 0},
			b:        []float64{1, 0, 0},
			expected: 0.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := cosineSimilarity(tt.a, tt.b)
			assert.InDelta(t, tt.expected, result, 0.0001)
		})
	}
}

func TestVectorSearchEngine_ReIndex(t *testing.T) {
	engine, err := NewVectorSearchEngine()
	require.NoError(t, err)
	defer func() { _ = engine.Close() }()

	// Initial index
	tools := createTestTools()
	err = engine.Index(tools)
	require.NoError(t, err)
	assert.Len(t, engine.tools, 5)

	// Re-index with new tools
	newTools := []ToolMetadata{
		{ID: "newTool", Name: "newTool", Description: "A completely new tool", Method: "GET", Path: "/new"},
	}
	err = engine.Index(newTools)
	require.NoError(t, err)
	assert.Len(t, engine.tools, 1)
	assert.Len(t, engine.vectors, 1)
}
