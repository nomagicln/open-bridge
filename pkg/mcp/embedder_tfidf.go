package mcp

import (
	"context"
	"math"
	"sync"
)

// TFIDFEmbedder implements Embedder using TF-IDF for lightweight local embedding.
// This is a simple baseline that works offline without external dependencies.
type TFIDFEmbedder struct {
	cfg        EmbedderConfig
	vocabulary map[string]int
	idf        map[string]float64
	dimension  int
	mu         sync.RWMutex
}

// NewTFIDFEmbedder creates a new TF-IDF based embedder.
func NewTFIDFEmbedder(cfg EmbedderConfig) (*TFIDFEmbedder, error) {
	dim := cfg.Dimension
	if dim <= 0 {
		dim = 1000 // Default vocabulary size for TF-IDF
	}

	return &TFIDFEmbedder{
		cfg:        cfg,
		vocabulary: make(map[string]int),
		idf:        make(map[string]float64),
		dimension:  dim,
	}, nil
}

// Embed converts multiple texts into TF-IDF vectors.
func (e *TFIDFEmbedder) Embed(_ context.Context, texts []string) ([][]float32, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	// Build vocabulary from input texts if empty
	if len(e.vocabulary) == 0 {
		e.buildVocabulary(texts)
	}

	vectors := make([][]float32, len(texts))
	for i, text := range texts {
		vectors[i] = e.textToVector(text)
	}

	return vectors, nil
}

// EmbedSingle embeds a single text.
func (e *TFIDFEmbedder) EmbedSingle(ctx context.Context, text string) ([]float32, error) {
	vectors, err := e.Embed(ctx, []string{text})
	if err != nil {
		return nil, err
	}
	return vectors[0], nil
}

// Dimension returns the embedding dimension.
func (e *TFIDFEmbedder) Dimension() int {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.dimension
}

// Type returns the embedder type.
func (e *TFIDFEmbedder) Type() EmbedderType {
	return EmbedderTFIDF
}

// Close releases resources (no-op for TF-IDF).
func (e *TFIDFEmbedder) Close() error {
	return nil
}

// Reconfigure updates the embedder configuration.
func (e *TFIDFEmbedder) Reconfigure(cfg EmbedderConfig) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if cfg.Dimension > 0 {
		e.dimension = cfg.Dimension
	}
	e.cfg = cfg

	// Clear vocabulary to rebuild on next embed
	e.vocabulary = make(map[string]int)
	e.idf = make(map[string]float64)

	return nil
}

// Config returns the current configuration.
func (e *TFIDFEmbedder) Config() EmbedderConfig {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.cfg
}

// IndexDocuments pre-indexes a set of documents to build the vocabulary.
// This should be called before embedding queries for better results.
func (e *TFIDFEmbedder) IndexDocuments(documents []string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.buildVocabulary(documents)
}

// buildVocabulary creates the vocabulary and IDF values from documents.
func (e *TFIDFEmbedder) buildVocabulary(documents []string) {
	// Count document frequency for each term
	docFreq := make(map[string]int)

	for _, doc := range documents {
		tokens := tfidfTokenize(doc)

		// Get unique tokens for this document
		seen := make(map[string]bool)
		for _, token := range tokens {
			if !seen[token] {
				docFreq[token]++
				seen[token] = true
			}
		}
	}

	// Build vocabulary and calculate IDF
	e.vocabulary = make(map[string]int)
	e.idf = make(map[string]float64)

	idx := 0
	n := float64(len(documents))
	if n == 0 {
		n = 1 // Avoid division by zero
	}

	// Sort terms by document frequency and take top N
	type termFreq struct {
		term string
		freq int
	}
	terms := make([]termFreq, 0, len(docFreq))
	for term, freq := range docFreq {
		terms = append(terms, termFreq{term, freq})
	}

	// Take top terms up to dimension limit
	maxTerms := min(len(terms), e.dimension)

	for i := range maxTerms {
		term := terms[i].term
		df := terms[i].freq
		e.vocabulary[term] = idx
		// IDF = log(N / df) + 1 (smoothed)
		e.idf[term] = math.Log(n/float64(df)) + 1.0
		idx++
	}

	// Update actual dimension
	e.dimension = len(e.vocabulary)
	if e.dimension == 0 {
		e.dimension = 1 // Minimum dimension
	}
}

// textToVector converts text to a TF-IDF vector.
func (e *TFIDFEmbedder) textToVector(text string) []float32 {
	tokens := tfidfTokenize(text)

	// Calculate term frequency
	tf := make(map[string]float64)
	for _, token := range tokens {
		tf[token]++
	}

	// Normalize TF
	maxTF := 0.0
	for _, count := range tf {
		if count > maxTF {
			maxTF = count
		}
	}
	if maxTF > 0 {
		for term := range tf {
			tf[term] = tf[term] / maxTF
		}
	}

	// Create TF-IDF vector
	dim := e.dimension
	if dim == 0 {
		dim = len(e.vocabulary)
	}
	if dim == 0 {
		dim = 1
	}

	vector := make([]float32, dim)
	for term, idx := range e.vocabulary {
		if idx < dim {
			if termTF, ok := tf[term]; ok {
				vector[idx] = float32(termTF * e.idf[term])
			}
		}
	}

	// L2 normalize the vector
	var norm float64
	for _, v := range vector {
		norm += float64(v * v)
	}
	if norm > 0 {
		norm = math.Sqrt(norm)
		for i := range vector {
			vector[i] = float32(float64(vector[i]) / norm)
		}
	}

	return vector
}

// tfidfTokenize splits text into normalized tokens.
// Uses the shared tokenize function from engine_vector.go.
func tfidfTokenize(text string) []string {
	return tokenize(text)
}

// CosineSimilarity calculates the cosine similarity between two vectors.
func CosineSimilarity(a, b []float32) float64 {
	if len(a) != len(b) {
		return 0
	}

	var dotProduct, normA, normB float64
	for i := range a {
		dotProduct += float64(a[i]) * float64(b[i])
		normA += float64(a[i]) * float64(a[i])
		normB += float64(b[i]) * float64(b[i])
	}

	if normA == 0 || normB == 0 {
		return 0
	}

	return dotProduct / (math.Sqrt(normA) * math.Sqrt(normB))
}
