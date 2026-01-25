// Package config provides configuration management for OpenBridge.
// This file contains spec watching functionality for automatic reload support.
package config

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
)

// SpecChangeEvent represents a change event for a spec source.
type SpecChangeEvent struct {
	AppName    string         // Name of the application whose spec changed.
	SourceURL  string         // Source URL or path that changed.
	ChangeType SpecChangeType // Type of change.
	NewContent []byte         // New spec content (may be nil for delete events).
	Error      error          // Error if change detection or fetch failed.
	Timestamp  time.Time      // When the change was detected.
}

// SpecChangeType describes the type of spec change.
type SpecChangeType string

const (
	SpecChangeModified SpecChangeType = "modified" // Spec content was modified.
	SpecChangeDeleted  SpecChangeType = "deleted"  // Spec source was deleted.
	SpecChangeExpired  SpecChangeType = "expired"  // Remote spec cache expired.
	SpecChangeError    SpecChangeType = "error"    // Error during change detection.
)

// SpecChangeHandler is called when a spec change is detected.
type SpecChangeHandler func(event SpecChangeEvent)

// SpecWatcher watches spec sources for changes and notifies handlers.
type SpecWatcher interface {
	Start(ctx context.Context) error
	Stop() error
	CheckNow() (*SpecChangeEvent, error)
	AddHandler(handler SpecChangeHandler)
	GetSourceURL() string
	GetAppName() string
}

// watcherBase provides common watcher functionality.
type watcherBase struct {
	appName  string
	mu       sync.RWMutex
	running  bool
	stopCh   chan struct{}
	handlers []SpecChangeHandler
}

func (w *watcherBase) setRunning(running bool) {
	w.mu.Lock()
	w.running = running
	w.mu.Unlock()
}

func (w *watcherBase) addHandler(handler SpecChangeHandler) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.handlers = append(w.handlers, handler)
}

func (w *watcherBase) notifyHandlers(event SpecChangeEvent) {
	w.mu.RLock()
	handlers := make([]SpecChangeHandler, len(w.handlers))
	copy(handlers, w.handlers)
	w.mu.RUnlock()

	for _, h := range handlers {
		h(event)
	}
}

func (w *watcherBase) stopWatcher() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if !w.running {
		return nil
	}
	close(w.stopCh)
	return nil
}

// WatchMode indicates the current watching mode.
type WatchMode int

const (
	WatchModeInotify WatchMode = iota // Using fsnotify (preferred)
	WatchModePolling                  // Fallback to polling
)

// LocalFileWatcher watches a local file for changes using fsnotify.
// Falls back to polling when the file's directory is deleted.
type LocalFileWatcher struct {
	watcherBase
	filePath     string
	pollInterval time.Duration
	lastModTime  time.Time
	lastHash     string
	mode         WatchMode
	fsWatcher    *fsnotify.Watcher
}

// LocalFileWatcherOption configures a LocalFileWatcher.
type LocalFileWatcherOption func(*LocalFileWatcher)

// WithPollInterval sets the polling interval for fallback mode.
func WithPollInterval(interval time.Duration) LocalFileWatcherOption {
	return func(w *LocalFileWatcher) { w.pollInterval = interval }
}

// NewLocalFileWatcher creates a new watcher for a local file.
func NewLocalFileWatcher(appName, filePath string, opts ...LocalFileWatcherOption) *LocalFileWatcher {
	w := &LocalFileWatcher{
		watcherBase:  watcherBase{appName: appName, stopCh: make(chan struct{})},
		filePath:     filePath,
		pollInterval: 2 * time.Second, // Fallback poll interval
	}
	for _, opt := range opts {
		opt(w)
	}
	return w
}

// Start begins watching the file for changes.
func (w *LocalFileWatcher) Start(ctx context.Context) error {
	w.mu.Lock()
	if w.running {
		w.mu.Unlock()
		return errors.New("watcher already running")
	}
	w.running = true
	w.stopCh = make(chan struct{})
	w.mu.Unlock()

	if err := w.initBaseline(); err != nil {
		w.setRunning(false)
		return fmt.Errorf("failed to initialize baseline: %w", err)
	}

	go w.watchLoop(ctx)
	return nil
}

func (w *LocalFileWatcher) initBaseline() error {
	info, err := os.Stat(w.filePath)
	if err != nil {
		return err
	}
	content, err := os.ReadFile(w.filePath)
	if err != nil {
		return err
	}
	w.mu.Lock()
	w.lastModTime = info.ModTime()
	w.lastHash = computeHash(content)
	w.mu.Unlock()
	return nil
}

// watchLoop is the main watch loop that handles mode switching.
func (w *LocalFileWatcher) watchLoop(ctx context.Context) {
	defer w.setRunning(false)

	for {
		// Try to use fsnotify first
		if w.tryStartFsnotify() {
			w.runFsnotifyMode(ctx)
		} else {
			w.runPollingMode(ctx)
		}

		// Check if we should stop
		select {
		case <-ctx.Done():
			return
		case <-w.stopCh:
			return
		default:
			// Continue to next iteration (mode switch)
		}
	}
}

// createFsWatcher creates a new fsnotify watcher for the directory.
func createFsWatcher(dir string) (*fsnotify.Watcher, error) {
	fsWatcher, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}
	if err := fsWatcher.Add(dir); err != nil {
		_ = fsWatcher.Close()
		return nil, err
	}
	return fsWatcher, nil
}

// tryStartFsnotify attempts to start fsnotify watching.
// Returns false if fsnotify is not available (unsupported OS, network filesystem, etc.)
func (w *LocalFileWatcher) tryStartFsnotify() bool {
	dir := filepath.Dir(w.filePath)
	if _, err := os.Stat(dir); err != nil {
		return false
	}

	fsWatcher, err := createFsWatcher(dir)
	if err != nil {
		return false
	}

	w.mu.Lock()
	w.fsWatcher = fsWatcher
	w.mode = WatchModeInotify
	w.mu.Unlock()

	return true
}

// runFsnotifyMode runs the fsnotify-based watching loop.
func (w *LocalFileWatcher) runFsnotifyMode(ctx context.Context) {
	w.mu.RLock()
	fsWatcher := w.fsWatcher
	w.mu.RUnlock()

	defer func() {
		w.mu.Lock()
		if w.fsWatcher != nil {
			_ = w.fsWatcher.Close()
			w.fsWatcher = nil
		}
		w.mu.Unlock()
	}()

	targetName := filepath.Base(w.filePath)
	dir := filepath.Dir(w.filePath)

	for {
		select {
		case <-ctx.Done():
			return
		case <-w.stopCh:
			return

		case event, ok := <-fsWatcher.Events:
			if !ok {
				return // Watcher closed
			}
			if w.handleFsEvent(event, targetName, dir) {
				return // Need to switch to polling
			}

		case err, ok := <-fsWatcher.Errors:
			if !ok {
				return // Watcher closed
			}
			// On error, switch to polling mode
			w.notifyHandlers(*w.newEvent(SpecChangeError, nil, err))
			return
		}
	}
}

// handleFsEvent processes a single fsnotify event.
// Returns true if we need to switch to polling mode.
func (w *LocalFileWatcher) handleFsEvent(event fsnotify.Event, targetName, dir string) bool {
	// Check if parent directory was removed
	if event.Op&fsnotify.Remove != 0 && event.Name == dir {
		// Directory removed, switch to polling
		return true
	}

	// Only process events for our target file
	if filepath.Base(event.Name) != targetName {
		return false
	}

	switch {
	case event.Op&fsnotify.Remove != 0:
		w.notifyHandlers(*w.newEvent(SpecChangeDeleted, nil, nil))
		// After file deletion, check if directory still exists
		if _, err := os.Stat(dir); err != nil {
			return true // Directory gone, switch to polling
		}

	case event.Op&(fsnotify.Write|fsnotify.Create) != 0:
		w.handleFileModified()

	default:
		// Ignore other events (chmod, rename, etc.)
	}

	return false
}

// readFileAndHash reads file content and computes its hash.
func readFileAndHash(filePath string) ([]byte, string, error) {
	content, err := os.ReadFile(filePath)
	if err != nil {
		return nil, "", err
	}
	return content, computeHash(content), nil
}

// updateFileState updates the cached file state.
func (w *LocalFileWatcher) updateFileState(info os.FileInfo, newHash string) {
	w.mu.Lock()
	w.lastModTime = info.ModTime()
	w.lastHash = newHash
	w.mu.Unlock()
}

// handleFileModified checks if file content actually changed.
func (w *LocalFileWatcher) handleFileModified() {
	time.Sleep(50 * time.Millisecond) // Small delay to ensure write is complete

	info, err := os.Stat(w.filePath)
	if err != nil {
		if os.IsNotExist(err) {
			w.notifyHandlers(*w.newEvent(SpecChangeDeleted, nil, nil))
		}
		return
	}

	content, newHash, err := readFileAndHash(w.filePath)
	if err != nil {
		return
	}

	w.mu.Lock()
	oldHash := w.lastHash
	w.mu.Unlock()

	if newHash != oldHash {
		w.updateFileState(info, newHash)
		w.notifyHandlers(*w.newEvent(SpecChangeModified, content, nil))
	}
}

// runPollingMode runs the polling-based watching loop.
func (w *LocalFileWatcher) runPollingMode(ctx context.Context) {
	w.mu.Lock()
	w.mode = WatchModePolling
	w.mu.Unlock()

	ticker := time.NewTicker(w.pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-w.stopCh:
			return
		case <-ticker.C:
			event := w.pollCheck()
			if event != nil {
				w.notifyHandlers(*event)
			}

			// Try to switch back to fsnotify if file is available
			if w.canSwitchToFsnotify() {
				return // Exit polling mode to try fsnotify
			}
		}
	}
}

// shouldCheckContent checks if we should read and compare file content.
func shouldCheckContent(info os.FileInfo, lastModTime time.Time) bool {
	return info.ModTime().After(lastModTime)
}

// pollCheck performs a single polling check for changes.
func (w *LocalFileWatcher) pollCheck() *SpecChangeEvent {
	info, err := os.Stat(w.filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return w.newEvent(SpecChangeDeleted, nil, nil)
		}
		return w.newEvent(SpecChangeError, nil, err)
	}

	w.mu.RLock()
	lastModTime, lastHash := w.lastModTime, w.lastHash
	w.mu.RUnlock()

	if !shouldCheckContent(info, lastModTime) {
		return nil
	}

	content, newHash, err := readFileAndHash(w.filePath)
	if err != nil {
		return w.newEvent(SpecChangeError, nil, err)
	}

	w.updateFileState(info, newHash)

	if newHash != lastHash {
		return w.newEvent(SpecChangeModified, content, nil)
	}
	return nil
}

// canSwitchToFsnotify checks if we can switch back to fsnotify mode.
func (w *LocalFileWatcher) canSwitchToFsnotify() bool {
	dir := filepath.Dir(w.filePath)
	if _, err := os.Stat(dir); err != nil {
		return false
	}
	if _, err := os.Stat(w.filePath); err != nil {
		return false
	}
	return true
}

func (w *LocalFileWatcher) newEvent(t SpecChangeType, content []byte, err error) *SpecChangeEvent {
	return &SpecChangeEvent{
		AppName: w.appName, SourceURL: w.filePath, ChangeType: t,
		NewContent: content, Error: err, Timestamp: time.Now(),
	}
}

// Stop stops watching for changes.
func (w *LocalFileWatcher) Stop() error {
	w.mu.Lock()
	if w.fsWatcher != nil {
		_ = w.fsWatcher.Close()
		w.fsWatcher = nil
	}
	w.mu.Unlock()
	return w.stopWatcher()
}

// CheckNow performs an immediate check for changes.
func (w *LocalFileWatcher) CheckNow() (*SpecChangeEvent, error) {
	w.mu.RLock()
	needsInit := w.lastHash == ""
	w.mu.RUnlock()

	if needsInit {
		if err := w.initBaseline(); err != nil {
			return w.newEvent(SpecChangeError, nil, err), err
		}
		return nil, nil
	}
	return w.checkForChanges()
}

func (w *LocalFileWatcher) checkForChanges() (*SpecChangeEvent, error) {
	info, err := os.Stat(w.filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return w.newEvent(SpecChangeDeleted, nil, nil), nil
		}
		return w.newEvent(SpecChangeError, nil, err), err
	}

	w.mu.RLock()
	lastModTime, lastHash := w.lastModTime, w.lastHash
	w.mu.RUnlock()

	if !shouldCheckContent(info, lastModTime) {
		return nil, nil
	}

	content, newHash, err := readFileAndHash(w.filePath)
	if err != nil {
		return w.newEvent(SpecChangeError, nil, err), err
	}

	if newHash == lastHash {
		w.updateFileState(info, newHash)
		return nil, nil
	}

	w.updateFileState(info, newHash)
	return w.newEvent(SpecChangeModified, content, nil), nil
}

// AddHandler registers a handler for change events.
func (w *LocalFileWatcher) AddHandler(handler SpecChangeHandler) { w.addHandler(handler) }

// GetSourceURL returns the source URL being watched.
func (w *LocalFileWatcher) GetSourceURL() string { return w.filePath }

// GetAppName returns the app name associated with this watcher.
func (w *LocalFileWatcher) GetAppName() string { return w.appName }

// GetWatchMode returns the current watch mode (for testing).
func (w *LocalFileWatcher) GetWatchMode() WatchMode {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.mode
}

// RemoteSpecWatcher watches a remote spec for expiration and changes.
type RemoteSpecWatcher struct {
	watcherBase
	sourceURL     string
	cacheManager  *SpecCacheManager
	checkInterval time.Duration
	refreshBefore time.Duration
}

// RemoteSpecWatcherOption configures a RemoteSpecWatcher.
type RemoteSpecWatcherOption func(*RemoteSpecWatcher)

// WithCheckInterval sets the interval between expiration checks.
func WithCheckInterval(interval time.Duration) RemoteSpecWatcherOption {
	return func(w *RemoteSpecWatcher) { w.checkInterval = interval }
}

// WithRefreshBefore sets how long before expiration to refresh.
func WithRefreshBefore(duration time.Duration) RemoteSpecWatcherOption {
	return func(w *RemoteSpecWatcher) { w.refreshBefore = duration }
}

// NewRemoteSpecWatcher creates a new watcher for a remote spec.
func NewRemoteSpecWatcher(
	appName, sourceURL string,
	cacheManager *SpecCacheManager,
	opts ...RemoteSpecWatcherOption,
) *RemoteSpecWatcher {
	w := &RemoteSpecWatcher{
		watcherBase:   watcherBase{appName: appName, stopCh: make(chan struct{})},
		sourceURL:     sourceURL,
		cacheManager:  cacheManager,
		checkInterval: 1 * time.Minute,
		refreshBefore: 5 * time.Minute,
	}
	for _, opt := range opts {
		opt(w)
	}
	return w
}

// Start begins watching for cache expiration.
func (w *RemoteSpecWatcher) Start(ctx context.Context) error {
	w.mu.Lock()
	if w.running {
		w.mu.Unlock()
		return errors.New("watcher already running")
	}
	w.running = true
	w.stopCh = make(chan struct{})
	w.mu.Unlock()

	go w.watchLoop(ctx)
	return nil
}

func (w *RemoteSpecWatcher) watchLoop(ctx context.Context) {
	defer w.setRunning(false)

	ticker := time.NewTicker(w.checkInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-w.stopCh:
			return
		case <-ticker.C:
			if event, _ := w.checkAndRefresh(); event != nil {
				w.notifyHandlers(*event)
			}
		}
	}
}

func (w *RemoteSpecWatcher) checkAndRefresh() (*SpecChangeEvent, error) {
	meta, err := w.cacheManager.LoadMeta(w.appName)
	if err != nil {
		return w.refreshSpec()
	}
	if time.Until(meta.ExpiresAt) > w.refreshBefore {
		return nil, nil
	}
	return w.refreshSpec()
}

func (w *RemoteSpecWatcher) refreshSpec() (*SpecChangeEvent, error) {
	oldHash := ""
	if meta, err := w.cacheManager.LoadMeta(w.appName); err == nil {
		oldHash = meta.ContentHash
	}

	result, err := w.cacheManager.FetchWithCache(w.appName, w.sourceURL)
	if err != nil {
		return w.newEvent(SpecChangeError, nil, err), err
	}

	newHash := computeHash(result.Content)
	if newHash == oldHash {
		return nil, nil
	}

	changeType := SpecChangeExpired
	if oldHash == "" {
		changeType = SpecChangeModified
	}
	return w.newEvent(changeType, result.Content, nil), nil
}

func (w *RemoteSpecWatcher) newEvent(t SpecChangeType, content []byte, err error) *SpecChangeEvent {
	return &SpecChangeEvent{
		AppName: w.appName, SourceURL: w.sourceURL, ChangeType: t,
		NewContent: content, Error: err, Timestamp: time.Now(),
	}
}

// Stop stops watching for changes.
func (w *RemoteSpecWatcher) Stop() error { return w.stopWatcher() }

// CheckNow performs an immediate check and refresh if needed.
func (w *RemoteSpecWatcher) CheckNow() (*SpecChangeEvent, error) { return w.checkAndRefresh() }

// AddHandler registers a handler for change events.
func (w *RemoteSpecWatcher) AddHandler(handler SpecChangeHandler) { w.addHandler(handler) }

// GetSourceURL returns the source URL being watched.
func (w *RemoteSpecWatcher) GetSourceURL() string { return w.sourceURL }

// GetAppName returns the app name associated with this watcher.
func (w *RemoteSpecWatcher) GetAppName() string { return w.appName }

// SpecWatcherManager manages multiple spec watchers.
type SpecWatcherManager struct {
	watchers     map[string]SpecWatcher
	handlers     []SpecChangeHandler
	mu           sync.RWMutex
	cacheManager *SpecCacheManager
	ctx          context.Context
	cancel       context.CancelFunc
}

// NewSpecWatcherManager creates a new watcher manager.
func NewSpecWatcherManager(cacheManager *SpecCacheManager) *SpecWatcherManager {
	return &SpecWatcherManager{
		watchers:     make(map[string]SpecWatcher),
		cacheManager: cacheManager,
	}
}

// AddHandler registers a global handler for all change events.
func (m *SpecWatcherManager) AddHandler(handler SpecChangeHandler) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.handlers = append(m.handlers, handler)
}

// WatchApp starts watching a spec source for an app.
func (m *SpecWatcherManager) WatchApp(appName, sourceURL string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if existing, ok := m.watchers[appName]; ok {
		_ = existing.Stop()
	}

	var watcher SpecWatcher
	if isWebURL(sourceURL) {
		watcher = NewRemoteSpecWatcher(appName, sourceURL, m.cacheManager)
	} else {
		watcher = NewLocalFileWatcher(appName, sourceURL)
	}

	for _, h := range m.handlers {
		watcher.AddHandler(h)
	}
	m.watchers[appName] = watcher

	if m.ctx != nil {
		return watcher.Start(m.ctx)
	}
	return nil
}

// UnwatchApp stops watching an app's spec source.
func (m *SpecWatcherManager) UnwatchApp(appName string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if watcher, ok := m.watchers[appName]; ok {
		delete(m.watchers, appName)
		return watcher.Stop()
	}
	return nil
}

// Start starts all registered watchers.
func (m *SpecWatcherManager) Start() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.ctx != nil {
		return errors.New("manager already running")
	}

	m.ctx, m.cancel = context.WithCancel(context.Background())

	var errs []error
	for _, watcher := range m.watchers {
		if err := watcher.Start(m.ctx); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// Stop stops all registered watchers.
func (m *SpecWatcherManager) Stop() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.cancel != nil {
		m.cancel()
		m.ctx = nil
		m.cancel = nil
	}

	var errs []error
	for _, watcher := range m.watchers {
		if err := watcher.Stop(); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// CheckAllNow performs an immediate check for changes on all watched apps.
func (m *SpecWatcherManager) CheckAllNow() map[string]*SpecChangeEvent {
	m.mu.RLock()
	watchers := make(map[string]SpecWatcher, len(m.watchers))
	maps.Copy(watchers, m.watchers)
	m.mu.RUnlock()

	results := make(map[string]*SpecChangeEvent)
	for appName, watcher := range watchers {
		event, _ := watcher.CheckNow()
		results[appName] = event
	}
	return results
}

// CheckAppNow performs an immediate check for a specific app.
func (m *SpecWatcherManager) CheckAppNow(appName string) (*SpecChangeEvent, error) {
	m.mu.RLock()
	watcher, ok := m.watchers[appName]
	m.mu.RUnlock()

	if !ok {
		return nil, fmt.Errorf("no watcher registered for app: %s", appName)
	}
	return watcher.CheckNow()
}

// GetWatchedApps returns a list of currently watched app names.
func (m *SpecWatcherManager) GetWatchedApps() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	apps := make([]string, 0, len(m.watchers))
	for appName := range m.watchers {
		apps = append(apps, appName)
	}
	return apps
}
