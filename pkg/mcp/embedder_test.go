package mcp

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

//nolint:dupl // Similar test structure is intentional for different type parsing
func TestParseEmbedderType(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    EmbedderType
		wantErr bool
	}{
		{
			name:  "tfidf type",
			input: "tfidf",
			want:  EmbedderTFIDF,
		},
		{
			name:  "ollama type",
			input: "ollama",
			want:  EmbedderOllama,
		},
		{
			name:  "onnx type",
			input: "onnx",
			want:  EmbedderONNX,
		},
		{
			name:  "adaptive type",
			input: "adaptive",
			want:  EmbedderAdaptive,
		},
		{
			name:  "auto type maps to adaptive",
			input: "auto",
			want:  EmbedderAdaptive,
		},
		{
			name:    "invalid type returns error",
			input:   "invalid",
			wantErr: true,
		},
		{
			name:    "empty string returns error",
			input:   "",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseEmbedderType(tt.input)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestDefaultEmbedderConfig(t *testing.T) {
	cfg := DefaultEmbedderConfig()

	assert.Equal(t, EmbedderAdaptive, cfg.Type)
	assert.Equal(t, "http://localhost:11434", cfg.OllamaEndpoint)
	assert.Equal(t, "nomic-embed-text", cfg.OllamaModel)
	assert.Equal(t, 0, cfg.Dimension) // Auto-detect
	assert.Equal(t, 32, cfg.BatchSize)
	assert.Equal(t, 30*time.Second, cfg.Timeout)
}

func TestEmbedderFactory_Create_TFIDF(t *testing.T) {
	factory := NewEmbedderFactory()

	cfg := EmbedderConfig{
		Type:      EmbedderTFIDF,
		Dimension: 100,
	}

	embedder, err := factory.Create(cfg)
	require.NoError(t, err)
	require.NotNil(t, embedder)
	defer func() { _ = embedder.Close() }()

	assert.Equal(t, EmbedderTFIDF, embedder.Type())
}

func TestEmbedderFactory_Create_UnknownType(t *testing.T) {
	factory := NewEmbedderFactory()

	cfg := EmbedderConfig{
		Type: "unknown",
	}

	embedder, err := factory.Create(cfg)
	require.Error(t, err)
	assert.Nil(t, embedder)
	assert.Contains(t, err.Error(), "unknown embedder type")
}

func TestNewEmbedder_Convenience(t *testing.T) {
	cfg := EmbedderConfig{
		Type:      EmbedderTFIDF,
		Dimension: 50,
	}

	embedder, err := NewEmbedder(cfg)
	require.NoError(t, err)
	require.NotNil(t, embedder)
	defer func() { _ = embedder.Close() }()

	assert.Equal(t, EmbedderTFIDF, embedder.Type())
}

func TestTFIDFEmbedder_Embed(t *testing.T) {
	cfg := EmbedderConfig{
		Type:      EmbedderTFIDF,
		Dimension: 100,
	}

	embedder, err := NewTFIDFEmbedder(cfg)
	require.NoError(t, err)
	defer func() { _ = embedder.Close() }()

	ctx := context.Background()
	texts := []string{
		"The quick brown fox jumps over the lazy dog",
		"A fast brown fox leaps over a sleeping dog",
		"Machine learning is a subset of artificial intelligence",
	}

	vectors, err := embedder.Embed(ctx, texts)
	require.NoError(t, err)
	require.Len(t, vectors, 3)

	// Check that vectors have correct dimension
	for i, vec := range vectors {
		assert.Greater(t, len(vec), 0, "vector %d should have non-zero dimension", i)
	}

	// Similar texts should have higher similarity
	sim1 := CosineSimilarity(vectors[0], vectors[1]) // fox sentences
	sim2 := CosineSimilarity(vectors[0], vectors[2]) // fox vs ML

	assert.Greater(t, sim1, sim2, "similar texts should have higher cosine similarity")
}

func TestTFIDFEmbedder_EmbedSingle(t *testing.T) {
	cfg := EmbedderConfig{
		Type:      EmbedderTFIDF,
		Dimension: 50,
	}

	embedder, err := NewTFIDFEmbedder(cfg)
	require.NoError(t, err)
	defer func() { _ = embedder.Close() }()

	ctx := context.Background()
	text := "Hello world this is a test"

	vec, err := embedder.EmbedSingle(ctx, text)
	require.NoError(t, err)
	assert.Greater(t, len(vec), 0)
}

func TestTFIDFEmbedder_EmptyInput(t *testing.T) {
	cfg := EmbedderConfig{
		Type:      EmbedderTFIDF,
		Dimension: 50,
	}

	embedder, err := NewTFIDFEmbedder(cfg)
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

func TestTFIDFEmbedder_Reconfigure(t *testing.T) {
	cfg := EmbedderConfig{
		Type:      EmbedderTFIDF,
		Dimension: 50,
	}

	embedder, err := NewTFIDFEmbedder(cfg)
	require.NoError(t, err)
	defer func() { _ = embedder.Close() }()

	// Initial config
	assert.Equal(t, 50, embedder.Dimension())

	// Reconfigure with new dimension
	newCfg := EmbedderConfig{
		Type:      EmbedderTFIDF,
		Dimension: 100,
	}

	err = embedder.Reconfigure(newCfg)
	require.NoError(t, err)
	assert.Equal(t, 100, embedder.Dimension())

	// Check Config() returns updated config
	currentCfg := embedder.Config()
	assert.Equal(t, 100, currentCfg.Dimension)
}

func TestTFIDFEmbedder_IndexDocuments(t *testing.T) {
	cfg := EmbedderConfig{
		Type:      EmbedderTFIDF,
		Dimension: 1000,
	}

	embedder, err := NewTFIDFEmbedder(cfg)
	require.NoError(t, err)
	defer func() { _ = embedder.Close() }()

	// Index some documents first
	documents := []string{
		"pet store api for managing pets",
		"user management create update delete",
		"order processing and payment handling",
	}
	embedder.IndexDocuments(documents)

	// Now embed a query
	ctx := context.Background()
	query := "create a new pet"

	vec, err := embedder.EmbedSingle(ctx, query)
	require.NoError(t, err)
	assert.Greater(t, len(vec), 0)

	// The dimension should match the vocabulary size
	assert.Equal(t, embedder.Dimension(), len(vec))
}

func TestCosineSimilarity(t *testing.T) {
	tests := []struct {
		name     string
		a        []float32
		b        []float32
		expected float64
	}{
		{
			name:     "identical vectors",
			a:        []float32{1, 0, 0},
			b:        []float32{1, 0, 0},
			expected: 1.0,
		},
		{
			name:     "orthogonal vectors",
			a:        []float32{1, 0, 0},
			b:        []float32{0, 1, 0},
			expected: 0.0,
		},
		{
			name:     "opposite vectors",
			a:        []float32{1, 0, 0},
			b:        []float32{-1, 0, 0},
			expected: -1.0,
		},
		{
			name:     "different lengths returns zero",
			a:        []float32{1, 0},
			b:        []float32{1, 0, 0},
			expected: 0.0,
		},
		{
			name:     "zero vector returns zero",
			a:        []float32{0, 0, 0},
			b:        []float32{1, 0, 0},
			expected: 0.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := CosineSimilarity(tt.a, tt.b)
			assert.InDelta(t, tt.expected, got, 0.0001)
		})
	}
}

func TestTfidfTokenize(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected []string
	}{
		{
			name:     "simple sentence",
			input:    "Hello World",
			expected: []string{"hello", "world"},
		},
		{
			name:     "with punctuation",
			input:    "Hello, World! How are you?",
			expected: []string{"hello", "world", "how", "are", "you"},
		},
		{
			name:     "with numbers",
			input:    "API v2.0 endpoint",
			expected: []string{"api", "v2", "0", "endpoint"},
		},
		{
			name:     "empty string",
			input:    "",
			expected: nil,
		},
		{
			name:     "only punctuation",
			input:    "!@#$%",
			expected: nil,
		},
		{
			name:     "mixed case",
			input:    "CamelCase UPPERCASE lowercase",
			expected: []string{"camelcase", "uppercase", "lowercase"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tfidfTokenize(tt.input)
			assert.Equal(t, tt.expected, got)
		})
	}
}
