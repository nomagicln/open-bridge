package mcp

import (
	"context"
	"fmt"
	"sync"
)

// AdaptiveEmbedder implements Embedder with automatic fallback between embedders.
// It tries embedders in order of preference: Ollama -> ONNX -> TF-IDF.
type AdaptiveEmbedder struct {
	cfg           EmbedderConfig
	activeEmbed   Embedder
	ollamaEmbed   *OllamaEmbedder
	onnxEmbed     Embedder // Will be *ONNXEmbedder when implemented
	tfidfEmbed    *TFIDFEmbedder
	mu            sync.RWMutex
	lastSelection EmbedderType
}

// NewAdaptiveEmbedder creates a new adaptive embedder that automatically
// selects the best available embedder.
func NewAdaptiveEmbedder(cfg EmbedderConfig) (*AdaptiveEmbedder, error) {
	ae := &AdaptiveEmbedder{
		cfg: cfg,
	}

	// Initialize TF-IDF as fallback (always available)
	tfidf, err := NewTFIDFEmbedder(cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to create TF-IDF embedder: %w", err)
	}
	ae.tfidfEmbed = tfidf

	// Initialize Ollama (may not be available)
	ollama, err := NewOllamaEmbedder(cfg)
	if err == nil {
		ae.ollamaEmbed = ollama
	}

	// ONNX will be initialized when implemented
	// For now, we skip it

	return ae, nil
}

// selectEmbedder chooses the best available embedder.
func (e *AdaptiveEmbedder) selectEmbedder(ctx context.Context) Embedder {
	e.mu.Lock()
	defer e.mu.Unlock()

	// Try Ollama first (best quality if available)
	if e.ollamaEmbed != nil && e.ollamaEmbed.IsAvailable(ctx) {
		if e.lastSelection != EmbedderOllama {
			e.lastSelection = EmbedderOllama
		}
		e.activeEmbed = e.ollamaEmbed
		return e.ollamaEmbed
	}

	// Try ONNX next (good quality, local)
	if e.onnxEmbed != nil {
		if e.lastSelection != EmbedderONNX {
			e.lastSelection = EmbedderONNX
		}
		e.activeEmbed = e.onnxEmbed
		return e.onnxEmbed
	}

	// Fall back to TF-IDF (always available)
	if e.lastSelection != EmbedderTFIDF {
		e.lastSelection = EmbedderTFIDF
	}
	e.activeEmbed = e.tfidfEmbed
	return e.tfidfEmbed
}

// Embed converts multiple texts into embedding vectors.
func (e *AdaptiveEmbedder) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	embedder := e.selectEmbedder(ctx)
	return embedder.Embed(ctx, texts)
}

// EmbedSingle embeds a single text.
func (e *AdaptiveEmbedder) EmbedSingle(ctx context.Context, text string) ([]float32, error) {
	embedder := e.selectEmbedder(ctx)
	return embedder.EmbedSingle(ctx, text)
}

// Dimension returns the embedding dimension of the active embedder.
func (e *AdaptiveEmbedder) Dimension() int {
	e.mu.RLock()
	defer e.mu.RUnlock()

	if e.activeEmbed != nil {
		return e.activeEmbed.Dimension()
	}
	return e.tfidfEmbed.Dimension()
}

// Type returns the embedder type.
func (e *AdaptiveEmbedder) Type() EmbedderType {
	return EmbedderAdaptive
}

// ActiveType returns the type of the currently active embedder.
func (e *AdaptiveEmbedder) ActiveType() EmbedderType {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.lastSelection
}

// Close releases resources held by all embedders.
func (e *AdaptiveEmbedder) Close() error {
	e.mu.Lock()
	defer e.mu.Unlock()

	var errs []error

	if e.ollamaEmbed != nil {
		if err := e.ollamaEmbed.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	if e.onnxEmbed != nil {
		if err := e.onnxEmbed.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	if e.tfidfEmbed != nil {
		if err := e.tfidfEmbed.Close(); err != nil {
			errs = append(errs, err)
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("errors closing embedders: %v", errs)
	}
	return nil
}

// Reconfigure updates the embedder configuration.
func (e *AdaptiveEmbedder) Reconfigure(cfg EmbedderConfig) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	e.cfg = cfg

	// Reconfigure all sub-embedders
	if e.ollamaEmbed != nil {
		if err := e.ollamaEmbed.Reconfigure(cfg); err != nil {
			return fmt.Errorf("failed to reconfigure Ollama embedder: %w", err)
		}
	}
	if e.tfidfEmbed != nil {
		if err := e.tfidfEmbed.Reconfigure(cfg); err != nil {
			return fmt.Errorf("failed to reconfigure TF-IDF embedder: %w", err)
		}
	}

	// Reset selection to trigger re-evaluation
	e.lastSelection = ""
	e.activeEmbed = nil

	return nil
}

// Config returns the current configuration.
func (e *AdaptiveEmbedder) Config() EmbedderConfig {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.cfg
}

// SetONNXEmbedder sets the ONNX embedder for the adaptive embedder.
// This is called when ONNX runtime becomes available.
func (e *AdaptiveEmbedder) SetONNXEmbedder(onnx Embedder) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.onnxEmbed = onnx
	// Reset selection to trigger re-evaluation
	e.lastSelection = ""
	e.activeEmbed = nil
}

// IndexDocuments indexes documents in the TF-IDF embedder for better results.
func (e *AdaptiveEmbedder) IndexDocuments(documents []string) {
	e.mu.RLock()
	tfidf := e.tfidfEmbed
	e.mu.RUnlock()

	if tfidf != nil {
		tfidf.IndexDocuments(documents)
	}
}
