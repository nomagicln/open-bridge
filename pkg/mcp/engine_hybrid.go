package mcp

import (
	"context"
	"fmt"
	"sort"
	"sync"
)

// FusionStrategy represents the strategy for fusing search results.
type FusionStrategy string

const (
	// FusionRRF uses Reciprocal Rank Fusion algorithm.
	FusionRRF FusionStrategy = "rrf"
	// FusionWeighted uses weighted score combination.
	FusionWeighted FusionStrategy = "weighted"
)

// HybridSearchConfig contains configuration for hybrid search.
type HybridSearchConfig struct {
	// Enabled indicates whether hybrid search is enabled.
	Enabled bool `yaml:"enabled" json:"enabled"`

	// Embedder is the configuration for the embedding engine.
	Embedder EmbedderConfig `yaml:"embedder" json:"embedder"`

	// Tokenizer is the configuration for the tokenizer.
	Tokenizer TokenizerConfig `yaml:"tokenizer" json:"tokenizer"`

	// FusionStrategy is the strategy for combining results.
	FusionStrategy FusionStrategy `yaml:"fusion_strategy" json:"fusion_strategy"`

	// RRFConstant is the k constant for RRF algorithm (default: 60).
	RRFConstant float64 `yaml:"rrf_constant" json:"rrf_constant"`

	// VectorWeight is the weight for vector search results (0.0-1.0).
	// SQL weight will be (1.0 - VectorWeight).
	VectorWeight float64 `yaml:"vector_weight" json:"vector_weight"`

	// TopK is the number of results to retrieve from each engine before fusion.
	TopK int `yaml:"top_k" json:"top_k"`

	// PredicateFilter is an optional Vulcand predicate expression for post-filtering.
	PredicateFilter string `yaml:"predicate_filter,omitempty" json:"predicate_filter,omitempty"`
}

// DefaultHybridSearchConfig returns the default hybrid search configuration.
func DefaultHybridSearchConfig() HybridSearchConfig {
	return HybridSearchConfig{
		Enabled:        true,
		Embedder:       DefaultEmbedderConfig(),
		Tokenizer:      DefaultTokenizerConfig(),
		FusionStrategy: FusionRRF,
		RRFConstant:    60.0,
		VectorWeight:   0.5,
		TopK:           50,
	}
}

// HybridSearchEngine implements ToolSearchEngine using a combination of
// SQL FTS5 (lexical) and Vector (semantic) search with RRF fusion.
type HybridSearchEngine struct {
	cfg             HybridSearchConfig
	sqlEngine       *SQLSearchEngine
	vectorEngine    *VectorSearchEngine
	predicateEngine *PredicateSearchEngine
	embedder        Embedder
	tokenizer       Tokenizer
	toolVectors     map[string][]float32 // tool ID -> embedding vector
	tools           []ToolMetadata
	mu              sync.RWMutex
}

// NewHybridSearchEngine creates a new hybrid search engine.
func NewHybridSearchEngine(cfg HybridSearchConfig) (*HybridSearchEngine, error) {
	// Create SQL engine
	sqlEngine, err := NewSQLSearchEngine()
	if err != nil {
		return nil, fmt.Errorf("failed to create SQL engine: %w", err)
	}

	// Create vector engine (we'll use it for fallback)
	vectorEngine, err := NewVectorSearchEngine()
	if err != nil {
		_ = sqlEngine.Close()
		return nil, fmt.Errorf("failed to create vector engine: %w", err)
	}

	// Create predicate engine for post-filtering
	predicateEngine, err := NewPredicateSearchEngine()
	if err != nil {
		_ = sqlEngine.Close()
		_ = vectorEngine.Close()
		return nil, fmt.Errorf("failed to create predicate engine: %w", err)
	}

	// Create embedder
	embedder, err := NewEmbedder(cfg.Embedder)
	if err != nil {
		_ = sqlEngine.Close()
		_ = vectorEngine.Close()
		_ = predicateEngine.Close()
		return nil, fmt.Errorf("failed to create embedder: %w", err)
	}

	// Create tokenizer
	tokenizer, err := NewTokenizer(cfg.Tokenizer)
	if err != nil {
		_ = sqlEngine.Close()
		_ = vectorEngine.Close()
		_ = predicateEngine.Close()
		_ = embedder.Close()
		return nil, fmt.Errorf("failed to create tokenizer: %w", err)
	}

	return &HybridSearchEngine{
		cfg:             cfg,
		sqlEngine:       sqlEngine,
		vectorEngine:    vectorEngine,
		predicateEngine: predicateEngine,
		embedder:        embedder,
		tokenizer:       tokenizer,
		toolVectors:     make(map[string][]float32),
		tools:           make([]ToolMetadata, 0),
	}, nil
}

// Search performs hybrid search combining lexical and semantic results.
//
//nolint:funlen // Parallel search and fusion logic requires complete implementation in one function
func (e *HybridSearchEngine) Search(query string) ([]ToolMetadata, error) {
	e.mu.RLock()
	defer e.mu.RUnlock()

	if query == "" || len(e.tools) == 0 {
		return e.getAllTools(), nil
	}

	ctx := context.Background()

	// Execute searches in parallel
	type searchResult struct {
		results []ToolMetadata
		err     error
	}

	sqlCh := make(chan searchResult, 1)
	vectorCh := make(chan searchResult, 1)

	// SQL FTS search with tokenized query
	go func() {
		tokenizedQuery := e.tokenizer.TokenizeForFTS(query)
		results, err := e.sqlEngine.Search(tokenizedQuery)
		sqlCh <- searchResult{results: results, err: err}
	}()

	// Vector similarity search
	go func() {
		results, err := e.vectorSearch(ctx, query)
		vectorCh <- searchResult{results: results, err: err}
	}()

	// Collect results
	sqlResult := <-sqlCh
	vectorResult := <-vectorCh

	// Handle errors - use available results
	var sqlResults, vectorResults []ToolMetadata
	if sqlResult.err == nil {
		sqlResults = sqlResult.results
	}
	if vectorResult.err == nil {
		vectorResults = vectorResult.results
	}

	// Fuse results
	var fused []ToolMetadata
	switch e.cfg.FusionStrategy {
	case FusionWeighted:
		fused = e.weightedFusion(sqlResults, vectorResults)
	default: // FusionRRF
		fused = e.rrfFusion(sqlResults, vectorResults)
	}

	// Apply predicate filter if configured
	if e.cfg.PredicateFilter != "" {
		fused = e.applyPredicateFilter(fused, e.cfg.PredicateFilter)
	}

	return fused, nil
}

// vectorSearch performs semantic search using embeddings.
func (e *HybridSearchEngine) vectorSearch(ctx context.Context, query string) ([]ToolMetadata, error) {
	if len(e.toolVectors) == 0 {
		return nil, nil
	}

	// Embed the query
	queryVector, err := e.embedder.EmbedSingle(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to embed query: %w", err)
	}

	// Calculate similarities
	type scoredTool struct {
		tool  ToolMetadata
		score float64
	}

	var scored []scoredTool
	for _, tool := range e.tools {
		if vec, ok := e.toolVectors[tool.ID]; ok {
			score := CosineSimilarity(queryVector, vec)
			if score > 0.01 { // Threshold
				scored = append(scored, scoredTool{tool: tool, score: score})
			}
		}
	}

	// Sort by score descending
	sort.Slice(scored, func(i, j int) bool {
		return scored[i].score > scored[j].score
	})

	// Return top K
	limit := e.cfg.TopK
	if limit <= 0 {
		limit = 50
	}
	if len(scored) > limit {
		scored = scored[:limit]
	}

	results := make([]ToolMetadata, len(scored))
	for i, s := range scored {
		results[i] = s.tool
	}

	return results, nil
}

// rrfFusion combines results using Reciprocal Rank Fusion.
// RRF Score(d) = Σ 1 / (k + rank_r(d)) for each result set r
func (e *HybridSearchEngine) rrfFusion(sqlResults, vectorResults []ToolMetadata) []ToolMetadata {
	k := e.cfg.RRFConstant
	if k <= 0 {
		k = 60.0
	}

	// Calculate RRF scores
	scores := make(map[string]float64)
	toolMap := make(map[string]ToolMetadata)

	// SQL results contribution
	for rank, tool := range sqlResults {
		scores[tool.ID] += 1.0 / (k + float64(rank+1))
		toolMap[tool.ID] = tool
	}

	// Vector results contribution
	for rank, tool := range vectorResults {
		scores[tool.ID] += 1.0 / (k + float64(rank+1))
		toolMap[tool.ID] = tool
	}

	// Sort by RRF score
	type scoredTool struct {
		tool  ToolMetadata
		score float64
	}

	var sorted []scoredTool
	for id, score := range scores {
		sorted = append(sorted, scoredTool{tool: toolMap[id], score: score})
	}

	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].score > sorted[j].score
	})

	// Return fused results
	limit := e.cfg.TopK
	if limit <= 0 {
		limit = 50
	}
	if len(sorted) > limit {
		sorted = sorted[:limit]
	}

	results := make([]ToolMetadata, len(sorted))
	for i, s := range sorted {
		results[i] = s.tool
	}

	return results
}

// weightedFusion combines results using weighted scoring.
//
//nolint:funlen // Score calculation and ranking logic is best kept together
func (e *HybridSearchEngine) weightedFusion(sqlResults, vectorResults []ToolMetadata) []ToolMetadata {
	vectorWeight := e.cfg.VectorWeight
	if vectorWeight < 0 {
		vectorWeight = 0
	}
	if vectorWeight > 1 {
		vectorWeight = 1
	}
	sqlWeight := 1.0 - vectorWeight

	// Normalize ranks to scores (higher rank = higher score)
	scores := make(map[string]float64)
	toolMap := make(map[string]ToolMetadata)

	// SQL results contribution
	sqlTotal := float64(len(sqlResults))
	for rank, tool := range sqlResults {
		// Score inversely proportional to rank
		normalizedScore := (sqlTotal - float64(rank)) / sqlTotal
		scores[tool.ID] += normalizedScore * sqlWeight
		toolMap[tool.ID] = tool
	}

	// Vector results contribution
	vectorTotal := float64(len(vectorResults))
	for rank, tool := range vectorResults {
		normalizedScore := (vectorTotal - float64(rank)) / vectorTotal
		scores[tool.ID] += normalizedScore * vectorWeight
		toolMap[tool.ID] = tool
	}

	// Sort by weighted score
	type scoredTool struct {
		tool  ToolMetadata
		score float64
	}

	var sorted []scoredTool
	for id, score := range scores {
		sorted = append(sorted, scoredTool{tool: toolMap[id], score: score})
	}

	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].score > sorted[j].score
	})

	// Return fused results
	limit := e.cfg.TopK
	if limit <= 0 {
		limit = 50
	}
	if len(sorted) > limit {
		sorted = sorted[:limit]
	}

	results := make([]ToolMetadata, len(sorted))
	for i, s := range sorted {
		results[i] = s.tool
	}

	return results
}

// applyPredicateFilter filters results using a Vulcand predicate expression.
func (e *HybridSearchEngine) applyPredicateFilter(tools []ToolMetadata, filter string) []ToolMetadata {
	if filter == "" || len(tools) == 0 {
		return tools
	}

	// Index tools in predicate engine temporarily
	if err := e.predicateEngine.Index(tools); err != nil {
		return tools // On error, return unfiltered
	}

	// Search with the filter
	filtered, err := e.predicateEngine.Search(filter)
	if err != nil {
		return tools // On error, return unfiltered
	}

	// Preserve the original order from fusion
	filterSet := make(map[string]bool)
	for _, t := range filtered {
		filterSet[t.ID] = true
	}

	result := make([]ToolMetadata, 0, len(filtered))
	for _, t := range tools {
		if filterSet[t.ID] {
			result = append(result, t)
		}
	}

	return result
}

// getAllTools returns all indexed tools (with limit).
func (e *HybridSearchEngine) getAllTools() []ToolMetadata {
	limit := 100
	if len(e.tools) > limit {
		return e.tools[:limit]
	}
	return e.tools
}

// GetDescription returns a description of the hybrid search engine.
func (e *HybridSearchEngine) GetDescription() string {
	return `Hybrid search engine combining lexical (FTS5) and semantic (vector) search.
Use natural language queries or keywords:
- "find all pets" (semantic)
- "user login API" (lexical + semantic)
- "create OR update pet" (boolean operators)
The engine automatically fuses results from both approaches using RRF algorithm.`
}

// GetQueryExample returns an example query.
func (e *HybridSearchEngine) GetQueryExample() string {
	return `"create a new user account" or "POST /users" or "user management"`
}

// Index adds or updates tools in the search index.
//
//nolint:funlen // Indexing across multiple engines needs to be atomic
func (e *HybridSearchEngine) Index(tools []ToolMetadata) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	// Store tools
	e.tools = make([]ToolMetadata, len(tools))
	copy(e.tools, tools)

	// Index in SQL engine with tokenized content
	tokenizedTools := make([]ToolMetadata, len(tools))
	for i, tool := range tools {
		tokenizedTools[i] = ToolMetadata{
			ID:          tool.ID,
			Name:        e.tokenizer.TokenizeForFTS(tool.Name),
			Description: e.tokenizer.TokenizeForFTS(tool.Description),
			Method:      tool.Method,
			Path:        e.tokenizer.TokenizeForFTS(tool.Path),
			Tags:        tool.Tags,
		}
	}
	if err := e.sqlEngine.Index(tokenizedTools); err != nil {
		return fmt.Errorf("failed to index in SQL engine: %w", err)
	}

	// Index in vector engine (fallback)
	if err := e.vectorEngine.Index(tools); err != nil {
		return fmt.Errorf("failed to index in vector engine: %w", err)
	}

	// Index in predicate engine (for filtering)
	if err := e.predicateEngine.Index(tools); err != nil {
		return fmt.Errorf("failed to index in predicate engine: %w", err)
	}

	// Generate embeddings for vector search
	ctx := context.Background()
	e.toolVectors = make(map[string][]float32)

	// Prepare text for embedding
	texts := make([]string, len(tools))
	for i, tool := range tools {
		texts[i] = getToolText(tool)
	}

	// Index documents in TF-IDF embedder if applicable
	if adaptive, ok := e.embedder.(*AdaptiveEmbedder); ok {
		adaptive.IndexDocuments(texts)
	} else if tfidf, ok := e.embedder.(*TFIDFEmbedder); ok {
		tfidf.IndexDocuments(texts)
	}

	// Generate embeddings
	vectors, err := e.embedder.Embed(ctx, texts)
	if err != nil {
		// Log but don't fail - we can still use SQL search
		return nil
	}

	// Store vectors
	for i, tool := range tools {
		if i < len(vectors) {
			e.toolVectors[tool.ID] = vectors[i]
		}
	}

	return nil
}

// Close releases resources held by the engine.
func (e *HybridSearchEngine) Close() error {
	e.mu.Lock()
	defer e.mu.Unlock()

	var errs []error

	if e.sqlEngine != nil {
		if err := e.sqlEngine.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	if e.vectorEngine != nil {
		if err := e.vectorEngine.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	if e.predicateEngine != nil {
		if err := e.predicateEngine.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	if e.embedder != nil {
		if err := e.embedder.Close(); err != nil {
			errs = append(errs, err)
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("errors closing hybrid engine: %v", errs)
	}
	return nil
}

// Reconfigure updates the engine configuration at runtime.
func (e *HybridSearchEngine) Reconfigure(cfg HybridSearchConfig) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	// Update fusion parameters
	e.cfg.FusionStrategy = cfg.FusionStrategy
	e.cfg.RRFConstant = cfg.RRFConstant
	e.cfg.VectorWeight = cfg.VectorWeight
	e.cfg.TopK = cfg.TopK
	e.cfg.PredicateFilter = cfg.PredicateFilter

	// Reconfigure embedder if it supports it
	if configurable, ok := e.embedder.(ConfigurableEmbedder); ok {
		if err := configurable.Reconfigure(cfg.Embedder); err != nil {
			return fmt.Errorf("failed to reconfigure embedder: %w", err)
		}
	}

	// Reconfigure tokenizer if it supports it
	if configurable, ok := e.tokenizer.(ConfigurableTokenizer); ok {
		if err := configurable.Reconfigure(cfg.Tokenizer); err != nil {
			return fmt.Errorf("failed to reconfigure tokenizer: %w", err)
		}
	}

	return nil
}

// Config returns the current configuration.
func (e *HybridSearchEngine) Config() HybridSearchConfig {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.cfg
}

// SetPredicateFilter sets the predicate filter for post-filtering.
func (e *HybridSearchEngine) SetPredicateFilter(filter string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.cfg.PredicateFilter = filter
}

// GetActiveEmbedderType returns the type of the currently active embedder.
func (e *HybridSearchEngine) GetActiveEmbedderType() EmbedderType {
	e.mu.RLock()
	defer e.mu.RUnlock()

	if adaptive, ok := e.embedder.(*AdaptiveEmbedder); ok {
		return adaptive.ActiveType()
	}
	return e.embedder.Type()
}
