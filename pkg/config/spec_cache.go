// Package config provides configuration management for OpenBridge.
// This file contains spec caching functionality for offline support and performance.
package config

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/getkin/kin-openapi/openapi3"
)

// DefaultCacheTTL is the default time-to-live for cached specs when no HTTP cache headers are present.
const DefaultCacheTTL = 24 * time.Hour

// ParserVersion is the version of the parser used for persistent caching.
const ParserVersion = "1.0"

// SpecCacheMeta contains metadata about a cached spec file.
type SpecCacheMeta struct {
	// SourceURL is the original URL or path of the spec.
	SourceURL string `json:"source_url"`

	// Format is the original format of the spec: "yaml" or "json".
	Format string `json:"format"`

	// ETag is the HTTP ETag header value from the server.
	ETag string `json:"etag,omitempty"`

	// LastModified is the HTTP Last-Modified header value from the server.
	LastModified string `json:"last_modified,omitempty"`

	// FetchedAt is when the spec was last fetched.
	FetchedAt time.Time `json:"fetched_at"`

	// ExpiresAt is when the cached spec should be considered stale.
	ExpiresAt time.Time `json:"expires_at"`

	// ContentHash is the SHA256 hash of the spec content for integrity checking.
	ContentHash string `json:"content_hash"`

	// Size is the size of the cached spec file in bytes.
	Size int64 `json:"size"`

	// FetchHeaders contains custom headers used when fetching the spec.
	// These are persisted to enable revalidation with the same credentials.
	FetchHeaders map[string]string `json:"fetch_headers,omitempty"`

	// FetchAuthType is the authentication type used when fetching: "bearer", "api_key", "basic", or empty.
	FetchAuthType string `json:"fetch_auth_type,omitempty"`

	// FetchAuthKeyName is the header/query parameter name for api_key auth.
	FetchAuthKeyName string `json:"fetch_auth_key_name,omitempty"`

	// FetchAuthLocation is where api_key auth was sent: "header" or "query".
	FetchAuthLocation string `json:"fetch_auth_location,omitempty"`

	// ParsedSpecPath is the file path where the parsed spec is stored.
	ParsedSpecPath string `json:"parsed_spec_path,omitempty"`

	// ParsedAt is when the spec was last parsed and saved.
	ParsedAt time.Time `json:"parsed_at,omitzero"`

	// ParserVersion is the version of the parser used to generate the cached spec.
	ParserVersion string `json:"parser_version,omitempty"`
}

// IsStale returns true if the cached spec has expired.
func (m *SpecCacheMeta) IsStale() bool {
	return time.Now().After(m.ExpiresAt)
}

// SpecFetchOptions contains options for fetching remote specs.
// This is imported from pkg/spec but redefined here to avoid circular imports.
// The values are compatible and can be converted between the two types.
type SpecFetchOptions struct {
	// Headers contains custom HTTP headers to send with the fetch request.
	Headers map[string]string

	// AuthType is the authentication type: "bearer", "api_key", "basic", or empty for none.
	AuthType string

	// AuthToken is the authentication token or credential value.
	AuthToken string

	// AuthKeyName is the header or query parameter name for api_key auth.
	AuthKeyName string

	// AuthLocation is where to send api_key auth: "header" or "query".
	AuthLocation string
}

// SpecCacheManager manages caching of OpenAPI specifications.
type SpecCacheManager struct {
	baseDir      string
	httpClient   *http.Client
	fetchOptions *SpecFetchOptions
}

// NewSpecCacheManager creates a new spec cache manager.
func NewSpecCacheManager(baseDir string) *SpecCacheManager {
	return &SpecCacheManager{
		baseDir: baseDir,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// NewSpecCacheManagerWithOptions creates a new spec cache manager with custom client and fetch options.
func NewSpecCacheManagerWithOptions(baseDir string, client *http.Client, opts *SpecFetchOptions) *SpecCacheManager {
	mgr := &SpecCacheManager{
		baseDir:      baseDir,
		fetchOptions: opts,
	}
	if client != nil {
		mgr.httpClient = client
	} else {
		mgr.httpClient = &http.Client{
			Timeout: 30 * time.Second,
		}
	}
	return mgr
}

// SetHTTPClient sets a custom HTTP client for fetching specs.
// This is useful for configuring TLS settings.
func (c *SpecCacheManager) SetHTTPClient(client *http.Client) {
	c.httpClient = client
}

// SetFetchOptions sets default fetch options for remote spec fetching.
func (c *SpecCacheManager) SetFetchOptions(opts *SpecFetchOptions) {
	c.fetchOptions = opts
}

// getCacheDir returns the cache directory for an app.
func (c *SpecCacheManager) getCacheDir(appName string) string {
	return filepath.Join(c.baseDir, appName, "cache")
}

// getSpecPath returns the path to the cached spec file.
func (c *SpecCacheManager) getSpecPath(appName, format string) string {
	ext := format
	if ext == "" {
		ext = "yaml"
	}
	return filepath.Join(c.getCacheDir(appName), fmt.Sprintf("spec.%s", ext))
}

// getMetaPath returns the path to the cache metadata file.
func (c *SpecCacheManager) getMetaPath(appName string) string {
	return filepath.Join(c.getCacheDir(appName), "meta.json")
}

// detectFormat detects the spec format from content type or URL.
func detectFormat(contentType, url string) string {
	// Check content type first
	if strings.Contains(contentType, "json") {
		return "json"
	}
	if strings.Contains(contentType, "yaml") || strings.Contains(contentType, "yml") {
		return "yaml"
	}

	// Fall back to URL extension
	lower := strings.ToLower(url)
	if strings.HasSuffix(lower, ".json") {
		return "json"
	}
	return "yaml" // Default to YAML
}

// computeHash computes SHA256 hash of content.
func computeHash(content []byte) string {
	hash := sha256.Sum256(content)
	return hex.EncodeToString(hash[:])
}

// parseCacheControl extracts cache directives from Cache-Control header.
func parseCacheControl(cacheControl string) (int, bool) {
	for directive := range strings.SplitSeq(cacheControl, ",") {
		directive = strings.TrimSpace(directive)
		if after, ok := strings.CutPrefix(directive, "max-age="); ok {
			if seconds, err := strconv.Atoi(after); err == nil {
				return seconds, true
			}
		}
		// no-cache or no-store means always revalidate
		if directive == "no-cache" || directive == "no-store" {
			return 0, false
		}
	}
	return 0, false
}

// parseExpiresAt parses HTTP cache headers to determine expiration time.
func parseExpiresAt(resp *http.Response) time.Time {
	now := time.Now()

	// Check Cache-Control header
	if cacheControl := resp.Header.Get("Cache-Control"); cacheControl != "" {
		if seconds, ok := parseCacheControl(cacheControl); ok {
			return now.Add(time.Duration(seconds) * time.Second)
		}
		// If no-cache/no-store, return already expired
		if seconds, ok := parseCacheControl(cacheControl); !ok && seconds == 0 {
			return now
		}
	}

	// Check Expires header
	if expires := resp.Header.Get("Expires"); expires != "" {
		if t, err := http.ParseTime(expires); err == nil {
			return t
		}
	}

	// Default TTL
	return now.Add(DefaultCacheTTL)
}

// FetchResult contains the result of fetching a spec.
type FetchResult struct {
	// Content is the raw spec content.
	Content []byte

	// Format is the detected format: "yaml" or "json".
	Format string

	// Meta contains cache metadata.
	Meta *SpecCacheMeta

	// FromCache indicates if the result was served from cache.
	FromCache bool

	// CacheWarning contains a warning message if cache was used due to network failure.
	CacheWarning string
}

// FetchWithCache fetches a spec with caching support.
// For HTTP URLs, it respects cache headers and supports offline fallback.
// For local files, it reads directly without caching.
func (c *SpecCacheManager) FetchWithCache(appName, source string) (*FetchResult, error) {
	return c.FetchWithCacheAndOptions(appName, source, nil)
}

// FetchWithCacheAndOptions fetches a spec with caching support and custom fetch options.
// The provided options are merged with manager defaults (per-spec override takes precedence).
func (c *SpecCacheManager) FetchWithCacheAndOptions(appName, source string, opts *SpecFetchOptions) (*FetchResult, error) {
	if !isWebURL(source) {
		return c.fetchLocalFile(source)
	}
	// Merge manager defaults with per-spec options
	effectiveOpts := c.mergeFetchOptions(opts)
	return c.fetchRemoteWithCache(appName, source, effectiveOpts)
}

// copyHeaders creates a copy of a header map.
func copyHeaders(headers map[string]string) map[string]string {
	if headers == nil {
		return nil
	}
	out := make(map[string]string, len(headers))
	maps.Copy(out, headers)
	return out
}

// mergeFetchOptions merges manager defaults with per-spec overrides.
func (c *SpecCacheManager) mergeFetchOptions(override *SpecFetchOptions) *SpecFetchOptions {
	switch {
	case c.fetchOptions == nil && override == nil:
		return nil
	case c.fetchOptions == nil:
		return override
	case override == nil:
		return c.fetchOptions
	}

	// Start with defaults
	merged := &SpecFetchOptions{
		AuthType:     c.fetchOptions.AuthType,
		AuthToken:    c.fetchOptions.AuthToken,
		AuthKeyName:  c.fetchOptions.AuthKeyName,
		AuthLocation: c.fetchOptions.AuthLocation,
		Headers:      copyHeaders(c.fetchOptions.Headers),
	}

	// Apply overrides (override takes precedence)
	if override.AuthType != "" {
		merged.AuthType = override.AuthType
		merged.AuthToken = override.AuthToken
		merged.AuthKeyName = override.AuthKeyName
		merged.AuthLocation = override.AuthLocation
	}
	if override.Headers != nil {
		if merged.Headers == nil {
			merged.Headers = make(map[string]string)
		}
		maps.Copy(merged.Headers, override.Headers)
	}

	return merged
}

// fetchLocalFile reads a local spec file.
func (c *SpecCacheManager) fetchLocalFile(path string) (*FetchResult, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read spec file: %w", err)
	}

	format := detectFormat("", path)
	return &FetchResult{
		Content:   content,
		Format:    format,
		FromCache: false,
		Meta: &SpecCacheMeta{
			SourceURL:   path,
			Format:      format,
			FetchedAt:   time.Now(),
			ExpiresAt:   time.Now().Add(DefaultCacheTTL),
			ContentHash: computeHash(content),
			Size:        int64(len(content)),
		},
	}, nil
}

// tryLoadValidCache attempts to load a valid cached spec.
func (c *SpecCacheManager) tryLoadValidCache(appName, url string, meta *SpecCacheMeta) (*FetchResult, bool) {
	if meta == nil || meta.IsStale() || meta.SourceURL != url {
		return nil, false
	}

	content, err := c.loadCachedContent(appName, meta.Format)
	if err != nil || computeHash(content) != meta.ContentHash {
		return nil, false
	}

	return &FetchResult{
		Content:   content,
		Format:    meta.Format,
		Meta:      meta,
		FromCache: true,
	}, true
}

// tryUseStaleCache attempts to use stale cache on network error.
func (c *SpecCacheManager) tryUseStaleCache(appName string, meta *SpecCacheMeta, fetchErr error) (*FetchResult, error) {
	if meta == nil {
		return nil, fmt.Errorf("failed to fetch spec: %w", fetchErr)
	}

	content, err := c.loadCachedContent(appName, meta.Format)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch spec: %w", fetchErr)
	}

	return &FetchResult{
		Content:      content,
		Format:       meta.Format,
		Meta:         meta,
		FromCache:    true,
		CacheWarning: fmt.Sprintf("Using cached spec due to network error: %v", fetchErr),
	}, nil
}

// handleNotModified handles 304 Not Modified response.
func (c *SpecCacheManager) handleNotModified(result *FetchResult, appName string, meta *SpecCacheMeta) error {
	content, err := c.loadCachedContent(appName, meta.Format)
	if err != nil {
		return err
	}
	result.Content = content
	return c.SaveMeta(appName, result.Meta)
}

// fetchRemoteWithCache fetches a remote spec with caching.
func (c *SpecCacheManager) fetchRemoteWithCache(appName, url string, opts *SpecFetchOptions) (*FetchResult, error) {
	meta, _ := c.LoadMeta(appName)

	// Try to use valid cache first
	if result, ok := c.tryLoadValidCache(appName, url, meta); ok {
		return result, nil
	}

	// Fetch from remote
	result, err := c.fetchRemote(url, meta, opts)
	if err != nil {
		return c.tryUseStaleCache(appName, meta, err)
	}

	// Handle 304 Not Modified
	if result.FromCache && result.Content == nil && meta != nil {
		if err := c.handleNotModified(result, appName, meta); err == nil {
			return result, nil
		}
	}

	// Save to cache (warning on failure, but don't fail)
	if err := c.saveToCache(appName, result); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to cache spec: %v\n", err)
	}

	return result, nil
}

// persistFetchOptions saves non-sensitive fetch options to metadata.
func persistFetchOptions(meta *SpecCacheMeta, opts *SpecFetchOptions) {
	if opts == nil {
		return
	}
	if opts.Headers != nil {
		meta.FetchHeaders = make(map[string]string, len(opts.Headers))
		maps.Copy(meta.FetchHeaders, opts.Headers)
	}
	meta.FetchAuthType = opts.AuthType
	meta.FetchAuthKeyName = opts.AuthKeyName
	meta.FetchAuthLocation = opts.AuthLocation
}

// buildMetaFromResponse creates SpecCacheMeta from HTTP response and content.
func buildMetaFromResponse(resp *http.Response, url string, content []byte, opts *SpecFetchOptions) *SpecCacheMeta {
	meta := &SpecCacheMeta{
		SourceURL:    url,
		Format:       detectFormat(resp.Header.Get("Content-Type"), url),
		ETag:         resp.Header.Get("ETag"),
		LastModified: resp.Header.Get("Last-Modified"),
		FetchedAt:    time.Now(),
		ExpiresAt:    parseExpiresAt(resp),
		ContentHash:  computeHash(content),
		Size:         int64(len(content)),
	}
	persistFetchOptions(meta, opts)
	return meta
}

// setConditionalHeaders sets HTTP conditional request headers based on existing metadata.
func setConditionalHeaders(req *http.Request, existingMeta *SpecCacheMeta) {
	if existingMeta == nil {
		return
	}
	if existingMeta.ETag != "" {
		req.Header.Set("If-None-Match", existingMeta.ETag)
	}
	if existingMeta.LastModified != "" {
		req.Header.Set("If-Modified-Since", existingMeta.LastModified)
	}
}

// createHTTPRequest creates a new HTTP GET request with standard headers.
func createHTTPRequest(url string) (*http.Request, error) {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json, application/yaml, text/yaml, */*")
	req.Header.Set("User-Agent", "OpenBridge/1.0")
	return req, nil
}

// fetchRemote fetches a spec from a remote URL.
func (c *SpecCacheManager) fetchRemote(url string, existingMeta *SpecCacheMeta, opts *SpecFetchOptions) (*FetchResult, error) {
	req, err := createHTTPRequest(url)
	if err != nil {
		return nil, err
	}

	setConditionalHeaders(req, existingMeta)
	applyFetchOptions(req, opts)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	// Handle 304 Not Modified
	if resp.StatusCode == http.StatusNotModified && existingMeta != nil {
		existingMeta.ExpiresAt = parseExpiresAt(resp)
		return &FetchResult{Content: nil, Format: existingMeta.Format, Meta: existingMeta, FromCache: true}, nil
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, resp.Status)
	}

	content, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	meta := buildMetaFromResponse(resp, url, content, opts)
	return &FetchResult{Content: content, Format: meta.Format, Meta: meta, FromCache: false}, nil
}

// applyFetchOptions applies custom headers and authentication to an HTTP request.
func applyFetchOptions(req *http.Request, opts *SpecFetchOptions) {
	if opts == nil {
		return
	}

	// Apply custom headers
	for key, value := range opts.Headers {
		req.Header.Set(key, value)
	}

	// Apply authentication
	if opts.AuthToken == "" {
		return
	}

	switch opts.AuthType {
	case "bearer":
		req.Header.Set("Authorization", "Bearer "+opts.AuthToken)
	case "basic":
		req.Header.Set("Authorization", "Basic "+opts.AuthToken)
	case "api_key":
		keyName := opts.AuthKeyName
		if keyName == "" {
			keyName = "X-API-Key"
		}
		// Apply api_key to query if explicitly specified, otherwise default to header
		if opts.AuthLocation == "query" {
			q := req.URL.Query()
			q.Set(keyName, opts.AuthToken)
			req.URL.RawQuery = q.Encode()
		} else {
			req.Header.Set(keyName, opts.AuthToken)
		}
	default:
		// Unknown auth type, skip authentication
	}
}

// saveToCache saves a fetch result to the cache.
func (c *SpecCacheManager) saveToCache(appName string, result *FetchResult) error {
	cacheDir := c.getCacheDir(appName)
	if err := os.MkdirAll(cacheDir, 0755); err != nil {
		return fmt.Errorf("failed to create cache directory: %w", err)
	}

	// Save spec content
	specPath := c.getSpecPath(appName, result.Format)
	if err := os.WriteFile(specPath, result.Content, 0644); err != nil {
		return fmt.Errorf("failed to write spec file: %w", err)
	}

	// Save metadata
	return c.SaveMeta(appName, result.Meta)
}

// loadCachedContent loads cached spec content.
func (c *SpecCacheManager) loadCachedContent(appName, format string) ([]byte, error) {
	specPath := c.getSpecPath(appName, format)
	return os.ReadFile(specPath)
}

// LoadMeta loads cache metadata for an app.
func (c *SpecCacheManager) LoadMeta(appName string) (*SpecCacheMeta, error) {
	metaPath := c.getMetaPath(appName)
	data, err := os.ReadFile(metaPath)
	if err != nil {
		return nil, err
	}

	var meta SpecCacheMeta
	if err := json.Unmarshal(data, &meta); err != nil {
		return nil, err
	}
	return &meta, nil
}

// SaveMeta saves cache metadata for an app.
func (c *SpecCacheManager) SaveMeta(appName string, meta *SpecCacheMeta) error {
	metaPath := c.getMetaPath(appName)
	data, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(metaPath, data, 0644)
}

// Clear removes all cached data for an app.
func (c *SpecCacheManager) Clear(appName string) error {
	cacheDir := c.getCacheDir(appName)
	return os.RemoveAll(cacheDir)
}

// ClearAll removes all cached data for all apps.
func (c *SpecCacheManager) ClearAll() error {
	entries, err := os.ReadDir(c.baseDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	for _, entry := range entries {
		if entry.IsDir() {
			cacheDir := filepath.Join(c.baseDir, entry.Name(), "cache")
			if err := os.RemoveAll(cacheDir); err != nil {
				return err
			}
		}
	}
	return nil
}

// Refresh forces a refresh of the cached spec.
func (c *SpecCacheManager) Refresh(appName, source string) (*FetchResult, error) {
	// Clear existing cache first
	if err := c.Clear(appName); err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("failed to clear cache: %w", err)
	}

	// Fetch fresh
	return c.FetchWithCache(appName, source)
}

// GetCacheInfo returns information about the cache for an app.
type CacheInfo struct {
	AppName      string
	SourceURL    string
	Format       string
	Size         int64
	FetchedAt    time.Time
	ExpiresAt    time.Time
	IsStale      bool
	CacheDir     string
	ContentHash  string
	ETag         string
	LastModified string
}

// GetCacheInfo returns cache information for an app.
func (c *SpecCacheManager) GetCacheInfo(appName string) (*CacheInfo, error) {
	meta, err := c.LoadMeta(appName)
	if err != nil {
		return nil, err
	}

	return &CacheInfo{
		AppName:      appName,
		SourceURL:    meta.SourceURL,
		Format:       meta.Format,
		Size:         meta.Size,
		FetchedAt:    meta.FetchedAt,
		ExpiresAt:    meta.ExpiresAt,
		IsStale:      meta.IsStale(),
		CacheDir:     c.getCacheDir(appName),
		ContentHash:  meta.ContentHash,
		ETag:         meta.ETag,
		LastModified: meta.LastModified,
	}, nil
}

// ListCachedApps returns a list of apps with cached specs.
func (c *SpecCacheManager) ListCachedApps() ([]string, error) {
	entries, err := os.ReadDir(c.baseDir)
	if err != nil {
		if os.IsNotExist(err) {
			return []string{}, nil
		}
		return nil, err
	}

	var apps []string
	for _, entry := range entries {
		if entry.IsDir() {
			metaPath := c.getMetaPath(entry.Name())
			if _, err := os.Stat(metaPath); err == nil {
				apps = append(apps, entry.Name())
			}
		}
	}
	return apps, nil
}

// SaveParsedSpec saves the parsed spec to disk for persistent caching.
func (c *SpecCacheManager) SaveParsedSpec(appName string, spec *openapi3.T) error {
	cacheDir := c.getCacheDir(appName)
	if err := os.MkdirAll(cacheDir, 0755); err != nil {
		return fmt.Errorf("failed to create cache directory: %w", err)
	}

	parsedPath := c.getParsedSpecPath(appName)

	// Marshal the spec to JSON
	data, err := json.MarshalIndent(spec, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal parsed spec: %w", err)
	}

	// Write to temporary file first, then rename (atomic write)
	tmpPath := parsedPath + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write parsed spec: %w", err)
	}
	if err := os.Rename(tmpPath, parsedPath); err != nil {
		return fmt.Errorf("failed to move parsed spec: %w", err)
	}

	// Update metadata
	meta, err := c.LoadMeta(appName)
	if err != nil {
		// If meta doesn't exist, create a new one
		meta = &SpecCacheMeta{
			SourceURL: "",
			Format:    "",
		}
	}

	meta.ParsedSpecPath = parsedPath
	meta.ParsedAt = time.Now()
	meta.ParserVersion = ParserVersion

	if err := c.SaveMeta(appName, meta); err != nil {
		return fmt.Errorf("failed to save metadata: %w", err)
	}

	return nil
}

// LoadParsedSpec loads the parsed spec from disk if available.
func (c *SpecCacheManager) LoadParsedSpec(appName string) (*openapi3.T, bool, error) {
	meta, err := c.LoadMeta(appName)
	if err != nil {
		return nil, false, nil // No cached metadata
	}

	// Check if parsed spec path exists
	if meta.ParsedSpecPath == "" {
		return nil, false, nil // No parsed spec cached
	}

	// Check if file exists
	if _, err := os.Stat(meta.ParsedSpecPath); err != nil {
		return nil, false, nil // File doesn't exist
	}

	// Read and unmarshal the parsed spec
	data, err := os.ReadFile(meta.ParsedSpecPath)
	if err != nil {
		return nil, false, fmt.Errorf("failed to read parsed spec: %w", err)
	}

	var spec openapi3.T
	if err := json.Unmarshal(data, &spec); err != nil {
		return nil, false, fmt.Errorf("failed to unmarshal parsed spec: %w", err)
	}

	return &spec, true, nil
}

// checkSourceIntegrity checks if the source document has changed.
func (c *SpecCacheManager) checkSourceIntegrity(meta *SpecCacheMeta) bool {
	if isWebURL(meta.SourceURL) {
		return true // Web URLs require network check, assume valid
	}

	// For local files, verify the content hash matches
	content, err := os.ReadFile(meta.SourceURL)
	if err != nil {
		return false
	}
	return computeHash(content) == meta.ContentHash
}

// ValidateParsedSpec validates that the parsed spec cache is still valid.
func (c *SpecCacheManager) ValidateParsedSpec(appName string) (bool, error) {
	meta, err := c.LoadMeta(appName)
	if err != nil {
		return false, nil
	}

	// Check basic cache metadata
	if meta.ParsedSpecPath == "" || meta.ParserVersion != ParserVersion {
		return false, nil
	}

	// Check if parsed spec file exists
	if _, err := os.Stat(meta.ParsedSpecPath); err != nil {
		return false, nil
	}

	// Check source integrity
	if !c.checkSourceIntegrity(meta) {
		return false, nil
	}

	// Check if cache is stale
	if meta.IsStale() {
		return false, nil
	}

	return true, nil
}

// getParsedSpecPath returns the file path for the parsed spec cache.
func (c *SpecCacheManager) getParsedSpecPath(appName string) string {
	return filepath.Join(c.getCacheDir(appName), "parsed.json")
}
