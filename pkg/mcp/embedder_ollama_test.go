package mcp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewOllamaEmbedder(t *testing.T) {
	cfg := EmbedderConfig{
		Type:           EmbedderOllama,
		OllamaEndpoint: "http://localhost:11434",
		OllamaModel:    "nomic-embed-text",
		Timeout:        10 * time.Second,
	}

	embedder, err := NewOllamaEmbedder(cfg)
	require.NoError(t, err)
	require.NotNil(t, embedder)
	defer func() { require.NoError(t, embedder.Close()) }()

	assert.Equal(t, EmbedderOllama, embedder.Type())
}

func TestOllamaEmbedder_IsAvailable(t *testing.T) {
	// Create mock server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	cfg := EmbedderConfig{
		Type:           EmbedderOllama,
		OllamaEndpoint: server.URL,
		OllamaModel:    "test-model",
		Timeout:        5 * time.Second,
	}

	embedder, err := NewOllamaEmbedder(cfg)
	require.NoError(t, err)
	defer func() { require.NoError(t, embedder.Close()) }()

	ctx := context.Background()
	available := embedder.IsAvailable(ctx)
	assert.True(t, available)
}

func TestOllamaEmbedder_IsAvailable_Unavailable(t *testing.T) {
	cfg := EmbedderConfig{
		Type:           EmbedderOllama,
		OllamaEndpoint: "http://localhost:99999", // Non-existent
		OllamaModel:    "test-model",
		Timeout:        1 * time.Second,
	}

	embedder, err := NewOllamaEmbedder(cfg)
	require.NoError(t, err)
	defer func() { require.NoError(t, embedder.Close()) }()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	available := embedder.IsAvailable(ctx)
	assert.False(t, available)
}

func TestOllamaEmbedder_Embed(t *testing.T) {
	// Create mock server that returns embeddings
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/embeddings" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			// Return mock embedding (single response for single text)
			_, _ = w.Write([]byte(`{"embedding":[0.1,0.2,0.3,0.4,0.5]}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	cfg := EmbedderConfig{
		Type:           EmbedderOllama,
		OllamaEndpoint: server.URL,
		OllamaModel:    "test-model",
		Timeout:        5 * time.Second,
	}

	embedder, err := NewOllamaEmbedder(cfg)
	require.NoError(t, err)
	defer func() { require.NoError(t, embedder.Close()) }()

	ctx := context.Background()
	texts := []string{"hello world"}

	vectors, err := embedder.Embed(ctx, texts)
	require.NoError(t, err)
	require.Len(t, vectors, 1)
	assert.Len(t, vectors[0], 5)
}

func TestOllamaEmbedder_EmbedSingle(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/embeddings" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"embedding":[0.5,0.5,0.5]}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	cfg := EmbedderConfig{
		Type:           EmbedderOllama,
		OllamaEndpoint: server.URL,
		OllamaModel:    "test-model",
		Timeout:        5 * time.Second,
	}

	embedder, err := NewOllamaEmbedder(cfg)
	require.NoError(t, err)
	defer func() { require.NoError(t, embedder.Close()) }()

	ctx := context.Background()
	vec, err := embedder.EmbedSingle(ctx, "test")
	require.NoError(t, err)
	assert.Len(t, vec, 3)
}

func TestOllamaEmbedder_Embed_Error(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":"model not found"}`))
	}))
	defer server.Close()

	cfg := EmbedderConfig{
		Type:           EmbedderOllama,
		OllamaEndpoint: server.URL,
		OllamaModel:    "nonexistent-model",
		Timeout:        5 * time.Second,
	}

	embedder, err := NewOllamaEmbedder(cfg)
	require.NoError(t, err)
	defer func() { require.NoError(t, embedder.Close()) }()

	ctx := context.Background()
	_, err = embedder.Embed(ctx, []string{"test"})
	require.Error(t, err)
}

func TestOllamaEmbedder_Embed_EmptyInput(t *testing.T) {
	cfg := EmbedderConfig{
		Type:           EmbedderOllama,
		OllamaEndpoint: "http://localhost:11434",
		OllamaModel:    "test-model",
		Timeout:        5 * time.Second,
	}

	embedder, err := NewOllamaEmbedder(cfg)
	require.NoError(t, err)
	defer func() { _ = embedder.Close() }()

	ctx := context.Background()
	vectors, err := embedder.Embed(ctx, []string{})
	require.NoError(t, err)
	assert.Empty(t, vectors)
}

func TestOllamaEmbedder_Reconfigure(t *testing.T) {
	cfg := EmbedderConfig{
		Type:           EmbedderOllama,
		OllamaEndpoint: "http://localhost:11434",
		OllamaModel:    "old-model",
		Timeout:        5 * time.Second,
	}

	embedder, err := NewOllamaEmbedder(cfg)
	require.NoError(t, err)
	defer func() { _ = embedder.Close() }()

	// Reconfigure with new model
	newCfg := EmbedderConfig{
		Type:           EmbedderOllama,
		OllamaEndpoint: "http://localhost:11434",
		OllamaModel:    "new-model",
		Timeout:        10 * time.Second,
	}

	err = embedder.Reconfigure(newCfg)
	require.NoError(t, err)

	// Check config was updated
	currentCfg := embedder.Config()
	assert.Equal(t, "new-model", currentCfg.OllamaModel)
	assert.Equal(t, 10*time.Second, currentCfg.Timeout)
}

func TestOllamaEmbedder_Config(t *testing.T) {
	cfg := EmbedderConfig{
		Type:           EmbedderOllama,
		OllamaEndpoint: "http://test:11434",
		OllamaModel:    "test-model",
		Dimension:      768,
		Timeout:        5 * time.Second,
	}

	embedder, err := NewOllamaEmbedder(cfg)
	require.NoError(t, err)
	defer func() { _ = embedder.Close() }()

	got := embedder.Config()
	assert.Equal(t, cfg.OllamaEndpoint, got.OllamaEndpoint)
	assert.Equal(t, cfg.OllamaModel, got.OllamaModel)
}

func TestOllamaEmbedder_BatchProcessing(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.Header().Set("Content-Type", "application/json")
		// Return single embedding for each request (current implementation calls embedSingle for each text)
		_, _ = w.Write([]byte(`{"embedding":[0.1,0.2]}`))
	}))
	defer server.Close()

	cfg := EmbedderConfig{
		Type:           EmbedderOllama,
		OllamaEndpoint: server.URL,
		OllamaModel:    "test-model",
		BatchSize:      10, // Process all in one batch
		Timeout:        5 * time.Second,
	}

	embedder, err := NewOllamaEmbedder(cfg)
	require.NoError(t, err)
	defer func() { _ = embedder.Close() }()

	ctx := context.Background()
	texts := []string{"text1", "text2", "text3"}

	vectors, err := embedder.Embed(ctx, texts)
	require.NoError(t, err)
	assert.Len(t, vectors, 3)
	// Current implementation calls embedSingle for each text
	assert.Equal(t, 3, callCount, "should make one API call per text")
}

func TestOllamaEmbedder_Dimension(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"embedding":[0.1,0.2,0.3,0.4,0.5,0.6,0.7,0.8]}`))
	}))
	defer server.Close()

	cfg := EmbedderConfig{
		Type:           EmbedderOllama,
		OllamaEndpoint: server.URL,
		OllamaModel:    "test-model",
		Dimension:      0, // Auto-detect
		Timeout:        5 * time.Second,
	}

	embedder, err := NewOllamaEmbedder(cfg)
	require.NoError(t, err)
	defer func() { _ = embedder.Close() }()

	// Before embedding, dimension is unknown or default
	initialDim := embedder.Dimension()

	ctx := context.Background()
	vec, err := embedder.EmbedSingle(ctx, "test")
	require.NoError(t, err)

	// After embedding, dimension should be detected from the response
	finalDim := embedder.Dimension()
	// Dimension should match the vector length
	assert.Equal(t, len(vec), finalDim, "dimension should match vector length")
	assert.True(t, finalDim > 0 || initialDim > 0, "dimension should be set")
}
