//nolint:dupl // Test code duplication is acceptable for readability and isolation.
package mcp

import (
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/nomagicln/open-bridge/pkg/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewToolRegistry(t *testing.T) {
	registry := NewToolRegistry()
	require.NotNil(t, registry)

	assert.NotNil(t, registry.allTools)
	assert.NotNil(t, registry.metadata)
	assert.NotNil(t, registry.loadedTools)
	assert.NotNil(t, registry.operationMap)
	assert.Equal(t, 0, registry.ToolCount())
	assert.Equal(t, 0, registry.LoadedCount())
}

func TestToolRegistry_BuildFromSpec(t *testing.T) {
	registry := NewToolRegistry()

	spec := createTestSpec()
	err := registry.BuildFromSpec(spec, nil)
	require.NoError(t, err)

	assert.Equal(t, 4, registry.ToolCount())

	// Check metadata
	metadata := registry.GetMetadata()
	assert.Len(t, metadata, 4)

	// Verify tool IDs
	ids := make(map[string]bool)
	for _, m := range metadata {
		ids[m.ID] = true
	}
	assert.True(t, ids["listPets"])
	assert.True(t, ids["createPet"])
	assert.True(t, ids["getPetById"])
	assert.True(t, ids["deletePet"])
}

func TestToolRegistry_BuildFromSpec_WithSafetyConfig(t *testing.T) {
	registry := NewToolRegistry()

	spec := createTestSpec()
	safetyConfig := &config.SafetyConfig{
		ReadOnlyMode: true,
	}

	err := registry.BuildFromSpec(spec, safetyConfig)
	require.NoError(t, err)

	// Only GET operations should be included
	assert.Equal(t, 2, registry.ToolCount())

	metadata := registry.GetMetadata()
	for _, m := range metadata {
		assert.Equal(t, "GET", m.Method)
	}
}

func TestToolRegistry_BuildFromSpec_WithDeniedOperations(t *testing.T) {
	registry := NewToolRegistry()

	spec := createTestSpec()
	safetyConfig := &config.SafetyConfig{
		DeniedOperations: []string{"deletePet"},
	}

	err := registry.BuildFromSpec(spec, safetyConfig)
	require.NoError(t, err)

	assert.Equal(t, 3, registry.ToolCount())

	_, ok := registry.GetTool("deletePet")
	assert.False(t, ok)
}

func TestToolRegistry_BuildFromSpec_WithAllowedOperations(t *testing.T) {
	registry := NewToolRegistry()

	spec := createTestSpec()
	safetyConfig := &config.SafetyConfig{
		AllowedOperations: []string{"listPets", "getPetById"},
	}

	err := registry.BuildFromSpec(spec, safetyConfig)
	require.NoError(t, err)

	assert.Equal(t, 2, registry.ToolCount())

	_, ok := registry.GetTool("listPets")
	assert.True(t, ok)

	_, ok = registry.GetTool("getPetById")
	assert.True(t, ok)

	_, ok = registry.GetTool("createPet")
	assert.False(t, ok)
}

func TestToolRegistry_Load(t *testing.T) {
	registry := NewToolRegistry()

	spec := createTestSpec()
	err := registry.BuildFromSpec(spec, nil)
	require.NoError(t, err)

	// Load a tool
	tool, fromCache, err := registry.Load("listPets")
	require.NoError(t, err)
	assert.False(t, fromCache)
	assert.Equal(t, "listPets", tool.Name)

	// Load again - should be from cache
	tool, fromCache, err = registry.Load("listPets")
	require.NoError(t, err)
	assert.True(t, fromCache)
	assert.Equal(t, "listPets", tool.Name)

	assert.Equal(t, 1, registry.LoadedCount())
}

func TestToolRegistry_Load_NotFound(t *testing.T) {
	registry := NewToolRegistry()

	spec := createTestSpec()
	err := registry.BuildFromSpec(spec, nil)
	require.NoError(t, err)

	_, _, err = registry.Load("nonExistent")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "tool not found")
}

func TestToolRegistry_IsLoaded(t *testing.T) {
	registry := NewToolRegistry()

	spec := createTestSpec()
	err := registry.BuildFromSpec(spec, nil)
	require.NoError(t, err)

	assert.False(t, registry.IsLoaded("listPets"))

	_, _, err = registry.Load("listPets")
	require.NoError(t, err)

	assert.True(t, registry.IsLoaded("listPets"))
	assert.False(t, registry.IsLoaded("createPet"))
}

func TestToolRegistry_GetLoadedTools(t *testing.T) {
	registry := NewToolRegistry()

	spec := createTestSpec()
	err := registry.BuildFromSpec(spec, nil)
	require.NoError(t, err)

	_, _, _ = registry.Load("listPets")
	_, _, _ = registry.Load("createPet")

	loaded := registry.GetLoadedTools()
	assert.Len(t, loaded, 2)
	assert.Contains(t, loaded, "listPets")
	assert.Contains(t, loaded, "createPet")
}

func TestToolRegistry_GetOperationInfo(t *testing.T) {
	registry := NewToolRegistry()

	spec := createTestSpec()
	err := registry.BuildFromSpec(spec, nil)
	require.NoError(t, err)

	info, ok := registry.GetOperationInfo("listPets")
	require.True(t, ok)
	assert.Equal(t, "GET", info.Method)
	assert.Equal(t, "/pets", info.Path)
	assert.NotNil(t, info.Operation)
}

func TestToolRegistry_GetOperationInfo_NotFound(t *testing.T) {
	registry := NewToolRegistry()

	spec := createTestSpec()
	err := registry.BuildFromSpec(spec, nil)
	require.NoError(t, err)

	_, ok := registry.GetOperationInfo("nonExistent")
	assert.False(t, ok)
}

func TestToolRegistry_GetTool(t *testing.T) {
	registry := NewToolRegistry()

	spec := createTestSpec()
	err := registry.BuildFromSpec(spec, nil)
	require.NoError(t, err)

	tool, ok := registry.GetTool("listPets")
	require.True(t, ok)
	assert.Equal(t, "listPets", tool.Name)

	// GetTool should not cache
	assert.False(t, registry.IsLoaded("listPets"))
}

func TestToolRegistry_ClearCache(t *testing.T) {
	registry := NewToolRegistry()

	spec := createTestSpec()
	err := registry.BuildFromSpec(spec, nil)
	require.NoError(t, err)

	_, _, _ = registry.Load("listPets")
	_, _, _ = registry.Load("createPet")
	assert.Equal(t, 2, registry.LoadedCount())

	registry.ClearCache()
	assert.Equal(t, 0, registry.LoadedCount())
	assert.False(t, registry.IsLoaded("listPets"))
}

func TestToolRegistry_BuildFromSpec_NilSpec(t *testing.T) {
	registry := NewToolRegistry()

	err := registry.BuildFromSpec(nil, nil)
	require.NoError(t, err)
	assert.Equal(t, 0, registry.ToolCount())
}

func TestToolRegistry_BuildFromSpec_PreservesCache(t *testing.T) {
	registry := NewToolRegistry()

	spec := createTestSpec()
	err := registry.BuildFromSpec(spec, nil)
	require.NoError(t, err)

	// Load some tools
	_, _, _ = registry.Load("listPets")
	assert.Equal(t, 1, registry.LoadedCount())

	// Rebuild from spec
	err = registry.BuildFromSpec(spec, nil)
	require.NoError(t, err)

	// Cache should be preserved
	assert.Equal(t, 1, registry.LoadedCount())
}

func createTestSpec() *openapi3.T {
	return createTestOpenAPISpec()
}
