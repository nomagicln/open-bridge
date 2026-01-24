package config

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// =============================================================================
// SpecChangeEvent Tests
// =============================================================================

func TestSpecChangeType_Values(t *testing.T) {
	// Verify all change types are defined correctly
	assert.Equal(t, SpecChangeType("modified"), SpecChangeModified)
	assert.Equal(t, SpecChangeType("deleted"), SpecChangeDeleted)
	assert.Equal(t, SpecChangeType("expired"), SpecChangeExpired)
	assert.Equal(t, SpecChangeType("error"), SpecChangeError)
}

// =============================================================================
// LocalFileWatcher Tests
// =============================================================================

func TestNewLocalFileWatcher(t *testing.T) {
	watcher := NewLocalFileWatcher("test-app", "/path/to/spec.yaml")
	require.NotNil(t, watcher)
	assert.Equal(t, "test-app", watcher.appName)
	assert.Equal(t, "/path/to/spec.yaml", watcher.filePath)
	assert.Equal(t, 2*time.Second, watcher.pollInterval) // Default changed to 2s for fallback
}

func TestNewLocalFileWatcher_WithOptions(t *testing.T) {
	watcher := NewLocalFileWatcher(
		"test-app",
		"/path/to/spec.yaml",
		WithPollInterval(10*time.Second),
	)
	require.NotNil(t, watcher)
	assert.Equal(t, 10*time.Second, watcher.pollInterval)
}

func TestLocalFileWatcher_GetSourceURL(t *testing.T) {
	watcher := NewLocalFileWatcher("test-app", "/path/to/spec.yaml")
	assert.Equal(t, "/path/to/spec.yaml", watcher.GetSourceURL())
}

func TestLocalFileWatcher_GetAppName(t *testing.T) {
	watcher := NewLocalFileWatcher("test-app", "/path/to/spec.yaml")
	assert.Equal(t, "test-app", watcher.GetAppName())
}

func TestLocalFileWatcher_AddHandler(t *testing.T) {
	watcher := NewLocalFileWatcher("test-app", "/path/to/spec.yaml")

	watcher.AddHandler(func(_ SpecChangeEvent) {
		// Handler registered
	})

	assert.Len(t, watcher.handlers, 1)
}

func TestLocalFileWatcher_Start_NonExistentFile(t *testing.T) {
	watcher := NewLocalFileWatcher("test-app", "/nonexistent/path/spec.yaml")

	ctx := t.Context()

	err := watcher.Start(ctx)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to initialize baseline")
}

func TestLocalFileWatcher_Start_Success(t *testing.T) {
	// Create a temporary file
	tmpDir := t.TempDir()
	specPath := filepath.Join(tmpDir, "spec.yaml")
	err := os.WriteFile(specPath, []byte(`openapi: "3.0.0"`), 0644)
	require.NoError(t, err)

	watcher := NewLocalFileWatcher("test-app", specPath, WithPollInterval(50*time.Millisecond))

	ctx := t.Context()

	err = watcher.Start(ctx)
	require.NoError(t, err)

	// Verify watcher is running
	watcher.mu.RLock()
	assert.True(t, watcher.running)
	watcher.mu.RUnlock()

	// Stop the watcher
	err = watcher.Stop()
	require.NoError(t, err)
}

func TestLocalFileWatcher_Start_AlreadyRunning(t *testing.T) {
	tmpDir := t.TempDir()
	specPath := filepath.Join(tmpDir, "spec.yaml")
	err := os.WriteFile(specPath, []byte(`openapi: "3.0.0"`), 0644)
	require.NoError(t, err)

	watcher := NewLocalFileWatcher("test-app", specPath)

	ctx := t.Context()

	err = watcher.Start(ctx)
	require.NoError(t, err)
	defer func() { _ = watcher.Stop() }()

	// Try to start again
	err = watcher.Start(ctx)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "already running")
}

func TestLocalFileWatcher_Stop_NotRunning(t *testing.T) {
	watcher := NewLocalFileWatcher("test-app", "/path/to/spec.yaml")

	// Stop without starting should be safe
	err := watcher.Stop()
	require.NoError(t, err)
}

func TestLocalFileWatcher_CheckNow_NonExistent(t *testing.T) {
	watcher := NewLocalFileWatcher("test-app", "/nonexistent/path/spec.yaml")

	event, err := watcher.CheckNow()
	require.Error(t, err)
	require.NotNil(t, event)
	assert.Equal(t, SpecChangeError, event.ChangeType)
}

func TestLocalFileWatcher_CheckNow_NoChange(t *testing.T) {
	tmpDir := t.TempDir()
	specPath := filepath.Join(tmpDir, "spec.yaml")
	content := []byte(`openapi: "3.0.0"`)
	err := os.WriteFile(specPath, content, 0644)
	require.NoError(t, err)

	watcher := NewLocalFileWatcher("test-app", specPath)

	// First check initializes baseline
	event, err := watcher.CheckNow()
	require.NoError(t, err)
	assert.Nil(t, event) // No change on first check (initialization)

	// Second check should also return nil (no change)
	event, err = watcher.CheckNow()
	require.NoError(t, err)
	assert.Nil(t, event)
}

func TestLocalFileWatcher_CheckNow_DetectsModification(t *testing.T) {
	tmpDir := t.TempDir()
	specPath := filepath.Join(tmpDir, "spec.yaml")
	content := []byte(`openapi: "3.0.0"`)
	err := os.WriteFile(specPath, content, 0644)
	require.NoError(t, err)

	watcher := NewLocalFileWatcher("test-app", specPath)

	// Initialize baseline
	_, err = watcher.CheckNow()
	require.NoError(t, err)

	// Modify file (need to ensure mod time changes)
	time.Sleep(10 * time.Millisecond)
	newContent := []byte(`openapi: "3.1.0"`)
	err = os.WriteFile(specPath, newContent, 0644)
	require.NoError(t, err)

	// Check should detect modification
	event, err := watcher.CheckNow()
	require.NoError(t, err)
	require.NotNil(t, event)
	assert.Equal(t, SpecChangeModified, event.ChangeType)
	assert.Equal(t, "test-app", event.AppName)
	assert.Equal(t, specPath, event.SourceURL)
	assert.Equal(t, newContent, event.NewContent)
}

func TestLocalFileWatcher_CheckNow_DetectsDeletion(t *testing.T) {
	tmpDir := t.TempDir()
	specPath := filepath.Join(tmpDir, "spec.yaml")
	content := []byte(`openapi: "3.0.0"`)
	err := os.WriteFile(specPath, content, 0644)
	require.NoError(t, err)

	watcher := NewLocalFileWatcher("test-app", specPath)

	// Initialize baseline
	_, err = watcher.CheckNow()
	require.NoError(t, err)

	// Delete file
	err = os.Remove(specPath)
	require.NoError(t, err)

	// Check should detect deletion
	event, err := watcher.CheckNow()
	require.NoError(t, err)
	require.NotNil(t, event)
	assert.Equal(t, SpecChangeDeleted, event.ChangeType)
}

func TestLocalFileWatcher_DetectsChange_WithRunningWatcher(t *testing.T) {
	tmpDir := t.TempDir()
	specPath := filepath.Join(tmpDir, "spec.yaml")
	content := []byte(`openapi: "3.0.0"`)
	err := os.WriteFile(specPath, content, 0644)
	require.NoError(t, err)

	watcher := NewLocalFileWatcher("test-app", specPath, WithPollInterval(50*time.Millisecond))

	var eventReceived atomic.Bool
	var receivedEvent SpecChangeEvent
	var mu sync.Mutex

	watcher.AddHandler(func(event SpecChangeEvent) {
		mu.Lock()
		receivedEvent = event
		mu.Unlock()
		eventReceived.Store(true)
	})

	ctx := t.Context()

	err = watcher.Start(ctx)
	require.NoError(t, err)
	defer func() { _ = watcher.Stop() }()

	// Wait for watcher to initialize
	time.Sleep(100 * time.Millisecond)

	// Modify file
	newContent := []byte(`openapi: "3.1.0"`)
	err = os.WriteFile(specPath, newContent, 0644)
	require.NoError(t, err)

	// Wait for detection
	require.Eventually(t, func() bool {
		return eventReceived.Load()
	}, 500*time.Millisecond, 10*time.Millisecond, "expected change event")

	mu.Lock()
	assert.Equal(t, SpecChangeModified, receivedEvent.ChangeType)
	assert.Equal(t, newContent, receivedEvent.NewContent)
	mu.Unlock()
}

func TestLocalFileWatcher_TouchWithoutContentChange(t *testing.T) {
	tmpDir := t.TempDir()
	specPath := filepath.Join(tmpDir, "spec.yaml")
	content := []byte(`openapi: "3.0.0"`)
	err := os.WriteFile(specPath, content, 0644)
	require.NoError(t, err)

	watcher := NewLocalFileWatcher("test-app", specPath)

	// Initialize baseline
	_, err = watcher.CheckNow()
	require.NoError(t, err)

	// Touch file (same content, new mod time)
	time.Sleep(10 * time.Millisecond)
	err = os.WriteFile(specPath, content, 0644)
	require.NoError(t, err)

	// Should not detect as change since content hash is the same
	event, err := watcher.CheckNow()
	require.NoError(t, err)
	assert.Nil(t, event) // No change because content is the same
}

// =============================================================================
// RemoteSpecWatcher Tests
// =============================================================================

func TestNewRemoteSpecWatcher(t *testing.T) {
	tmpDir := t.TempDir()
	cacheManager := NewSpecCacheManager(tmpDir)

	watcher := NewRemoteSpecWatcher("test-app", "https://example.com/spec.json", cacheManager)
	require.NotNil(t, watcher)
	assert.Equal(t, "test-app", watcher.appName)
	assert.Equal(t, "https://example.com/spec.json", watcher.sourceURL)
	assert.Equal(t, 1*time.Minute, watcher.checkInterval)
	assert.Equal(t, 5*time.Minute, watcher.refreshBefore)
}

func TestNewRemoteSpecWatcher_WithOptions(t *testing.T) {
	tmpDir := t.TempDir()
	cacheManager := NewSpecCacheManager(tmpDir)

	watcher := NewRemoteSpecWatcher(
		"test-app",
		"https://example.com/spec.json",
		cacheManager,
		WithCheckInterval(30*time.Second),
		WithRefreshBefore(2*time.Minute),
	)
	require.NotNil(t, watcher)
	assert.Equal(t, 30*time.Second, watcher.checkInterval)
	assert.Equal(t, 2*time.Minute, watcher.refreshBefore)
}

func TestRemoteSpecWatcher_GetSourceURL(t *testing.T) {
	tmpDir := t.TempDir()
	cacheManager := NewSpecCacheManager(tmpDir)

	watcher := NewRemoteSpecWatcher("test-app", "https://example.com/spec.json", cacheManager)
	assert.Equal(t, "https://example.com/spec.json", watcher.GetSourceURL())
}

func TestRemoteSpecWatcher_GetAppName(t *testing.T) {
	tmpDir := t.TempDir()
	cacheManager := NewSpecCacheManager(tmpDir)

	watcher := NewRemoteSpecWatcher("test-app", "https://example.com/spec.json", cacheManager)
	assert.Equal(t, "test-app", watcher.GetAppName())
}

func TestRemoteSpecWatcher_Start_Success(t *testing.T) {
	tmpDir := t.TempDir()
	cacheManager := NewSpecCacheManager(tmpDir)

	watcher := NewRemoteSpecWatcher(
		"test-app",
		"https://example.com/spec.json",
		cacheManager,
		WithCheckInterval(50*time.Millisecond),
	)

	ctx := t.Context()

	err := watcher.Start(ctx)
	require.NoError(t, err)

	watcher.mu.RLock()
	assert.True(t, watcher.running)
	watcher.mu.RUnlock()

	err = watcher.Stop()
	require.NoError(t, err)
}

func TestRemoteSpecWatcher_Start_AlreadyRunning(t *testing.T) {
	tmpDir := t.TempDir()
	cacheManager := NewSpecCacheManager(tmpDir)

	watcher := NewRemoteSpecWatcher("test-app", "https://example.com/spec.json", cacheManager)

	ctx := t.Context()

	err := watcher.Start(ctx)
	require.NoError(t, err)
	defer func() { _ = watcher.Stop() }()

	err = watcher.Start(ctx)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "already running")
}

func TestRemoteSpecWatcher_Stop_NotRunning(t *testing.T) {
	tmpDir := t.TempDir()
	cacheManager := NewSpecCacheManager(tmpDir)

	watcher := NewRemoteSpecWatcher("test-app", "https://example.com/spec.json", cacheManager)

	err := watcher.Stop()
	require.NoError(t, err)
}

func TestRemoteSpecWatcher_CheckNow_FetchesNew(t *testing.T) {
	specContent := `{"openapi": "3.0.0"}`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "max-age=3600")
		_, _ = w.Write([]byte(specContent))
	}))
	defer server.Close()

	tmpDir := t.TempDir()
	cacheManager := NewSpecCacheManager(tmpDir)

	watcher := NewRemoteSpecWatcher("test-app", server.URL+"/spec.json", cacheManager)

	event, err := watcher.CheckNow()
	require.NoError(t, err)
	require.NotNil(t, event)
	assert.Equal(t, SpecChangeModified, event.ChangeType)
	assert.Equal(t, []byte(specContent), event.NewContent)
}

func TestRemoteSpecWatcher_CheckNow_ValidCache(t *testing.T) {
	specContent := []byte(`{"openapi": "3.0.0"}`)

	tmpDir := t.TempDir()
	cacheManager := NewSpecCacheManager(tmpDir)

	// Pre-populate cache with valid (non-expired) data
	appName := "test-app"
	cacheDir := cacheManager.getCacheDir(appName)
	err := os.MkdirAll(cacheDir, 0755)
	require.NoError(t, err)

	specPath := cacheManager.getSpecPath(appName, "json")
	err = os.WriteFile(specPath, specContent, 0644)
	require.NoError(t, err)

	meta := &SpecCacheMeta{
		SourceURL:   "https://example.com/spec.json",
		Format:      "json",
		FetchedAt:   time.Now(),
		ExpiresAt:   time.Now().Add(time.Hour), // Valid for 1 hour
		ContentHash: computeHash(specContent),
		Size:        int64(len(specContent)),
	}
	err = cacheManager.SaveMeta(appName, meta)
	require.NoError(t, err)

	watcher := NewRemoteSpecWatcher(
		appName,
		"https://example.com/spec.json",
		cacheManager,
		WithRefreshBefore(5*time.Minute),
	)

	// Cache is valid (expires in 1 hour > 5 min refresh threshold)
	event, err := watcher.CheckNow()
	require.NoError(t, err)
	assert.Nil(t, event) // No change needed
}

func TestRemoteSpecWatcher_CheckNow_ExpiredCache(t *testing.T) {
	specContent := `{"openapi": "3.0.0"}`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "max-age=3600")
		_, _ = w.Write([]byte(specContent))
	}))
	defer server.Close()

	tmpDir := t.TempDir()
	cacheManager := NewSpecCacheManager(tmpDir)

	// Pre-populate cache with expired data
	appName := "test-app"
	cacheDir := cacheManager.getCacheDir(appName)
	err := os.MkdirAll(cacheDir, 0755)
	require.NoError(t, err)

	oldContent := []byte(`{"openapi": "2.0"}`) // Different content
	specPath := cacheManager.getSpecPath(appName, "json")
	err = os.WriteFile(specPath, oldContent, 0644)
	require.NoError(t, err)

	meta := &SpecCacheMeta{
		SourceURL:   server.URL + "/spec.json",
		Format:      "json",
		FetchedAt:   time.Now().Add(-2 * time.Hour),
		ExpiresAt:   time.Now().Add(-time.Hour), // Expired 1 hour ago
		ContentHash: computeHash(oldContent),
		Size:        int64(len(oldContent)),
	}
	err = cacheManager.SaveMeta(appName, meta)
	require.NoError(t, err)

	watcher := NewRemoteSpecWatcher(appName, server.URL+"/spec.json", cacheManager)

	event, err := watcher.CheckNow()
	require.NoError(t, err)
	require.NotNil(t, event)
	assert.Equal(t, SpecChangeExpired, event.ChangeType)
	assert.Equal(t, []byte(specContent), event.NewContent)
}

func TestRemoteSpecWatcher_AddHandler(t *testing.T) {
	tmpDir := t.TempDir()
	cacheManager := NewSpecCacheManager(tmpDir)

	watcher := NewRemoteSpecWatcher("test-app", "https://example.com/spec.json", cacheManager)

	watcher.AddHandler(func(event SpecChangeEvent) {})

	assert.Len(t, watcher.handlers, 1)
}

// =============================================================================
// SpecWatcherManager Tests
// =============================================================================

func TestNewSpecWatcherManager(t *testing.T) {
	tmpDir := t.TempDir()
	cacheManager := NewSpecCacheManager(tmpDir)

	manager := NewSpecWatcherManager(cacheManager)
	require.NotNil(t, manager)
	assert.NotNil(t, manager.watchers)
	assert.Equal(t, cacheManager, manager.cacheManager)
}

func TestSpecWatcherManager_AddHandler(t *testing.T) {
	tmpDir := t.TempDir()
	cacheManager := NewSpecCacheManager(tmpDir)
	manager := NewSpecWatcherManager(cacheManager)

	manager.AddHandler(func(event SpecChangeEvent) {})

	assert.Len(t, manager.handlers, 1)
}

func TestSpecWatcherManager_WatchApp_LocalFile(t *testing.T) {
	tmpDir := t.TempDir()
	specPath := filepath.Join(tmpDir, "spec.yaml")
	err := os.WriteFile(specPath, []byte(`openapi: "3.0.0"`), 0644)
	require.NoError(t, err)

	cacheManager := NewSpecCacheManager(tmpDir)
	manager := NewSpecWatcherManager(cacheManager)

	err = manager.WatchApp("test-app", specPath)
	require.NoError(t, err)

	apps := manager.GetWatchedApps()
	assert.Contains(t, apps, "test-app")
}

func TestSpecWatcherManager_WatchApp_RemoteURL(t *testing.T) {
	tmpDir := t.TempDir()
	cacheManager := NewSpecCacheManager(tmpDir)
	manager := NewSpecWatcherManager(cacheManager)

	err := manager.WatchApp("test-app", "https://example.com/spec.json")
	require.NoError(t, err)

	apps := manager.GetWatchedApps()
	assert.Contains(t, apps, "test-app")
}

func TestSpecWatcherManager_WatchApp_ReplaceExisting(t *testing.T) {
	tmpDir := t.TempDir()
	specPath1 := filepath.Join(tmpDir, "spec1.yaml")
	specPath2 := filepath.Join(tmpDir, "spec2.yaml")
	err := os.WriteFile(specPath1, []byte(`openapi: "3.0.0"`), 0644)
	require.NoError(t, err)
	err = os.WriteFile(specPath2, []byte(`openapi: "3.1.0"`), 0644)
	require.NoError(t, err)

	cacheManager := NewSpecCacheManager(tmpDir)
	manager := NewSpecWatcherManager(cacheManager)

	err = manager.WatchApp("test-app", specPath1)
	require.NoError(t, err)

	// Watch same app with different source
	err = manager.WatchApp("test-app", specPath2)
	require.NoError(t, err)

	// Should still have only one watcher
	apps := manager.GetWatchedApps()
	assert.Len(t, apps, 1)

	// Watcher should be for the new source
	manager.mu.RLock()
	watcher := manager.watchers["test-app"]
	manager.mu.RUnlock()
	assert.Equal(t, specPath2, watcher.GetSourceURL())
}

func TestSpecWatcherManager_UnwatchApp(t *testing.T) {
	tmpDir := t.TempDir()
	specPath := filepath.Join(tmpDir, "spec.yaml")
	err := os.WriteFile(specPath, []byte(`openapi: "3.0.0"`), 0644)
	require.NoError(t, err)

	cacheManager := NewSpecCacheManager(tmpDir)
	manager := NewSpecWatcherManager(cacheManager)

	err = manager.WatchApp("test-app", specPath)
	require.NoError(t, err)

	err = manager.UnwatchApp("test-app")
	require.NoError(t, err)

	apps := manager.GetWatchedApps()
	assert.NotContains(t, apps, "test-app")
}

func TestSpecWatcherManager_UnwatchApp_NotFound(t *testing.T) {
	tmpDir := t.TempDir()
	cacheManager := NewSpecCacheManager(tmpDir)
	manager := NewSpecWatcherManager(cacheManager)

	err := manager.UnwatchApp("nonexistent")
	require.NoError(t, err) // Should not error for non-existent app
}

func TestSpecWatcherManager_Start_Stop(t *testing.T) {
	tmpDir := t.TempDir()
	specPath := filepath.Join(tmpDir, "spec.yaml")
	err := os.WriteFile(specPath, []byte(`openapi: "3.0.0"`), 0644)
	require.NoError(t, err)

	cacheManager := NewSpecCacheManager(tmpDir)
	manager := NewSpecWatcherManager(cacheManager)

	err = manager.WatchApp("test-app", specPath)
	require.NoError(t, err)

	err = manager.Start()
	require.NoError(t, err)

	// Verify context is set
	manager.mu.RLock()
	assert.NotNil(t, manager.ctx)
	manager.mu.RUnlock()

	err = manager.Stop()
	require.NoError(t, err)

	// Verify context is cleared
	manager.mu.RLock()
	assert.Nil(t, manager.ctx)
	manager.mu.RUnlock()
}

func TestSpecWatcherManager_Start_AlreadyRunning(t *testing.T) {
	tmpDir := t.TempDir()
	cacheManager := NewSpecCacheManager(tmpDir)
	manager := NewSpecWatcherManager(cacheManager)

	err := manager.Start()
	require.NoError(t, err)
	defer func() { _ = manager.Stop() }()

	err = manager.Start()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "already running")
}

func TestSpecWatcherManager_CheckAllNow(t *testing.T) {
	tmpDir := t.TempDir()
	specPath := filepath.Join(tmpDir, "spec.yaml")
	err := os.WriteFile(specPath, []byte(`openapi: "3.0.0"`), 0644)
	require.NoError(t, err)

	cacheManager := NewSpecCacheManager(tmpDir)
	manager := NewSpecWatcherManager(cacheManager)

	err = manager.WatchApp("test-app", specPath)
	require.NoError(t, err)

	results := manager.CheckAllNow()
	assert.Contains(t, results, "test-app")
	// First check should return nil (initialization)
	assert.Nil(t, results["test-app"])
}

func TestSpecWatcherManager_CheckAppNow(t *testing.T) {
	tmpDir := t.TempDir()
	specPath := filepath.Join(tmpDir, "spec.yaml")
	err := os.WriteFile(specPath, []byte(`openapi: "3.0.0"`), 0644)
	require.NoError(t, err)

	cacheManager := NewSpecCacheManager(tmpDir)
	manager := NewSpecWatcherManager(cacheManager)

	err = manager.WatchApp("test-app", specPath)
	require.NoError(t, err)

	event, err := manager.CheckAppNow("test-app")
	require.NoError(t, err)
	assert.Nil(t, event) // First check initializes baseline
}

func TestSpecWatcherManager_CheckAppNow_NotFound(t *testing.T) {
	tmpDir := t.TempDir()
	cacheManager := NewSpecCacheManager(tmpDir)
	manager := NewSpecWatcherManager(cacheManager)

	_, err := manager.CheckAppNow("nonexistent")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no watcher registered")
}

func TestSpecWatcherManager_GetWatchedApps(t *testing.T) {
	tmpDir := t.TempDir()
	cacheManager := NewSpecCacheManager(tmpDir)
	manager := NewSpecWatcherManager(cacheManager)

	// Empty initially
	apps := manager.GetWatchedApps()
	assert.Empty(t, apps)

	// Add watchers
	err := manager.WatchApp("app1", "https://example.com/spec1.json")
	require.NoError(t, err)
	err = manager.WatchApp("app2", "https://example.com/spec2.json")
	require.NoError(t, err)

	apps = manager.GetWatchedApps()
	assert.Len(t, apps, 2)
	assert.Contains(t, apps, "app1")
	assert.Contains(t, apps, "app2")
}

func TestSpecWatcherManager_GlobalHandler_ReceivesEvents(t *testing.T) {
	tmpDir := t.TempDir()
	specPath := filepath.Join(tmpDir, "spec.yaml")
	err := os.WriteFile(specPath, []byte(`openapi: "3.0.0"`), 0644)
	require.NoError(t, err)

	cacheManager := NewSpecCacheManager(tmpDir)
	manager := NewSpecWatcherManager(cacheManager)

	var eventReceived atomic.Bool
	manager.AddHandler(func(_ SpecChangeEvent) {
		eventReceived.Store(true)
	})

	// Create watcher with short poll interval directly
	watcher := NewLocalFileWatcher("test-app", specPath, WithPollInterval(50*time.Millisecond))
	for _, h := range manager.handlers {
		watcher.AddHandler(h)
	}
	manager.mu.Lock()
	manager.watchers["test-app"] = watcher
	manager.mu.Unlock()

	err = manager.Start()
	require.NoError(t, err)
	defer func() { _ = manager.Stop() }()

	// Wait for watcher to initialize
	time.Sleep(100 * time.Millisecond)

	// Modify file
	err = os.WriteFile(specPath, []byte(`openapi: "3.1.0"`), 0644)
	require.NoError(t, err)

	require.Eventually(t, func() bool {
		return eventReceived.Load()
	}, 500*time.Millisecond, 10*time.Millisecond, "expected handler to be called")
}

// =============================================================================
// Integration Tests
// =============================================================================

func TestLocalFileWatcher_StopViaContext(t *testing.T) {
	tmpDir := t.TempDir()
	specPath := filepath.Join(tmpDir, "spec.yaml")
	err := os.WriteFile(specPath, []byte(`openapi: "3.0.0"`), 0644)
	require.NoError(t, err)

	watcher := NewLocalFileWatcher("test-app", specPath, WithPollInterval(50*time.Millisecond))

	ctx, cancel := context.WithCancel(context.Background())

	err = watcher.Start(ctx)
	require.NoError(t, err)

	// Cancel context
	cancel()

	// Wait for watcher to stop
	time.Sleep(100 * time.Millisecond)

	watcher.mu.RLock()
	running := watcher.running
	watcher.mu.RUnlock()
	assert.False(t, running)
}

func TestRemoteSpecWatcher_StopViaContext(t *testing.T) {
	tmpDir := t.TempDir()
	cacheManager := NewSpecCacheManager(tmpDir)

	watcher := NewRemoteSpecWatcher(
		"test-app",
		"https://example.com/spec.json",
		cacheManager,
		WithCheckInterval(50*time.Millisecond),
	)

	ctx, cancel := context.WithCancel(context.Background())

	err := watcher.Start(ctx)
	require.NoError(t, err)

	// Cancel context
	cancel()

	// Wait for watcher to stop
	time.Sleep(100 * time.Millisecond)

	watcher.mu.RLock()
	running := watcher.running
	watcher.mu.RUnlock()
	assert.False(t, running)
}

// =============================================================================
// Fsnotify Mode Tests
// =============================================================================

func TestLocalFileWatcher_UsesFsnotifyMode(t *testing.T) {
	tmpDir := t.TempDir()
	specPath := filepath.Join(tmpDir, "spec.yaml")
	err := os.WriteFile(specPath, []byte(`openapi: "3.0.0"`), 0644)
	require.NoError(t, err)

	watcher := NewLocalFileWatcher("test-app", specPath, WithPollInterval(50*time.Millisecond))

	ctx := t.Context()

	err = watcher.Start(ctx)
	require.NoError(t, err)
	defer func() { _ = watcher.Stop() }()

	// Give watcher time to initialize fsnotify
	time.Sleep(100 * time.Millisecond)

	// Should be using fsnotify mode
	mode := watcher.GetWatchMode()
	assert.Equal(t, WatchModeInotify, mode, "expected fsnotify mode")
}

func TestLocalFileWatcher_FsnotifyDetectsChange(t *testing.T) {
	tmpDir := t.TempDir()
	specPath := filepath.Join(tmpDir, "spec.yaml")
	err := os.WriteFile(specPath, []byte(`openapi: "3.0.0"`), 0644)
	require.NoError(t, err)

	watcher := NewLocalFileWatcher("test-app", specPath, WithPollInterval(50*time.Millisecond))

	var eventReceived atomic.Bool
	var receivedEvent SpecChangeEvent
	var mu sync.Mutex

	watcher.AddHandler(func(event SpecChangeEvent) {
		mu.Lock()
		receivedEvent = event
		mu.Unlock()
		eventReceived.Store(true)
	})

	ctx := t.Context()

	err = watcher.Start(ctx)
	require.NoError(t, err)
	defer func() { _ = watcher.Stop() }()

	// Wait for fsnotify to be ready
	time.Sleep(100 * time.Millisecond)

	// Modify file - fsnotify should detect this quickly
	newContent := []byte(`openapi: "3.1.0"`)
	err = os.WriteFile(specPath, newContent, 0644)
	require.NoError(t, err)

	// Fsnotify should detect change faster than polling interval
	require.Eventually(t, func() bool {
		return eventReceived.Load()
	}, 500*time.Millisecond, 10*time.Millisecond, "expected change event from fsnotify")

	mu.Lock()
	assert.Equal(t, SpecChangeModified, receivedEvent.ChangeType)
	assert.Equal(t, newContent, receivedEvent.NewContent)
	mu.Unlock()
}

func TestLocalFileWatcher_FallsBackToPolling_WhenDirDeleted(t *testing.T) {
	// Create a nested directory structure
	tmpDir := t.TempDir()
	nestedDir := filepath.Join(tmpDir, "nested")
	err := os.MkdirAll(nestedDir, 0755)
	require.NoError(t, err)

	specPath := filepath.Join(nestedDir, "spec.yaml")
	err = os.WriteFile(specPath, []byte(`openapi: "3.0.0"`), 0644)
	require.NoError(t, err)

	watcher := NewLocalFileWatcher("test-app", specPath, WithPollInterval(100*time.Millisecond))

	ctx := t.Context()

	err = watcher.Start(ctx)
	require.NoError(t, err)
	defer func() { _ = watcher.Stop() }()

	// Wait for fsnotify mode
	time.Sleep(100 * time.Millisecond)
	assert.Equal(t, WatchModeInotify, watcher.GetWatchMode())

	// Delete the directory (this should cause fsnotify to fail)
	err = os.RemoveAll(nestedDir)
	require.NoError(t, err)

	// Wait for mode switch to polling
	require.Eventually(t, func() bool {
		return watcher.GetWatchMode() == WatchModePolling
	}, 1*time.Second, 50*time.Millisecond, "expected switch to polling mode")
}

func TestLocalFileWatcher_SwitchesBackToFsnotify_WhenFileRestored(t *testing.T) {
	// Create a nested directory structure
	tmpDir := t.TempDir()
	nestedDir := filepath.Join(tmpDir, "nested")
	err := os.MkdirAll(nestedDir, 0755)
	require.NoError(t, err)

	specPath := filepath.Join(nestedDir, "spec.yaml")
	err = os.WriteFile(specPath, []byte(`openapi: "3.0.0"`), 0644)
	require.NoError(t, err)

	watcher := NewLocalFileWatcher("test-app", specPath, WithPollInterval(100*time.Millisecond))

	ctx := t.Context()

	err = watcher.Start(ctx)
	require.NoError(t, err)
	defer func() { _ = watcher.Stop() }()

	// Wait for initial fsnotify mode
	time.Sleep(100 * time.Millisecond)
	assert.Equal(t, WatchModeInotify, watcher.GetWatchMode())

	// Delete the directory
	err = os.RemoveAll(nestedDir)
	require.NoError(t, err)

	// Wait for mode switch to polling
	require.Eventually(t, func() bool {
		return watcher.GetWatchMode() == WatchModePolling
	}, 1*time.Second, 50*time.Millisecond, "expected switch to polling mode")

	// Recreate directory and file
	err = os.MkdirAll(nestedDir, 0755)
	require.NoError(t, err)
	err = os.WriteFile(specPath, []byte(`openapi: "3.1.0"`), 0644)
	require.NoError(t, err)

	// Should switch back to fsnotify
	require.Eventually(t, func() bool {
		return watcher.GetWatchMode() == WatchModeInotify
	}, 1*time.Second, 50*time.Millisecond, "expected switch back to fsnotify mode")
}

func TestLocalFileWatcher_FsnotifyDetectsDelete(t *testing.T) {
	tmpDir := t.TempDir()
	specPath := filepath.Join(tmpDir, "spec.yaml")
	err := os.WriteFile(specPath, []byte(`openapi: "3.0.0"`), 0644)
	require.NoError(t, err)

	watcher := NewLocalFileWatcher("test-app", specPath, WithPollInterval(50*time.Millisecond))

	var eventReceived atomic.Bool
	var receivedEvent SpecChangeEvent
	var mu sync.Mutex

	watcher.AddHandler(func(event SpecChangeEvent) {
		mu.Lock()
		receivedEvent = event
		mu.Unlock()
		eventReceived.Store(true)
	})

	ctx := t.Context()

	err = watcher.Start(ctx)
	require.NoError(t, err)
	defer func() { _ = watcher.Stop() }()

	// Wait for fsnotify to be ready
	time.Sleep(100 * time.Millisecond)

	// Delete file
	err = os.Remove(specPath)
	require.NoError(t, err)

	// Fsnotify should detect deletion quickly
	require.Eventually(t, func() bool {
		return eventReceived.Load()
	}, 500*time.Millisecond, 10*time.Millisecond, "expected delete event from fsnotify")

	mu.Lock()
	assert.Equal(t, SpecChangeDeleted, receivedEvent.ChangeType)
	mu.Unlock()
}
