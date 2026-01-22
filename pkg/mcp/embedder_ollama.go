package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"
)

// OllamaEmbedder implements Embedder using Ollama API for embedding generation.
type OllamaEmbedder struct {
	cfg        EmbedderConfig
	client     *http.Client
	dimension  int
	mu         sync.RWMutex
	available  bool
	lastCheck  time.Time
	checkMutex sync.Mutex
}

// ollamaEmbedRequest represents the request body for Ollama embeddings API.
type ollamaEmbedRequest struct {
	Model  string `json:"model"`
	Prompt string `json:"prompt"`
}

// ollamaEmbedResponse represents the response from Ollama embeddings API.
type ollamaEmbedResponse struct {
	Embedding []float64 `json:"embedding"`
}

// NewOllamaEmbedder creates a new Ollama-based embedder.
func NewOllamaEmbedder(cfg EmbedderConfig) (*OllamaEmbedder, error) {
	if cfg.OllamaEndpoint == "" {
		cfg.OllamaEndpoint = "http://localhost:11434"
	}
	if cfg.OllamaModel == "" {
		cfg.OllamaModel = "nomic-embed-text"
	}
	if cfg.Timeout == 0 {
		cfg.Timeout = 30 * time.Second
	}

	client := &http.Client{
		Timeout: cfg.Timeout,
	}

	return &OllamaEmbedder{
		cfg:       cfg,
		client:    client,
		dimension: cfg.Dimension,
		available: false,
	}, nil
}

// Embed converts multiple texts into embedding vectors using Ollama API.
func (e *OllamaEmbedder) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return [][]float32{}, nil
	}

	vectors := make([][]float32, len(texts))

	// Process in batches if needed
	batchSize := e.cfg.BatchSize
	if batchSize <= 0 {
		batchSize = 32
	}

	for i := 0; i < len(texts); i += batchSize {
		end := min(i+batchSize, len(texts))

		batch := texts[i:end]
		for j, text := range batch {
			vec, err := e.embedSingle(ctx, text)
			if err != nil {
				return nil, fmt.Errorf("failed to embed text %d: %w", i+j, err)
			}
			vectors[i+j] = vec
		}
	}

	return vectors, nil
}

// embedSingle embeds a single text using Ollama API.
//
//nolint:funlen // HTTP request handling logic requires more lines for proper error handling
func (e *OllamaEmbedder) embedSingle(ctx context.Context, text string) ([]float32, error) {
	e.mu.RLock()
	endpoint := e.cfg.OllamaEndpoint
	model := e.cfg.OllamaModel
	e.mu.RUnlock()

	url := fmt.Sprintf("%s/api/embeddings", endpoint)

	reqBody := ollamaEmbedRequest{
		Model:  model,
		Prompt: text,
	}

	jsonBody, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(jsonBody))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := e.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("ollama API error (status %d): %s", resp.StatusCode, string(body))
	}

	var embedResp ollamaEmbedResponse
	if err := json.NewDecoder(resp.Body).Decode(&embedResp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	// Convert float64 to float32
	vec := make([]float32, len(embedResp.Embedding))
	for i, v := range embedResp.Embedding {
		vec[i] = float32(v)
	}

	// Update dimension if not set
	e.mu.Lock()
	if e.dimension == 0 && len(vec) > 0 {
		e.dimension = len(vec)
	}
	e.available = true
	e.lastCheck = time.Now()
	e.mu.Unlock()

	return vec, nil
}

// EmbedSingle embeds a single text.
func (e *OllamaEmbedder) EmbedSingle(ctx context.Context, text string) ([]float32, error) {
	return e.embedSingle(ctx, text)
}

// Dimension returns the embedding dimension.
func (e *OllamaEmbedder) Dimension() int {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.dimension
}

// Type returns the embedder type.
func (e *OllamaEmbedder) Type() EmbedderType {
	return EmbedderOllama
}

// Close releases resources.
func (e *OllamaEmbedder) Close() error {
	e.client.CloseIdleConnections()
	return nil
}

// Reconfigure updates the embedder configuration.
func (e *OllamaEmbedder) Reconfigure(cfg EmbedderConfig) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if cfg.OllamaEndpoint != "" {
		e.cfg.OllamaEndpoint = cfg.OllamaEndpoint
	}
	if cfg.OllamaModel != "" {
		e.cfg.OllamaModel = cfg.OllamaModel
	}
	if cfg.Timeout > 0 {
		e.cfg.Timeout = cfg.Timeout
		e.client.Timeout = cfg.Timeout
	}
	if cfg.Dimension > 0 {
		e.dimension = cfg.Dimension
	}
	if cfg.BatchSize > 0 {
		e.cfg.BatchSize = cfg.BatchSize
	}

	// Reset availability check
	e.available = false
	e.lastCheck = time.Time{}

	return nil
}

// Config returns the current configuration.
func (e *OllamaEmbedder) Config() EmbedderConfig {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.cfg
}

// IsAvailable checks if Ollama is available and responding.
func (e *OllamaEmbedder) IsAvailable(ctx context.Context) bool {
	e.checkMutex.Lock()
	defer e.checkMutex.Unlock()

	// Cache the availability check for 30 seconds
	e.mu.RLock()
	if e.available && time.Since(e.lastCheck) < 30*time.Second {
		e.mu.RUnlock()
		return true
	}
	endpoint := e.cfg.OllamaEndpoint
	e.mu.RUnlock()

	// Check if Ollama is responding
	url := fmt.Sprintf("%s/api/tags", endpoint)

	reqCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, url, nil)
	if err != nil {
		return false
	}

	resp, err := e.client.Do(req)
	if err != nil {
		e.mu.Lock()
		e.available = false
		e.mu.Unlock()
		return false
	}
	defer func() { _ = resp.Body.Close() }()

	available := resp.StatusCode == http.StatusOK

	e.mu.Lock()
	e.available = available
	e.lastCheck = time.Now()
	e.mu.Unlock()

	return available
}
