// Package spec provides OpenAPI specification parsing and validation.
package spec

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"maps"
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

// computeHash computes the SHA256 hash of the given content.
func computeHash(content []byte) string {
	hash := sha256.Sum256(content)
	return hex.EncodeToString(hash[:])
}

// SpecFetchOptions contains options for fetching remote OpenAPI specifications.
// This allows passing custom headers, authentication, and other HTTP settings
// when loading specs from remote URLs.
type SpecFetchOptions struct {
	// Headers contains custom HTTP headers to send with the fetch request.
	// These are merged with default headers (Accept, User-Agent).
	Headers map[string]string

	// AuthType is the authentication type: "bearer", "api_key", "basic", or empty for none.
	AuthType string

	// AuthToken is the authentication token or credential value.
	// For bearer: the token value
	// For api_key: the API key value
	// For basic: base64-encoded "username:password"
	AuthToken string

	// AuthKeyName is the header or query parameter name for api_key auth.
	// Defaults to "Authorization" for bearer, or the specified key name for api_key.
	AuthKeyName string

	// AuthLocation is where to send api_key auth: "header" or "query".
	// Defaults to "header".
	AuthLocation string
}

// Clone returns a deep copy of SpecFetchOptions.
func (o *SpecFetchOptions) Clone() *SpecFetchOptions {
	if o == nil {
		return nil
	}
	clone := &SpecFetchOptions{
		AuthType:     o.AuthType,
		AuthToken:    o.AuthToken,
		AuthKeyName:  o.AuthKeyName,
		AuthLocation: o.AuthLocation,
	}
	if o.Headers != nil {
		clone.Headers = make(map[string]string, len(o.Headers))
		maps.Copy(clone.Headers, o.Headers)
	}
	return clone
}

// Merge combines two SpecFetchOptions, with override taking precedence.
// This allows per-spec overrides to be layered on top of profile defaults.
func (o *SpecFetchOptions) Merge(override *SpecFetchOptions) *SpecFetchOptions {
	if o == nil && override == nil {
		return nil
	}
	if o == nil {
		return override.Clone()
	}
	if override == nil {
		return o.Clone()
	}

	merged := o.Clone()

	// Override auth settings if specified
	if override.AuthType != "" {
		merged.AuthType = override.AuthType
		merged.AuthToken = override.AuthToken
		merged.AuthKeyName = override.AuthKeyName
		merged.AuthLocation = override.AuthLocation
	}

	// Merge headers (override takes precedence)
	if override.Headers != nil {
		if merged.Headers == nil {
			merged.Headers = make(map[string]string)
		}
		maps.Copy(merged.Headers, override.Headers)
	}

	return merged
}

// Parser handles loading, parsing, and caching of OpenAPI specifications.
type Parser struct {
	cache        sync.Map // map[string]*CachedSpec
	cacheTTL     time.Duration
	client       *http.Client
	loader       *openapi3.Loader
	fetchOptions *SpecFetchOptions

	// baseDir is the base directory for persistent caching
	baseDir string
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

// WithFetchOptions sets default fetch options for remote spec loading.
// These options provide custom headers and authentication for fetching specs.
func WithFetchOptions(opts *SpecFetchOptions) ParserOption {
	return func(p *Parser) {
		p.fetchOptions = opts
	}
}

// WithBaseDir sets the base directory for persistent caching.
func WithBaseDir(baseDir string) ParserOption {
	return func(p *Parser) {
		p.baseDir = baseDir
	}
}

// NewParser creates a new Parser with the given options. 🐾
func NewParser(opts ...ParserOption) *Parser {
	loader := openapi3.NewLoader()
	loader.IsExternalRefsAllowed = true

	p := &Parser{
		cacheTTL: 5 * time.Minute, // Default TTL for cache entries
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
		return p.loadFromURL(ctx, source, nil)
	}
	return p.loadFromFile(ctx, source)
}

// LoadSpecWithOptions loads an OpenAPI specification with custom fetch options.
// The provided options are merged with parser defaults (per-spec override takes precedence).
func (p *Parser) LoadSpecWithOptions(ctx context.Context, source string, opts *SpecFetchOptions) (*openapi3.T, error) {
	if isURL(source) {
		return p.loadFromURL(ctx, source, opts)
	}
	return p.loadFromFile(ctx, source)
}

// GetHTTPClient returns the HTTP client used by the parser.
// This allows sharing the same client with other components.
func (p *Parser) GetHTTPClient() *http.Client {
	return p.client
}

// GetFetchOptions returns the default fetch options configured for the parser.
func (p *Parser) GetFetchOptions() *SpecFetchOptions {
	return p.fetchOptions
}

// loadFromFile loads a specification from a local file.
func (p *Parser) loadFromFile(ctx context.Context, path string) (*openapi3.T, error) {
	absPath, err := p.resolveAbsolutePath(path)
	if err != nil {
		return nil, err
	}

	data, err := p.readSpecFile(path, absPath)
	if err != nil {
		return nil, err
	}

	return p.parseSpec(ctx, data)
}

// resolveAbsolutePath resolves the file path to an absolute path.
func (p *Parser) resolveAbsolutePath(path string) (string, error) {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("failed to resolve path: %w", err)
	}
	return absPath, nil
}

// readSpecFile reads the spec file content.
func (p *Parser) readSpecFile(path, absPath string) ([]byte, error) {
	data, err := os.ReadFile(absPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read spec file '%s': %w", path, err)
	}

	if len(data) == 0 {
		return nil, fmt.Errorf("spec file '%s' is empty", path)
	}

	return data, nil
}

// loadFromURL loads a specification from a remote URL.
// If perSpecOpts is provided, it is merged with parser defaults (per-spec takes precedence).
func (p *Parser) loadFromURL(ctx context.Context, specURL string, perSpecOpts *SpecFetchOptions) (*openapi3.T, error) {
	parsedURL, err := p.validateAndParseURL(specURL)
	if err != nil {
		return nil, err
	}

	req, err := p.createHTTPRequest(ctx, specURL)
	if err != nil {
		return nil, err
	}

	p.setDefaultHeaders(req)
	p.applyFetchOptions(req, p.fetchOptions.Merge(perSpecOpts))

	resp, err := p.executeHTTPRequest(specURL, req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	if err := p.checkHTTPResponse(specURL, resp); err != nil {
		return nil, err
	}

	data, err := p.readResponseBody(specURL, resp)
	if err != nil {
		return nil, err
	}

	return p.parseSpecWithBaseURL(ctx, data, parsedURL)
}

// validateAndParseURL validates and parses the URL.
func (p *Parser) validateAndParseURL(specURL string) (*url.URL, error) {
	parsedURL, err := url.Parse(specURL)
	if err != nil {
		return nil, fmt.Errorf("invalid URL: %w", err)
	}
	return parsedURL, nil
}

// createHTTPRequest creates a new HTTP request with context.
func (p *Parser) createHTTPRequest(ctx context.Context, specURL string) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, specURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	return req, nil
}

// setDefaultHeaders sets default headers on the request.
func (p *Parser) setDefaultHeaders(req *http.Request) {
	req.Header.Set("Accept", "application/json, application/yaml, text/yaml, */*")
	req.Header.Set("User-Agent", "OpenBridge/1.0")
}

// executeHTTPRequest executes the HTTP request.
func (p *Parser) executeHTTPRequest(specURL string, req *http.Request) (*http.Response, error) {
	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch spec from '%s': %w", specURL, err)
	}
	return resp, nil
}

// checkHTTPResponse checks if the HTTP response is valid.
func (p *Parser) checkHTTPResponse(specURL string, resp *http.Response) error {
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("failed to fetch spec from '%s': HTTP %d %s", specURL, resp.StatusCode, resp.Status)
	}
	return nil
}

// readResponseBody reads the response body.
func (p *Parser) readResponseBody(specURL string, resp *http.Response) ([]byte, error) {
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	if len(data) == 0 {
		return nil, fmt.Errorf("received empty response from '%s'", specURL)
	}

	return data, nil
}

// applyFetchOptions applies custom headers and authentication to an HTTP request.
func (p *Parser) applyFetchOptions(req *http.Request, opts *SpecFetchOptions) {
	if opts == nil {
		return
	}

	p.applyCustomHeaders(req, opts.Headers)
	p.applyAuthentication(req, opts)
}

// applyCustomHeaders sets custom headers on the request.
func (p *Parser) applyCustomHeaders(req *http.Request, headers map[string]string) {
	for key, value := range headers {
		req.Header.Set(key, value)
	}
}

// applyAuthentication applies authentication to the request based on auth type.
func (p *Parser) applyAuthentication(req *http.Request, opts *SpecFetchOptions) {
	if opts.AuthToken == "" {
		return
	}

	switch opts.AuthType {
	case "bearer":
		req.Header.Set("Authorization", "Bearer "+opts.AuthToken)
	case "basic":
		req.Header.Set("Authorization", "Basic "+opts.AuthToken)
	case "api_key":
		p.applyAPIKeyAuth(req, opts)
	default:
		// Unknown auth type, skip authentication
	}
}

// applyAPIKeyAuth applies API key authentication to the request.
func (p *Parser) applyAPIKeyAuth(req *http.Request, opts *SpecFetchOptions) {
	keyName := opts.AuthKeyName
	if keyName == "" {
		keyName = "X-API-Key"
	}

	location := opts.AuthLocation
	if location == "query" {
		q := req.URL.Query()
		q.Set(keyName, opts.AuthToken)
		req.URL.RawQuery = q.Encode()
	} else {
		req.Header.Set(keyName, opts.AuthToken)
	}
}

// parseSpec parses specification data and handles version detection.
func (p *Parser) parseSpec(ctx context.Context, data []byte) (*openapi3.T, error) {
	// Detect version from content
	version := detectVersion(data)

	switch version {
	case Version20:
		return p.parseSwagger(ctx, data)
	case Version30, Version31:
		return p.parseOpenAPI3(ctx, data)
	default:
		// Try OpenAPI 3.x first, then fall back to 2.0
		doc, err := p.parseOpenAPI3(ctx, data)
		if err == nil {
			return doc, nil
		}
		return p.parseSwagger(ctx, data)
	}
}

// parseSpecWithBaseURL parses spec data with a base URL for reference resolution.
func (p *Parser) parseSpecWithBaseURL(ctx context.Context, data []byte, baseURL *url.URL) (*openapi3.T, error) {
	version := detectVersion(data)

	var spec *openapi3.T
	var err error

	switch version {
	case Version20:
		spec, err = p.parseSwaggerWithBaseURL(ctx, data)
	case Version30, Version31:
		spec, err = p.parseOpenAPI3WithBaseURL(ctx, data, baseURL)
	default:
		spec, err = p.parseOpenAPI3WithBaseURL(ctx, data, baseURL)
		if err == nil {
			break
		}
		spec, err = p.parseSwaggerWithBaseURL(ctx, data)
	}

	if err != nil {
		return nil, err
	}

	return spec, nil
}

// parseOpenAPI3 parses an OpenAPI 3.x specification.
func (p *Parser) parseOpenAPI3(ctx context.Context, data []byte) (*openapi3.T, error) {
	doc, err := p.loader.LoadFromData(data)
	if err != nil {
		return nil, fmt.Errorf("failed to parse OpenAPI 3.x spec: %w", err)
	}

	// Validate the specification
	if err := doc.Validate(ctx); err != nil {
		// Workaround for kin-openapi validation issue with OpenAPI 3.1 type: "null"
		// See: https://github.com/getkin/kin-openapi/issues/???
		if strings.Contains(err.Error(), `unsupported 'type' value "null"`) {
			return doc, nil
		}
		return nil, fmt.Errorf("OpenAPI 3.x validation failed: %w", err)
	}

	return doc, nil
}

// parseOpenAPI3WithBaseURL parses OpenAPI 3.x with a base URL.
func (p *Parser) parseOpenAPI3WithBaseURL(_ context.Context, data []byte, baseURL *url.URL) (*openapi3.T, error) {
	doc, err := p.loader.LoadFromDataWithPath(data, baseURL)
	if err != nil {
		return nil, fmt.Errorf("failed to parse OpenAPI 3.x spec: %w", err)
	}

	ctx := context.Background()
	if err := doc.Validate(ctx); err != nil {
		// Workaround for kin-openapi validation issue with OpenAPI 3.1 type: "null"
		if strings.Contains(err.Error(), `unsupported 'type' value "null"`) {
			return doc, nil
		}
		return nil, fmt.Errorf("OpenAPI 3.x validation failed: %w", err)
	}

	return doc, nil
}

// parseSwagger parses an OpenAPI 2.0 (Swagger) specification.
func (p *Parser) parseSwagger(_ context.Context, data []byte) (*openapi3.T, error) {
	var swagger openapi2.T

	// Use ContentDetector to handle format detection and conversion
	detector := NewContentDetector()

	// Convert to JSON (handles both JSON and YAML input)
	// This is necessary because some nested types (e.g., openapi3.Types) only implement
	// UnmarshalJSON but not UnmarshalYAML, causing YAML parsing to fail with type errors
	// like "cannot unmarshal !!str `string` into openapi3.Types"
	jsonData, err := detector.ToJSONWithFallback(data)
	if err != nil {
		return nil, fmt.Errorf("failed to parse Swagger 2.0 spec: %w", err)
	}

	// Unmarshal the JSON data
	if err := json.Unmarshal(jsonData, &swagger); err != nil {
		return nil, fmt.Errorf("failed to unmarshal Swagger 2.0 spec: %w", err)
	}

	// Convert Swagger 2.0 to OpenAPI 3.0
	doc, err := openapi2conv.ToV3(&swagger)
	if err != nil {
		return nil, fmt.Errorf("failed to convert Swagger 2.0 to OpenAPI 3.0: %w", err)
	}

	// Validate the converted specification
	ctx := context.Background()
	if err := doc.Validate(ctx); err != nil {
		return nil, fmt.Errorf("converted OpenAPI 3.0 validation failed: %w", err)
	}

	return doc, nil
}

// parseSwaggerWithBaseURL parses Swagger 2.0 with a base URL.
func (p *Parser) parseSwaggerWithBaseURL(_ context.Context, data []byte) (*openapi3.T, error) {
	// Base URL does not change Swagger 2.0 conversion behavior here.
	ctx := context.Background()
	return p.parseSwagger(ctx, data)
}

// ValidateSpec validates an OpenAPI specification.
func (p *Parser) ValidateSpec(spec *openapi3.T) error {
	if spec == nil {
		return fmt.Errorf("spec is nil")
	}
	ctx := context.Background()
	return spec.Validate(ctx)
}

// validateSpecInfo validates the info section and adds errors/warnings.
func validateSpecInfo(spec *openapi3.T, result *ValidationResult) {
	if spec.Info == nil {
		result.Valid = false
		result.Errors = append(result.Errors, ValidationError{
			Path: "info", Message: "info object is required", Type: "required",
		})
		return
	}

	if spec.Info.Title == "" {
		result.Warnings = append(result.Warnings, ValidationError{
			Path: "info.title", Message: "title should not be empty", Type: "warning",
		})
	}
	if spec.Info.Version == "" {
		result.Warnings = append(result.Warnings, ValidationError{
			Path: "info.version", Message: "version should not be empty", Type: "warning",
		})
	}
}

// validateSpecPaths validates the paths section.
func validateSpecPaths(spec *openapi3.T, result *ValidationResult) {
	if spec.Paths == nil || spec.Paths.Len() == 0 {
		result.Warnings = append(result.Warnings, ValidationError{
			Path: "paths", Message: "no paths defined", Type: "warning",
		})
	}
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
			Path: "", Message: "specification is nil", Type: "fatal",
		})
		return result
	}

	config := &validationConfig{}
	for _, opt := range opts {
		opt(config)
	}

	ctx := context.Background()
	if err := spec.Validate(ctx); err != nil {
		result.Valid = false
		result.Errors = append(result.Errors, ValidationError{
			Path: "", Message: err.Error(), Type: "schema",
		})
	}

	validateSpecInfo(spec, result)
	validateSpecPaths(spec, result)

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
	p.cache.Range(func(key, value any) bool {
		p.cache.Delete(key)
		return true
	})
}

// GetCacheStats returns statistics about the cache.
func (p *Parser) GetCacheStats() CacheStats {
	stats := CacheStats{}
	now := time.Now()

	p.cache.Range(func(key, value any) bool {
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

// countPathOperations counts the number of operations in a path item.
func countPathOperations(pathItem *openapi3.PathItem) int {
	count := 0
	if pathItem.Get != nil {
		count++
	}
	if pathItem.Post != nil {
		count++
	}
	if pathItem.Put != nil {
		count++
	}
	if pathItem.Patch != nil {
		count++
	}
	if pathItem.Delete != nil {
		count++
	}
	if pathItem.Head != nil {
		count++
	}
	if pathItem.Options != nil {
		count++
	}
	return count
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
		for _, pathItem := range spec.Paths.Map() {
			info.Operations += countPathOperations(pathItem)
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

// ParseSpecFromJSON parses an OpenAPI specification from JSON bytes.
// This is useful for testing with inline spec definitions.
func (p *Parser) ParseSpecFromJSON(data []byte) (*openapi3.T, error) {
	ctx := context.Background()
	return p.parseSpec(ctx, data)
}

// LoadSpecWithPersistentCache loads an OpenAPI specification with persistent caching support.
// This enables cross-process caching of parsed specs to avoid re-parsing on each restart.
func (p *Parser) LoadSpecWithPersistentCache(ctx context.Context, source string, appName string) (*openapi3.T, error) {
	if !p.isPersistentCacheEnabled(appName) {
		return p.LoadSpecWithContext(ctx, source)
	}

	spec, err := p.tryLoadFromPersistentCache(ctx, source, appName)
	if err == nil && spec != nil {
		return spec, nil
	}

	return p.loadAndCacheSpec(ctx, source, appName)
}

// isPersistentCacheEnabled checks if persistent caching is enabled for the app.
func (p *Parser) isPersistentCacheEnabled(appName string) bool {
	return p.baseDir != "" && appName != ""
}

// tryLoadFromPersistentCache attempts to load spec from persistent cache.
func (p *Parser) tryLoadFromPersistentCache(_ context.Context, source string, appName string) (*openapi3.T, error) {
	spec, ok, err := p.loadFromPersistentCache(appName, source)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, nil
	}
	return spec, nil
}

// loadAndCacheSpec loads spec from source and saves it to cache.
func (p *Parser) loadAndCacheSpec(ctx context.Context, source string, appName string) (*openapi3.T, error) {
	spec, err := p.loadSpecFromSource(ctx, source)
	if err != nil {
		return nil, err
	}

	if err := p.saveToPersistentCache(appName, source, spec); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to save persistent cache: %v\n", err)
	}

	return spec, nil
}

// loadSpecFromSource loads a spec from file or URL.
func (p *Parser) loadSpecFromSource(ctx context.Context, source string) (*openapi3.T, error) {
	if isURL(source) {
		return p.loadFromURL(ctx, source, nil)
	}
	return p.loadFromFile(ctx, source)
}

// loadFromPersistentCache loads a spec from the persistent cache.
func (p *Parser) loadFromPersistentCache(appName string, source string) (*openapi3.T, bool, error) {
	cachePaths := p.getCachePaths(appName)

	if !p.cacheFilesExist(cachePaths) {
		return nil, false, nil
	}

	meta, err := p.readCacheMetadata(cachePaths.metaPath)
	if err != nil {
		return nil, false, err
	}

	if !p.isCacheValid(source, meta) {
		return nil, false, nil
	}

	return p.readParsedSpec(cachePaths.parsedPath)
}

// cachePaths contains the paths to cache files.
type cachePaths struct {
	parsedPath string
	metaPath   string
}

// getCachePaths returns the cache file paths for an app.
func (p *Parser) getCachePaths(appName string) cachePaths {
	cacheDir := p.getCacheDir(appName)
	return cachePaths{
		parsedPath: filepath.Join(cacheDir, "parsed.json"),
		metaPath:   filepath.Join(cacheDir, "meta.json"),
	}
}

// cacheFilesExist checks if cache files exist.
func (p *Parser) cacheFilesExist(paths cachePaths) bool {
	if _, err := os.Stat(paths.parsedPath); err != nil {
		return false
	}
	if _, err := os.Stat(paths.metaPath); err != nil {
		return false
	}
	return true
}

// readCacheMetadata reads and parses the cache metadata.
func (p *Parser) readCacheMetadata(metaPath string) (*ParsedCacheMeta, error) {
	data, err := os.ReadFile(metaPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read cache metadata: %w", err)
	}

	var meta ParsedCacheMeta
	if err := json.Unmarshal(data, &meta); err != nil {
		return nil, fmt.Errorf("failed to unmarshal cache metadata: %w", err)
	}

	return &meta, nil
}

// readParsedSpec reads and parses the cached spec.
func (p *Parser) readParsedSpec(parsedPath string) (*openapi3.T, bool, error) {
	data, err := os.ReadFile(parsedPath)
	if err != nil {
		return nil, false, fmt.Errorf("failed to read parsed spec: %w", err)
	}

	var spec openapi3.T
	if err := json.Unmarshal(data, &spec); err != nil {
		return nil, false, fmt.Errorf("failed to unmarshal parsed spec: %w", err)
	}

	return &spec, true, nil
}

// saveToPersistentCache saves a parsed spec to the persistent cache.
func (p *Parser) saveToPersistentCache(appName string, source string, spec *openapi3.T) error {
	cachePaths := p.getCachePaths(appName)

	if err := p.createCacheDirectory(cachePaths.parsedPath); err != nil {
		return err
	}

	meta := p.createCacheMetadata(source)
	if err := p.computeContentHash(source, meta); err != nil {
		return err
	}

	if err := p.writeParsedSpec(cachePaths.parsedPath, spec); err != nil {
		return err
	}

	return p.writeMetadata(cachePaths.metaPath, meta)
}

// createCacheDirectory creates the cache directory if it doesn't exist.
func (p *Parser) createCacheDirectory(parsedPath string) error {
	cacheDir := filepath.Dir(parsedPath)
	if err := os.MkdirAll(cacheDir, 0755); err != nil {
		return fmt.Errorf("failed to create cache directory: %w", err)
	}
	return nil
}

// createCacheMetadata creates the cache metadata.
func (p *Parser) createCacheMetadata(source string) *ParsedCacheMeta {
	return &ParsedCacheMeta{
		SourceURL:     source,
		ParsedAt:      time.Now(),
		ParserVersion: ParserVersion,
	}
}

// computeContentHash computes and sets the content hash for local files.
func (p *Parser) computeContentHash(source string, meta *ParsedCacheMeta) error {
	if isURL(source) {
		return nil
	}

	content, err := os.ReadFile(source)
	if err != nil {
		return fmt.Errorf("failed to read source for hashing: %w", err)
	}
	meta.ContentHash = computeHash(content)
	return nil
}

// writeParsedSpec writes the parsed spec to cache.
func (p *Parser) writeParsedSpec(parsedPath string, spec *openapi3.T) error {
	specData, err := json.MarshalIndent(spec, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal parsed spec: %w", err)
	}

	tmpPath := parsedPath + ".tmp"
	if err := os.WriteFile(tmpPath, specData, 0644); err != nil {
		return fmt.Errorf("failed to write parsed spec: %w", err)
	}
	if err := os.Rename(tmpPath, parsedPath); err != nil {
		return fmt.Errorf("failed to move parsed spec: %w", err)
	}

	return nil
}

// writeMetadata writes the metadata to cache.
func (p *Parser) writeMetadata(metaPath string, meta *ParsedCacheMeta) error {
	metaData, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal metadata: %w", err)
	}

	if err := os.WriteFile(metaPath, metaData, 0644); err != nil {
		return fmt.Errorf("failed to write metadata: %w", err)
	}

	return nil
}

// isCacheValid checks if the persistent cache is still valid.
func (p *Parser) isCacheValid(source string, meta *ParsedCacheMeta) bool {
	// Check parser version
	if meta.ParserVersion != ParserVersion {
		return false
	}

	// Check if source has changed (for local files)
	if !isURL(source) {
		content, err := os.ReadFile(source)
		if err != nil {
			return false
		}
		currentHash := computeHash(content)
		if currentHash != meta.ContentHash {
			return false
		}
	}

	return true
}

// getCacheDir returns the cache directory for an app.
func (p *Parser) getCacheDir(appName string) string {
	return filepath.Join(p.baseDir, appName, "cache")
}

// ParsedCacheMeta contains metadata about a cached parsed spec.
type ParsedCacheMeta struct {
	SourceURL     string    `json:"source_url"`
	ParsedAt      time.Time `json:"parsed_at"`
	ParserVersion string    `json:"parser_version"`
	ContentHash   string    `json:"content_hash,omitempty"`
}

// ParserVersion is the version of the parser used for persistent caching.
const ParserVersion = "1.0"
