package config

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// createTestCacheDir creates a temporary directory for spec cache testing.
// The returned cleanup function should be deferred by the caller.
func createTestCacheDir(t *testing.T) (string, func()) {
	t.Helper()
	tmpDir, err := os.MkdirTemp("", "spec-cache-test-*")
	require.NoError(t, err)
	return tmpDir, func() { _ = os.RemoveAll(tmpDir) }
}

func TestNewSpecCacheManager(t *testing.T) {
	tmpDir, cleanup := createTestCacheDir(t)
	defer cleanup()

	manager := NewSpecCacheManager(tmpDir)
	require.NotNil(t, manager)
	assert.Equal(t, tmpDir, manager.baseDir)
	assert.NotNil(t, manager.httpClient)
}

func TestSpecCacheManager_LoadMeta(t *testing.T) {
	tmpDir, cleanup := createTestCacheDir(t)
	defer cleanup()

	manager := NewSpecCacheManager(tmpDir)

	t.Run("non-existent app returns error", func(t *testing.T) {
		meta, err := manager.LoadMeta("nonexistent")
		require.Error(t, err)
		assert.True(t, os.IsNotExist(err))
		assert.Nil(t, meta)
	})

	t.Run("load saved meta", func(t *testing.T) {
		appName := "test-app"
		specContent := []byte(`{"openapi": "3.0.0"}`)
		meta := &SpecCacheMeta{
			SourceURL:   "https://example.com/spec.json",
			Format:      "json",
			FetchedAt:   time.Now().Truncate(time.Second),
			ExpiresAt:   time.Now().Add(time.Hour).Truncate(time.Second),
			ContentHash: computeHash(specContent),
			Size:        int64(len(specContent)),
		}

		// Save the cache
		cacheDir := manager.getCacheDir(appName)
		err := os.MkdirAll(cacheDir, 0755)
		require.NoError(t, err)

		err = manager.SaveMeta(appName, meta)
		require.NoError(t, err)

		// Load meta
		loadedMeta, err := manager.LoadMeta(appName)
		require.NoError(t, err)
		require.NotNil(t, loadedMeta)
		assert.Equal(t, meta.SourceURL, loadedMeta.SourceURL)
		assert.Equal(t, meta.Format, loadedMeta.Format)
		assert.Equal(t, meta.ContentHash, loadedMeta.ContentHash)
	})
}

func TestSpecCacheManager_Clear(t *testing.T) {
	tmpDir, cleanup := createTestCacheDir(t)
	defer cleanup()

	manager := NewSpecCacheManager(tmpDir)
	appName := "test-app"

	// Create cache files
	cacheDir := manager.getCacheDir(appName)
	err := os.MkdirAll(cacheDir, 0755)
	require.NoError(t, err)

	specPath := manager.getSpecPath(appName, "json")
	err = os.WriteFile(specPath, []byte(`{}`), 0644)
	require.NoError(t, err)

	// Verify files exist
	_, err = os.Stat(cacheDir)
	require.NoError(t, err)

	// Clear cache
	err = manager.Clear(appName)
	require.NoError(t, err)

	// Verify files are removed
	_, err = os.Stat(cacheDir)
	assert.True(t, os.IsNotExist(err))
}

func TestSpecCacheManager_ListCachedApps(t *testing.T) {
	tmpDir, cleanup := createTestCacheDir(t)
	defer cleanup()

	manager := NewSpecCacheManager(tmpDir)

	// Create some cached apps
	apps := []string{"app1", "app2", "app3"}
	for _, app := range apps {
		cacheDir := manager.getCacheDir(app)
		err := os.MkdirAll(cacheDir, 0755)
		require.NoError(t, err)

		metaPath := filepath.Join(cacheDir, "meta.json")
		err = os.WriteFile(metaPath, []byte(`{}`), 0644)
		require.NoError(t, err)
	}

	// Create a directory without meta.json (should not be listed)
	nonCacheDir := filepath.Join(tmpDir, "non-cached-app", "cache")
	err := os.MkdirAll(nonCacheDir, 0755)
	require.NoError(t, err)

	// List cached apps
	cachedApps, err := manager.ListCachedApps()
	require.NoError(t, err)
	assert.Len(t, cachedApps, 3)

	// Verify all apps are in the list
	appSet := make(map[string]bool)
	for _, app := range cachedApps {
		appSet[app] = true
	}
	for _, app := range apps {
		assert.True(t, appSet[app], "Expected app %s to be in cached apps list", app)
	}
}

func TestSpecCacheManager_FetchWithCache(t *testing.T) {
	tmpDir, cleanup := createTestCacheDir(t)
	defer cleanup()

	manager := NewSpecCacheManager(tmpDir)

	t.Run("fetch from server", func(t *testing.T) {
		specContent := `{"openapi": "3.0.0", "info": {"title": "Test API", "version": "1.0.0"}}`
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("ETag", `"abc123"`)
			w.Header().Set("Cache-Control", "max-age=3600")
			_, _ = w.Write([]byte(specContent))
		}))
		defer server.Close()

		result, err := manager.FetchWithCache("test-fetch", server.URL+"/spec.json")
		require.NoError(t, err)
		assert.JSONEq(t, specContent, string(result.Content))
		assert.False(t, result.FromCache)

		// Verify cache was saved
		meta, err := manager.LoadMeta("test-fetch")
		require.NoError(t, err)
		require.NotNil(t, meta)
		assert.Equal(t, `"abc123"`, meta.ETag)
	})

	t.Run("use cached content when valid", func(t *testing.T) {
		appName := "cached-app"
		specContent := []byte(`{"openapi": "3.0.0"}`)
		meta := &SpecCacheMeta{
			SourceURL:   "https://example.com/spec.json",
			Format:      "json",
			FetchedAt:   time.Now(),
			ExpiresAt:   time.Now().Add(time.Hour),
			ContentHash: computeHash(specContent),
			Size:        int64(len(specContent)),
		}

		// Pre-save cache
		cacheDir := manager.getCacheDir(appName)
		err := os.MkdirAll(cacheDir, 0755)
		require.NoError(t, err)

		specPath := manager.getSpecPath(appName, "json")
		err = os.WriteFile(specPath, specContent, 0644)
		require.NoError(t, err)

		err = manager.SaveMeta(appName, meta)
		require.NoError(t, err)

		// Fetch should return cached content without hitting server
		result, err := manager.FetchWithCache(appName, meta.SourceURL)
		require.NoError(t, err)
		assert.Equal(t, specContent, result.Content)
		assert.True(t, result.FromCache)
	})

	t.Run("handle 304 not modified", func(t *testing.T) {
		appName := "test-304"
		specContent := []byte(`{"openapi": "3.0.0"}`)

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("If-None-Match") == `"test-etag"` {
				w.Header().Set("Cache-Control", "max-age=3600")
				w.WriteHeader(http.StatusNotModified)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write(specContent)
		}))
		defer server.Close()

		// Start with expired cache
		meta := &SpecCacheMeta{
			SourceURL:   server.URL + "/spec.json",
			Format:      "json",
			ETag:        `"test-etag"`,
			FetchedAt:   time.Now().Add(-2 * time.Hour),
			ExpiresAt:   time.Now().Add(-time.Hour),
			ContentHash: computeHash(specContent),
			Size:        int64(len(specContent)),
		}

		// Pre-save cache
		cacheDir := manager.getCacheDir(appName)
		err := os.MkdirAll(cacheDir, 0755)
		require.NoError(t, err)

		specPath := manager.getSpecPath(appName, "json")
		err = os.WriteFile(specPath, specContent, 0644)
		require.NoError(t, err)

		err = manager.SaveMeta(appName, meta)
		require.NoError(t, err)

		// Fetch should use conditional request
		result, err := manager.FetchWithCache(appName, meta.SourceURL)
		require.NoError(t, err)
		assert.Equal(t, specContent, result.Content)
		assert.True(t, result.FromCache)
	})

	t.Run("fetch local file", func(t *testing.T) {
		specContent := `{"openapi": "3.0.0"}`
		tmpFile, err := os.CreateTemp("", "spec-*.json")
		require.NoError(t, err)
		defer func() { _ = os.Remove(tmpFile.Name()) }()
		_, err = tmpFile.WriteString(specContent)
		require.NoError(t, err)
		require.NoError(t, tmpFile.Close())

		result, err := manager.FetchWithCache("local-app", tmpFile.Name())
		require.NoError(t, err)
		assert.JSONEq(t, specContent, string(result.Content))
		assert.Equal(t, "json", result.Format)
	})
}

func TestDetectFormat(t *testing.T) {
	tests := []struct {
		contentType string
		url         string
		expected    string
	}{
		{"application/json", "https://example.com/spec", "json"},
		{"application/yaml", "https://example.com/spec", "yaml"},
		{"text/yaml", "https://example.com/spec", "yaml"},
		{"application/x-yaml", "https://example.com/spec", "yaml"},
		{"", "https://example.com/spec.json", "json"},
		{"", "https://example.com/spec.yaml", "yaml"},
		{"", "https://example.com/spec.yml", "yaml"},
		{"text/plain", "https://example.com/spec", "yaml"}, // default to yaml when no hint
	}

	for _, tt := range tests {
		t.Run(tt.contentType+"_"+tt.url, func(t *testing.T) {
			result := detectFormat(tt.contentType, tt.url)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestComputeHash(t *testing.T) {
	content1 := []byte("hello world")
	content2 := []byte("hello world")
	content3 := []byte("different content")

	hash1 := computeHash(content1)
	hash2 := computeHash(content2)
	hash3 := computeHash(content3)

	assert.Equal(t, hash1, hash2, "Same content should have same hash")
	assert.NotEqual(t, hash1, hash3, "Different content should have different hash")
	assert.Len(t, hash1, 64, "SHA-256 hash should be 64 hex characters")
}

func TestParseExpiresAt(t *testing.T) {
	t.Run("max-age in Cache-Control", func(t *testing.T) {
		recorder := httptest.NewRecorder()
		recorder.Header().Set("Cache-Control", "max-age=3600")
		resp := recorder.Result()

		expiresAt := parseExpiresAt(resp)
		assert.False(t, expiresAt.IsZero())
		// Should be approximately 1 hour from now
		assert.WithinDuration(t, time.Now().Add(time.Hour), expiresAt, time.Minute)
	})

	t.Run("Expires header", func(t *testing.T) {
		futureTime := time.Now().Add(2 * time.Hour).UTC()
		recorder := httptest.NewRecorder()
		recorder.Header().Set("Expires", futureTime.Format(http.TimeFormat))
		resp := recorder.Result()

		expiresAt := parseExpiresAt(resp)
		assert.WithinDuration(t, futureTime, expiresAt, time.Second)
	})

	t.Run("no cache headers uses default", func(t *testing.T) {
		recorder := httptest.NewRecorder()
		resp := recorder.Result()

		expiresAt := parseExpiresAt(resp)
		// Should use default TTL (24 hours)
		assert.WithinDuration(t, time.Now().Add(24*time.Hour), expiresAt, time.Minute)
	})

	t.Run("no-store disables caching", func(t *testing.T) {
		recorder := httptest.NewRecorder()
		recorder.Header().Set("Cache-Control", "no-store")
		resp := recorder.Result()

		expiresAt := parseExpiresAt(resp)
		assert.True(t, expiresAt.IsZero() || expiresAt.Before(time.Now().Add(time.Second)))
	})
}

func TestGetCacheInfo(t *testing.T) {
	tmpDir, cleanup := createTestCacheDir(t)
	defer cleanup()

	manager := NewSpecCacheManager(tmpDir)

	t.Run("non-existent app", func(t *testing.T) {
		info, err := manager.GetCacheInfo("nonexistent")
		require.Error(t, err) // LoadMeta returns error for non-existent app
		assert.Nil(t, info)
	})

	t.Run("existing cache", func(t *testing.T) {
		appName := "test-cache-info"
		specContent := []byte(`{"openapi": "3.0.0"}`)
		meta := &SpecCacheMeta{
			SourceURL:   "https://example.com/spec.json",
			Format:      "json",
			FetchedAt:   time.Now(),
			ExpiresAt:   time.Now().Add(time.Hour),
			ContentHash: computeHash(specContent),
			Size:        int64(len(specContent)),
		}

		cacheDir := manager.getCacheDir(appName)
		err := os.MkdirAll(cacheDir, 0755)
		require.NoError(t, err)

		specPath := manager.getSpecPath(appName, "json")
		err = os.WriteFile(specPath, specContent, 0644)
		require.NoError(t, err)

		err = manager.SaveMeta(appName, meta)
		require.NoError(t, err)

		info, err := manager.GetCacheInfo(appName)
		require.NoError(t, err)
		require.NotNil(t, info)
		assert.Equal(t, meta.SourceURL, info.SourceURL)
		assert.False(t, info.IsStale) // Cache should be valid (not stale)
	})
}

// =============================================================================
// SpecFetchOptions Tests
// =============================================================================

func TestNewSpecCacheManagerWithOptions(t *testing.T) {
	tmpDir, cleanup := createTestCacheDir(t)
	defer cleanup()

	customClient := &http.Client{Timeout: 60 * time.Second}
	opts := &SpecFetchOptions{
		Headers:  map[string]string{"X-Custom": "value"},
		AuthType: "bearer",
	}

	manager := NewSpecCacheManagerWithOptions(tmpDir, customClient, opts)
	require.NotNil(t, manager)
	assert.Equal(t, customClient, manager.httpClient)
	assert.Equal(t, opts, manager.fetchOptions)
}

func TestSetFetchOptions(t *testing.T) {
	tmpDir, cleanup := createTestCacheDir(t)
	defer cleanup()

	manager := NewSpecCacheManager(tmpDir)
	opts := &SpecFetchOptions{
		AuthType:  "bearer",
		AuthToken: "test-token",
	}

	manager.SetFetchOptions(opts)
	assert.Equal(t, opts, manager.fetchOptions)
}

func TestFetchWithCacheAndOptions_BearerAuth(t *testing.T) {
	tmpDir, cleanup := createTestCacheDir(t)
	defer cleanup()

	var receivedHeaders http.Header

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedHeaders = r.Header.Clone()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"openapi":"3.0.0","info":{"title":"Test","version":"1.0.0"},"paths":{}}`))
	}))
	defer server.Close()

	manager := NewSpecCacheManager(tmpDir)
	opts := &SpecFetchOptions{
		Headers: map[string]string{
			"X-Custom-Header": "custom-value",
		},
		AuthType:  "bearer",
		AuthToken: "my-bearer-token",
	}

	result, err := manager.FetchWithCacheAndOptions("test-app", server.URL, opts)
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.False(t, result.FromCache)

	// Verify custom header was sent
	assert.Equal(t, "custom-value", receivedHeaders.Get("X-Custom-Header"))

	// Verify bearer auth was sent
	assert.Equal(t, "Bearer my-bearer-token", receivedHeaders.Get("Authorization"))
}

func TestFetchWithCacheAndOptions_APIKeyHeader(t *testing.T) {
	tmpDir, cleanup := createTestCacheDir(t)
	defer cleanup()

	var receivedHeaders http.Header

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedHeaders = r.Header.Clone()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"openapi":"3.0.0","info":{"title":"Test","version":"1.0.0"},"paths":{}}`))
	}))
	defer server.Close()

	manager := NewSpecCacheManager(tmpDir)
	opts := &SpecFetchOptions{
		AuthType:     "api_key",
		AuthToken:    "my-api-key",
		AuthKeyName:  "X-API-Key",
		AuthLocation: "header",
	}

	result, err := manager.FetchWithCacheAndOptions("test-app", server.URL, opts)
	require.NoError(t, err)
	require.NotNil(t, result)

	// Verify api_key header was sent
	assert.Equal(t, "my-api-key", receivedHeaders.Get("X-API-Key"))
}

func TestFetchWithCacheAndOptions_APIKeyQuery(t *testing.T) {
	tmpDir, cleanup := createTestCacheDir(t)
	defer cleanup()

	var receivedQuery string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedQuery = r.URL.RawQuery
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"openapi":"3.0.0","info":{"title":"Test","version":"1.0.0"},"paths":{}}`))
	}))
	defer server.Close()

	manager := NewSpecCacheManager(tmpDir)
	opts := &SpecFetchOptions{
		AuthType:     "api_key",
		AuthToken:    "my-api-key",
		AuthKeyName:  "api_key",
		AuthLocation: "query",
	}

	result, err := manager.FetchWithCacheAndOptions("test-app", server.URL, opts)
	require.NoError(t, err)
	require.NotNil(t, result)

	// Verify api_key was added to query
	assert.Equal(t, "api_key=my-api-key", receivedQuery)
}

func TestFetchWithCacheAndOptions_BasicAuth(t *testing.T) {
	tmpDir, cleanup := createTestCacheDir(t)
	defer cleanup()

	var receivedHeaders http.Header

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedHeaders = r.Header.Clone()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"openapi":"3.0.0","info":{"title":"Test","version":"1.0.0"},"paths":{}}`))
	}))
	defer server.Close()

	manager := NewSpecCacheManager(tmpDir)
	opts := &SpecFetchOptions{
		AuthType:  "basic",
		AuthToken: "dXNlcjpwYXNz", // base64("user:pass")
	}

	result, err := manager.FetchWithCacheAndOptions("test-app", server.URL, opts)
	require.NoError(t, err)
	require.NotNil(t, result)

	assert.Equal(t, "Basic dXNlcjpwYXNz", receivedHeaders.Get("Authorization"))
}

func TestFetchWithCacheAndOptions_MergeFetchOptions(t *testing.T) {
	tmpDir, cleanup := createTestCacheDir(t)
	defer cleanup()

	var receivedHeaders http.Header

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedHeaders = r.Header.Clone()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"openapi":"3.0.0","info":{"title":"Test","version":"1.0.0"},"paths":{}}`))
	}))
	defer server.Close()

	// Manager with default options
	defaultOpts := &SpecFetchOptions{
		Headers: map[string]string{
			"X-Default": "default-value",
		},
		AuthType:  "bearer",
		AuthToken: "default-token",
	}
	manager := NewSpecCacheManagerWithOptions(tmpDir, nil, defaultOpts)

	// Per-spec override
	overrideOpts := &SpecFetchOptions{
		Headers: map[string]string{
			"X-Override": "override-value",
		},
		AuthType:  "api_key",
		AuthToken: "override-key",
	}

	result, err := manager.FetchWithCacheAndOptions("test-app", server.URL, overrideOpts)
	require.NoError(t, err)
	require.NotNil(t, result)

	// Verify default header is present (merged)
	assert.Equal(t, "default-value", receivedHeaders.Get("X-Default"))

	// Verify override header is present
	assert.Equal(t, "override-value", receivedHeaders.Get("X-Override"))

	// Verify override auth takes precedence
	assert.Equal(t, "override-key", receivedHeaders.Get("X-API-Key"))

	// Bearer should not be present (overridden)
	assert.Empty(t, receivedHeaders.Get("Authorization"))
}

func TestSpecCacheMeta_FetchOptionsFields(t *testing.T) {
	tmpDir, cleanup := createTestCacheDir(t)
	defer cleanup()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("ETag", `"abc123"`)
		_, _ = w.Write([]byte(`{"openapi":"3.0.0","info":{"title":"Test","version":"1.0.0"},"paths":{}}`))
	}))
	defer server.Close()

	manager := NewSpecCacheManager(tmpDir)
	opts := &SpecFetchOptions{
		Headers: map[string]string{
			"X-Custom": "value",
		},
		AuthType:     "api_key",
		AuthKeyName:  "X-API-Key",
		AuthLocation: "header",
		AuthToken:    "secret-token", // Should NOT be persisted
	}

	result, err := manager.FetchWithCacheAndOptions("test-app", server.URL, opts)
	require.NoError(t, err)
	require.NotNil(t, result)

	// Load persisted meta
	meta, err := manager.LoadMeta("test-app")
	require.NoError(t, err)
	require.NotNil(t, meta)

	// Verify fetch options metadata is persisted
	assert.Equal(t, "value", meta.FetchHeaders["X-Custom"])
	assert.Equal(t, "api_key", meta.FetchAuthType)
	assert.Equal(t, "X-API-Key", meta.FetchAuthKeyName)
	assert.Equal(t, "header", meta.FetchAuthLocation)
}

func TestMergeFetchOptions(t *testing.T) {
	tmpDir, cleanup := createTestCacheDir(t)
	defer cleanup()

	manager := NewSpecCacheManager(tmpDir)

	t.Run("both nil", func(t *testing.T) {
		result := manager.mergeFetchOptions(nil)
		assert.Nil(t, result)
	})

	t.Run("manager nil, override provided", func(t *testing.T) {
		override := &SpecFetchOptions{AuthType: "bearer"}
		result := manager.mergeFetchOptions(override)
		assert.Equal(t, "bearer", result.AuthType)
	})

	t.Run("manager provided, override nil", func(t *testing.T) {
		mgr := NewSpecCacheManagerWithOptions(tmpDir, nil, &SpecFetchOptions{AuthType: "bearer"})
		result := mgr.mergeFetchOptions(nil)
		assert.Equal(t, "bearer", result.AuthType)
	})

	t.Run("both provided, override takes precedence", func(t *testing.T) {
		mgr := NewSpecCacheManagerWithOptions(tmpDir, nil, &SpecFetchOptions{
			AuthType:  "bearer",
			AuthToken: "base-token",
			Headers:   map[string]string{"X-Base": "base"},
		})
		override := &SpecFetchOptions{
			AuthType:  "api_key",
			AuthToken: "override-token",
			Headers:   map[string]string{"X-Override": "override"},
		}
		result := mgr.mergeFetchOptions(override)
		assert.Equal(t, "api_key", result.AuthType)
		assert.Equal(t, "override-token", result.AuthToken)
		assert.Equal(t, "base", result.Headers["X-Base"])
		assert.Equal(t, "override", result.Headers["X-Override"])
	})
}
