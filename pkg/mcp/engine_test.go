package mcp

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

//nolint:dupl // Similar test structure is intentional for different type parsing
func TestSearchEngineType_Parse(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		expected    SearchEngineType
		expectError bool
	}{
		{
			name:     "predicate engine",
			input:    "predicate",
			expected: SearchEnginePredicate,
		},
		{
			name:        "invalid engine",
			input:       "invalid",
			expectError: true,
		},
		{
			name:        "empty string",
			input:       "",
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := ParseSearchEngineType(tt.input)
			if tt.expectError {
				assert.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.expected, result)
			}
		})
	}
}

func TestNewSearchEngine(t *testing.T) {
	tests := []struct {
		name        string
		engineType  SearchEngineType
		expectError bool
	}{
		{
			name:       "create predicate engine",
			engineType: SearchEnginePredicate,
		},
		{
			name:        "invalid engine type",
			engineType:  SearchEngineType("invalid"),
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			engine, err := NewSearchEngine(tt.engineType)
			if tt.expectError {
				assert.Error(t, err)
				assert.Nil(t, engine)
			} else {
				require.NoError(t, err)
				require.NotNil(t, engine)
				assert.NoError(t, engine.Close())
			}
		})
	}
}

func TestToolMetadata(t *testing.T) {
	meta := ToolMetadata{
		ID:          "getPetById",
		Name:        "getPetById",
		Description: "Get a pet by its ID",
		Method:      "GET",
		Path:        "/pets/{petId}",
		Tags:        []string{"pets", "read"},
	}

	assert.Equal(t, "getPetById", meta.ID)
	assert.Equal(t, "GET", meta.Method)
	assert.Equal(t, "/pets/{petId}", meta.Path)
	assert.Contains(t, meta.Tags, "pets")
	assert.Contains(t, meta.Tags, "read")
}

func TestSearchEngineInterface(t *testing.T) {
	// Verify all engines implement the interface
	engines := []ToolSearchEngine{}

	predicateEngine, err := NewPredicateSearchEngine()
	require.NoError(t, err)
	engines = append(engines, predicateEngine)

	sampleTools := []ToolMetadata{
		{ID: "listPets", Name: "listPets", Description: "List all pets", Method: "GET", Path: "/pets"},
		{ID: "createPet", Name: "createPet", Description: "Create a new pet", Method: "POST", Path: "/pets"},
		{ID: "getPetById", Name: "getPetById", Description: "Get pet by ID", Method: "GET", Path: "/pets/{petId}"},
	}

	for _, engine := range engines {
		// Test Index
		err := engine.Index(sampleTools)
		assert.NoError(t, err)

		// Test GetDescription
		desc := engine.GetDescription()
		assert.NotEmpty(t, desc)

		// Test GetQueryExample
		example := engine.GetQueryExample()
		assert.NotEmpty(t, example)

		// Test Search with empty query
		results, err := engine.Search("")
		assert.NoError(t, err)
		assert.NotEmpty(t, results)

		// Test Close
		err = engine.Close()
		assert.NoError(t, err)
	}
}

func TestNewSearchEngineWithConfig(t *testing.T) {
	t.Run("predicate with config", func(t *testing.T) {
		engine, err := NewSearchEngineWithConfig(SearchEnginePredicate, nil)
		require.NoError(t, err)
		defer func() { _ = engine.Close() }()

		_, ok := engine.(*PredicateSearchEngine)
		assert.True(t, ok)
	})
}
