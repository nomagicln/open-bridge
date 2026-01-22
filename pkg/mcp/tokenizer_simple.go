package mcp

import (
	"strings"
	"unicode"
)

// SimpleTokenizer implements basic whitespace and punctuation tokenization.
type SimpleTokenizer struct {
	cfg TokenizerConfig
}

// NewSimpleTokenizer creates a new simple tokenizer.
func NewSimpleTokenizer(cfg TokenizerConfig) (*SimpleTokenizer, error) {
	if cfg.Type == "" {
		cfg.Type = TokenizerSimple
	}
	return &SimpleTokenizer{cfg: cfg}, nil
}

// Tokenize splits text into tokens using whitespace and punctuation.
func (t *SimpleTokenizer) Tokenize(text string) []string {
	if text == "" {
		return nil
	}

	// Apply lowercase if configured (default is true)
	if t.cfg.Lowercase {
		text = strings.ToLower(text)
	}

	var tokens []string
	var current strings.Builder

	for _, r := range text {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			current.WriteRune(r)
		} else if current.Len() > 0 {
			tokens = append(tokens, current.String())
			current.Reset()
		}
	}
	if current.Len() > 0 {
		tokens = append(tokens, current.String())
	}

	return filterTokens(tokens, t.cfg)
}

// TokenizeForFTS returns text tokenized for FTS5.
func (t *SimpleTokenizer) TokenizeForFTS(text string) string {
	tokens := t.Tokenize(text)
	return strings.Join(tokens, " ")
}

// Type returns the tokenizer type.
func (t *SimpleTokenizer) Type() TokenizerType {
	return TokenizerSimple
}

// Config returns the current configuration.
func (t *SimpleTokenizer) Config() TokenizerConfig {
	return t.cfg
}

// Reconfigure updates the tokenizer configuration.
func (t *SimpleTokenizer) Reconfigure(cfg TokenizerConfig) error {
	t.cfg = cfg
	return nil
}

// UnicodeTokenizer implements Unicode-aware tokenization.
type UnicodeTokenizer struct {
	cfg TokenizerConfig
}

// NewUnicodeTokenizer creates a new Unicode tokenizer.
func NewUnicodeTokenizer(cfg TokenizerConfig) (*UnicodeTokenizer, error) {
	if cfg.Type == "" {
		cfg.Type = TokenizerUnicode
	}
	return &UnicodeTokenizer{cfg: cfg}, nil
}

// Tokenize splits text into tokens using Unicode word boundaries.
func (t *UnicodeTokenizer) Tokenize(text string) []string {
	if text == "" {
		return nil
	}

	var tokens []string
	var current strings.Builder
	var lastCategory int // 0: none, 1: letter, 2: digit, 3: CJK

	for _, r := range text {
		var category int
		if isCJKRune(r) {
			category = 3
		} else if unicode.IsLetter(r) {
			category = 1
		} else if unicode.IsDigit(r) {
			category = 2
		}

		switch {
		case category == 0:
			// Non-word character - emit current token
			emitCurrent(&current, &tokens)
			lastCategory = 0

		case category == 3:
			// CJK character - emit as individual token
			emitCurrent(&current, &tokens)
			tokens = append(tokens, string(r))
			lastCategory = 3

		case lastCategory == category || lastCategory == 0:
			// Same category or starting - accumulate
			current.WriteRune(r)
			lastCategory = category

		default:
			// Category change - emit current token
			emitCurrent(&current, &tokens)
			current.WriteRune(r)
			lastCategory = category
		}
	}

	emitCurrent(&current, &tokens)

	return filterTokens(tokens, t.cfg)
}

// TokenizeForFTS returns text tokenized for FTS5.
func (t *UnicodeTokenizer) TokenizeForFTS(text string) string {
	tokens := t.Tokenize(text)
	return strings.Join(tokens, " ")
}

// Type returns the tokenizer type.
func (t *UnicodeTokenizer) Type() TokenizerType {
	return TokenizerUnicode
}

// Config returns the current configuration.
func (t *UnicodeTokenizer) Config() TokenizerConfig {
	return t.cfg
}

// Reconfigure updates the tokenizer configuration.
func (t *UnicodeTokenizer) Reconfigure(cfg TokenizerConfig) error {
	t.cfg = cfg
	return nil
}

// isCJKRune checks if a rune is a CJK character.
func isCJKRune(r rune) bool {
	return unicode.Is(unicode.Han, r) ||
		unicode.Is(unicode.Hiragana, r) ||
		unicode.Is(unicode.Katakana, r) ||
		unicode.Is(unicode.Hangul, r)
}
