package mcp

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewONNXEmbedder(t *testing.T) {
	// Note: This test creates an ONNX embedder that falls back to word vectors
	// since we don't have an actual ONNX model in tests.
	// Use short timeout to quickly fall back without attempting real download.
	cfg := EmbedderConfig{
		Type:          EmbedderONNX,
		ONNXModelPath: filepath.Join(t.TempDir(), "all-MiniLM-L6-v2"),
		Dimension:     384,
		Timeout:       100 * time.Millisecond,
	}

	embedder, err := NewONNXEmbedder(cfg)
	require.NoError(t, err)
	require.NotNil(t, embedder)
	defer func() { _ = embedder.Close() }()

	assert.Equal(t, EmbedderONNX, embedder.Type())
}

func TestONNXEmbedder_FallbackMode(t *testing.T) {
	// Test that ONNX embedder works in fallback mode without actual model.
	// Use short timeout to quickly fall back without attempting real download.
	cfg := EmbedderConfig{
		Type:          EmbedderONNX,
		ONNXModelPath: filepath.Join(t.TempDir(), "nonexistent-model"),
		Dimension:     100,
		Timeout:       100 * time.Millisecond,
	}

	embedder, err := NewONNXEmbedder(cfg)
	require.NoError(t, err)
	defer func() { _ = embedder.Close() }()

	ctx := context.Background()
	texts := []string{"hello world", "test text"}

	vectors, err := embedder.Embed(ctx, texts)
	require.NoError(t, err)
	assert.Len(t, vectors, 2)

	// Vectors should have correct dimension
	for _, vec := range vectors {
		assert.Equal(t, cfg.Dimension, len(vec))
	}
}

func TestONNXEmbedder_EmbedSingle(t *testing.T) {
	cfg := EmbedderConfig{
		Type:      EmbedderONNX,
		Dimension: 50,
		Timeout:   100 * time.Millisecond,
	}

	embedder, err := NewONNXEmbedder(cfg)
	require.NoError(t, err)
	defer func() { _ = embedder.Close() }()

	ctx := context.Background()
	vec, err := embedder.EmbedSingle(ctx, "test text")
	require.NoError(t, err)
	assert.Len(t, vec, 50)
}

func TestONNXEmbedder_EmptyInput(t *testing.T) {
	cfg := EmbedderConfig{
		Type:      EmbedderONNX,
		Dimension: 50,
		Timeout:   100 * time.Millisecond,
	}

	embedder, err := NewONNXEmbedder(cfg)
	require.NoError(t, err)
	defer func() { _ = embedder.Close() }()

	ctx := context.Background()

	// Empty slice
	vectors, err := embedder.Embed(ctx, []string{})
	require.NoError(t, err)
	assert.Empty(t, vectors)

	// Empty string
	vec, err := embedder.EmbedSingle(ctx, "")
	require.NoError(t, err)
	assert.NotNil(t, vec)
}

func TestONNXEmbedder_Reconfigure(t *testing.T) {
	cfg := EmbedderConfig{
		Type:      EmbedderONNX,
		Dimension: 50,
		Timeout:   100 * time.Millisecond,
	}

	embedder, err := NewONNXEmbedder(cfg)
	require.NoError(t, err)
	defer func() { _ = embedder.Close() }()

	assert.Equal(t, 50, embedder.Dimension())

	// Reconfigure
	newCfg := EmbedderConfig{
		Type:      EmbedderONNX,
		Dimension: 100,
		Timeout:   100 * time.Millisecond,
	}

	err = embedder.Reconfigure(newCfg)
	require.NoError(t, err)
	assert.Equal(t, 100, embedder.Dimension())

	// Config should be updated
	currentCfg := embedder.Config()
	assert.Equal(t, 100, currentCfg.Dimension)
}

func TestONNXEmbedder_Dimension(t *testing.T) {
	cfg := EmbedderConfig{
		Type:      EmbedderONNX,
		Dimension: 128,
		Timeout:   100 * time.Millisecond,
	}

	embedder, err := NewONNXEmbedder(cfg)
	require.NoError(t, err)
	defer func() { _ = embedder.Close() }()

	assert.Equal(t, 128, embedder.Dimension())
}

func TestONNXEmbedder_Close(t *testing.T) {
	cfg := EmbedderConfig{
		Type:      EmbedderONNX,
		Dimension: 50,
		Timeout:   100 * time.Millisecond,
	}

	embedder, err := NewONNXEmbedder(cfg)
	require.NoError(t, err)

	err = embedder.Close()
	require.NoError(t, err)

	// Double close should not error
	err = embedder.Close()
	require.NoError(t, err)
}

func TestONNXEmbedder_ModelPath(t *testing.T) {
	customModelPath := filepath.Join(t.TempDir(), "custom-model")
	cfg := EmbedderConfig{
		Type:          EmbedderONNX,
		ONNXModelPath: customModelPath,
		Dimension:     256,
	}

	embedder, err := NewONNXEmbedder(cfg)
	require.NoError(t, err)
	defer func() { _ = embedder.Close() }()

	// ModelPath should return expected path
	modelPath := embedder.ModelPath()
	assert.Equal(t, customModelPath, modelPath)
}

func TestONNXEmbedder_SimilarTexts(t *testing.T) {
	cfg := EmbedderConfig{
		Type:      EmbedderONNX,
		Dimension: 200,
		Timeout:   100 * time.Millisecond,
	}

	embedder, err := NewONNXEmbedder(cfg)
	require.NoError(t, err)
	defer func() { _ = embedder.Close() }()

	ctx := context.Background()
	texts := []string{
		"The quick brown fox jumps over the lazy dog",
		"A fast brown fox leaps over a sleepy dog",
		"Machine learning is a branch of artificial intelligence",
	}

	vectors, err := embedder.Embed(ctx, texts)
	require.NoError(t, err)

	// Similar texts should have higher similarity
	sim1 := CosineSimilarity(vectors[0], vectors[1]) // fox sentences
	sim2 := CosineSimilarity(vectors[0], vectors[2]) // fox vs ML

	// The first two texts are more similar
	assert.Greater(t, sim1, sim2, "similar texts should have higher cosine similarity")
}
