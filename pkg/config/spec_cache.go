// Package config provides configuration management for OpenBridge.
// This file contains spec caching functionality for offline support and performance.
package config

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// DefaultCacheTTL is the default time-to-live for cached specs when no HTTP cache headers are present.
const DefaultCacheTTL = 24 * time.Hour

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
}

// IsStale returns true if the cached spec has expired.
func (m *SpecCacheMeta) IsStale() bool {
	return time.Now().After(m.ExpiresAt)
}

// SpecCacheManager manages caching of OpenAPI specifications.
type SpecCacheManager struct {
	baseDir    string
	httpClient *http.Client
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

// SetHTTPClient sets a custom HTTP client for fetching specs.
// This is useful for configuring TLS settings.
func (c *SpecCacheManager) SetHTTPClient(client *http.Client) {
	c.httpClient = client
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

// parseExpiresAt parses HTTP cache headers to determine expiration time.
func parseExpiresAt(resp *http.Response) time.Time {
	now := time.Now()

	// Check Cache-Control header
	cacheControl := resp.Header.Get("Cache-Control")
	if cacheControl != "" {
		// Parse max-age directive
		for directive := range strings.SplitSeq(cacheControl, ",") {
			directive = strings.TrimSpace(directive)
			if after, ok := strings.CutPrefix(directive, "max-age="); ok {
				if seconds, err := strconv.Atoi(after); err == nil {
					return now.Add(time.Duration(seconds) * time.Second)
				}
			}
			// no-cache or no-store means always revalidate
			if directive == "no-cache" || directive == "no-store" {
				return now // Already expired
			}
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
	if !isWebURL(source) {
		return c.fetchLocalFile(source)
	}
	return c.fetchRemoteWithCache(appName, source)
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

// fetchRemoteWithCache fetches a remote spec with caching.
func (c *SpecCacheManager) fetchRemoteWithCache(appName, url string) (*FetchResult, error) {
	// Try to load existing cache metadata
	meta, _ := c.LoadMeta(appName)

	// Check if cache is still valid
	if meta != nil && !meta.IsStale() && meta.SourceURL == url {
		content, err := c.loadCachedContent(appName, meta.Format)
		if err == nil {
			// Verify content hash
			if computeHash(content) == meta.ContentHash {
				return &FetchResult{
					Content:   content,
					Format:    meta.Format,
					Meta:      meta,
					FromCache: true,
				}, nil
			}
		}
	}

	// Fetch from remote
	result, err := c.fetchRemote(url, meta)
	if err != nil {
		// Network error - try to use stale cache if available
		if meta != nil {
			content, cacheErr := c.loadCachedContent(appName, meta.Format)
			if cacheErr == nil {
				return &FetchResult{
					Content:      content,
					Format:       meta.Format,
					Meta:         meta,
					FromCache:    true,
					CacheWarning: fmt.Sprintf("Using cached spec due to network error: %v", err),
				}, nil
			}
		}
		return nil, fmt.Errorf("failed to fetch spec: %w", err)
	}

	// Handle 304 Not Modified - load content from cache
	if result.FromCache && result.Content == nil && meta != nil {
		content, err := c.loadCachedContent(appName, meta.Format)
		if err == nil {
			result.Content = content
			// Update cache metadata with new expiration
			if err := c.SaveMeta(appName, result.Meta); err != nil {
				fmt.Fprintf(os.Stderr, "Warning: failed to update cache metadata: %v\n", err)
			}
			return result, nil
		}
	}

	// Save to cache
	if err := c.saveToCache(appName, result); err != nil {
		// Warning but don't fail
		fmt.Fprintf(os.Stderr, "Warning: failed to cache spec: %v\n", err)
	}

	return result, nil
}

// fetchRemote fetches a spec from a remote URL.
// buildMetaFromResponse creates SpecCacheMeta from HTTP response and content.
func buildMetaFromResponse(resp *http.Response, url string, content []byte) *SpecCacheMeta {
	format := detectFormat(resp.Header.Get("Content-Type"), url)
	return &SpecCacheMeta{
		SourceURL:    url,
		Format:       format,
		ETag:         resp.Header.Get("ETag"),
		LastModified: resp.Header.Get("Last-Modified"),
		FetchedAt:    time.Now(),
		ExpiresAt:    parseExpiresAt(resp),
		ContentHash:  computeHash(content),
		Size:         int64(len(content)),
	}
}

func (c *SpecCacheManager) fetchRemote(url string, existingMeta *SpecCacheMeta) (*FetchResult, error) {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}

	// Add conditional headers if we have existing cache
	if existingMeta != nil {
		if existingMeta.ETag != "" {
			req.Header.Set("If-None-Match", existingMeta.ETag)
		}
		if existingMeta.LastModified != "" {
			req.Header.Set("If-Modified-Since", existingMeta.LastModified)
		}
	}

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

	meta := buildMetaFromResponse(resp, url, content)
	return &FetchResult{Content: content, Format: meta.Format, Meta: meta, FromCache: false}, nil
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
