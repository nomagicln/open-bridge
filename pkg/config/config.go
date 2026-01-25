// Package config provides configuration management for OpenBridge.
package config

import (
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// AppConfig represents the configuration for an installed API application.
type AppConfig struct {
	// Name is the unique identifier for the application.
	Name string `yaml:"name"`

	// SpecSource is the path or URL to the OpenAPI specification.
	SpecSource string `yaml:"spec_source"`

	// SpecSources allows multiple spec files for merged APIs.
	SpecSources []string `yaml:"spec_sources,omitempty"`

	// Profiles contains named configuration profiles.
	Profiles map[string]Profile `yaml:"profiles"`

	// DefaultProfile is the name of the default profile to use.
	DefaultProfile string `yaml:"default_profile"`

	// Description is an optional description of the application.
	Description string `yaml:"description,omitempty"`

	// Version is the application configuration version (for migrations).
	Version string `yaml:"version,omitempty"`

	// CreatedAt is when the app was installed.
	CreatedAt time.Time `yaml:"created_at,omitempty"`

	// UpdatedAt is when the config was last updated.
	UpdatedAt time.Time `yaml:"updated_at,omitempty"`

	// Metadata contains arbitrary user-defined metadata.
	Metadata map[string]string `yaml:"metadata,omitempty"`

	// OperationCount is the number of operations in the spec.
	// Used for determining progressive disclosure recommendation.
	OperationCount int `yaml:"operation_count,omitempty"`
}

// Profile represents a configuration profile for an app.
type Profile struct {
	// Name is the profile identifier.
	Name string `yaml:"name"`

	// BaseURL is the API base URL for this profile.
	BaseURL string `yaml:"base_url"`

	// Auth contains authentication configuration.
	Auth AuthConfig `yaml:"auth,omitempty"`

	// Headers contains custom headers to send with every request.
	Headers map[string]string `yaml:"headers,omitempty"`

	// QueryParams contains custom query parameters to send with every request.
	QueryParams map[string]string `yaml:"query_params,omitempty"`

	// TLSConfig contains TLS/SSL configuration.
	TLSConfig TLSConfig `yaml:"tls,omitempty"`

	// SafetyConfig contains AI safety controls.
	SafetyConfig SafetyConfig `yaml:"safety,omitempty"`

	// Timeout is the request timeout for this profile.
	Timeout Duration `yaml:"timeout,omitempty"`

	// RetryConfig contains retry configuration.
	RetryConfig RetryConfig `yaml:"retry,omitempty"`

	// Description is an optional description of this profile.
	Description string `yaml:"description,omitempty"`

	// IsDefault indicates if this is the default profile.
	IsDefault bool `yaml:"is_default,omitempty"`

	// SpecFetchAuth contains authentication configuration for fetching remote specs.
	// This allows accessing private/protected OpenAPI specification URLs.
	// If not set, specs are fetched without authentication.
	SpecFetchAuth *SpecFetchAuthConfig `yaml:"spec_fetch_auth,omitempty"`

	// SpecFetchHeaders contains custom headers to send when fetching remote specs.
	// These headers are applied in addition to default Accept and User-Agent headers.
	SpecFetchHeaders map[string]string `yaml:"spec_fetch_headers,omitempty"`

	// ProtectSensitiveInfo controls whether sensitive information (API keys, tokens, etc.)
	// should be masked when generating code. When true, credentials are replaced with
	// placeholders like <YOUR_API_KEY>. Default is false (not protected).
	ProtectSensitiveInfo bool `yaml:"protect_sensitive_info,omitempty"`
}

// SpecFetchAuthConfig contains authentication configuration for fetching remote specs.
type SpecFetchAuthConfig struct {
	// Type is the authentication type: "bearer", "api_key", "basic", or empty for none.
	Type string `yaml:"type"`

	// KeyName is the header or query parameter name for api_key auth.
	// Defaults to "X-API-Key" if not specified.
	KeyName string `yaml:"key_name,omitempty"`

	// Location is where to send api_key auth: "header" or "query".
	// Defaults to "header" if not specified.
	Location string `yaml:"location,omitempty"`

	// Note: Actual credentials (tokens) should be stored in the system keyring
	// and retrieved at runtime, similar to API authentication.
}

// BuildSpecFetchOptions constructs SpecFetchOptions from profile settings and a credential token.
// The token should be retrieved from the system keyring at runtime.
func (p *Profile) BuildSpecFetchOptions(authToken string) *SpecFetchOptions {
	opts := &SpecFetchOptions{}

	// Copy headers
	if len(p.SpecFetchHeaders) > 0 {
		opts.Headers = make(map[string]string, len(p.SpecFetchHeaders))
		maps.Copy(opts.Headers, p.SpecFetchHeaders)
	}

	// Apply auth configuration
	if p.SpecFetchAuth != nil && p.SpecFetchAuth.Type != "" {
		opts.AuthType = p.SpecFetchAuth.Type
		opts.AuthToken = authToken
		opts.AuthKeyName = p.SpecFetchAuth.KeyName
		opts.AuthLocation = p.SpecFetchAuth.Location
	}

	return opts
}

// Duration is a wrapper around time.Duration for YAML serialization.
type Duration struct {
	time.Duration
}

// MarshalYAML implements yaml.Marshaler.
func (d Duration) MarshalYAML() (any, error) {
	return d.String(), nil
}

// UnmarshalYAML implements yaml.Unmarshaler.
func (d *Duration) UnmarshalYAML(value *yaml.Node) error {
	var s string
	if err := value.Decode(&s); err != nil {
		return err
	}
	if s == "" {
		d.Duration = 0
		return nil
	}
	dur, err := time.ParseDuration(s)
	if err != nil {
		return err
	}
	d.Duration = dur
	return nil
}

// AuthConfig represents authentication configuration.
type AuthConfig struct {
	// Type is the authentication type: "bearer", "api_key", "basic", "oauth2", "none".
	Type string `yaml:"type"`

	// Location is where to send the credential: "header", "query", "cookie".
	// Used for api_key and bearer token.
	Location string `yaml:"location,omitempty"`

	// KeyName is the header/query parameter name for the credential.
	KeyName string `yaml:"key_name,omitempty"`

	// Scheme is the authorization scheme (e.g., "Bearer" for Authorization header).
	Scheme string `yaml:"scheme,omitempty"`

	// OAuth2Config contains OAuth2-specific configuration.
	OAuth2Config *OAuth2Config `yaml:"oauth2,omitempty"`

	// Note: Actual credentials (tokens, passwords) are stored in the system keyring,
	// NEVER in this configuration file.
}

// OAuth2Config represents OAuth2 authentication configuration.
type OAuth2Config struct {
	// TokenURL is the OAuth2 token endpoint.
	TokenURL string `yaml:"token_url,omitempty"`

	// AuthURL is the OAuth2 authorization endpoint.
	AuthURL string `yaml:"auth_url,omitempty"`

	// Scopes are the requested OAuth2 scopes.
	Scopes []string `yaml:"scopes,omitempty"`

	// GrantType is the OAuth2 grant type.
	GrantType string `yaml:"grant_type,omitempty"`
}

// TLSConfig represents TLS/SSL configuration.
type TLSConfig struct {
	// InsecureSkipVerify disables certificate verification (not recommended).
	InsecureSkipVerify bool `yaml:"insecure_skip_verify,omitempty"`

	// CAFile is the path to a custom CA certificate file.
	CAFile string `yaml:"ca_file,omitempty"`

	// CertFile is the path to a client certificate file.
	CertFile string `yaml:"cert_file,omitempty"`

	// KeyFile is the path to a client key file.
	KeyFile string `yaml:"key_file,omitempty"`

	// ServerName is the expected server name for SNI.
	ServerName string `yaml:"server_name,omitempty"`

	// MinVersion is the minimum TLS version (e.g., "1.2", "1.3").
	MinVersion string `yaml:"min_version,omitempty"`
}

// SafetyConfig represents AI safety controls for MCP mode.
type SafetyConfig struct {
	// ReadOnlyMode restricts the AI to read-only operations (GET only).
	ReadOnlyMode bool `yaml:"read_only_mode,omitempty"`

	// AllowedOperations is a whitelist of allowed operation IDs.
	AllowedOperations []string `yaml:"allowed_operations,omitempty"`

	// DeniedOperations is a blacklist of denied operation IDs.
	DeniedOperations []string `yaml:"denied_operations,omitempty"`

	// RequireConfirm lists HTTP methods that require user confirmation.
	RequireConfirm []string `yaml:"require_confirm,omitempty"`

	// MaxRequestsPerMinute limits the AI's request rate.
	MaxRequestsPerMinute int `yaml:"max_requests_per_minute,omitempty"`

	// DangerousOperationPatterns are regex patterns for dangerous operations.
	DangerousOperationPatterns []string `yaml:"dangerous_operation_patterns,omitempty"`

	// ProgressiveDisclosure enables progressive tool disclosure mode.
	// When enabled, only meta-tools (SearchTools, LoadTool, InvokeTool) are exposed
	// instead of all API operations, reducing context usage for large APIs.
	ProgressiveDisclosure bool `yaml:"progressive_disclosure,omitempty"`

	// SearchEngine specifies the search engine type for progressive disclosure.
	// Valid values: "sql" (default), "predicate", "vector", "hybrid".
	SearchEngine string `yaml:"search_engine,omitempty"`

	// HybridSearch contains configuration for the hybrid search engine.
	// Only used when SearchEngine is set to "hybrid".
	HybridSearch *HybridSearchSettings `yaml:"hybrid_search,omitempty"`
}

// HybridSearchSettings contains configuration for hybrid search.
type HybridSearchSettings struct {
	// Enabled indicates whether hybrid search is enabled.
	Enabled bool `yaml:"enabled,omitempty"`

	// EmbedderType specifies the embedder type: "adaptive", "tfidf", "ollama", "onnx".
	EmbedderType string `yaml:"embedder_type,omitempty"`

	// OllamaEndpoint is the Ollama API endpoint (e.g., "http://localhost:11434").
	OllamaEndpoint string `yaml:"ollama_endpoint,omitempty"`

	// OllamaModel is the Ollama model for embeddings (e.g., "nomic-embed-text").
	OllamaModel string `yaml:"ollama_model,omitempty"`

	// ONNXModelPath is the path to a custom ONNX model file.
	ONNXModelPath string `yaml:"onnx_model_path,omitempty"`

	// TokenizerType specifies the tokenizer type: "simple", "unicode", "cjk".
	TokenizerType string `yaml:"tokenizer_type,omitempty"`

	// FusionStrategy specifies the fusion strategy: "rrf" or "weighted".
	FusionStrategy string `yaml:"fusion_strategy,omitempty"`

	// RRFConstant is the k constant for RRF algorithm (default: 60).
	RRFConstant float64 `yaml:"rrf_constant,omitempty"`

	// VectorWeight is the weight for vector search results (0.0-1.0).
	VectorWeight float64 `yaml:"vector_weight,omitempty"`

	// TopK is the number of results to retrieve from each engine before fusion.
	TopK int `yaml:"top_k,omitempty"`

	// PredicateFilter is an optional Vulcand predicate expression for post-filtering.
	PredicateFilter string `yaml:"predicate_filter,omitempty"`
}

// RetryConfig represents retry configuration.
type RetryConfig struct {
	// MaxRetries is the maximum number of retry attempts.
	MaxRetries int `yaml:"max_retries,omitempty"`

	// InitialDelay is the initial delay between retries.
	InitialDelay Duration `yaml:"initial_delay,omitempty"`

	// MaxDelay is the maximum delay between retries.
	MaxDelay Duration `yaml:"max_delay,omitempty"`

	// RetryableStatusCodes are HTTP status codes that trigger a retry.
	RetryableStatusCodes []int `yaml:"retryable_status_codes,omitempty"`
}

// Manager handles configuration persistence and retrieval.
type Manager struct {
	configDir string
}

// ManagerOption is a function that configures a Manager.
type ManagerOption func(*Manager)

// WithConfigDir sets a custom configuration directory.
func WithConfigDir(dir string) ManagerOption {
	return func(m *Manager) {
		m.configDir = dir
	}
}

// ensureDir creates a directory with proper permissions, returning an error if it fails.
func ensureDir(path string) error {
	return os.MkdirAll(path, 0755)
}

// NewManager creates a new configuration manager.
func NewManager(opts ...ManagerOption) (*Manager, error) {
	configDir, err := GetConfigDir()
	if err != nil {
		return nil, fmt.Errorf("failed to get config directory: %w", err)
	}

	m := &Manager{configDir: configDir}

	// Apply options
	for _, opt := range opts {
		opt(m)
	}

	// Ensure directories exist
	dirs := []string{m.configDir, filepath.Join(m.configDir, "apps")}
	for _, dir := range dirs {
		if err := ensureDir(dir); err != nil {
			return nil, fmt.Errorf("failed to create directory %s: %w", dir, err)
		}
	}

	return m, nil
}

// GetConfigDir returns the platform-specific configuration directory.
func GetConfigDir() (string, error) {
	// Check for override environment variable
	if dir := os.Getenv("OPENBRIDGE_CONFIG_DIR"); dir != "" {
		return dir, nil
	}

	var baseDir string

	switch runtime.GOOS {
	case "darwin":
		// macOS: ~/Library/Application Support/openbridge
		homeDir, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("failed to get home directory: %w", err)
		}
		baseDir = filepath.Join(homeDir, "Library", "Application Support", "openbridge")

	case "windows":
		// Windows: %APPDATA%\openbridge
		appData := os.Getenv("APPDATA")
		if appData == "" {
			homeDir, err := os.UserHomeDir()
			if err != nil {
				return "", fmt.Errorf("failed to get home directory: %w", err)
			}
			appData = filepath.Join(homeDir, "AppData", "Roaming")
		}
		baseDir = filepath.Join(appData, "openbridge")

	default:
		// Linux/Unix: ~/.config/openbridge (XDG Base Directory Specification)
		xdgConfig := os.Getenv("XDG_CONFIG_HOME")
		if xdgConfig == "" {
			homeDir, err := os.UserHomeDir()
			if err != nil {
				return "", fmt.Errorf("failed to get home directory: %w", err)
			}
			xdgConfig = filepath.Join(homeDir, ".config")
		}
		baseDir = filepath.Join(xdgConfig, "openbridge")
	}

	return baseDir, nil
}

// ConfigDir returns the configuration directory path.
func (m *Manager) ConfigDir() string {
	return m.configDir
}

// AppsDir returns the apps configuration directory path.
func (m *Manager) AppsDir() string {
	return filepath.Join(m.configDir, "apps")
}

// readAndParseConfig reads and parses a configuration file.
func readAndParseConfig(configPath string) (*AppConfig, error) {
	data, err := os.ReadFile(configPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, &AppNotFoundError{AppName: filepath.Base(filepath.Dir(configPath))}
		}
		return nil, fmt.Errorf("failed to read config file '%s': %w", configPath, err)
	}

	var config AppConfig
	if err := yaml.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("failed to parse config file '%s': %w", configPath, err)
	}
	return &config, nil
}

// GetAppConfig retrieves the configuration for an installed app.
func (m *Manager) GetAppConfig(appName string) (*AppConfig, error) {
	if err := validateAppName(appName); err != nil {
		return nil, err
	}

	config, err := readAndParseConfig(m.getAppConfigPath(appName))
	if err != nil {
		return nil, err
	}

	// Ensure name matches
	if config.Name == "" {
		config.Name = appName
	}

	return config, nil
}

// writeConfigAtomically writes configuration data atomically to disk.
func writeConfigAtomically(configPath string, data []byte) error {
	tmpPath := configPath + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write config: %w", err)
	}
	if err := os.Rename(tmpPath, configPath); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("failed to save config: %w", err)
	}
	return nil
}

// SaveAppConfig saves the configuration for an app.
func (m *Manager) SaveAppConfig(config *AppConfig) error {
	if config == nil {
		return fmt.Errorf("config cannot be nil")
	}

	if err := validateAppName(config.Name); err != nil {
		return err
	}

	// Update metadata
	now := time.Now()
	config.UpdatedAt = now
	if config.Version == "" {
		config.Version = "1.0"
	}

	// Ensure apps directory exists
	if err := ensureDir(filepath.Join(m.configDir, "apps")); err != nil {
		return err
	}

	// Serialize and write atomically
	data, err := yaml.Marshal(config)
	if err != nil {
		return fmt.Errorf("failed to serialize config: %w", err)
	}
	return writeConfigAtomically(m.getAppConfigPath(config.Name), data)
}

// DeleteAppConfig removes the configuration for an app.
func (m *Manager) DeleteAppConfig(appName string) error {
	if err := validateAppName(appName); err != nil {
		return err
	}

	configPath := m.getAppConfigPath(appName)

	if err := os.Remove(configPath); err != nil {
		if os.IsNotExist(err) {
			return &AppNotFoundError{AppName: appName}
		}
		return fmt.Errorf("failed to delete config: %w", err)
	}

	return nil
}

// AppExists checks if an app configuration exists.
func (m *Manager) AppExists(appName string) bool {
	configPath := m.getAppConfigPath(appName)
	_, err := os.Stat(configPath)
	return err == nil
}

// ListApps returns a list of all installed app names.
func (m *Manager) ListApps() ([]string, error) {
	appsDir := filepath.Join(m.configDir, "apps")

	entries, err := os.ReadDir(appsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return []string{}, nil
		}
		return nil, fmt.Errorf("failed to read apps directory: %w", err)
	}

	var apps []string
	for _, entry := range entries {
		if !entry.IsDir() && filepath.Ext(entry.Name()) == ".yaml" {
			appName := strings.TrimSuffix(entry.Name(), ".yaml")
			apps = append(apps, appName)
		}
	}

	sort.Strings(apps)
	return apps, nil
}

// toAppInfo converts an AppConfig to AppInfo.
func toAppInfo(config *AppConfig) AppInfo {
	return AppInfo{
		Name:           config.Name,
		Description:    config.Description,
		SpecSource:     config.SpecSource,
		DefaultProfile: config.DefaultProfile,
		ProfileCount:   len(config.Profiles),
		CreatedAt:      config.CreatedAt,
		UpdatedAt:      config.UpdatedAt,
	}
}

// ListAppsWithInfo returns a list of all installed apps with their info.
func (m *Manager) ListAppsWithInfo() ([]AppInfo, error) {
	appNames, err := m.ListApps()
	if err != nil {
		return nil, err
	}

	var apps []AppInfo
	for _, name := range appNames {
		config, err := m.GetAppConfig(name)
		if err != nil {
			// Skip apps with invalid configs
			continue
		}
		apps = append(apps, toAppInfo(config))
	}

	return apps, nil
}

// AppInfo contains summary information about an installed app.
type AppInfo struct {
	Name           string
	Description    string
	SpecSource     string
	DefaultProfile string
	ProfileCount   int
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

// getAppConfigPath returns the path to an app's config file.
func (m *Manager) getAppConfigPath(appName string) string {
	return filepath.Join(m.configDir, "apps", appName+".yaml")
}

// ExportProfile exports a profile configuration without credentials.
func (m *Manager) ExportProfile(appName, profileName string) ([]byte, error) {
	config, err := m.GetAppConfig(appName)
	if err != nil {
		return nil, err
	}

	profile, ok := config.Profiles[profileName]
	if !ok {
		return nil, &ProfileNotFoundError{AppName: appName, ProfileName: profileName}
	}

	// Create export format (without any credential references)
	exportData := ProfileExport{
		Version:     "1.0",
		AppName:     appName,
		ProfileName: profileName,
		Profile:     profile,
		ExportedAt:  time.Now(),
	}

	// Clear any sensitive data that might have leaked
	exportData.Profile.Auth.OAuth2Config = nil

	return yaml.Marshal(exportData)
}

// ProfileExport represents an exported profile.
type ProfileExport struct {
	Version     string    `yaml:"version"`
	AppName     string    `yaml:"app_name"`
	ProfileName string    `yaml:"profile_name"`
	Profile     Profile   `yaml:"profile"`
	ExportedAt  time.Time `yaml:"exported_at"`
}

// extractProfileName extracts the profile name from import data.
func extractProfileName(importData *ProfileExport) (string, error) {
	if importData.ProfileName != "" {
		return importData.ProfileName, nil
	}
	if importData.Profile.Name != "" {
		return importData.Profile.Name, nil
	}
	return "", fmt.Errorf("profile_name is required in import data")
}

// ImportProfile imports a profile from exported data.
func (m *Manager) ImportProfile(appName string, data []byte) error {
	var importData ProfileExport
	if err := yaml.Unmarshal(data, &importData); err != nil {
		// Try legacy format
		return m.importProfileLegacy(appName, data)
	}

	config, err := m.GetAppConfig(appName)
	if err != nil {
		return err
	}

	profileName, err := extractProfileName(&importData)
	if err != nil {
		return err
	}

	profile := importData.Profile
	profile.Name = profileName

	if config.Profiles == nil {
		config.Profiles = make(map[string]Profile)
	}
	config.Profiles[profileName] = profile

	return m.SaveAppConfig(config)
}

// getStringFromMap extracts a string value from a map with type assertion.
func getStringFromMap(data map[string]any, key string) (string, bool) {
	if val, ok := data[key]; ok {
		if str, ok := val.(string); ok {
			return str, true
		}
	}
	return "", false
}

// importProfileLegacy handles legacy import format.
func (m *Manager) importProfileLegacy(appName string, data []byte) error {
	var importData map[string]any
	if err := yaml.Unmarshal(data, &importData); err != nil {
		return fmt.Errorf("failed to parse import data: %w", err)
	}

	config, err := m.GetAppConfig(appName)
	if err != nil {
		return err
	}

	profileName, ok := getStringFromMap(importData, "profile_name")
	if !ok || profileName == "" {
		return fmt.Errorf("profile_name is required in import data")
	}

	profile := Profile{Name: profileName}

	// Extract optional fields
	if baseURL, _ := getStringFromMap(importData, "base_url"); baseURL != "" {
		profile.BaseURL = baseURL
	}
	if authType, _ := getStringFromMap(importData, "auth_type"); authType != "" {
		profile.Auth.Type = authType
	}

	// Extract headers map
	if headers, ok := importData["headers"].(map[string]any); ok {
		profile.Headers = make(map[string]string)
		for k, v := range headers {
			if s, ok := v.(string); ok {
				profile.Headers[k] = s
			}
		}
	}

	if config.Profiles == nil {
		config.Profiles = make(map[string]Profile)
	}
	config.Profiles[profileName] = profile

	return m.SaveAppConfig(config)
}

// validateProfiles validates all profiles in the configuration.
func validateProfiles(config *AppConfig) error {
	for name, profile := range config.Profiles {
		// Ensure profile has a name
		if profile.Name == "" {
			profile.Name = name
			config.Profiles[name] = profile
		}
		// Validate profile has required fields
		if profile.BaseURL == "" {
			return fmt.Errorf("profile '%s': base_url is required", name)
		}
	}
	return nil
}

// validateDefaultProfile validates that the default profile exists.
func validateDefaultProfile(config *AppConfig) error {
	if config.DefaultProfile == "" {
		return nil
	}
	if _, ok := config.Profiles[config.DefaultProfile]; !ok {
		return fmt.Errorf("default_profile '%s' does not exist", config.DefaultProfile)
	}
	return nil
}

// ValidateConfig validates an app configuration.
func ValidateConfig(config *AppConfig) error {
	if config == nil {
		return fmt.Errorf("config is nil")
	}

	if err := validateAppName(config.Name); err != nil {
		return err
	}

	if config.SpecSource == "" && len(config.SpecSources) == 0 {
		return fmt.Errorf("spec_source or spec_sources is required")
	}

	if err := validateProfiles(config); err != nil {
		return err
	}

	return validateDefaultProfile(config)
}

// appNameRegex validates the app name format.
var appNameRegex = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9_-]*$`)

// reservedAppNames contains reserved app names.
var reservedAppNames = map[string]bool{
	"ob": true, "openbridge": true, "help": true, "version": true,
	"install": true, "uninstall": true, "list": true, "config": true,
}

// validateAppName validates an application name.
func validateAppName(name string) error {
	return validateAppNameWithRules(name, 64, reservedAppNames)
}

// validateAppNameWithRules validates an app name with custom rules.
func validateAppNameWithRules(name string, maxLen int, reserved map[string]bool) error {
	if name == "" {
		return fmt.Errorf("app name cannot be empty")
	}
	if len(name) > maxLen {
		return fmt.Errorf("app name cannot exceed %d characters", maxLen)
	}
	if !appNameRegex.MatchString(name) {
		return fmt.Errorf("app name must start with a letter and can only contain letters, numbers, hyphens, and underscores")
	}
	if reserved[strings.ToLower(name)] {
		return fmt.Errorf("app name '%s' is reserved", name)
	}
	return nil
}

// Error Types

// AppNotFoundError indicates an app was not found.
type AppNotFoundError struct {
	AppName string
}

func (e *AppNotFoundError) Error() string {
	return fmt.Sprintf("app '%s' is not installed", e.AppName)
}

// ProfileNotFoundError indicates a profile was not found.
type ProfileNotFoundError struct {
	AppName     string
	ProfileName string
}

func (e *ProfileNotFoundError) Error() string {
	return fmt.Sprintf("profile '%s' not found in app '%s'", e.ProfileName, e.AppName)
}

// NewAppConfig creates a new AppConfig with sensible defaults.
func NewAppConfig(name, specSource string) *AppConfig {
	now := time.Now()
	return &AppConfig{
		Name:           name,
		SpecSource:     specSource,
		Profiles:       make(map[string]Profile),
		DefaultProfile: "default",
		Version:        "1.0",
		CreatedAt:      now,
		UpdatedAt:      now,
	}
}

// NewProfile creates a new Profile with sensible defaults.
func NewProfile(name, baseURL string) Profile {
	return Profile{
		Name:    name,
		BaseURL: baseURL,
		Timeout: Duration{Duration: 30 * time.Second},
	}
}

// AddProfile adds a profile to an app configuration.
func (c *AppConfig) AddProfile(profile Profile) {
	if c.Profiles == nil {
		c.Profiles = make(map[string]Profile)
	}
	c.Profiles[profile.Name] = profile
}

// GetProfile returns a profile by name.
func (c *AppConfig) GetProfile(name string) (*Profile, bool) {
	if c.Profiles == nil {
		return nil, false
	}
	profile, ok := c.Profiles[name]
	if !ok {
		return nil, false
	}
	return &profile, true
}

// getFirstProfile returns the first profile from the map.
func getFirstProfile(profiles map[string]Profile) (*Profile, bool) {
	for _, profile := range profiles {
		return &profile, true
	}
	return nil, false
}

// setDefaultToFirstProfile sets the first remaining profile as default.
func (c *AppConfig) setDefaultToFirstProfile() {
	for pname := range c.Profiles {
		c.DefaultProfile = pname
		break
	}
}

// GetDefaultProfile returns the default profile.
func (c *AppConfig) GetDefaultProfile() (*Profile, bool) {
	if c.DefaultProfile == "" {
		return getFirstProfile(c.Profiles)
	}
	return c.GetProfile(c.DefaultProfile)
}

// SetDefaultProfile sets the default profile.
func (c *AppConfig) SetDefaultProfile(name string) error {
	if _, ok := c.Profiles[name]; !ok {
		return fmt.Errorf("profile '%s' does not exist", name)
	}
	c.DefaultProfile = name
	return nil
}

// DeleteProfile removes a profile from the configuration.
func (c *AppConfig) DeleteProfile(name string) error {
	if _, ok := c.Profiles[name]; !ok {
		return fmt.Errorf("profile '%s' does not exist", name)
	}
	delete(c.Profiles, name)

	// Update default if needed
	if c.DefaultProfile == name {
		c.DefaultProfile = ""
		c.setDefaultToFirstProfile()
	}

	return nil
}

// ListProfiles returns the names of all profiles.
func (c *AppConfig) ListProfiles() []string {
	names := make([]string, 0, len(c.Profiles))
	for name := range c.Profiles {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
