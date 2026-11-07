package spec

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"sync/atomic"
	"testing"
	"time"

	"github.com/leanovate/gopter"
	"github.com/leanovate/gopter/gen"
	"github.com/leanovate/gopter/prop"
	"github.com/nomagicln/open-bridge/internal/proptest"
)

const remoteCacheSpecV1 = `{
  "openapi": "3.0.3",
  "info": {"title": "Remote Cache API", "version": "1.0.0"},
  "paths": {
    "/pets": {
      "get": {"operationId": "listPets", "responses": {"200": {"description": "ok"}}}
    }
  }
}`

const remoteCacheSpecV2 = `{
  "openapi": "3.0.3",
  "info": {"title": "Remote Cache API", "version": "2.0.0"},
  "paths": {
    "/pets": {
      "get": {"operationId": "listPets", "responses": {"200": {"description": "ok"}}}
    },
    "/toys": {
      "get": {"operationId": "listToys", "responses": {"200": {"description": "ok"}}}
    }
  }
}`

// cacheFiles returns the cache directory entries excluding metadata bookkeeping.
func cacheFiles(t *testing.T, dir string) []string {
	t.Helper()

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir(%s) failed: %v", dir, err)
	}

	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		names = append(names, entry.Name())
	}

	return names
}

// readCacheMetaFile reads and decodes the single metadata file in dir.
func readCacheMetaFile(t *testing.T, dir string) *remoteCacheMeta {
	t.Helper()

	for _, name := range cacheFiles(t, dir) {
		if filepath.Ext(name) != ".json" {
			continue
		}

		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatalf("ReadFile(%s) failed: %v", name, err)
		}

		meta := &remoteCacheMeta{}
		if err := json.Unmarshal(data, meta); err != nil {
			t.Fatalf("unmarshal cache meta failed: %v", err)
		}

		return meta
	}

	t.Fatal("no cache metadata file found")
	return nil
}

func TestWithRemoteCacheDisabledByEmptyDir(t *testing.T) {
	if p := NewParser(WithRemoteCache("")); p.remoteCache != nil {
		t.Error("expected empty cache dir to disable persistent cache")
	}

	dir := t.TempDir()
	if p := NewParser(WithRemoteCache(dir)); p.remoteCache == nil {
		t.Error("expected non-empty cache dir to enable persistent cache")
	} else if p.remoteCache.dir != dir {
		t.Errorf("expected cache dir %q, got %q", dir, p.remoteCache.dir)
	}
}

func TestRemoteCacheStoresAndReusesEntry(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("ETag", `"v1"`)
		w.Header().Set("Cache-Control", "max-age=3600")
		_, _ = w.Write([]byte(remoteCacheSpecV1))
	}))
	defer server.Close()

	cacheDir := t.TempDir()
	source := server.URL + "/openapi.json"

	parser := NewParser(WithRemoteCache(cacheDir))
	doc, err := parser.LoadSpec(source)
	if err != nil {
		t.Fatalf("first LoadSpec failed: %v", err)
	}
	if doc.Info.Title != "Remote Cache API" {
		t.Errorf("unexpected title: %q", doc.Info.Title)
	}
	if got := requests.Load(); got != 1 {
		t.Fatalf("expected 1 request after first load, got %d", got)
	}

	names := cacheFiles(t, cacheDir)
	if len(names) != 2 {
		t.Fatalf("expected 2 cache files (meta+content), got %v", names)
	}
	for _, name := range names {
		info, err := os.Stat(filepath.Join(cacheDir, name))
		if err != nil {
			t.Fatalf("Stat(%s) failed: %v", name, err)
		}
		if runtime.GOOS != "windows" && info.Mode().Perm() != remoteCacheFilePerm {
			t.Errorf("cache file %s has perm %o, want %o", name, info.Mode().Perm(), remoteCacheFilePerm)
		}
		if filepath.Ext(name) == ".tmp" {
			t.Errorf("unexpected temporary file left in cache: %s", name)
		}
	}

	meta := readCacheMetaFile(t, cacheDir)
	if meta.URL != source {
		t.Errorf("cache meta URL = %q, want %q", meta.URL, source)
	}
	if meta.ETag != `"v1"` {
		t.Errorf("cache meta ETag = %q, want %q", meta.ETag, `"v1"`)
	}
	if meta.SHA256 == "" {
		t.Error("cache meta SHA256 is empty")
	}
	if !meta.ExpiresAt.After(meta.FetchedAt) {
		t.Errorf("expected ExpiresAt after FetchedAt, got %v / %v", meta.FetchedAt, meta.ExpiresAt)
	}

	// A fresh parser pointed at the same cache dir must not hit the network.
	second := NewParser(WithRemoteCache(cacheDir))
	doc, err = second.LoadSpec(source)
	if err != nil {
		t.Fatalf("second LoadSpec failed: %v", err)
	}
	if doc.Info.Title != "Remote Cache API" {
		t.Errorf("unexpected title from cache: %q", doc.Info.Title)
	}
	if got := requests.Load(); got != 1 {
		t.Errorf("expected cached load to skip network (requests=1), got %d", got)
	}
}

func TestRemoteCacheExpiredRevalidatesWith304(t *testing.T) {
	var requests atomic.Int32
	var sawConditional atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if r.Header.Get("If-None-Match") == `"v1"` {
			sawConditional.Store(true)
			w.Header().Set("Cache-Control", "max-age=60")
			w.WriteHeader(http.StatusNotModified)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("ETag", `"v1"`)
		w.Header().Set("Cache-Control", "max-age=0")
		_, _ = w.Write([]byte(remoteCacheSpecV1))
	}))
	defer server.Close()

	cacheDir := t.TempDir()
	source := server.URL + "/openapi.json"
	parser := NewParser(WithRemoteCache(cacheDir))

	if _, err := parser.LoadSpec(source); err != nil {
		t.Fatalf("first LoadSpec failed: %v", err)
	}

	revalidating := NewParser(WithRemoteCache(cacheDir))
	doc, err := revalidating.LoadSpec(source)
	if err != nil {
		t.Fatalf("revalidating LoadSpec failed: %v", err)
	}
	if doc.Info.Title != "Remote Cache API" {
		t.Errorf("unexpected title after 304: %q", doc.Info.Title)
	}
	if !sawConditional.Load() {
		t.Error("expected If-None-Match conditional request on stale entry")
	}
	if got := requests.Load(); got != 2 {
		t.Errorf("expected 2 requests (fetch+revalidate), got %d", got)
	}

	meta := readCacheMetaFile(t, cacheDir)
	if time.Now().After(meta.ExpiresAt) {
		t.Errorf("expected refreshed ExpiresAt in the future, got %v", meta.ExpiresAt)
	}
}

func TestRemoteCacheNon304RefreshesContent(t *testing.T) {
	var requests atomic.Int32
	var releaseV2 atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if r.Header.Get("If-None-Match") == `"v1"` && releaseV2.Load() {
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("ETag", `"v2"`)
			w.Header().Set("Cache-Control", "max-age=3600")
			_, _ = w.Write([]byte(remoteCacheSpecV2))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("ETag", `"v1"`)
		w.Header().Set("Cache-Control", "max-age=0")
		_, _ = w.Write([]byte(remoteCacheSpecV1))
	}))
	defer server.Close()

	cacheDir := t.TempDir()
	source := server.URL + "/openapi.json"

	if _, err := NewParser(WithRemoteCache(cacheDir)).LoadSpec(source); err != nil {
		t.Fatalf("first LoadSpec failed: %v", err)
	}

	releaseV2.Store(true)
	doc, err := NewParser(WithRemoteCache(cacheDir)).LoadSpec(source)
	if err != nil {
		t.Fatalf("second LoadSpec failed: %v", err)
	}
	if doc.Info.Version != "2.0.0" {
		t.Errorf("expected refreshed spec version 2.0.0, got %q", doc.Info.Version)
	}
	if got := requests.Load(); got != 2 {
		t.Errorf("expected 2 requests, got %d", got)
	}

	meta := readCacheMetaFile(t, cacheDir)
	if meta.ETag != `"v2"` {
		t.Errorf("expected cache meta ETag refreshed to v2, got %q", meta.ETag)
	}

	// Third load must reuse the updated cache without network.
	if _, err := NewParser(WithRemoteCache(cacheDir)).LoadSpec(source); err != nil {
		t.Fatalf("third LoadSpec failed: %v", err)
	}
	if got := requests.Load(); got != 2 {
		t.Errorf("expected updated cache to be reused, got %d requests", got)
	}
}

func TestRemoteCacheNetworkErrorFallsBackToStale(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "max-age=0")
		_, _ = w.Write([]byte(remoteCacheSpecV1))
	}))

	cacheDir := t.TempDir()
	source := server.URL + "/openapi.json"

	if _, err := NewParser(WithRemoteCache(cacheDir)).LoadSpec(source); err != nil {
		t.Fatalf("initial LoadSpec failed: %v", err)
	}
	server.Close()

	doc, err := NewParser(WithRemoteCache(cacheDir)).LoadSpec(source)
	if err != nil {
		t.Fatalf("expected stale cache fallback, got error: %v", err)
	}
	if doc.Info.Title != "Remote Cache API" {
		t.Errorf("unexpected stale fallback title: %q", doc.Info.Title)
	}
	if got := requests.Load(); got != 1 {
		t.Errorf("expected 1 successful request, got %d", got)
	}
}

func TestRemoteCacheNetworkErrorWithoutCacheReturnsError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	source := server.URL + "/openapi.json"
	server.Close()

	parser := NewParser(WithRemoteCache(t.TempDir()))
	if _, err := parser.LoadSpec(source); err == nil {
		t.Fatal("expected error when network fails and cache is empty")
	}
}

func TestRemoteCacheCorruptEntryIsRefetched(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "max-age=3600")
		_, _ = w.Write([]byte(remoteCacheSpecV1))
	}))
	defer server.Close()

	cacheDir := t.TempDir()
	source := server.URL + "/openapi.json"
	if _, err := NewParser(WithRemoteCache(cacheDir)).LoadSpec(source); err != nil {
		t.Fatalf("initial LoadSpec failed: %v", err)
	}

	// Corrupt every non-metadata cache file (content blob).
	for _, name := range cacheFiles(t, cacheDir) {
		if filepath.Ext(name) == ".json" {
			continue
		}
		if err := os.WriteFile(filepath.Join(cacheDir, name), []byte("corrupted"), remoteCacheFilePerm); err != nil {
			t.Fatalf("corrupt cache file failed: %v", err)
		}
	}

	doc, err := NewParser(WithRemoteCache(cacheDir)).LoadSpec(source)
	if err != nil {
		t.Fatalf("expected refetch after corrupt cache, got: %v", err)
	}
	if doc.Info.Title != "Remote Cache API" {
		t.Errorf("unexpected title after refetch: %q", doc.Info.Title)
	}
	if got := requests.Load(); got != 2 {
		t.Errorf("expected refetch (2 requests), got %d", got)
	}
}

func TestRemoteCacheLocalFilesAreNotCached(t *testing.T) {
	specPath := filepath.Join(t.TempDir(), "local.json")
	if err := os.WriteFile(specPath, []byte(remoteCacheSpecV1), 0o600); err != nil {
		t.Fatalf("write local spec failed: %v", err)
	}

	cacheDir := filepath.Join(t.TempDir(), "cache")
	doc, err := NewParser(WithRemoteCache(cacheDir)).LoadSpec(specPath)
	if err != nil {
		t.Fatalf("LoadSpec(local) failed: %v", err)
	}
	if doc.Info.Title != "Remote Cache API" {
		t.Errorf("unexpected local spec title: %q", doc.Info.Title)
	}

	entries, statErr := os.ReadDir(cacheDir)
	if statErr == nil && len(entries) != 0 {
		t.Errorf("expected local file spec not to create cache entries, got %v", entries)
	}
}

func TestRemoteCache304WithoutEntryReturnsError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotModified)
	}))
	defer server.Close()

	parser := NewParser(WithRemoteCache(t.TempDir()))
	if _, err := parser.LoadSpec(server.URL + "/openapi.json"); err == nil {
		t.Fatal("expected error for 304 without cached entry")
	}
}

func TestSetConditionalHeaders(t *testing.T) {
	t.Run("both validators", func(t *testing.T) {
		req, err := http.NewRequest(http.MethodGet, "https://example.com/spec.json", nil)
		if err != nil {
			t.Fatalf("NewRequest failed: %v", err)
		}
		setConditionalHeaders(req, &remoteCacheMeta{ETag: `"abc"`, LastModified: "Wed, 21 Oct 2015 07:28:00 GMT"})
		if got := req.Header.Get("If-None-Match"); got != `"abc"` {
			t.Errorf("If-None-Match = %q", got)
		}
		if got := req.Header.Get("If-Modified-Since"); got != "Wed, 21 Oct 2015 07:28:00 GMT" {
			t.Errorf("If-Modified-Since = %q", got)
		}
	})

	t.Run("last modified only", func(t *testing.T) {
		req, err := http.NewRequest(http.MethodGet, "https://example.com/spec.json", nil)
		if err != nil {
			t.Fatalf("NewRequest failed: %v", err)
		}
		setConditionalHeaders(req, &remoteCacheMeta{LastModified: "Wed, 21 Oct 2015 07:28:00 GMT"})
		if got := req.Header.Get("If-None-Match"); got != "" {
			t.Errorf("expected no If-None-Match, got %q", got)
		}
		if got := req.Header.Get("If-Modified-Since"); got == "" {
			t.Error("expected If-Modified-Since to be set")
		}
	})

	t.Run("nil meta and no validators", func(t *testing.T) {
		req, err := http.NewRequest(http.MethodGet, "https://example.com/spec.json", nil)
		if err != nil {
			t.Fatalf("NewRequest failed: %v", err)
		}
		setConditionalHeaders(req, nil)
		setConditionalHeaders(req, &remoteCacheMeta{})
		if got := req.Header.Get("If-None-Match"); got != "" {
			t.Errorf("expected no If-None-Match, got %q", got)
		}
		if got := req.Header.Get("If-Modified-Since"); got != "" {
			t.Errorf("expected no If-Modified-Since, got %q", got)
		}
	})
}

func TestRemoteCacheExpiry(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		name   string
		header http.Header
		want   time.Time
	}{
		{"max-age", http.Header{"Cache-Control": {"public, max-age=120"}}, now.Add(2 * time.Minute)},
		{"no-store means immediately stale", http.Header{"Cache-Control": {"no-store"}}, now},
		{"expires header", http.Header{"Expires": {"Thu, 01 Jan 2026 13:00:00 GMT"}}, now.Add(time.Hour)},
		{"default ttl", http.Header{}, now.Add(defaultRemoteCacheTTL)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := remoteCacheExpiry(tt.header, now); !got.Equal(tt.want) {
				t.Errorf("remoteCacheExpiry() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestRemoteCacheMetaIsExpired(t *testing.T) {
	now := time.Now()
	expired := &remoteCacheMeta{ExpiresAt: now.Add(-time.Second)}
	fresh := &remoteCacheMeta{ExpiresAt: now.Add(time.Hour)}

	if !expired.isExpired(now) {
		t.Error("expected expired meta to be stale")
	}
	if fresh.isExpired(now) {
		t.Error("expected fresh meta to be valid")
	}
}

func TestRemoteCacheInvalidDirectoryFallsBackToNetwork(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(remoteCacheSpecV1))
	}))
	defer server.Close()

	// A regular file cannot be used as a cache directory: caching must degrade
	// gracefully instead of failing the spec load.
	blocker := filepath.Join(t.TempDir(), "blocker")
	if err := os.WriteFile(blocker, []byte("x"), 0o600); err != nil {
		t.Fatalf("write blocker failed: %v", err)
	}

	doc, err := NewParser(WithRemoteCache(blocker)).LoadSpec(server.URL + "/openapi.json")
	if err != nil {
		t.Fatalf("expected graceful degradation, got: %v", err)
	}
	if doc.Info.Title != "Remote Cache API" {
		t.Errorf("unexpected title: %q", doc.Info.Title)
	}
	if got := requests.Load(); got != 1 {
		t.Errorf("expected 1 network request, got %d", got)
	}
}

// TestPropertyRemoteCacheMetaRoundTrip validates that on-disk cache metadata
// survives JSON serialization without losing fields.
func TestPropertyRemoteCacheMetaRoundTrip(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping property test in short mode")
	}

	properties := gopter.NewProperties(proptest.TestParameters())
	properties.Property("cache metadata JSON round-trip preserves all fields", prop.ForAll(
		func(url, etag, lastModified, digest string, size int64) bool {
			now := time.Now().UTC().Truncate(time.Second)
			original := &remoteCacheMeta{
				URL:          url,
				ETag:         etag,
				LastModified: lastModified,
				FetchedAt:    now,
				ExpiresAt:    now.Add(time.Hour),
				SHA256:       digest,
				Size:         size,
			}

			data, err := json.Marshal(original)
			if err != nil {
				return false
			}

			var decoded remoteCacheMeta
			if err := json.Unmarshal(data, &decoded); err != nil {
				return false
			}

			return decoded.URL == original.URL &&
				decoded.ETag == original.ETag &&
				decoded.LastModified == original.LastModified &&
				decoded.FetchedAt.Equal(original.FetchedAt) &&
				decoded.ExpiresAt.Equal(original.ExpiresAt) &&
				decoded.SHA256 == original.SHA256 &&
				decoded.Size == original.Size
		},
		gen.AlphaString(),
		gen.AlphaString(),
		gen.AlphaString(),
		gen.AlphaString(),
		gen.Int64(),
	))

	properties.TestingRun(t)
}
