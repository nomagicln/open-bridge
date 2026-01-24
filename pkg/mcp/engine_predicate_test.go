//nolint:dupl // Test code duplication is acceptable for readability and isolation.
package mcp

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewPredicateSearchEngine(t *testing.T) {
	engine, err := NewPredicateSearchEngine()
	require.NoError(t, err)
	require.NotNil(t, engine)
	defer func() { _ = engine.Close() }()
}

func TestPredicateSearchEngine_Index(t *testing.T) {
	engine, err := NewPredicateSearchEngine()
	require.NoError(t, err)
	defer func() { _ = engine.Close() }()

	tools := createTestTools()
	err = engine.Index(tools)
	require.NoError(t, err)

	assert.Len(t, engine.tools, 5)
}

func TestPredicateSearchEngine_Search_EmptyQuery(t *testing.T) {
	engine, err := NewPredicateSearchEngine()
	require.NoError(t, err)
	defer func() { _ = engine.Close() }()

	tools := createTestTools()
	err = engine.Index(tools)
	require.NoError(t, err)

	results, err := engine.Search("")
	require.NoError(t, err)
	assert.Len(t, results, 5)
}

func TestPredicateSearchEngine_Search_MethodEquals(t *testing.T) {
	engine, err := NewPredicateSearchEngine()
	require.NoError(t, err)
	defer func() { _ = engine.Close() }()

	tools := createTestTools()
	err = engine.Index(tools)
	require.NoError(t, err)

	results, err := engine.Search(`MethodIs("GET")`)
	require.NoError(t, err)
	assert.Len(t, results, 3) // listPets, getPetById, listUsers

	for _, r := range results {
		assert.Equal(t, "GET", r.Method)
	}
}

func TestPredicateSearchEngine_Search_MethodNotEquals(t *testing.T) {
	engine, err := NewPredicateSearchEngine()
	require.NoError(t, err)
	defer func() { _ = engine.Close() }()

	tools := createTestTools()
	err = engine.Index(tools)
	require.NoError(t, err)

	results, err := engine.Search(`!MethodIs("GET")`)
	require.NoError(t, err)
	assert.Len(t, results, 2) // createPet (POST), deletePet (DELETE)

	for _, r := range results {
		assert.NotEqual(t, "GET", r.Method)
	}
}

func TestPredicateSearchEngine_Search_Contains(t *testing.T) {
	engine, err := NewPredicateSearchEngine()
	require.NoError(t, err)
	defer func() { _ = engine.Close() }()

	tools := createTestTools()
	err = engine.Index(tools)
	require.NoError(t, err)

	results, err := engine.Search(`PathContains("/pets")`)
	require.NoError(t, err)
	assert.Len(t, results, 4) // All pet-related

	for _, r := range results {
		assert.Contains(t, r.Path, "/pets")
	}
}

func TestPredicateSearchEngine_Search_HasTag(t *testing.T) {
	engine, err := NewPredicateSearchEngine()
	require.NoError(t, err)
	defer func() { _ = engine.Close() }()

	tools := createTestTools()
	err = engine.Index(tools)
	require.NoError(t, err)

	results, err := engine.Search(`HasTag("admin")`)
	require.NoError(t, err)
	assert.Len(t, results, 1)
	assert.Equal(t, "deletePet", results[0].ID)
}

func TestPredicateSearchEngine_Search_CombinedExpression(t *testing.T) {
	engine, err := NewPredicateSearchEngine()
	require.NoError(t, err)
	defer func() { _ = engine.Close() }()

	tools := createTestTools()
	err = engine.Index(tools)
	require.NoError(t, err)

	// GET method AND pet-related path
	results, err := engine.Search(`MethodIs("GET") && PathContains("/pets")`)
	require.NoError(t, err)
	assert.Len(t, results, 2) // listPets, getPetById

	for _, r := range results {
		assert.Equal(t, "GET", r.Method)
		assert.Contains(t, r.Path, "/pets")
	}
}

func TestPredicateSearchEngine_Search_OrExpression(t *testing.T) {
	engine, err := NewPredicateSearchEngine()
	require.NoError(t, err)
	defer func() { _ = engine.Close() }()

	tools := createTestTools()
	err = engine.Index(tools)
	require.NoError(t, err)

	// POST or DELETE
	results, err := engine.Search(`MethodIs("POST") || MethodIs("DELETE")`)
	require.NoError(t, err)
	assert.Len(t, results, 2)
}

func TestPredicateSearchEngine_Search_NotExpression(t *testing.T) {
	engine, err := NewPredicateSearchEngine()
	require.NoError(t, err)
	defer func() { _ = engine.Close() }()

	tools := createTestTools()
	err = engine.Index(tools)
	require.NoError(t, err)

	// NOT users
	results, err := engine.Search(`!PathContains("/users")`)
	require.NoError(t, err)
	assert.Len(t, results, 4)

	for _, r := range results {
		assert.NotContains(t, r.Path, "/users")
	}
}

func TestPredicateSearchEngine_Search_InvalidExpression(t *testing.T) {
	engine, err := NewPredicateSearchEngine()
	require.NoError(t, err)
	defer func() { _ = engine.Close() }()

	tools := createTestTools()
	err = engine.Index(tools)
	require.NoError(t, err)

	_, err = engine.Search(`invalid expression syntax`)
	assert.Error(t, err)
}

func TestPredicateSearchEngine_Search_StartsWith(t *testing.T) {
	engine, err := NewPredicateSearchEngine()
	require.NoError(t, err)
	defer func() { _ = engine.Close() }()

	tools := createTestTools()
	err = engine.Index(tools)
	require.NoError(t, err)

	results, err := engine.Search(`NameStartsWith("list")`)
	require.NoError(t, err)
	assert.Len(t, results, 2) // listPets, listUsers
}

func TestPredicateSearchEngine_Search_EndsWith(t *testing.T) {
	engine, err := NewPredicateSearchEngine()
	require.NoError(t, err)
	defer func() { _ = engine.Close() }()

	tools := createTestTools()
	err = engine.Index(tools)
	require.NoError(t, err)

	results, err := engine.Search(`NameEndsWith("Pet")`)
	require.NoError(t, err)
	assert.Len(t, results, 2) // createPet, deletePet
}

func TestPredicateSearchEngine_GetDescription(t *testing.T) {
	engine, err := NewPredicateSearchEngine()
	require.NoError(t, err)
	defer func() { _ = engine.Close() }()

	desc := engine.GetDescription()
	assert.Contains(t, desc, "predicate")
	assert.Contains(t, desc, "MethodIs")
}

func TestPredicateSearchEngine_GetQueryExample(t *testing.T) {
	engine, err := NewPredicateSearchEngine()
	require.NoError(t, err)
	defer func() { _ = engine.Close() }()

	example := engine.GetQueryExample()
	assert.NotEmpty(t, example)
	assert.Contains(t, example, "MethodIs")
}

func TestPredicateSearchEngine_Close(t *testing.T) {
	engine, err := NewPredicateSearchEngine()
	require.NoError(t, err)

	err = engine.Close()
	assert.NoError(t, err)
}

func TestPredicateSearchEngine_GetBestPractices(t *testing.T) {
	engine, err := NewPredicateSearchEngine()
	require.NoError(t, err)
	defer func() { _ = engine.Close() }()

	practices := engine.GetBestPractices()
	assert.NotEmpty(t, practices)
	assert.Contains(t, practices, "MethodIs")
	assert.Contains(t, practices, "prefer")
}

func TestPredicateSearchEngine_GetExamples(t *testing.T) {
	engine, err := NewPredicateSearchEngine()
	require.NoError(t, err)
	defer func() { _ = engine.Close() }()

	examples := engine.GetExamples()
	assert.NotEmpty(t, examples)
	assert.GreaterOrEqual(t, len(examples), 2)
	for _, ex := range examples {
		assert.NotEmpty(t, ex)
	}
}
