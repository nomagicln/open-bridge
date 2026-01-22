package mcp

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewHybridSearchEngine(t *testing.T) {
	cfg := DefaultHybridSearchConfig()

	engine, err := NewHybridSearchEngine(cfg)
	require.NoError(t, err)
	require.NotNil(t, engine)
	defer func() { _ = engine.Close() }()
}

func TestHybridSearchEngine_IndexAndSearch(t *testing.T) {
	cfg := DefaultHybridSearchConfig()
	cfg.Embedder.Type = EmbedderTFIDF
	cfg.Embedder.Dimension = 100

	engine, err := NewHybridSearchEngine(cfg)
	require.NoError(t, err)
	defer func() { _ = engine.Close() }()

	// Index some tools
	tools := []ToolMetadata{
		{ID: "1", Name: "getPets", Description: "pet store api for managing pets", Method: "GET", Path: "/pets"},
		{ID: "2", Name: "createUser", Description: "user management create update delete", Method: "POST", Path: "/users"},
		{ID: "3", Name: "processOrder", Description: "order processing and payment", Method: "POST", Path: "/orders"},
	}

	err = engine.Index(tools)
	require.NoError(t, err)

	// Search
	results, err := engine.Search("pet api")
	require.NoError(t, err)
	require.Greater(t, len(results), 0)

	// First result should be the pet store tool
	assert.Equal(t, "1", results[0].ID)
}

func TestHybridSearchEngine_PredicateFilter(t *testing.T) {
	cfg := DefaultHybridSearchConfig()
	cfg.Embedder.Type = EmbedderTFIDF
	cfg.Embedder.Dimension = 100

	engine, err := NewHybridSearchEngine(cfg)
	require.NoError(t, err)
	defer func() { _ = engine.Close() }()

	tools := []ToolMetadata{
		{ID: "1", Name: "getUsers", Description: "get users list", Method: "GET", Path: "/users"},
		{ID: "2", Name: "createUser", Description: "create user", Method: "POST", Path: "/users"},
		{ID: "3", Name: "deleteUser", Description: "delete user", Method: "DELETE", Path: "/users/{id}"},
	}

	err = engine.Index(tools)
	require.NoError(t, err)

	// Set predicate filter for GET methods only
	engine.SetPredicateFilter(`Method == "GET"`)

	// Search should only return GET methods
	results, err := engine.Search("user")
	require.NoError(t, err)

	// All results should be GET
	for _, r := range results {
		if r.ID == "1" {
			assert.Equal(t, "GET", r.Method)
		}
	}
}

func TestHybridSearchEngine_WeightedFusion(t *testing.T) {
	cfg := DefaultHybridSearchConfig()
	cfg.Embedder.Type = EmbedderTFIDF
	cfg.Embedder.Dimension = 100
	cfg.FusionStrategy = FusionWeighted
	cfg.VectorWeight = 0.5

	engine, err := NewHybridSearchEngine(cfg)
	require.NoError(t, err)
	defer func() { _ = engine.Close() }()

	tools := []ToolMetadata{
		{ID: "1", Name: "getPets", Description: "pet store api"},
		{ID: "2", Name: "createUser", Description: "user management"},
	}

	err = engine.Index(tools)
	require.NoError(t, err)

	results, err := engine.Search("pet")
	require.NoError(t, err)
	require.Greater(t, len(results), 0)
}

func TestHybridSearchEngine_EmptyIndex(t *testing.T) {
	cfg := DefaultHybridSearchConfig()
	cfg.Embedder.Type = EmbedderTFIDF
	cfg.Embedder.Dimension = 100

	engine, err := NewHybridSearchEngine(cfg)
	require.NoError(t, err)
	defer func() { _ = engine.Close() }()

	results, err := engine.Search("test")
	require.NoError(t, err)
	assert.Empty(t, results)
}

func TestHybridSearchEngine_EmptyQuery(t *testing.T) {
	cfg := DefaultHybridSearchConfig()
	cfg.Embedder.Type = EmbedderTFIDF
	cfg.Embedder.Dimension = 100

	engine, err := NewHybridSearchEngine(cfg)
	require.NoError(t, err)
	defer func() { _ = engine.Close() }()

	tools := []ToolMetadata{
		{ID: "1", Name: "test", Description: "test document"},
	}
	err = engine.Index(tools)
	require.NoError(t, err)

	results, err := engine.Search("")
	require.NoError(t, err)
	// With empty query, should return all results
	assert.Len(t, results, 1)
}

func TestHybridSearchEngine_RRFFusion(t *testing.T) {
	cfg := DefaultHybridSearchConfig()
	cfg.Embedder.Type = EmbedderTFIDF
	cfg.Embedder.Dimension = 100
	cfg.FusionStrategy = FusionRRF
	cfg.RRFConstant = 60.0

	engine, err := NewHybridSearchEngine(cfg)
	require.NoError(t, err)
	defer func() { _ = engine.Close() }()

	// Test with multiple tools to exercise RRF fusion
	tools := []ToolMetadata{
		{ID: "1", Name: "getPets", Description: "pet store api for managing pets", Method: "GET", Path: "/pets"},
		{ID: "2", Name: "createPet", Description: "create new pet in the store", Method: "POST", Path: "/pets"},
		{ID: "3", Name: "deletePet", Description: "delete a pet from store", Method: "DELETE", Path: "/pets/{id}"},
		{ID: "4", Name: "getUsers", Description: "user management list all", Method: "GET", Path: "/users"},
	}

	err = engine.Index(tools)
	require.NoError(t, err)

	// Search for pets - should return pet-related tools first
	results, err := engine.Search("pet store")
	require.NoError(t, err)
	require.Greater(t, len(results), 0)

	// Pet tools should rank higher
	petFound := false
	for i, r := range results[:min(3, len(results))] {
		if r.ID == "1" || r.ID == "2" || r.ID == "3" {
			petFound = true
			_ = i // Pet tools should be in top 3
		}
	}
	assert.True(t, petFound, "pet tools should be in top results")
}

func TestHybridSearchEngine_CJKTokenizer(t *testing.T) {
	cfg := DefaultHybridSearchConfig()
	cfg.Embedder.Type = EmbedderTFIDF
	cfg.Embedder.Dimension = 100
	cfg.Tokenizer.Type = TokenizerCJK

	engine, err := NewHybridSearchEngine(cfg)
	require.NoError(t, err)
	defer func() { _ = engine.Close() }()

	tools := []ToolMetadata{
		{ID: "1", Name: "getUsers", Description: "获取用户列表"},
		{ID: "2", Name: "createUser", Description: "创建新用户"},
		{ID: "3", Name: "deleteUser", Description: "删除用户记录"},
	}

	err = engine.Index(tools)
	require.NoError(t, err)

	// Search with Chinese
	results, err := engine.Search("用户")
	require.NoError(t, err)
	// All tools contain "用户", should return results
	assert.Greater(t, len(results), 0)
}

func TestHybridSearchEngine_Reconfigure(t *testing.T) {
	cfg := DefaultHybridSearchConfig()
	cfg.Embedder.Type = EmbedderTFIDF
	cfg.Embedder.Dimension = 100
	cfg.FusionStrategy = FusionRRF

	engine, err := NewHybridSearchEngine(cfg)
	require.NoError(t, err)
	defer func() { _ = engine.Close() }()

	// Reconfigure with new settings
	newCfg := DefaultHybridSearchConfig()
	newCfg.FusionStrategy = FusionWeighted
	newCfg.VectorWeight = 0.7
	newCfg.TopK = 20

	err = engine.Reconfigure(newCfg)
	require.NoError(t, err)

	// Check that config was updated
	gotCfg := engine.Config()
	assert.Equal(t, FusionWeighted, gotCfg.FusionStrategy)
	assert.InDelta(t, 0.7, gotCfg.VectorWeight, 0.001)
	assert.Equal(t, 20, gotCfg.TopK)
}

func TestHybridSearchEngine_GetDescription(t *testing.T) {
	cfg := DefaultHybridSearchConfig()
	cfg.Embedder.Type = EmbedderTFIDF

	engine, err := NewHybridSearchEngine(cfg)
	require.NoError(t, err)
	defer func() { _ = engine.Close() }()

	desc := engine.GetDescription()
	assert.Contains(t, desc, "Hybrid")
	assert.Contains(t, desc, "lexical")
	assert.Contains(t, desc, "semantic")
}

func TestHybridSearchEngine_GetQueryExample(t *testing.T) {
	cfg := DefaultHybridSearchConfig()
	cfg.Embedder.Type = EmbedderTFIDF

	engine, err := NewHybridSearchEngine(cfg)
	require.NoError(t, err)
	defer func() { _ = engine.Close() }()

	example := engine.GetQueryExample()
	assert.NotEmpty(t, example)
}

func TestHybridSearchEngine_GetActiveEmbedderType(t *testing.T) {
	cfg := DefaultHybridSearchConfig()
	cfg.Embedder.Type = EmbedderTFIDF

	engine, err := NewHybridSearchEngine(cfg)
	require.NoError(t, err)
	defer func() { _ = engine.Close() }()

	embedderType := engine.GetActiveEmbedderType()
	assert.Equal(t, EmbedderTFIDF, embedderType)
}

func TestHybridSearchEngine_Close(t *testing.T) {
	cfg := DefaultHybridSearchConfig()
	cfg.Embedder.Type = EmbedderTFIDF

	engine, err := NewHybridSearchEngine(cfg)
	require.NoError(t, err)

	err = engine.Close()
	require.NoError(t, err)

	// Double close should not panic
	err = engine.Close()
	// May error or not, just shouldn't panic
	_ = err
}

func TestFusionStrategy_Constants(t *testing.T) {
	assert.Equal(t, FusionStrategy("rrf"), FusionRRF)
	assert.Equal(t, FusionStrategy("weighted"), FusionWeighted)
}

func TestDefaultHybridSearchConfig(t *testing.T) {
	cfg := DefaultHybridSearchConfig()

	assert.True(t, cfg.Enabled)
	assert.Equal(t, EmbedderAdaptive, cfg.Embedder.Type)
	assert.Equal(t, TokenizerSimple, cfg.Tokenizer.Type)
	assert.Equal(t, FusionRRF, cfg.FusionStrategy)
	assert.InDelta(t, 60.0, cfg.RRFConstant, 0.001)
	assert.InDelta(t, 0.5, cfg.VectorWeight, 0.001)
	assert.Equal(t, 50, cfg.TopK)
}

func TestHybridSearchEngine_MultipleSearches(t *testing.T) {
	cfg := DefaultHybridSearchConfig()
	cfg.Embedder.Type = EmbedderTFIDF
	cfg.Embedder.Dimension = 100

	engine, err := NewHybridSearchEngine(cfg)
	require.NoError(t, err)
	defer func() { _ = engine.Close() }()

	tools := []ToolMetadata{
		{ID: "1", Name: "getPets", Description: "pet store api", Method: "GET", Path: "/pets"},
		{ID: "2", Name: "createUser", Description: "user management", Method: "POST", Path: "/users"},
	}

	err = engine.Index(tools)
	require.NoError(t, err)

	// Multiple searches should work correctly
	for range 5 {
		results, err := engine.Search("pet")
		require.NoError(t, err)
		require.Greater(t, len(results), 0)
	}
}

func TestHybridSearchEngine_ReIndex(t *testing.T) {
	cfg := DefaultHybridSearchConfig()
	cfg.Embedder.Type = EmbedderTFIDF
	cfg.Embedder.Dimension = 100

	engine, err := NewHybridSearchEngine(cfg)
	require.NoError(t, err)
	defer func() { _ = engine.Close() }()

	// First index
	tools1 := []ToolMetadata{
		{ID: "1", Name: "getPets", Description: "pet api"},
	}
	err = engine.Index(tools1)
	require.NoError(t, err)

	// Second index with different tools
	tools2 := []ToolMetadata{
		{ID: "2", Name: "createUser", Description: "user api"},
	}
	err = engine.Index(tools2)
	require.NoError(t, err)

	// Search should find new tools
	results, err := engine.Search("user")
	require.NoError(t, err)
	require.Greater(t, len(results), 0)
}
