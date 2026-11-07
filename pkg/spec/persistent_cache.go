// Package spec provides OpenAPI specification parsing and validation.
// This file implements the persistent on-disk cache for remote (HTTP/HTTPS)
// specifications. The cache is opt-in via WithRemoteCache and is intentionally
// independent from pkg/config (the cache directory is injected by the caller),
// so that the spec package keeps no dependency on the configuration package.
package spec

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const (
	// remoteCacheDataSuffix is the suffix of the cached specification content.
	remoteCacheDataSuffix = ".spec"
	// remoteCacheMetaSuffix is the suffix of the cached specification metadata.
	remoteCacheMetaSuffix = ".meta.json"
	// remoteCacheTempSuffix is appended to files being written atomically.
	remoteCacheTempSuffix = ".tmp"
	// remoteCacheDirPerm is the permission used for the cache directory.
	remoteCacheDirPerm = 0o755
	// remoteCacheFilePerm is the permission used for cache files. Specs may
	// contain sensitive examples, so they are only readable by the owner.
	remoteCacheFilePerm = 0o600
	// defaultRemoteCacheTTL is the freshness lifetime used when the server does
	// not provide Cache-Control/Expires metadata.
	defaultRemoteCacheTTL = 24 * time.Hour
)

// remoteCacheMeta describes one cached remote specification.
type remoteCacheMeta struct {
	// URL is the original specification URL.
	URL string `json:"url"`
	// ETag is the HTTP ETag header value from the server.
	ETag string `json:"etag,omitempty"`
	// LastModified is the HTTP Last-Modified header value from the server.
	LastModified string `json:"last_modified,omitempty"`
	// FetchedAt is when the specification content was last fetched.
	FetchedAt time.Time `json:"fetched_at"`
	// ExpiresAt is when the cached specification should be considered stale.
	ExpiresAt time.Time `json:"expires_at"`
	// SHA256 is the hex encoded SHA-256 digest of the cached content.
	SHA256 string `json:"sha256"`
	// Size is the size of the cached content in bytes.
	Size int64 `json:"size,omitempty"`
}

// isExpired reports whether the cached specification is stale at now.
func (m *remoteCacheMeta) isExpired(now time.Time) bool {
	return !now.Before(m.ExpiresAt)
}

// hasValidators reports whether the metadata carries an ETag or Last-Modified
// value that can be used for a conditional request.
func (m *remoteCacheMeta) hasValidators() bool {
	return m != nil && (m.ETag != "" || m.LastModified != "")
}

// remoteCache stores remote specifications on disk, keyed by URL.
type remoteCache struct {
	dir string
}

// WithRemoteCache enables a persistent on-disk cache for remote (HTTP/HTTPS)
// specifications. An empty dir disables the cache. Local file specifications
// are never cached.
func WithRemoteCache(dir string) ParserOption {
	return func(p *Parser) {
		if dir == "" {
			p.remoteCache = nil
			return
		}

		p.remoteCache = &remoteCache{dir: dir}
	}
}

// paths returns the metadata and content paths for the given URL.
func (c *remoteCache) paths(url string) (metaPath, dataPath string) {
	sum := sha256.Sum256([]byte(url))
	base := filepath.Join(c.dir, hex.EncodeToString(sum[:]))

	return base + remoteCacheMetaSuffix, base + remoteCacheDataSuffix
}

// load returns the cached content and metadata for url. It returns (nil, nil)
// when the cache is absent or unreadable/corrupt (corrupt entries are ignored
// and refreshed on the next successful fetch).
func (c *remoteCache) load(url string) ([]byte, *remoteCacheMeta) {
	metaPath, dataPath := c.paths(url)

	meta, err := readRemoteCacheMeta(metaPath)
	if err != nil || meta == nil || meta.URL != url {
		if err != nil {
			c.removeCorruptEntry(url)
		}

		return nil, nil
	}

	content, err := os.ReadFile(dataPath)
	if err != nil || len(content) == 0 {
		c.removeCorruptEntry(url)

		return nil, nil
	}

	if meta.SHA256 != "" && contentHash(content) != meta.SHA256 {
		c.removeCorruptEntry(url)

		return nil, nil
	}

	return content, meta
}

// store writes the specification content and its metadata atomically. Both the
// content and the metadata are written to a temporary file in the cache
// directory and moved into place with os.Rename.
func (c *remoteCache) store(url string, content []byte, meta *remoteCacheMeta) error {
	if meta == nil {
		meta = newRemoteCacheMeta(url, content, nil, time.Now())
	}

	if meta.SHA256 == "" {
		meta.SHA256 = contentHash(content)
	}

	if !meta.FetchedAt.IsZero() && meta.Size == 0 {
		meta.Size = int64(len(content))
	}

	data, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal cache metadata: %w", err)
	}

	if err := os.MkdirAll(c.dir, remoteCacheDirPerm); err != nil {
		return fmt.Errorf("failed to create spec cache directory: %w", err)
	}

	metaPath, dataPath := c.paths(url)

	if err := writeFileAtomic(dataPath, content, remoteCacheFilePerm); err != nil {
		return err
	}

	return writeFileAtomic(metaPath, data, remoteCacheFilePerm)
}

// refresh updates the freshness metadata after a 304 Not Modified response and
// persists it atomically.
func (c *remoteCache) refresh(meta *remoteCacheMeta, header http.Header, now time.Time) (*remoteCacheMeta, error) {
	if meta == nil {
		return nil, fmt.Errorf("cannot refresh nil cache metadata")
	}

	updated := *meta
	updated.FetchedAt = now
	updated.ExpiresAt = remoteCacheExpiry(header, now)

	if etag := header.Get("ETag"); etag != "" {
		updated.ETag = etag
	}

	if lastModified := header.Get("Last-Modified"); lastModified != "" {
		updated.LastModified = lastModified
	}

	metaPath, _ := c.paths(updated.URL)
	if err := c.writeCacheMeta(metaPath, &updated); err != nil {
		return nil, err
	}

	return &updated, nil
}

// newRemoteCacheMeta builds metadata for freshly fetched content.
func newRemoteCacheMeta(url string, content []byte, header http.Header, now time.Time) *remoteCacheMeta {
	meta := &remoteCacheMeta{
		URL:       url,
		FetchedAt: now,
		ExpiresAt: remoteCacheExpiry(header, now),
		SHA256:    contentHash(content),
		Size:      int64(len(content)),
	}

	if header != nil {
		meta.ETag = header.Get("ETag")
		meta.LastModified = header.Get("Last-Modified")
	}

	return meta
}

// remoteCacheExpiry computes the freshness deadline from HTTP response headers.
func remoteCacheExpiry(header http.Header, now time.Time) time.Time {
	if header == nil {
		return now.Add(defaultRemoteCacheTTL)
	}

	if cacheControl := header.Get("Cache-Control"); cacheControl != "" {
		if maxAge, ok := parseMaxAge(cacheControl); ok {
			return now.Add(maxAge)
		}

		lower := strings.ToLower(cacheControl)
		if strings.Contains(lower, "no-store") || strings.Contains(lower, "no-cache") {
			return now
		}
	}

	if expires := header.Get("Expires"); expires != "" {
		if expiresAt, err := http.ParseTime(expires); err == nil {
			return expiresAt
		}
	}

	return now.Add(defaultRemoteCacheTTL)
}

// parseMaxAge extracts the max-age directive from a Cache-Control header value.
func parseMaxAge(cacheControl string) (time.Duration, bool) {
	for directive := range strings.SplitSeq(cacheControl, ",") {
		lower := strings.ToLower(strings.TrimSpace(directive))
		value, ok := strings.CutPrefix(lower, "max-age=")
		if !ok {
			continue
		}

		seconds, err := strconv.Atoi(strings.TrimSpace(value))
		if err != nil || seconds < 0 {
			return 0, false
		}

		return time.Duration(seconds) * time.Second, true
	}

	return 0, false
}

// contentHash returns the hex encoded SHA-256 digest of content.
func contentHash(content []byte) string {
	sum := sha256.Sum256(content)

	return hex.EncodeToString(sum[:])
}

// writeFileAtomic writes data to path atomically using a temporary file in the
// same directory followed by an os.Rename.
func writeFileAtomic(path string, data []byte, perm os.FileMode) error {
	tmpPath := path + "." + strconv.Itoa(os.Getpid()) + remoteCacheTempSuffix
	if err := os.WriteFile(tmpPath, data, perm); err != nil {
		_ = os.Remove(tmpPath)

		return fmt.Errorf("failed to write temporary cache file: %w", err)
	}

	if err := os.Rename(tmpPath, path); err != nil {
		_ = os.Remove(tmpPath)

		return fmt.Errorf("failed to move cache file into place: %w", err)
	}

	return nil
}

// setConditionalHeaders adds If-None-Match/If-Modified-Since headers when the
// cached metadata carries validators.
func setConditionalHeaders(req *http.Request, meta *remoteCacheMeta) {
	if !meta.hasValidators() {
		return
	}

	if meta.ETag != "" {
		req.Header.Set("If-None-Match", meta.ETag)
	}

	if meta.LastModified != "" {
		req.Header.Set("If-Modified-Since", meta.LastModified)
	}
}

// writeCacheMeta writes metadata atomically to path.
func (c *remoteCache) writeCacheMeta(path string, meta *remoteCacheMeta) error {
	data, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal cache metadata: %w", err)
	}

	if err := os.MkdirAll(c.dir, remoteCacheDirPerm); err != nil {
		return fmt.Errorf("failed to create spec cache directory: %w", err)
	}

	return writeFileAtomic(path, data, remoteCacheFilePerm)
}

// removeCorruptEntry removes both cache files when an entry is unreadable.
func (c *remoteCache) removeCorruptEntry(url string) {
	metaPath, dataPath := c.paths(url)
	_ = os.Remove(metaPath)
	_ = os.Remove(dataPath)
}

// readRemoteCacheMeta reads and decodes one metadata file. It returns
// (nil, nil) when the file does not exist.
func readRemoteCacheMeta(path string) (*remoteCacheMeta, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil //nolint:nilerr // missing cache entry is not an error
		}

		return nil, fmt.Errorf("failed to read spec cache metadata: %w", err)
	}

	meta := &remoteCacheMeta{}
	if err := json.Unmarshal(data, meta); err != nil {
		return nil, fmt.Errorf("failed to parse spec cache metadata: %w", err)
	}

	return meta, nil
}
