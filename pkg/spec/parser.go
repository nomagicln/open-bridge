// Package spec provides OpenAPI specification parsing and validation.
package spec

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/getkin/kin-openapi/openapi2"
	"github.com/getkin/kin-openapi/openapi2conv"
	"github.com/getkin/kin-openapi/openapi3"
)

// SpecVersion represents the OpenAPI specification version.
type SpecVersion string

const (
	// VersionUnknown indicates an unknown or invalid version.
	VersionUnknown SpecVersion = "unknown"
	// Version20 represents OpenAPI/Swagger 2.0.
	Version20 SpecVersion = "2.0"
	// Version30 represents OpenAPI 3.0.x.
	Version30 SpecVersion = "3.0"
	// Version31 represents OpenAPI 3.1.x.
	Version31 SpecVersion = "3.1"
)

// Parser handles loading, parsing, and caching of OpenAPI specifications.
type Parser struct {
	cache    sync.Map // map[string]*CachedSpec
	cacheTTL time.Duration
	client   *http.Client
	loader   *openapi3.Loader
}

// CachedSpec represents a cached OpenAPI specification with metadata.
type CachedSpec struct {
	Spec       *openapi3.T
	Version    SpecVersion
	Source     string
	CachedAt   time.Time
	ExpiresAt  time.Time
	AccessedAt time.Time
}

// SpecInfo contains metadata about a parsed specification.
type SpecInfo struct {
	Title       string
	Version     string
	SpecVersion SpecVersion
	Source      string
	PathCount   int
	Operations  int
}

// ParserOption is a function that configures a Parser.
type ParserOption func(*Parser)

// WithCacheTTL sets the cache TTL for parsed specifications.
func WithCacheTTL(ttl time.Duration) ParserOption {
	return func(p *Parser) {
		p.cacheTTL = ttl
	}
}

// WithHTTPClient sets a custom HTTP client for fetching remote specs.
func WithHTTPClient(client *http.Client) ParserOption {
	return func(p *Parser) {
		p.client = client
	}
}

// NewParser creates a new Parser with the given options.
func NewParser(opts ...ParserOption) *Parser {
	loader := openapi3.NewLoader()
	loader.IsExternalRefsAllowed = true

	p := &Parser{
		cacheTTL: 5 * time.Minute, // Default TTL
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
		loader: loader,
	}
	for _, opt := range opts {
		opt(p)
	}
	return p
}

// LoadSpec loads an OpenAPI specification from a file path or URL.
// It automatically detects the version (2.0, 3.0, or 3.1) and converts
// OpenAPI 2.0 (Swagger) specs to 3.x format.
func (p *Parser) LoadSpec(source string) (*openapi3.T, error) {
	ctx := context.Background()
	return p.LoadSpecWithContext(ctx, source)
}

// LoadSpecWithContext loads an OpenAPI specification with context support.
func (p *Parser) LoadSpecWithContext(ctx context.Context, source string) (*openapi3.T, error) {
	// Detect if source is a URL or file path
	if isURL(source) {
		return p.loadFromURL(ctx, source)
	}
	return p.loadFromFile(ctx, source)
}

// loadFromFile loads a specification from a local file.
func (p *Parser) loadFromFile(ctx context.Context, path string) (*openapi3.T, error) {
	// Resolve absolute path
	absPath, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve path: %w", err)
	}

	// Read file content
	data, err := os.ReadFile(absPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read spec file '%s': %w", path, err)
	}

	if len(data) == 0 {
		return nil, fmt.Errorf("spec file '%s' is empty", path)
	}

	return p.parseSpec(ctx, data, absPath)
}

// loadFromURL loads a specification from a remote URL.
func (p *Parser) loadFromURL(ctx context.Context, specURL string) (*openapi3.T, error) {
	// Validate URL
	parsedURL, err := url.Parse(specURL)
	if err != nil {
		return nil, fmt.Errorf("invalid URL: %w", err)
	}

	// Fetch the spec
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, specURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// Set common headers
	req.Header.Set("Accept", "application/json, application/yaml, text/yaml, */*")
	req.Header.Set("User-Agent", "OpenBridge/1.0")

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch spec from '%s': %w", specURL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to fetch spec from '%s': HTTP %d %s", specURL, resp.StatusCode, resp.Status)
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	if len(data) == 0 {
		return nil, fmt.Errorf("received empty response from '%s'", specURL)
	}

	// Use the URL as the source for relative reference resolution
	return p.parseSpecWithBaseURL(ctx, data, parsedURL)
}

// parseSpec parses specification data and handles version detection.
func (p *Parser) parseSpec(ctx context.Context, data []byte, source string) (*openapi3.T, error) {
	// Detect version from content
	version := detectVersion(data)

	switch version {
	case Version20:
		return p.parseSwagger(ctx, data, source)
	case Version30, Version31:
		return p.parseOpenAPI3(ctx, data, source)
	default:
		// Try OpenAPI 3.x first, then fall back to 2.0
		doc, err := p.parseOpenAPI3(ctx, data, source)
		if err == nil {
			return doc, nil
		}
		return p.parseSwagger(ctx, data, source)
	}
}

// parseSpecWithBaseURL parses spec data with a base URL for reference resolution.
func (p *Parser) parseSpecWithBaseURL(ctx context.Context, data []byte, baseURL *url.URL) (*openapi3.T, error) {
	version := detectVersion(data)

	switch version {
	case Version20:
		return p.parseSwaggerWithBaseURL(ctx, data, baseURL)
	case Version30, Version31:
		return p.parseOpenAPI3WithBaseURL(ctx, data, baseURL)
	default:
		doc, err := p.parseOpenAPI3WithBaseURL(ctx, data, baseURL)
		if err == nil {
			return doc, nil
		}
		return p.parseSwaggerWithBaseURL(ctx, data, baseURL)
	}
}

// parseOpenAPI3 parses an OpenAPI 3.x specification.
func (p *Parser) parseOpenAPI3(ctx context.Context, data []byte, source string) (*openapi3.T, error) {
	doc, err := p.loader.LoadFromData(data)
	if err != nil {
		return nil, fmt.Errorf("failed to parse OpenAPI 3.x spec: %w", err)
	}

	// Validate the specification
	if err := doc.Validate(ctx); err != nil {
		return nil, fmt.Errorf("OpenAPI 3.x validation failed: %w", err)
	}

	return doc, nil
}

// parseOpenAPI3WithBaseURL parses OpenAPI 3.x with a base URL.
func (p *Parser) parseOpenAPI3WithBaseURL(ctx context.Context, data []byte, baseURL *url.URL) (*openapi3.T, error) {
	doc, err := p.loader.LoadFromDataWithPath(data, baseURL)
	if err != nil {
		return nil, fmt.Errorf("failed to parse OpenAPI 3.x spec: %w", err)
	}

	if err := doc.Validate(ctx); err != nil {
		return nil, fmt.Errorf("OpenAPI 3.x validation failed: %w", err)
	}

	return doc, nil
}

// parseSwagger parses an OpenAPI 2.0 (Swagger) specification.
func (p *Parser) parseSwagger(ctx context.Context, data []byte, source string) (*openapi3.T, error) {
	var swagger openapi2.T
	if err := json.Unmarshal(data, &swagger); err != nil {
		// Try YAML format by using the loader's yaml support
		return nil, fmt.Errorf("failed to parse Swagger 2.0 spec: %w", err)
	}

	// Convert Swagger 2.0 to OpenAPI 3.0
	doc, err := openapi2conv.ToV3(&swagger)
	if err != nil {
		return nil, fmt.Errorf("failed to convert Swagger 2.0 to OpenAPI 3.0: %w", err)
	}

	// Validate the converted specification
	if err := doc.Validate(ctx); err != nil {
		return nil, fmt.Errorf("converted OpenAPI 3.0 validation failed: %w", err)
	}

	return doc, nil
}

// parseSwaggerWithBaseURL parses Swagger 2.0 with a base URL.
func (p *Parser) parseSwaggerWithBaseURL(ctx context.Context, data []byte, baseURL *url.URL) (*openapi3.T, error) {
	var swagger openapi2.T
	if err := json.Unmarshal(data, &swagger); err != nil {
		return nil, fmt.Errorf("failed to parse Swagger 2.0 spec: %w", err)
	}

	doc, err := openapi2conv.ToV3(&swagger)
	if err != nil {
		return nil, fmt.Errorf("failed to convert Swagger 2.0 to OpenAPI 3.0: %w", err)
	}

	if err := doc.Validate(ctx); err != nil {
		return nil, fmt.Errorf("converted OpenAPI 3.0 validation failed: %w", err)
	}

	return doc, nil
}

// ValidateSpec validates an OpenAPI specification.
func (p *Parser) ValidateSpec(spec *openapi3.T) error {
	if spec == nil {
		return fmt.Errorf("spec is nil")
	}
	ctx := context.Background()
	return spec.Validate(ctx)
}

// ValidateSpecWithOptions provides detailed validation with options.
func (p *Parser) ValidateSpecWithOptions(spec *openapi3.T, opts ...ValidationOption) *ValidationResult {
	result := &ValidationResult{
		Valid:    true,
		Errors:   make([]ValidationError, 0),
		Warnings: make([]ValidationError, 0),
	}

	if spec == nil {
		result.Valid = false
		result.Errors = append(result.Errors, ValidationError{
			Path:    "",
			Message: "specification is nil",
			Type:    "fatal",
		})
		return result
	}

	// Apply options
	config := &validationConfig{}
	for _, opt := range opts {
		opt(config)
	}

	// Basic validation
	ctx := context.Background()
	if err := spec.Validate(ctx); err != nil {
		result.Valid = false
		result.Errors = append(result.Errors, ValidationError{
			Path:    "",
			Message: err.Error(),
			Type:    "schema",
		})
	}

	// Check for required fields
	if spec.Info == nil {
		result.Valid = false
		result.Errors = append(result.Errors, ValidationError{
			Path:    "info",
			Message: "info object is required",
			Type:    "required",
		})
	} else {
		if spec.Info.Title == "" {
			result.Warnings = append(result.Warnings, ValidationError{
				Path:    "info.title",
				Message: "title should not be empty",
				Type:    "warning",
			})
		}
		if spec.Info.Version == "" {
			result.Warnings = append(result.Warnings, ValidationError{
				Path:    "info.version",
				Message: "version should not be empty",
				Type:    "warning",
			})
		}
	}

	// Check paths
	if spec.Paths == nil || spec.Paths.Len() == 0 {
		result.Warnings = append(result.Warnings, ValidationError{
			Path:    "paths",
			Message: "no paths defined",
			Type:    "warning",
		})
	}

	return result
}

// ValidationResult contains the result of spec validation.
type ValidationResult struct {
	Valid    bool
	Errors   []ValidationError
	Warnings []ValidationError
}

// ValidationError represents a validation error or warning.
type ValidationError struct {
	Path    string
	Message string
	Type    string // "fatal", "schema", "required", "warning"
}

// ValidationOption configures validation behavior.
type ValidationOption func(*validationConfig)

type validationConfig struct {
	strictMode bool
}

// WithStrictValidation enables strict validation mode.
func WithStrictValidation() ValidationOption {
	return func(c *validationConfig) {
		c.strictMode = true
	}
}

// GetCachedSpec retrieves a cached spec by app name if not expired.
func (p *Parser) GetCachedSpec(appName string) (*openapi3.T, bool) {
	value, ok := p.cache.Load(appName)
	if !ok {
		return nil, false
	}

	cached := value.(*CachedSpec)
	if time.Now().After(cached.ExpiresAt) {
		p.cache.Delete(appName)
		return nil, false
	}

	// Update access time
	cached.AccessedAt = time.Now()

	return cached.Spec, true
}

// GetCachedSpecInfo retrieves cached spec metadata.
func (p *Parser) GetCachedSpecInfo(appName string) (*CachedSpec, bool) {
	value, ok := p.cache.Load(appName)
	if !ok {
		return nil, false
	}

	cached := value.(*CachedSpec)
	if time.Now().After(cached.ExpiresAt) {
		p.cache.Delete(appName)
		return nil, false
	}

	return cached, true
}

// CacheSpec stores a parsed spec in the cache.
func (p *Parser) CacheSpec(appName string, spec *openapi3.T) {
	p.CacheSpecWithSource(appName, spec, "", VersionUnknown)
}

// CacheSpecWithSource stores a parsed spec with source metadata.
func (p *Parser) CacheSpecWithSource(appName string, spec *openapi3.T, source string, version SpecVersion) {
	now := time.Now()
	p.cache.Store(appName, &CachedSpec{
		Spec:       spec,
		Version:    version,
		Source:     source,
		CachedAt:   now,
		ExpiresAt:  now.Add(p.cacheTTL),
		AccessedAt: now,
	})
}

// InvalidateCache removes a specific spec from the cache.
func (p *Parser) InvalidateCache(appName string) {
	p.cache.Delete(appName)
}

// ClearCache removes all cached specifications.
func (p *Parser) ClearCache() {
	p.cache.Range(func(key, value interface{}) bool {
		p.cache.Delete(key)
		return true
	})
}

// GetCacheStats returns statistics about the cache.
func (p *Parser) GetCacheStats() CacheStats {
	stats := CacheStats{}
	now := time.Now()

	p.cache.Range(func(key, value interface{}) bool {
		stats.TotalEntries++
		cached := value.(*CachedSpec)
		if now.After(cached.ExpiresAt) {
			stats.ExpiredEntries++
		} else {
			stats.ActiveEntries++
		}
		return true
	})

	return stats
}

// CacheStats contains cache statistics.
type CacheStats struct {
	TotalEntries   int
	ActiveEntries  int
	ExpiredEntries int
}

// GetSpecInfo extracts metadata from a parsed specification.
func GetSpecInfo(spec *openapi3.T, source string) *SpecInfo {
	if spec == nil {
		return nil
	}

	info := &SpecInfo{
		Source:      source,
		SpecVersion: detectVersionFromSpec(spec),
	}

	if spec.Info != nil {
		info.Title = spec.Info.Title
		info.Version = spec.Info.Version
	}

	if spec.Paths != nil {
		info.PathCount = spec.Paths.Len()

		// Count operations
		for _, pathItem := range spec.Paths.Map() {
			if pathItem.Get != nil {
				info.Operations++
			}
			if pathItem.Post != nil {
				info.Operations++
			}
			if pathItem.Put != nil {
				info.Operations++
			}
			if pathItem.Patch != nil {
				info.Operations++
			}
			if pathItem.Delete != nil {
				info.Operations++
			}
			if pathItem.Head != nil {
				info.Operations++
			}
			if pathItem.Options != nil {
				info.Operations++
			}
		}
	}

	return info
}

// detectVersion detects the OpenAPI version from raw data.
func detectVersion(data []byte) SpecVersion {
	// Quick detection using string matching
	content := string(data)

	// Check for OpenAPI 3.x
	if strings.Contains(content, `"openapi"`) || strings.Contains(content, "openapi:") {
		if strings.Contains(content, `"3.1`) || strings.Contains(content, "3.1.") {
			return Version31
		}
		if strings.Contains(content, `"3.0`) || strings.Contains(content, "3.0.") {
			return Version30
		}
	}

	// Check for Swagger 2.0
	if strings.Contains(content, `"swagger"`) || strings.Contains(content, "swagger:") {
		if strings.Contains(content, `"2.0"`) || strings.Contains(content, `'2.0'`) || strings.Contains(content, "2.0") {
			return Version20
		}
	}

	return VersionUnknown
}

// detectVersionFromSpec detects the version from a parsed spec.
func detectVersionFromSpec(spec *openapi3.T) SpecVersion {
	if spec == nil {
		return VersionUnknown
	}

	openapi := spec.OpenAPI
	if strings.HasPrefix(openapi, "3.1") {
		return Version31
	}
	if strings.HasPrefix(openapi, "3.0") {
		return Version30
	}
	// Converted from 2.0 will show as 3.0
	return Version30
}

// isURL checks if a string is a URL.
func isURL(s string) bool {
	u, err := url.Parse(s)
	if err != nil {
		return false
	}
	return u.Scheme == "http" || u.Scheme == "https"
}
