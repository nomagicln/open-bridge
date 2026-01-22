package mcp

import (
	"math"
	"sort"
	"strings"
	"sync"
)

// VectorSearchEngine implements ToolSearchEngine using vector similarity for semantic search.
// It uses a simple TF-IDF based approach with cosine similarity for lightweight semantic matching.
// For production use with better accuracy, consider integrating with external embedding services.
type VectorSearchEngine struct {
	tools      []ToolMetadata
	vectors    [][]float64 // TF-IDF vectors for each tool
	vocabulary map[string]int
	idf        map[string]float64
	mu         sync.RWMutex
}

// NewVectorSearchEngine creates a new vector-based search engine.
func NewVectorSearchEngine() (*VectorSearchEngine, error) {
	return &VectorSearchEngine{
		tools:      make([]ToolMetadata, 0),
		vectors:    make([][]float64, 0),
		vocabulary: make(map[string]int),
		idf:        make(map[string]float64),
	}, nil
}

// tokenize splits text into normalized tokens.
func tokenize(text string) []string {
	// Convert to lowercase and split on non-alphanumeric characters
	text = strings.ToLower(text)
	var tokens []string
	var current strings.Builder

	for _, r := range text {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			current.WriteRune(r)
		} else if current.Len() > 0 {
			tokens = append(tokens, current.String())
			current.Reset()
		}
	}
	if current.Len() > 0 {
		tokens = append(tokens, current.String())
	}

	return tokens
}

// getToolText concatenates all searchable text from a tool.
func getToolText(tool ToolMetadata) string {
	parts := []string{
		tool.ID,
		tool.Name,
		tool.Description,
		tool.Method,
		tool.Path,
	}
	parts = append(parts, tool.Tags...)
	return strings.Join(parts, " ")
}

// buildVocabulary creates the vocabulary and IDF values from all tools.
func (e *VectorSearchEngine) buildVocabulary() {
	// Count document frequency for each term
	docFreq := make(map[string]int)

	for _, tool := range e.tools {
		text := getToolText(tool)
		tokens := tokenize(text)

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
	n := float64(len(e.tools))
	for term, df := range docFreq {
		e.vocabulary[term] = idx
		// IDF = log(N / df) + 1 (smoothed)
		e.idf[term] = math.Log(n/float64(df)) + 1.0
		idx++
	}
}

// textToVector converts text to a TF-IDF vector.
func (e *VectorSearchEngine) textToVector(text string) []float64 {
	tokens := tokenize(text)

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
	vector := make([]float64, len(e.vocabulary))
	for term, idx := range e.vocabulary {
		if termTF, ok := tf[term]; ok {
			vector[idx] = termTF * e.idf[term]
		}
	}

	return vector
}

// cosineSimilarity calculates the cosine similarity between two vectors.
func cosineSimilarity(a, b []float64) float64 {
	if len(a) != len(b) {
		return 0
	}

	var dotProduct, normA, normB float64
	for i := range a {
		dotProduct += a[i] * b[i]
		normA += a[i] * a[i]
		normB += b[i] * b[i]
	}

	if normA == 0 || normB == 0 {
		return 0
	}

	return dotProduct / (math.Sqrt(normA) * math.Sqrt(normB))
}

// searchResult holds a tool and its similarity score.
type searchResult struct {
	tool  ToolMetadata
	score float64
}

// Search finds tools semantically similar to the natural language query.
// The query can be a description of what you want to do, and the engine
// will find tools with similar descriptions using TF-IDF cosine similarity.
func (e *VectorSearchEngine) Search(query string) ([]ToolMetadata, error) {
	e.mu.RLock()
	defer e.mu.RUnlock()

	if query == "" || len(e.tools) == 0 {
		return e.getAllTools(), nil
	}

	// Convert query to vector
	queryVector := e.textToVector(query)

	// Calculate similarity with all tools
	var results []searchResult
	for i, toolVector := range e.vectors {
		score := cosineSimilarity(queryVector, toolVector)
		if score > 0.01 { // Threshold to filter out very low matches
			results = append(results, searchResult{
				tool:  e.tools[i],
				score: score,
			})
		}
	}

	// Sort by similarity score (descending)
	sort.Slice(results, func(i, j int) bool {
		return results[i].score > results[j].score
	})

	// Return top results
	limit := min(len(results), 50)

	tools := make([]ToolMetadata, limit)
	for i := range limit {
		tools[i] = results[i].tool
	}

	return tools, nil
}

// getAllTools returns all indexed tools.
func (e *VectorSearchEngine) getAllTools() []ToolMetadata {
	if len(e.tools) > 100 {
		return e.tools[:100]
	}
	return e.tools
}

// GetDescription returns a description of the vector search engine.
func (e *VectorSearchEngine) GetDescription() string {
	return `Semantic vector search engine using TF-IDF similarity.
Use natural language to describe what you want to do:
- "get information about a user"
- "create a new pet in the store"
- "update order status"
- "delete customer record"
The engine finds tools with semantically similar descriptions.`
}

// GetQueryExample returns an example query.
func (e *VectorSearchEngine) GetQueryExample() string {
	return `"find all pets by status" or "create new user account"`
}

// Index adds or updates tools in the search index.
func (e *VectorSearchEngine) Index(tools []ToolMetadata) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	e.tools = make([]ToolMetadata, len(tools))
	copy(e.tools, tools)

	// Build vocabulary from all tools
	e.buildVocabulary()

	// Build vectors for all tools
	e.vectors = make([][]float64, len(tools))
	for i, tool := range tools {
		text := getToolText(tool)
		e.vectors[i] = e.textToVector(text)
	}

	return nil
}

// Close releases any resources (no-op for vector engine).
func (e *VectorSearchEngine) Close() error {
	return nil
}
