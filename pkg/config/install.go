// Package config provides configuration management for OpenBridge.
// This file contains app installation and management functionality.
package config

import (
	"bufio"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"time"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/nomagicln/open-bridge/pkg/spec"
)

// DefaultProgressiveThreshold is the default number of operations above which
// progressive disclosure is recommended.
const DefaultProgressiveThreshold = 25

// InstallOptions contains options for app installation.
type InstallOptions struct {
	// SpecSource is the path or URL to the OpenAPI specification.
	SpecSource string

	// SpecSources allows multiple spec sources for merged APIs.
	SpecSources []string

	// BaseURL is the default API base URL.
	BaseURL string

	// Description is an optional description of the application.
	Description string

	// AuthType is the authentication type.
	AuthType string

	// CreateShim indicates whether to create a shim script.
	CreateShim bool

	// ShimDir is the directory for shim scripts (defaults to user's bin directory).
	ShimDir string

	// Interactive enables interactive prompts for missing information.
	Interactive bool

	// Reader is the input source for interactive prompts (defaults to os.Stdin).
	Reader io.Reader

	// Writer is the output destination for prompts (defaults to os.Stdout).
	Writer io.Writer

	// Force overwrites existing app configuration.
	Force bool

	// Headers to include in the default profile.
	Headers map[string]string

	// AuthParams contains authentication credentials (token, username, password).
	// These are returned by the interactive wizard but NOT saved to the config file.
	// The caller is responsible for saving them to the keyring.
	AuthParams map[string]string

	// TLS Configuration
	// TLSSkipVerify disables TLS certificate verification.
	TLSSkipVerify bool

	// TLSCACert is the path to a custom CA certificate file.
	TLSCACert string

	// TLSClientCert is the path to a client certificate file.
	TLSClientCert string

	// TLSClientKey is the path to a client private key file.
	TLSClientKey string

	// MCP Configuration
	// ProgressiveDisclosure enables progressive tool disclosure mode.
	ProgressiveDisclosure *bool

	// SearchEngine specifies the search engine type for progressive disclosure.
	// Valid values: "predicate" (default), "sql", "vector", "hybrid".
	SearchEngine string

	// ReadOnlyMode restricts the AI to read-only operations (GET only).
	ReadOnlyMode bool

	// ProgressiveThreshold is the number of operations above which
	// progressive disclosure is automatically recommended.
	// Defaults to DefaultProgressiveThreshold (25).
	ProgressiveThreshold int

	// OperationCount is populated after loading the spec, used for
	// determining progressive disclosure recommendation.
	OperationCount int

	// ProtectSensitiveInfo controls whether sensitive information should be masked
	// when generating code. When true, credentials are replaced with placeholders
	// like <YOUR_API_KEY>. Default is false (not protected).
	ProtectSensitiveInfo bool
}

// InstallResult contains the result of an app installation.
type InstallResult struct {
	AppName    string
	ConfigPath string
	ShimPath   string
	SpecInfo   *spec.SpecInfo
}

// mergeExistingAppConfig merges existing app config into install options if options are empty.
func mergeExistingAppConfig(opts InstallOptions, existing *AppConfig) InstallOptions {
	if opts.SpecSource == "" && len(opts.SpecSources) == 0 {
		opts.SpecSource = existing.SpecSource
		opts.SpecSources = existing.SpecSources
	}
	if opts.Description == "" {
		opts.Description = existing.Description
	}
	if profile, ok := existing.Profiles[existing.DefaultProfile]; ok {
		if opts.BaseURL == "" {
			opts.BaseURL = profile.BaseURL
		}
		if opts.AuthType == "" {
			opts.AuthType = profile.Auth.Type
		}
		// Merge TLS configuration
		if opts.TLSCACert == "" {
			opts.TLSCACert = profile.TLSConfig.CAFile
		}
		if opts.TLSClientCert == "" {
			opts.TLSClientCert = profile.TLSConfig.CertFile
		}
		if opts.TLSClientKey == "" {
			opts.TLSClientKey = profile.TLSConfig.KeyFile
		}
		if !opts.TLSSkipVerify {
			opts.TLSSkipVerify = profile.TLSConfig.InsecureSkipVerify
		}
		// Merge MCP configuration
		if opts.ProgressiveDisclosure == nil && profile.SafetyConfig.ProgressiveDisclosure {
			progressive := true
			opts.ProgressiveDisclosure = &progressive
		}
		if opts.SearchEngine == "" {
			opts.SearchEngine = profile.SafetyConfig.SearchEngine
		}
		if !opts.ReadOnlyMode {
			opts.ReadOnlyMode = profile.SafetyConfig.ReadOnlyMode
		}
	}
	return opts
}

// prepareSpecSources normalizes spec sources and returns the primary source.
func prepareSpecSources(specSource string, specSources []string) (string, []string, string, error) {
	var err error
	if specSource != "" {
		specSource, err = normalizeSpecSource(specSource)
		if err != nil {
			return "", nil, "", err
		}
	}
	if len(specSources) > 0 {
		specSources, err = normalizeSpecSources(specSources)
		if err != nil {
			return "", nil, "", err
		}
	}
	primarySource := specSource
	if primarySource == "" && len(specSources) > 0 {
		primarySource = specSources[0]
	}
	return specSource, specSources, primarySource, nil
}

// resolveBaseURL determines the base URL from options, spec, or prompts.
func resolveBaseURL(opts InstallOptions, specDoc *openapi3.T) (string, error) {
	baseURL := opts.BaseURL
	if baseURL == "" && len(specDoc.Servers) > 0 {
		baseURL = specDoc.Servers[0].URL
	}

	if baseURL == "" && opts.Interactive {
		_, _ = fmt.Fprintln(opts.Writer, "No server URL found in spec.")
		var err error
		baseURL, err = promptString(opts.Reader, opts.Writer, "Base URL", "http://localhost:8080")
		if err != nil {
			return "", err
		}
	}

	if baseURL == "" {
		return "", fmt.Errorf("base URL is required (use --base-url or add servers to spec)")
	}
	return baseURL, nil
}

// buildTLSConfig creates the TLS configuration from install options.
func buildTLSConfig(opts InstallOptions) TLSConfig {
	return TLSConfig{
		InsecureSkipVerify: opts.TLSSkipVerify,
		CAFile:             opts.TLSCACert,
		CertFile:           opts.TLSClientCert,
		KeyFile:            opts.TLSClientKey,
	}
}

// buildSafetyConfig creates the safety configuration for MCP from install options.
func buildSafetyConfig(opts InstallOptions) SafetyConfig {
	safetyConfig := SafetyConfig{
		ReadOnlyMode: opts.ReadOnlyMode,
		SearchEngine: opts.SearchEngine,
	}

	// Set progressive disclosure based on explicit setting or threshold
	if opts.ProgressiveDisclosure != nil {
		safetyConfig.ProgressiveDisclosure = *opts.ProgressiveDisclosure
	} else if opts.OperationCount > 0 {
		threshold := opts.ProgressiveThreshold
		if threshold <= 0 {
			threshold = DefaultProgressiveThreshold
		}
		safetyConfig.ProgressiveDisclosure = opts.OperationCount > threshold
	}

	// Default search engine to predicate if progressive disclosure is enabled
	if safetyConfig.ProgressiveDisclosure && safetyConfig.SearchEngine == "" {
		safetyConfig.SearchEngine = "predicate"
	}

	return safetyConfig
}

// createAppConfig creates the app configuration structure.
func createAppConfig(appName, specSource string, specSources []string, opts InstallOptions, baseURL string, specInfo *spec.SpecInfo) *AppConfig {
	now := time.Now()
	tlsConfig := buildTLSConfig(opts)
	safetyConfig := buildSafetyConfig(opts)

	config := &AppConfig{
		Name:           appName,
		SpecSource:     specSource,
		SpecSources:    specSources,
		Description:    opts.Description,
		DefaultProfile: "default",
		Version:        "1.0",
		CreatedAt:      now,
		UpdatedAt:      now,
		Profiles: map[string]Profile{
			"default": {
				Name:         "default",
				BaseURL:      baseURL,
				Auth:         AuthConfig{Type: opts.AuthType},
				Headers:      opts.Headers,
				TLSConfig:    tlsConfig,
				SafetyConfig: safetyConfig,
				Timeout:      Duration{Duration: 30 * time.Second},
				IsDefault:    true,
			},
		},
	}

	if config.Description == "" && specInfo != nil {
		config.Description = specInfo.Title
	}
	return config
}

// createShimIfRequested creates a shim for the app if requested.
func (m *Manager) createShimIfRequested(opts InstallOptions, appName string, result *InstallResult) {
	if !opts.CreateShim {
		return
	}

	shimPath, err := m.CreateShim(appName, opts.ShimDir)
	if err != nil {
		_, _ = fmt.Fprintf(opts.Writer, "Warning: failed to create shim: %v\n", err)
		return
	}

	result.ShimPath = shimPath

	shimDir := filepath.Dir(shimPath)
	if !IsShimDirInPath(shimDir) {
		_, _ = fmt.Fprintf(opts.Writer, "\nNote: The shim directory is not in your PATH.\n")
		_, _ = fmt.Fprintf(opts.Writer, "%s\n", GetPathInstructions(shimDir))
	}
}

// handleExistingApp checks if app exists and handles force option.
func (m *Manager) handleExistingApp(appName string, opts InstallOptions) (InstallOptions, error) {
	if !m.AppExists(appName) {
		return opts, nil
	}
	if !opts.Force {
		return opts, fmt.Errorf("app '%s' already exists (use --force to overwrite)", appName)
	}
	if existingConfig, err := m.GetAppConfig(appName); err == nil {
		opts = mergeExistingAppConfig(opts, existingConfig)
	}
	return opts, nil
}

// setDefaultIO sets default reader/writer if not provided.
func setDefaultIO(opts InstallOptions) InstallOptions {
	if opts.Reader == nil {
		opts.Reader = os.Stdin
	}
	if opts.Writer == nil {
		opts.Writer = os.Stdout
	}
	return opts
}

// InstallApp installs an API as a CLI application.
func (m *Manager) InstallApp(appName string, opts InstallOptions) (*InstallResult, error) {
	if err := validateAppName(appName); err != nil {
		return nil, err
	}

	opts, err := m.handleExistingApp(appName, opts)
	if err != nil {
		return nil, err
	}

	opts = setDefaultIO(opts)

	if opts.Interactive {
		opts, err = m.promptForMissingInfo(opts)
		if err != nil {
			return nil, err
		}
	}

	if opts.SpecSource == "" && len(opts.SpecSources) == 0 {
		return nil, fmt.Errorf("spec source is required")
	}

	return m.doInstall(appName, opts)
}

// doInstall performs the actual installation after validation.
func (m *Manager) doInstall(appName string, opts InstallOptions) (*InstallResult, error) {
	specSource, specSources, primarySource, err := prepareSpecSources(opts.SpecSource, opts.SpecSources)
	if err != nil {
		return nil, err
	}

	parser := spec.NewParser()
	specDoc, err := parser.LoadSpec(primarySource)
	if err != nil {
		return nil, fmt.Errorf("failed to load spec: %w", err)
	}

	baseURL, err := resolveBaseURL(opts, specDoc)
	if err != nil {
		return nil, err
	}

	specInfo := spec.GetSpecInfo(specDoc, primarySource)

	// Set operation count for progressive disclosure decision
	if specInfo != nil {
		opts.OperationCount = specInfo.Operations
	}

	config := createAppConfig(appName, specSource, specSources, opts, baseURL, specInfo)

	// Store operation count in config for future reference
	if specInfo != nil {
		config.OperationCount = specInfo.Operations
	}

	if err := m.SaveAppConfig(config); err != nil {
		return nil, fmt.Errorf("failed to save config: %w", err)
	}

	// Cache the spec if it's from a remote URL
	if isWebURL(primarySource) {
		cacheManager := NewSpecCacheManager(m.AppsDir())
		_, cacheErr := cacheManager.FetchWithCache(appName, primarySource)
		if cacheErr != nil {
			// Warning but don't fail installation
			fmt.Fprintf(os.Stderr, "Warning: failed to cache spec: %v\n", cacheErr)
		}
	}

	result := &InstallResult{
		AppName:    appName,
		ConfigPath: m.getAppConfigPath(appName),
		SpecInfo:   specInfo,
	}

	m.createShimIfRequested(opts, appName, result)
	return result, nil
}

// normalizeSpecSource resolves local spec paths to absolute paths and preserves HTTP URLs.
// It does not validate that the file exists; loading the spec fails later if the path is invalid.
func normalizeSpecSource(source string) (string, error) {
	if source == "" {
		return "", nil
	}
	if isWebURL(source) {
		return source, nil
	}
	absPath, err := filepath.Abs(source)
	if err != nil {
		return "", fmt.Errorf("failed to resolve spec path: %w", err)
	}
	return absPath, nil
}

// normalizeSpecSources resolves local spec paths to absolute paths and preserves HTTP URLs.
func normalizeSpecSources(sources []string) ([]string, error) {
	normalized := make([]string, 0, len(sources))
	for _, source := range sources {
		value, err := normalizeSpecSource(source)
		if err != nil {
			return nil, err
		}
		normalized = append(normalized, value)
	}
	return normalized, nil
}

// isWebURL checks for HTTP/HTTPS URLs only.
func isWebURL(source string) bool {
	parsed, err := url.Parse(source)
	if err != nil {
		return false
	}
	return parsed.Scheme == "http" || parsed.Scheme == "https"
}

// promptAndNormalizeSpec prompts for spec source and normalizes paths.
func promptAndNormalizeSpec(opts InstallOptions) (InstallOptions, error) {
	reader, writer := opts.Reader, opts.Writer

	if opts.SpecSource == "" && len(opts.SpecSources) == 0 {
		source, err := promptString(reader, writer, "OpenAPI spec path or URL", "")
		if err != nil {
			return opts, err
		}
		if source == "" {
			return opts, fmt.Errorf("spec source is required")
		}
		opts.SpecSource = source
	}

	if opts.SpecSource != "" {
		source, err := normalizeSpecSource(opts.SpecSource)
		if err != nil {
			return opts, err
		}
		opts.SpecSource = source
	}

	if len(opts.SpecSources) > 0 {
		sources, err := normalizeSpecSources(opts.SpecSources)
		if err != nil {
			return opts, err
		}
		opts.SpecSources = sources
	}

	return opts, nil
}

// promptForMissingInfo prompts the user for missing installation information.
func (m *Manager) promptForMissingInfo(opts InstallOptions) (InstallOptions, error) {
	reader, writer := opts.Reader, opts.Writer

	opts, err := promptAndNormalizeSpec(opts)
	if err != nil {
		return opts, err
	}

	if opts.Description == "" {
		desc, err := promptString(reader, writer, "Description (optional)", "")
		if err != nil {
			return opts, err
		}
		opts.Description = desc
	}

	if opts.AuthType == "" {
		authType, err := promptChoice(reader, writer, "Authentication type",
			[]string{"none", "bearer", "api_key", "basic"}, "none")
		if err != nil {
			return opts, err
		}
		opts.AuthType = authType
	}

	createShim, err := promptYesNo(reader, writer, "Create command shortcut (shim)?", true)
	if err != nil {
		return opts, err
	}
	opts.CreateShim = createShim

	return opts, nil
}

// UninstallApp removes an installed application and cleans up resources.
func (m *Manager) UninstallApp(appName string, removeShim bool) error {
	// Validate app name
	if err := validateAppName(appName); err != nil {
		return err
	}

	// Check if app exists
	if !m.AppExists(appName) {
		return &AppNotFoundError{AppName: appName}
	}

	// Remove shim if requested
	if removeShim {
		if err := m.RemoveShim(appName); err != nil {
			// Don't fail uninstall, just warn
			fmt.Fprintf(os.Stderr, "Warning: failed to remove shim: %v\n", err)
		}
	}

	// Delete app configuration
	if err := m.DeleteAppConfig(appName); err != nil {
		return fmt.Errorf("failed to delete config: %w", err)
	}

	return nil
}

// CreateShim creates a shim script for the app in the specified directory.
func (m *Manager) CreateShim(appName, shimDir string) (string, error) {
	if shimDir == "" {
		var err error
		shimDir, err = getDefaultShimDir()
		if err != nil {
			return "", fmt.Errorf("failed to get shim directory: %w", err)
		}
	}

	// Ensure shim directory exists
	if err := os.MkdirAll(shimDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create shim directory: %w", err)
	}

	// Find the ob binary
	obPath, err := getOBBinaryPath()
	if err != nil {
		return "", fmt.Errorf("failed to find ob binary: %w", err)
	}

	var shimPath string
	var shimContent string

	switch runtime.GOOS {
	case "windows":
		// Create batch file on Windows
		shimPath = filepath.Join(shimDir, appName+".cmd")
		shimContent = fmt.Sprintf(`@echo off
"%s" run %s %%*
`, obPath, appName)

	default:
		// Create symlink on Unix-like systems
		// This makes the binary name be the app name, which triggers shim mode in main.go
		shimPath = filepath.Join(shimDir, appName)

		// Remove existing file/symlink if present
		_ = os.Remove(shimPath)

		// Create symlink to ob binary
		if err := os.Symlink(obPath, shimPath); err != nil {
			return "", fmt.Errorf("failed to create symlink: %w", err)
		}
		return shimPath, nil
	}

	// Write shim file (Windows only now)
	if err := os.WriteFile(shimPath, []byte(shimContent), 0755); err != nil {
		return "", fmt.Errorf("failed to write shim: %w", err)
	}

	return shimPath, nil
}

// RemoveShim removes the shim script for an app.
func (m *Manager) RemoveShim(appName string) error {
	shimDir, err := getDefaultShimDir()
	if err != nil {
		return err
	}

	var shimPath string
	switch runtime.GOOS {
	case "windows":
		shimPath = filepath.Join(shimDir, appName+".cmd")
	default:
		shimPath = filepath.Join(shimDir, appName)
	}

	if err := os.Remove(shimPath); err != nil {
		if os.IsNotExist(err) {
			return nil // Already removed
		}
		return err
	}

	return nil
}

// ShimExists checks if a shim exists for the app.
func (m *Manager) ShimExists(appName string) bool {
	shimDir, err := getDefaultShimDir()
	if err != nil {
		return false
	}

	var shimPath string
	switch runtime.GOOS {
	case "windows":
		shimPath = filepath.Join(shimDir, appName+".cmd")
	default:
		shimPath = filepath.Join(shimDir, appName)
	}

	_, err = os.Stat(shimPath)
	return err == nil
}

// getDefaultShimDir returns the default directory for shim scripts.
func getDefaultShimDir() (string, error) {
	if runtime.GOOS == "windows" {
		// Use user's local bin directory
		localAppData := os.Getenv("LOCALAPPDATA")
		if localAppData == "" {
			homeDir, err := os.UserHomeDir()
			if err != nil {
				return "", err
			}
			localAppData = filepath.Join(homeDir, "AppData", "Local")
		}
		return filepath.Join(localAppData, "OpenBridge", "bin"), nil
	}

	// macOS/Linux/Unix: use ~/.local/bin (standard user bin directory)
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(homeDir, ".local", "bin"), nil
}

// IsShimDirInPath checks if the shim directory is in the user's PATH.
func IsShimDirInPath(shimDir string) bool {
	pathEnv := os.Getenv("PATH")
	if pathEnv == "" {
		return false
	}

	var pathSep string
	if runtime.GOOS == "windows" {
		pathSep = ";"
	} else {
		pathSep = ":"
	}

	paths := strings.SplitSeq(pathEnv, pathSep)
	for p := range paths {
		// Resolve to absolute path for comparison
		absPath, err := filepath.Abs(p)
		if err != nil {
			continue
		}
		absShimDir, err := filepath.Abs(shimDir)
		if err != nil {
			continue
		}
		if absPath == absShimDir {
			return true
		}
	}

	return false
}

// GetPathInstructions returns platform-specific instructions for adding the shim directory to PATH.
func GetPathInstructions(shimDir string) string {
	switch runtime.GOOS {
	case "windows":
		return fmt.Sprintf(`To use the installed app command, add the following directory to your PATH:
  %s

Instructions:
  1. Open System Properties > Environment Variables
  2. Edit the PATH variable for your user
  3. Add: %s
  4. Restart your terminal

Or run this PowerShell command:
  [Environment]::SetEnvironmentVariable("Path", $env:Path + ";%s", "User")`, shimDir, shimDir, shimDir)

	case "darwin":
		return fmt.Sprintf(`To use the installed app command, add the following directory to your PATH:
  %s

Add this line to your shell profile (~/.zshrc or ~/.bash_profile):
  export PATH="$PATH:%s"

Then reload your shell:
  source ~/.zshrc  # or source ~/.bash_profile`, shimDir, shimDir)

	default:
		return fmt.Sprintf(`To use the installed app command, add the following directory to your PATH:
  %s

Add this line to your shell profile (~/.bashrc or ~/.profile):
  export PATH="$PATH:%s"

Then reload your shell:
  source ~/.bashrc  # or source ~/.profile`, shimDir, shimDir)
	}
}

// getOBBinaryPath finds the path to the ob binary.
func getOBBinaryPath() (string, error) {
	// Try to find ob in PATH
	path, err := exec.LookPath("ob")
	if err == nil {
		return filepath.Abs(path)
	}

	// Try current executable
	executable, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("cannot find ob binary: %w", err)
	}

	return filepath.Abs(executable)
}

// Helper functions for interactive prompts

func promptString(reader io.Reader, writer io.Writer, prompt, defaultVal string) (string, error) {
	if defaultVal != "" {
		_, _ = fmt.Fprintf(writer, "%s [%s]: ", prompt, defaultVal)
	} else {
		_, _ = fmt.Fprintf(writer, "%s: ", prompt)
	}

	scanner := bufio.NewScanner(reader)
	if scanner.Scan() {
		input := strings.TrimSpace(scanner.Text())
		if input == "" {
			return defaultVal, nil
		}
		return input, nil
	}

	if err := scanner.Err(); err != nil {
		return "", err
	}

	return defaultVal, nil
}

func promptYesNo(reader io.Reader, writer io.Writer, prompt string, defaultVal bool) (bool, error) {
	defaultStr := "y/N"
	if defaultVal {
		defaultStr = "Y/n"
	}

	_, _ = fmt.Fprintf(writer, "%s [%s]: ", prompt, defaultStr)

	scanner := bufio.NewScanner(reader)
	if scanner.Scan() {
		input := strings.TrimSpace(strings.ToLower(scanner.Text()))
		if input == "" {
			return defaultVal, nil
		}
		return input == "y" || input == "yes", nil
	}

	if err := scanner.Err(); err != nil {
		return false, err
	}

	return defaultVal, nil
}

func promptChoice(reader io.Reader, writer io.Writer, prompt string, choices []string, defaultVal string) (string, error) {
	_, _ = fmt.Fprintf(writer, "%s (%s)", prompt, strings.Join(choices, "/"))
	if defaultVal != "" {
		_, _ = fmt.Fprintf(writer, " [%s]", defaultVal)
	}
	_, _ = fmt.Fprint(writer, ": ")

	scanner := bufio.NewScanner(reader)
	if scanner.Scan() {
		input := strings.TrimSpace(strings.ToLower(scanner.Text()))
		if input == "" {
			return defaultVal, nil
		}

		// Validate choice
		if slices.Contains(choices, input) {
			return input, nil
		}

		return "", fmt.Errorf("invalid choice: %s (must be one of: %s)", input, strings.Join(choices, ", "))
	}

	if err := scanner.Err(); err != nil {
		return "", err
	}

	return defaultVal, nil
}

// UpdateApp updates an existing app's spec or configuration.
func (m *Manager) UpdateApp(appName string, opts InstallOptions) error {
	config, err := m.GetAppConfig(appName)
	if err != nil {
		return err
	}

	// Update spec source if provided
	if opts.SpecSource != "" {
		normalized, err := normalizeSpecSource(opts.SpecSource)
		if err != nil {
			return err
		}
		config.SpecSource = normalized
	}
	if len(opts.SpecSources) > 0 {
		normalized, err := normalizeSpecSources(opts.SpecSources)
		if err != nil {
			return err
		}
		config.SpecSources = normalized
	}

	// Update description if provided
	if opts.Description != "" {
		config.Description = opts.Description
	}

	// Reload spec to validate
	if opts.SpecSource != "" || len(opts.SpecSources) > 0 {
		parser := spec.NewParser()
		specSource := config.SpecSource
		if specSource == "" && len(config.SpecSources) > 0 {
			specSource = config.SpecSources[0]
		}

		_, err := parser.LoadSpec(specSource)
		if err != nil {
			return fmt.Errorf("failed to load updated spec: %w", err)
		}
	}

	config.UpdatedAt = time.Now()

	return m.SaveAppConfig(config)
}

// GetInstalledAppInfo returns detailed information about an installed app.
func (m *Manager) GetInstalledAppInfo(appName string) (*InstalledAppInfo, error) {
	config, err := m.GetAppConfig(appName)
	if err != nil {
		return nil, err
	}

	info := &InstalledAppInfo{
		Name:           config.Name,
		Description:    config.Description,
		SpecSource:     config.SpecSource,
		ConfigPath:     m.getAppConfigPath(appName),
		DefaultProfile: config.DefaultProfile,
		ProfileCount:   len(config.Profiles),
		Profiles:       config.ListProfiles(),
		CreatedAt:      config.CreatedAt,
		UpdatedAt:      config.UpdatedAt,
		ShimExists:     m.ShimExists(appName),
	}

	// Load spec info
	if config.SpecSource != "" {
		parser := spec.NewParser()
		if specDoc, err := parser.LoadSpec(config.SpecSource); err == nil {
			info.SpecInfo = spec.GetSpecInfo(specDoc, config.SpecSource)
		}
	}

	return info, nil
}

// InstalledAppInfo contains detailed information about an installed app.
type InstalledAppInfo struct {
	Name           string
	Description    string
	SpecSource     string
	ConfigPath     string
	DefaultProfile string
	ProfileCount   int
	Profiles       []string
	CreatedAt      time.Time
	UpdatedAt      time.Time
	ShimExists     bool
	SpecInfo       *spec.SpecInfo
}
