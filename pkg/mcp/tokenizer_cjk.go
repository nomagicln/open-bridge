package mcp

import (
	"strings"
	"sync"
	"unicode"
)

// CJKTokenizer implements Chinese/Japanese/Korean segmentation.
// It uses a combination of dictionary-based and statistical methods
// for word segmentation in CJK languages.
//
// This implementation provides a lightweight pure-Go segmentation approach.
// For production use with higher accuracy, consider integrating with
// github.com/go-ego/gse which provides more sophisticated segmentation.
type CJKTokenizer struct {
	cfg         TokenizerConfig
	dictionary  map[string]bool
	maxWordLen  int
	mu          sync.RWMutex
	initialized bool
	initOnce    sync.Once
}

// NewCJKTokenizer creates a new CJK tokenizer.
func NewCJKTokenizer(cfg TokenizerConfig) (*CJKTokenizer, error) {
	if cfg.Type == "" {
		cfg.Type = TokenizerCJK
	}

	t := &CJKTokenizer{
		cfg:        cfg,
		dictionary: make(map[string]bool),
		maxWordLen: 4, // Maximum word length for dictionary lookup
	}

	return t, nil
}

// initialize loads the dictionary lazily.
func (t *CJKTokenizer) initialize() {
	t.initOnce.Do(func() {
		t.mu.Lock()
		defer t.mu.Unlock()

		// Load a basic dictionary of common CJK words
		// This is a simplified dictionary for demonstration
		// In production, load from a file or use gse library
		commonWords := []string{
			// Common Chinese words
			"数据库", "连接", "超时", "用户", "密码", "登录", "注册",
			"查询", "删除", "更新", "创建", "修改", "配置", "设置",
			"文件", "系统", "服务", "接口", "参数", "返回", "请求",
			"响应", "错误", "成功", "失败", "处理", "执行", "操作",
			"信息", "数据", "内容", "状态", "类型", "名称", "描述",
			"时间", "日期", "开始", "结束", "搜索", "过滤", "排序",
			"分页", "列表", "详情", "添加", "编辑", "保存", "提交",
			// Common Japanese words
			"ユーザー", "パスワード", "ログイン", "データ", "システム",
			"エラー", "成功", "失敗", "処理", "実行", "操作", "情報",
			// Common Korean words
			"사용자", "비밀번호", "로그인", "데이터", "시스템",
			"오류", "성공", "실패", "처리", "실행", "작업", "정보",
		}

		for _, word := range commonWords {
			t.dictionary[word] = true
			if len([]rune(word)) > t.maxWordLen {
				t.maxWordLen = len([]rune(word))
			}
		}

		t.initialized = true
	})
}

// Tokenize splits CJK text into tokens using dictionary-based segmentation.
//
//nolint:funlen,gocognit // CJK segmentation logic requires comprehensive handling
func (t *CJKTokenizer) Tokenize(text string) []string {
	if text == "" {
		return nil
	}

	t.initialize()

	t.mu.RLock()
	defer t.mu.RUnlock()

	var tokens []string
	runes := []rune(text)
	n := len(runes)

	var current strings.Builder
	i := 0

	for i < n {
		r := runes[i]

		switch {
		case isCJKRune(r):
			// Emit any accumulated non-CJK text
			emitCurrent(&current, &tokens)

			// Try to match longest word in dictionary (forward maximum matching)
			matchLen := t.matchDictionaryWord(runes, i, n)
			if matchLen > 1 {
				tokens = append(tokens, string(runes[i:i+matchLen]))
				i += matchLen
			} else {
				// No dictionary match - emit single character
				tokens = append(tokens, string(r))
				i++
			}

		case unicode.IsLetter(r) || unicode.IsDigit(r):
			// Accumulate non-CJK letters and digits
			current.WriteRune(r)
			i++

		default:
			// Punctuation or whitespace - emit current token
			emitCurrent(&current, &tokens)
			i++
		}
	}

	// Emit any remaining text
	emitCurrent(&current, &tokens)

	return filterTokens(tokens, t.cfg)
}

// matchDictionaryWord finds the longest dictionary match starting at position i.
// Returns the match length if found, 0 otherwise.
func (t *CJKTokenizer) matchDictionaryWord(runes []rune, i, n int) int {
	for length := min(t.maxWordLen, n-i); length > 1; length-- {
		word := string(runes[i : i+length])
		if t.dictionary[word] {
			return length
		}
	}
	return 0
}

// emitCurrent appends the current buffer to tokens if non-empty and resets it.
func emitCurrent(current *strings.Builder, tokens *[]string) {
	if current.Len() > 0 {
		*tokens = append(*tokens, current.String())
		current.Reset()
	}
}

// TokenizeForFTS returns text tokenized for FTS5.
// This is critical for making SQLite FTS5 work with CJK text.
func (t *CJKTokenizer) TokenizeForFTS(text string) string {
	tokens := t.Tokenize(text)
	return strings.Join(tokens, " ")
}

// Type returns the tokenizer type.
func (t *CJKTokenizer) Type() TokenizerType {
	return TokenizerCJK
}

// Config returns the current configuration.
func (t *CJKTokenizer) Config() TokenizerConfig {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.cfg
}

// Reconfigure updates the tokenizer configuration.
func (t *CJKTokenizer) Reconfigure(cfg TokenizerConfig) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.cfg = cfg

	// If dictionary path changed, reset for re-initialization
	if cfg.DictPath != "" && cfg.DictPath != t.cfg.DictPath {
		t.initialized = false
		t.initOnce = sync.Once{}
	}

	return nil
}

// AddWords adds words to the dictionary.
func (t *CJKTokenizer) AddWords(words []string) {
	t.initialize()

	t.mu.Lock()
	defer t.mu.Unlock()

	for _, word := range words {
		t.dictionary[word] = true
		runeLen := len([]rune(word))
		if runeLen > t.maxWordLen {
			t.maxWordLen = runeLen
		}
	}
}

// RemoveWords removes words from the dictionary.
func (t *CJKTokenizer) RemoveWords(words []string) {
	t.mu.Lock()
	defer t.mu.Unlock()

	for _, word := range words {
		delete(t.dictionary, word)
	}
}

// DictionarySize returns the number of words in the dictionary.
func (t *CJKTokenizer) DictionarySize() int {
	t.initialize()

	t.mu.RLock()
	defer t.mu.RUnlock()
	return len(t.dictionary)
}
