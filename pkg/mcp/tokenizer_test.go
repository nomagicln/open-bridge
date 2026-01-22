package mcp

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseTokenizerType(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    TokenizerType
		wantErr bool
	}{
		{
			name:  "simple type",
			input: "simple",
			want:  TokenizerSimple,
		},
		{
			name:  "unicode type",
			input: "unicode",
			want:  TokenizerUnicode,
		},
		{
			name:  "cjk type",
			input: "cjk",
			want:  TokenizerCJK,
		},
		{
			name:  "chinese maps to cjk",
			input: "chinese",
			want:  TokenizerCJK,
		},
		{
			name:  "japanese maps to cjk",
			input: "japanese",
			want:  TokenizerCJK,
		},
		{
			name:  "korean maps to cjk",
			input: "korean",
			want:  TokenizerCJK,
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
			got, err := ParseTokenizerType(tt.input)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestDefaultTokenizerConfig(t *testing.T) {
	cfg := DefaultTokenizerConfig()

	assert.Equal(t, TokenizerSimple, cfg.Type)
	assert.Equal(t, 1, cfg.MinTokenLength)
	assert.True(t, cfg.Lowercase)
}

func TestTokenizerFactory_Create_Simple(t *testing.T) {
	factory := NewTokenizerFactory()

	cfg := TokenizerConfig{
		Type: TokenizerSimple,
	}

	tokenizer, err := factory.Create(cfg)
	require.NoError(t, err)
	require.NotNil(t, tokenizer)

	assert.Equal(t, TokenizerSimple, tokenizer.Type())
}

func TestTokenizerFactory_Create_Unicode(t *testing.T) {
	factory := NewTokenizerFactory()

	cfg := TokenizerConfig{
		Type: TokenizerUnicode,
	}

	tokenizer, err := factory.Create(cfg)
	require.NoError(t, err)
	require.NotNil(t, tokenizer)

	assert.Equal(t, TokenizerUnicode, tokenizer.Type())
}

func TestTokenizerFactory_Create_CJK(t *testing.T) {
	factory := NewTokenizerFactory()

	cfg := TokenizerConfig{
		Type: TokenizerCJK,
	}

	tokenizer, err := factory.Create(cfg)
	require.NoError(t, err)
	require.NotNil(t, tokenizer)

	assert.Equal(t, TokenizerCJK, tokenizer.Type())
}

func TestTokenizerFactory_Create_UnknownType(t *testing.T) {
	factory := NewTokenizerFactory()

	cfg := TokenizerConfig{
		Type: "unknown",
	}

	tokenizer, err := factory.Create(cfg)
	require.Error(t, err)
	assert.Nil(t, tokenizer)
	assert.Contains(t, err.Error(), "unknown tokenizer type")
}

func TestNewTokenizer_Convenience(t *testing.T) {
	cfg := TokenizerConfig{
		Type: TokenizerSimple,
	}

	tokenizer, err := NewTokenizer(cfg)
	require.NoError(t, err)
	require.NotNil(t, tokenizer)

	assert.Equal(t, TokenizerSimple, tokenizer.Type())
}

func TestSimpleTokenizer_Tokenize(t *testing.T) {
	tokenizer, err := NewSimpleTokenizer(TokenizerConfig{Type: TokenizerSimple, Lowercase: true})
	require.NoError(t, err)

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
			input:    "Hello, World!",
			expected: []string{"hello", "world"},
		},
		{
			name:     "empty string",
			input:    "",
			expected: nil,
		},
		{
			name:     "only whitespace",
			input:    "   ",
			expected: nil,
		},
		{
			name:     "camelCase splits",
			input:    "getUserById",
			expected: []string{"getuserbyid"},
		},
		{
			name:     "snake_case",
			input:    "get_user_by_id",
			expected: []string{"get", "user", "by", "id"},
		},
		{
			name:     "path-like",
			input:    "/api/v1/users/{id}",
			expected: []string{"api", "v1", "users", "id"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tokenizer.Tokenize(tt.input)
			assert.Equal(t, tt.expected, got)
		})
	}
}

func TestSimpleTokenizer_TokenizeForFTS(t *testing.T) {
	tokenizer, err := NewSimpleTokenizer(TokenizerConfig{Type: TokenizerSimple, Lowercase: true})
	require.NoError(t, err)

	input := "Hello, World!"
	expected := "hello world"

	got := tokenizer.TokenizeForFTS(input)
	assert.Equal(t, expected, got)
}

func TestSimpleTokenizer_WithMinLength(t *testing.T) {
	cfg := TokenizerConfig{
		Type:           TokenizerSimple,
		MinTokenLength: 3,
	}

	tokenizer, err := NewSimpleTokenizer(cfg)
	require.NoError(t, err)

	tokens := tokenizer.Tokenize("I am a very important person")
	// Should filter out "I", "am", "a"
	for _, token := range tokens {
		assert.GreaterOrEqual(t, len(token), 3, "token should have min length 3: %s", token)
	}
}

func TestUnicodeTokenizer_Tokenize(t *testing.T) {
	tokenizer, err := NewUnicodeTokenizer(TokenizerConfig{Type: TokenizerUnicode, Lowercase: true})
	require.NoError(t, err)

	tests := []struct {
		name     string
		input    string
		hasToken string
	}{
		{
			name:     "english text",
			input:    "Hello World",
			hasToken: "hello",
		},
		{
			name:     "mixed script",
			input:    "Hello 世界",
			hasToken: "世", // CJK characters are split individually
		},
		{
			name:     "accented characters",
			input:    "café résumé",
			hasToken: "café",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tokens := tokenizer.Tokenize(tt.input)
			assert.Contains(t, tokens, tt.hasToken)
		})
	}
}

func TestUnicodeTokenizer_CJKDetection(t *testing.T) {
	tokenizer, err := NewUnicodeTokenizer(TokenizerConfig{Type: TokenizerUnicode})
	require.NoError(t, err)

	// Chinese
	tokens := tokenizer.Tokenize("你好世界")
	assert.Greater(t, len(tokens), 0)

	// Japanese
	tokens = tokenizer.Tokenize("こんにちは世界")
	assert.Greater(t, len(tokens), 0)

	// Korean
	tokens = tokenizer.Tokenize("안녕하세요")
	assert.Greater(t, len(tokens), 0)
}

func TestCJKTokenizer_Tokenize(t *testing.T) {
	tokenizer, err := NewCJKTokenizer(TokenizerConfig{Type: TokenizerCJK})
	require.NoError(t, err)

	tests := []struct {
		name        string
		input       string
		minTokens   int
		containsAll []string
	}{
		{
			name:      "pure chinese",
			input:     "数据库连接超时",
			minTokens: 1,
		},
		{
			name:        "mixed chinese and english",
			input:       "API数据库连接",
			minTokens:   2,
			containsAll: []string{"API"},
		},
		{
			name:      "pure english",
			input:     "Hello World",
			minTokens: 2,
		},
		{
			name:      "empty string",
			input:     "",
			minTokens: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tokens := tokenizer.Tokenize(tt.input)
			assert.GreaterOrEqual(t, len(tokens), tt.minTokens, "input: %s, tokens: %v", tt.input, tokens)
			for _, expected := range tt.containsAll {
				assert.Contains(t, tokens, expected, "should contain %s in %v", expected, tokens)
			}
		})
	}
}

func TestCJKTokenizer_DictionaryMatching(t *testing.T) {
	tokenizer, err := NewCJKTokenizer(TokenizerConfig{Type: TokenizerCJK})
	require.NoError(t, err)

	// The tokenizer should recognize common words from its built-in dictionary
	text := "数据库连接超时"
	tokens := tokenizer.Tokenize(text)

	// Should have some tokens
	assert.Greater(t, len(tokens), 0)
}

func TestCJKTokenizer_AddWords(t *testing.T) {
	tokenizer, err := NewCJKTokenizer(TokenizerConfig{Type: TokenizerCJK})
	require.NoError(t, err)

	initialSize := tokenizer.DictionarySize()

	// Add new words
	newWords := []string{"人工智能", "机器学习"}
	tokenizer.AddWords(newWords)

	newSize := tokenizer.DictionarySize()
	assert.GreaterOrEqual(t, newSize, initialSize+len(newWords))

	// Test that new words are recognized
	tokens := tokenizer.Tokenize("人工智能是机器学习")
	// The dictionary should help segment "人工智能" as a single word
	found := false
	for _, token := range tokens {
		if token == "人工智能" || token == "机器学习" {
			found = true
			break
		}
	}
	assert.True(t, found, "should find added words in tokens: %v", tokens)
}

func TestCJKTokenizer_RemoveWords(t *testing.T) {
	tokenizer, err := NewCJKTokenizer(TokenizerConfig{Type: TokenizerCJK})
	require.NoError(t, err)

	// First add words
	words := []string{"测试词汇"}
	tokenizer.AddWords(words)
	sizeAfterAdd := tokenizer.DictionarySize()

	// Then remove them
	tokenizer.RemoveWords(words)
	sizeAfterRemove := tokenizer.DictionarySize()

	assert.Less(t, sizeAfterRemove, sizeAfterAdd)
}

func TestCJKTokenizer_TokenizeForFTS(t *testing.T) {
	tokenizer, err := NewCJKTokenizer(TokenizerConfig{Type: TokenizerCJK})
	require.NoError(t, err)

	input := "数据库连接超时"
	result := tokenizer.TokenizeForFTS(input)

	// Should be space-separated tokens
	assert.Contains(t, result, " ")
	assert.Greater(t, len(result), 0)
}

func TestCJKTokenizer_Reconfigure(t *testing.T) {
	tokenizer, err := NewCJKTokenizer(TokenizerConfig{
		Type:           TokenizerCJK,
		MinTokenLength: 1,
	})
	require.NoError(t, err)

	// Reconfigure
	newCfg := TokenizerConfig{
		Type:           TokenizerCJK,
		MinTokenLength: 2,
	}
	err = tokenizer.Reconfigure(newCfg)
	require.NoError(t, err)

	// Check config was updated
	cfg := tokenizer.Config()
	assert.Equal(t, 2, cfg.MinTokenLength)
}

func TestFilterTokens(t *testing.T) {
	tests := []struct {
		name     string
		tokens   []string
		cfg      TokenizerConfig
		expected []string
	}{
		{
			name:   "no filtering",
			tokens: []string{"hello", "world"},
			cfg: TokenizerConfig{
				MinTokenLength: 0,
			},
			expected: []string{"hello", "world"},
		},
		{
			name:   "filter by min length",
			tokens: []string{"a", "bb", "ccc"},
			cfg: TokenizerConfig{
				MinTokenLength: 2,
			},
			expected: []string{"bb", "ccc"},
		},
		{
			name:   "filter empty tokens",
			tokens: []string{"hello", "", "world"},
			cfg: TokenizerConfig{
				MinTokenLength: 0,
			},
			expected: []string{"hello", "world"},
		},
		{
			name:     "nil input",
			tokens:   nil,
			cfg:      TokenizerConfig{},
			expected: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := filterTokens(tt.tokens, tt.cfg)
			assert.Equal(t, tt.expected, got)
		})
	}
}

func TestIsCJKRune(t *testing.T) {
	tests := []struct {
		name     string
		r        rune
		expected bool
	}{
		{"chinese character", '中', true},
		{"japanese hiragana", 'あ', true},
		{"japanese katakana", 'ア', true},
		{"korean hangul", '한', true},
		{"english letter", 'A', false},
		{"digit", '1', false},
		{"space", ' ', false},
		{"punctuation", '.', false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isCJKRune(tt.r)
			assert.Equal(t, tt.expected, got)
		})
	}
}
