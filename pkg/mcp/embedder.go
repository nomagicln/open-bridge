// Package mcp provides MCP (Model Context Protocol) server functionality.
package mcp

import (
	"context"
	"fmt"
	"time"
)

// EmbedderType represents the type of embedder to use for vector generation.
type EmbedderType string

const (
	// EmbedderTFIDF uses local TF-IDF for lightweight embedding.
	EmbedderTFIDF EmbedderType = "tfidf"
	// EmbedderOllama uses Ollama API for embedding generation.
	EmbedderOllama EmbedderType = "ollama"
	// EmbedderONNX uses local ONNX Runtime for embedding generation.
	EmbedderONNX EmbedderType = "onnx"
	// EmbedderAdaptive automatically selects the best available embedder.
	EmbedderAdaptive EmbedderType = "adaptive"
)

// EmbedderConfig contains configuration for embedders.
type EmbedderConfig struct {
	// Type specifies the embedder type to use.
	Type EmbedderType `yaml:"type" json:"type"`

	// OllamaEndpoint is the Ollama API endpoint (e.g., "http://localhost:11434").
	OllamaEndpoint string `yaml:"ollama_endpoint,omitempty" json:"ollama_endpoint,omitempty"`

	// OllamaModel is the Ollama model to use (e.g., "nomic-embed-text").
	OllamaModel string `yaml:"ollama_model,omitempty" json:"ollama_model,omitempty"`

	// ONNXModelPath is the path to the ONNX model file.
	// If empty, the model will be auto-downloaded to ~/.openbridge/models/.
	ONNXModelPath string `yaml:"onnx_model_path,omitempty" json:"onnx_model_path,omitempty"`

	// Dimension is the expected embedding dimension.
	// If 0, it will be auto-detected from the embedder.
	Dimension int `yaml:"dimension,omitempty" json:"dimension,omitempty"`

	// BatchSize is the maximum number of texts to embed in a single batch.
	BatchSize int `yaml:"batch_size,omitempty" json:"batch_size,omitempty"`

	// Timeout is the timeout for embedding operations.
	Timeout time.Duration `yaml:"timeout,omitempty" json:"timeout,omitempty"`
}

// DefaultEmbedderConfig returns the default embedder configuration.
func DefaultEmbedderConfig() EmbedderConfig {
	return EmbedderConfig{
		Type:           EmbedderAdaptive,
		OllamaEndpoint: "http://localhost:11434",
		OllamaModel:    "nomic-embed-text",
		Dimension:      0, // Auto-detect
		BatchSize:      32,
		Timeout:        30 * time.Second,
	}
}

// Embedder defines the interface for text embedding generation.
type Embedder interface {
	// Embed converts multiple texts into embedding vectors.
	// Returns a slice of vectors, one for each input text.
	Embed(ctx context.Context, texts []string) ([][]float32, error)

	// EmbedSingle embeds a single text and returns its vector.
	EmbedSingle(ctx context.Context, text string) ([]float32, error)

	// Dimension returns the embedding vector dimension.
	Dimension() int

	// Type returns the embedder type.
	Type() EmbedderType

	// Close releases any resources held by the embedder.
	Close() error
}

// ConfigurableEmbedder extends Embedder with runtime reconfiguration support.
type ConfigurableEmbedder interface {
	Embedder

	// Reconfigure updates the embedder configuration at runtime.
	// Returns error if the new configuration is invalid or cannot be applied.
	Reconfigure(cfg EmbedderConfig) error

	// Config returns the current configuration.
	Config() EmbedderConfig
}

// EmbedderFactory creates embedders based on configuration.
type EmbedderFactory struct{}

// NewEmbedderFactory creates a new embedder factory.
func NewEmbedderFactory() *EmbedderFactory {
	return &EmbedderFactory{}
}

// Create creates an embedder based on the given configuration.
func (f *EmbedderFactory) Create(cfg EmbedderConfig) (Embedder, error) {
	switch cfg.Type {
	case EmbedderTFIDF:
		return NewTFIDFEmbedder(cfg)
	case EmbedderOllama:
		return NewOllamaEmbedder(cfg)
	case EmbedderONNX:
		return NewONNXEmbedder(cfg)
	case EmbedderAdaptive:
		return NewAdaptiveEmbedder(cfg)
	default:
		return nil, fmt.Errorf("unknown embedder type: %s", cfg.Type)
	}
}

// NewEmbedder creates an embedder with the given configuration.
// This is a convenience function that uses the default factory.
func NewEmbedder(cfg EmbedderConfig) (Embedder, error) {
	return NewEmbedderFactory().Create(cfg)
}

// ParseEmbedderType parses a string into an EmbedderType.
func ParseEmbedderType(s string) (EmbedderType, error) {
	switch s {
	case "tfidf":
		return EmbedderTFIDF, nil
	case "ollama":
		return EmbedderOllama, nil
	case "onnx":
		return EmbedderONNX, nil
	case "adaptive", "auto":
		return EmbedderAdaptive, nil
	default:
		return "", fmt.Errorf("invalid embedder type: %s (valid: tfidf, ollama, onnx, adaptive)", s)
	}
}
