// Package mcp provides MCP (Model Context Protocol) server functionality.
package mcp

import (
	"fmt"
	"slices"
	"strings"
)

// TokenizerType represents the type of tokenizer to use.
type TokenizerType string

const (
	// TokenizerSimple uses basic whitespace and punctuation splitting.
	TokenizerSimple TokenizerType = "simple"
	// TokenizerUnicode uses Unicode-aware tokenization.
	TokenizerUnicode TokenizerType = "unicode"
	// TokenizerCJK uses Chinese/Japanese/Korean segmentation.
	TokenizerCJK TokenizerType = "cjk"
)

// TokenizerConfig contains configuration for tokenizers.
type TokenizerConfig struct {
	// Type specifies the tokenizer type to use.
	Type TokenizerType `yaml:"type" json:"type"`

	// Lowercase indicates whether to convert tokens to lowercase.
	Lowercase bool `yaml:"lowercase,omitempty" json:"lowercase,omitempty"`

	// StopWords is a list of words to exclude from tokenization.
	StopWords []string `yaml:"stop_words,omitempty" json:"stop_words,omitempty"`

	// MinTokenLength is the minimum length of tokens to include.
	MinTokenLength int `yaml:"min_token_length,omitempty" json:"min_token_length,omitempty"`

	// MaxTokenLength is the maximum length of tokens to include.
	MaxTokenLength int `yaml:"max_token_length,omitempty" json:"max_token_length,omitempty"`

	// DictPath is the path to a custom dictionary file (for CJK tokenizer).
	DictPath string `yaml:"dict_path,omitempty" json:"dict_path,omitempty"`
}

// DefaultTokenizerConfig returns the default tokenizer configuration.
func DefaultTokenizerConfig() TokenizerConfig {
	return TokenizerConfig{
		Type:           TokenizerSimple,
		Lowercase:      true,
		MinTokenLength: 1,
		MaxTokenLength: 100,
	}
}

// Tokenizer defines the interface for text tokenization.
type Tokenizer interface {
	// Tokenize splits text into tokens.
	Tokenize(text string) []string

	// TokenizeForFTS tokenizes text and returns a string suitable for FTS5.
	// For CJK text, this adds spaces between tokens.
	TokenizeForFTS(text string) string

	// Type returns the tokenizer type.
	Type() TokenizerType

	// Config returns the current configuration.
	Config() TokenizerConfig
}

// ConfigurableTokenizer extends Tokenizer with runtime reconfiguration.
type ConfigurableTokenizer interface {
	Tokenizer

	// Reconfigure updates the tokenizer configuration at runtime.
	Reconfigure(cfg TokenizerConfig) error
}

// TokenizerFactory creates tokenizers based on configuration.
type TokenizerFactory struct{}

// NewTokenizerFactory creates a new tokenizer factory.
func NewTokenizerFactory() *TokenizerFactory {
	return &TokenizerFactory{}
}

// Create creates a tokenizer based on the given configuration.
func (f *TokenizerFactory) Create(cfg TokenizerConfig) (Tokenizer, error) {
	switch cfg.Type {
	case TokenizerSimple:
		return NewSimpleTokenizer(cfg)
	case TokenizerUnicode:
		return NewUnicodeTokenizer(cfg)
	case TokenizerCJK:
		return NewCJKTokenizer(cfg)
	default:
		return nil, fmt.Errorf("unknown tokenizer type: %s", cfg.Type)
	}
}

// NewTokenizer creates a tokenizer with the given configuration.
func NewTokenizer(cfg TokenizerConfig) (Tokenizer, error) {
	return NewTokenizerFactory().Create(cfg)
}

// ParseTokenizerType parses a string into a TokenizerType.
func ParseTokenizerType(s string) (TokenizerType, error) {
	switch strings.ToLower(s) {
	case "simple":
		return TokenizerSimple, nil
	case "unicode":
		return TokenizerUnicode, nil
	case "cjk", "chinese", "japanese", "korean":
		return TokenizerCJK, nil
	default:
		return "", fmt.Errorf("invalid tokenizer type: %s (valid: simple, unicode, cjk)", s)
	}
}

// isStopWord checks if a word is in the stop words list.
func isStopWord(word string, stopWords []string) bool {
	return slices.Contains(stopWords, word)
}

// filterTokens applies length and stop word filters to tokens.
func filterTokens(tokens []string, cfg TokenizerConfig) []string {
	if len(tokens) == 0 {
		return tokens
	}

	result := make([]string, 0, len(tokens))
	for _, token := range tokens {
		// Apply lowercase if configured
		if cfg.Lowercase {
			token = strings.ToLower(token)
		}

		// Skip empty tokens
		if token == "" {
			continue
		}

		// Check length constraints
		if cfg.MinTokenLength > 0 && len(token) < cfg.MinTokenLength {
			continue
		}
		if cfg.MaxTokenLength > 0 && len(token) > cfg.MaxTokenLength {
			continue
		}

		// Check stop words
		if isStopWord(token, cfg.StopWords) {
			continue
		}

		result = append(result, token)
	}

	return result
}
