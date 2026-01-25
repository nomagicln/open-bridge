// Package config provides configuration management for OpenBridge.
// This file contains profile import/export functionality.
package config

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// ExportFormat represents the format for exported profiles.
type ExportFormat string

const (
	// ExportFormatYAML exports as YAML.
	ExportFormatYAML ExportFormat = "yaml"
	// ExportFormatJSON exports as JSON.
	ExportFormatJSON ExportFormat = "json"
)

// ExportOptions contains options for profile export.
type ExportOptions struct {
	// Format is the export format (yaml or json).
	Format ExportFormat

	// IncludeSafety includes safety configuration in export.
	IncludeSafety bool

	// IncludeTLS includes TLS configuration in export.
	IncludeTLS bool

	// IncludeRetry includes retry configuration in export.
	IncludeRetry bool

	// StripComments removes any comments from the export.
	StripComments bool
}

// DefaultExportOptions returns the default export options.
func DefaultExportOptions() ExportOptions {
	return ExportOptions{
		Format:        ExportFormatYAML,
		IncludeSafety: true,
		IncludeTLS:    true,
		IncludeRetry:  true,
	}
}

// ExportProfileWithOptions exports a profile with the given options.
func (m *Manager) ExportProfileWithOptions(appName, profileName string, opts ExportOptions) ([]byte, error) {
	config, err := m.GetAppConfig(appName)
	if err != nil {
		return nil, err
	}

	profile, ok := config.Profiles[profileName]
	if !ok {
		return nil, &ProfileNotFoundError{AppName: appName, ProfileName: profileName}
	}

	// Create a clean copy for export
	exportProfile := cleanProfileForExport(profile, opts)

	exportData := ProfileExportV2{
		Version:     "2.0",
		AppName:     appName,
		ProfileName: profileName,
		Profile:     exportProfile,
		ExportedAt:  time.Now(),
		Metadata: ExportMetadata{
			SourceApp:     appName,
			SourceProfile: profileName,
			ExportOptions: opts,
		},
	}

	switch opts.Format {
	case ExportFormatJSON:
		return json.MarshalIndent(exportData, "", "  ")
	default:
		return yaml.Marshal(exportData)
	}
}

// ProfileExportV2 represents the v2 export format.
type ProfileExportV2 struct {
	Version     string          `yaml:"version" json:"version"`
	AppName     string          `yaml:"app_name" json:"app_name"`
	ProfileName string          `yaml:"profile_name" json:"profile_name"`
	Profile     ExportedProfile `yaml:"profile" json:"profile"`
	ExportedAt  time.Time       `yaml:"exported_at" json:"exported_at"`
	Metadata    ExportMetadata  `yaml:"metadata,omitempty" json:"metadata,omitzero"`
}

// ExportedProfile is a cleaned profile structure for export.
type ExportedProfile struct {
	Name         string            `yaml:"name" json:"name"`
	BaseURL      string            `yaml:"base_url" json:"base_url"`
	Description  string            `yaml:"description,omitempty" json:"description,omitempty"`
	Auth         ExportedAuth      `yaml:"auth,omitempty" json:"auth,omitzero"`
	Headers      map[string]string `yaml:"headers,omitempty" json:"headers,omitempty"`
	QueryParams  map[string]string `yaml:"query_params,omitempty" json:"query_params,omitempty"`
	Timeout      string            `yaml:"timeout,omitempty" json:"timeout,omitempty"`
	TLSConfig    *TLSConfig        `yaml:"tls,omitempty" json:"tls,omitempty"`
	SafetyConfig *SafetyConfig     `yaml:"safety,omitempty" json:"safety,omitempty"`
	RetryConfig  *RetryConfig      `yaml:"retry,omitempty" json:"retry,omitempty"`
}

// ExportedAuth contains auth configuration for export (no credentials).
type ExportedAuth struct {
	Type     string `yaml:"type,omitempty" json:"type,omitempty"`
	Location string `yaml:"location,omitempty" json:"location,omitempty"`
	KeyName  string `yaml:"key_name,omitempty" json:"key_name,omitempty"`
	Scheme   string `yaml:"scheme,omitempty" json:"scheme,omitempty"`
	// Note: Credentials are explicitly excluded
}

// ExportMetadata contains metadata about the export.
type ExportMetadata struct {
	SourceApp     string        `yaml:"source_app,omitempty" json:"source_app,omitempty"`
	SourceProfile string        `yaml:"source_profile,omitempty" json:"source_profile,omitempty"`
	ExportOptions ExportOptions `yaml:"-" json:"-"` // Not serialized
}

// cleanProfileForExport creates a clean copy of a profile for export.
// copyHeadersForExport copies headers, filtering out credential headers.
func copyHeadersForExport(profile Profile) map[string]string {
	if len(profile.Headers) == 0 {
		return nil
	}
	headers := make(map[string]string)
	for k, v := range profile.Headers {
		if !isCredentialHeader(k) {
			headers[k] = v
		}
	}
	return headers
}

// copyQueryParamsForExport copies query params, filtering out credential params.
func copyQueryParamsForExport(profile Profile) map[string]string {
	if len(profile.QueryParams) == 0 {
		return nil
	}
	params := make(map[string]string)
	for k, v := range profile.QueryParams {
		if !isCredentialParam(k) {
			params[k] = v
		}
	}
	return params
}

// parseTimeout exports timeout string if duration is set.
func parseTimeout(profile Profile) string {
	if profile.Timeout.Duration > 0 {
		return profile.Timeout.String()
	}
	return ""
}

// includeOptionalConfig includes optional configurations based on export options.
func includeOptionalConfig(opts ExportOptions, profile Profile) (tls *TLSConfig, safety *SafetyConfig, retry *RetryConfig) {
	if opts.IncludeTLS {
		tls = &profile.TLSConfig
	}
	if opts.IncludeSafety {
		safety = &profile.SafetyConfig
	}
	if opts.IncludeRetry {
		retry = &profile.RetryConfig
	}
	return
}

func cleanProfileForExport(profile Profile, opts ExportOptions) ExportedProfile {
	exported := ExportedProfile{
		Name:        profile.Name,
		BaseURL:     profile.BaseURL,
		Description: profile.Description,
		Auth: ExportedAuth{
			Type:     profile.Auth.Type,
			Location: profile.Auth.Location,
			KeyName:  profile.Auth.KeyName,
			Scheme:   profile.Auth.Scheme,
		},
		Headers:     copyHeadersForExport(profile),
		QueryParams: copyQueryParamsForExport(profile),
		Timeout:     parseTimeout(profile),
	}

	exported.TLSConfig, exported.SafetyConfig, exported.RetryConfig = includeOptionalConfig(opts, profile)
	return exported
}

// isCredentialHeader checks if a header name might contain credentials.
func isCredentialHeader(name string) bool {
	lower := strings.ToLower(name)
	credentialHints := []string{
		"authorization",
		"x-api-key",
		"x-auth",
		"x-token",
		"bearer",
		"api-key",
		"apikey",
		"secret",
		"password",
		"credential",
	}
	for _, hint := range credentialHints {
		if strings.Contains(lower, hint) {
			return true
		}
	}
	return false
}

// isCredentialParam checks if a query param name might contain credentials.
func isCredentialParam(name string) bool {
	lower := strings.ToLower(name)
	credentialHints := []string{
		"key",
		"token",
		"secret",
		"password",
		"auth",
		"api_key",
		"apikey",
		"access_token",
	}
	for _, hint := range credentialHints {
		if strings.Contains(lower, hint) {
			return true
		}
	}
	return false
}

// ImportOptions contains options for profile import.
type ImportOptions struct {
	// TargetProfileName overrides the profile name from the import.
	TargetProfileName string

	// Overwrite allows overwriting an existing profile.
	Overwrite bool

	// SetAsDefault sets the imported profile as default.
	SetAsDefault bool

	// ValidateBaseURL validates that the base URL is reachable.
	ValidateBaseURL bool

	// MergeHeaders merges headers with existing profile instead of replacing.
	MergeHeaders bool
}

// ImportValidationResult contains the result of import validation.
type ImportValidationResult struct {
	Valid    bool
	Errors   []string
	Warnings []string
	Profile  *ExportedProfile
}

// validateImportData validates the export data and populates the result.
// validateImportProfileName validates the profile name for import and adds error if invalid.
func validateImportProfileName(profileName string, result *ImportValidationResult) bool {
	if profileName == "" {
		result.Errors = append(result.Errors, "profile name is required")
		return false
	}
	if err := validateProfileName(profileName); err != nil {
		result.Errors = append(result.Errors, fmt.Sprintf("invalid profile name: %v", err))
		return false
	}
	return true
}

// validateBaseURL validates that base URL is present.
func validateBaseURL(exportData *ProfileExportV2, result *ImportValidationResult) bool {
	if exportData.Profile.BaseURL == "" {
		result.Errors = append(result.Errors, "base URL is required")
		return false
	}
	return true
}

func (m *Manager) validateImportData(appName string, exportData *ProfileExportV2, result *ImportValidationResult) string {
	result.Profile = &exportData.Profile

	profileName := exportData.ProfileName
	if profileName == "" {
		profileName = exportData.Profile.Name
	}

	result.Valid = validateImportProfileName(profileName, result) && validateBaseURL(exportData, result)
	m.addImportWarnings(appName, exportData, profileName, result)
	return profileName
}

// addImportWarnings adds validation warnings to the result.
func (m *Manager) addImportWarnings(appName string, exportData *ProfileExportV2, profileName string, result *ImportValidationResult) {
	if exportData.Version == "" {
		result.Warnings = append(result.Warnings, "no version specified in import data, assuming legacy format")
	}

	config, err := m.GetAppConfig(appName)
	if err == nil {
		if _, exists := config.Profiles[profileName]; exists {
			result.Warnings = append(result.Warnings, fmt.Sprintf("profile '%s' already exists and will be overwritten if imported with --overwrite", profileName))
		}
	}

	if exportData.Profile.Auth.Type == "" {
		result.Warnings = append(result.Warnings, "no authentication type specified")
	}
}

// ValidateImport validates import data without actually importing.
func (m *Manager) ValidateImport(appName string, data []byte) (*ImportValidationResult, error) {
	result := &ImportValidationResult{
		Valid:    true,
		Errors:   make([]string, 0),
		Warnings: make([]string, 0),
	}

	if !m.AppExists(appName) {
		result.Valid = false
		result.Errors = append(result.Errors, fmt.Sprintf("app '%s' does not exist", appName))
		return result, nil
	}

	exportData, err := parseImportData(data)
	if err != nil {
		result.Valid = false
		result.Errors = append(result.Errors, fmt.Sprintf("failed to parse import data: %v", err))
		return result, nil
	}

	m.validateImportData(appName, exportData, result)
	return result, nil
}

// resolveImportProfileName determines the profile name for import.
func resolveImportProfileName(exportData *ProfileExportV2, opts ImportOptions) (string, error) {
	profileName := opts.TargetProfileName
	if profileName == "" {
		profileName = exportData.ProfileName
		if profileName == "" {
			profileName = exportData.Profile.Name
		}
	}

	if profileName == "" {
		return "", fmt.Errorf("profile name is required")
	}

	if err := validateProfileName(profileName); err != nil {
		return "", err
	}

	return profileName, nil
}

// mergeProfileHeaders merges existing headers into the new profile.
func mergeProfileHeaders(profile *Profile, existingProfile Profile) {
	if profile.Headers == nil {
		profile.Headers = make(map[string]string)
	}
	for k, v := range existingProfile.Headers {
		if _, ok := profile.Headers[k]; !ok {
			profile.Headers[k] = v
		}
	}
}

// ImportProfileWithOptions imports a profile with the given options.
// checkProfileExists checks if profile exists and handles overwrite logic.
func checkProfileExists(config *AppConfig, profileName string, opts ImportOptions) (Profile, bool, error) {
	existingProfile, exists := config.Profiles[profileName]
	if exists && !opts.Overwrite {
		return Profile{}, false, &ProfileExistsError{AppName: config.Name, ProfileName: profileName}
	}
	return existingProfile, exists, nil
}

// updateConfigWithProfile updates the app config with the new profile.
func updateConfigWithProfile(config *AppConfig, profileName string, profile Profile, opts ImportOptions, existingProfile Profile, exists bool) {
	if opts.MergeHeaders && exists {
		mergeProfileHeaders(&profile, existingProfile)
	}

	if config.Profiles == nil {
		config.Profiles = make(map[string]Profile)
	}
	config.Profiles[profileName] = profile

	if opts.SetAsDefault {
		config.DefaultProfile = profileName
	}

	config.UpdatedAt = time.Now()
}

func (m *Manager) ImportProfileWithOptions(appName string, data []byte, opts ImportOptions) error {
	exportData, err := parseImportData(data)
	if err != nil {
		return fmt.Errorf("failed to parse import data: %w", err)
	}

	config, err := m.GetAppConfig(appName)
	if err != nil {
		return err
	}

	profileName, err := resolveImportProfileName(exportData, opts)
	if err != nil {
		return err
	}

	existingProfile, exists, err := checkProfileExists(config, profileName, opts)
	if err != nil {
		return err
	}

	profile := convertExportedToProfile(exportData.Profile, profileName)
	updateConfigWithProfile(config, profileName, profile, opts, existingProfile, exists)

	return m.SaveAppConfig(config)
}

// parseImportData parses various import formats.
func parseImportData(data []byte) (*ProfileExportV2, error) {
	// Try V2 format first
	var v2 ProfileExportV2
	if err := yaml.Unmarshal(data, &v2); err == nil && v2.Version != "" {
		return &v2, nil
	}

	// Try JSON format
	if err := json.Unmarshal(data, &v2); err == nil && v2.Version != "" {
		return &v2, nil
	}

	// Try V1 format
	var v1 ProfileExport
	if err := yaml.Unmarshal(data, &v1); err == nil && v1.Version != "" {
		return convertV1ToV2(&v1), nil
	}

	// Try legacy format
	var legacy map[string]any
	if err := yaml.Unmarshal(data, &legacy); err != nil {
		return nil, fmt.Errorf("unrecognized import format")
	}

	return convertLegacyToV2(legacy)
}

// convertV1ToV2 converts V1 export format to V2.
func convertV1ToV2(v1 *ProfileExport) *ProfileExportV2 {
	return &ProfileExportV2{
		Version:     v1.Version,
		AppName:     v1.AppName,
		ProfileName: v1.ProfileName,
		ExportedAt:  v1.ExportedAt,
		Profile: ExportedProfile{
			Name:        v1.Profile.Name,
			BaseURL:     v1.Profile.BaseURL,
			Description: v1.Profile.Description,
			Auth: ExportedAuth{
				Type:     v1.Profile.Auth.Type,
				Location: v1.Profile.Auth.Location,
				KeyName:  v1.Profile.Auth.KeyName,
				Scheme:   v1.Profile.Auth.Scheme,
			},
			Headers:     v1.Profile.Headers,
			QueryParams: v1.Profile.QueryParams,
		},
	}
}

// convertLegacyToV2 converts legacy format to V2.
func convertLegacyToV2(legacy map[string]any) (*ProfileExportV2, error) {
	v2 := &ProfileExportV2{
		Version: "1.0",
	}

	if name, ok := legacy["profile_name"].(string); ok {
		v2.ProfileName = name
	}

	if appName, ok := legacy["app_name"].(string); ok {
		v2.AppName = appName
	}

	v2.Profile = ExportedProfile{
		Name: v2.ProfileName,
	}

	if baseURL, ok := legacy["base_url"].(string); ok {
		v2.Profile.BaseURL = baseURL
	}

	if authType, ok := legacy["auth_type"].(string); ok {
		v2.Profile.Auth.Type = authType
	}

	if headers, ok := legacy["headers"].(map[string]any); ok {
		v2.Profile.Headers = make(map[string]string)
		for k, v := range headers {
			if s, ok := v.(string); ok {
				v2.Profile.Headers[k] = s
			}
		}
	}

	return v2, nil
}

// convertExportedToProfile converts ExportedProfile to Profile.
// parseTimeoutValue parses timeout from string to Duration.
func parseTimeoutValue(exported ExportedProfile) Duration {
	if exported.Timeout == "" {
		return Duration{}
	}
	if d, err := time.ParseDuration(exported.Timeout); err == nil {
		return Duration{Duration: d}
	}
	return Duration{}
}

// copyOptionalConfigs copies optional configurations from exported profile.
func copyOptionalConfigs(exported ExportedProfile) (TLSConfig, SafetyConfig, RetryConfig) {
	var tls TLSConfig
	var safety SafetyConfig
	var retry RetryConfig

	if exported.TLSConfig != nil {
		tls = *exported.TLSConfig
	}
	if exported.SafetyConfig != nil {
		safety = *exported.SafetyConfig
	}
	if exported.RetryConfig != nil {
		retry = *exported.RetryConfig
	}
	return tls, safety, retry
}

func convertExportedToProfile(exported ExportedProfile, name string) Profile {
	profile := Profile{
		Name:        name,
		BaseURL:     exported.BaseURL,
		Description: exported.Description,
		Auth: AuthConfig{
			Type:     exported.Auth.Type,
			Location: exported.Auth.Location,
			KeyName:  exported.Auth.KeyName,
			Scheme:   exported.Auth.Scheme,
		},
		Headers:     exported.Headers,
		QueryParams: exported.QueryParams,
		Timeout:     parseTimeoutValue(exported),
	}

	profile.TLSConfig, profile.SafetyConfig, profile.RetryConfig = copyOptionalConfigs(exported)
	return profile
}

// ExportProfileToFile exports a profile to a file.
func (m *Manager) ExportProfileToFile(appName, profileName, filePath string, opts ExportOptions) error {
	data, err := m.ExportProfileWithOptions(appName, profileName, opts)
	if err != nil {
		return err
	}

	return os.WriteFile(filePath, data, 0644)
}

// ImportProfileFromFile imports a profile from a file.
func (m *Manager) ImportProfileFromFile(appName, filePath string, opts ImportOptions) error {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return fmt.Errorf("failed to read file: %w", err)
	}

	return m.ImportProfileWithOptions(appName, data, opts)
}

// ExportAllProfiles exports all profiles from an app.
func (m *Manager) ExportAllProfiles(appName string, opts ExportOptions) (map[string][]byte, error) {
	config, err := m.GetAppConfig(appName)
	if err != nil {
		return nil, err
	}

	exports := make(map[string][]byte)
	for profileName := range config.Profiles {
		data, err := m.ExportProfileWithOptions(appName, profileName, opts)
		if err != nil {
			return nil, fmt.Errorf("failed to export profile '%s': %w", profileName, err)
		}
		exports[profileName] = data
	}

	return exports, nil
}

// BulkExport represents a bulk export of multiple profiles.
type BulkExport struct {
	Version    string                     `yaml:"version" json:"version"`
	AppName    string                     `yaml:"app_name" json:"app_name"`
	ExportedAt time.Time                  `yaml:"exported_at" json:"exported_at"`
	Profiles   map[string]ExportedProfile `yaml:"profiles" json:"profiles"`
}

// ExportAllProfilesAsSingle exports all profiles as a single file.
func (m *Manager) ExportAllProfilesAsSingle(appName string, opts ExportOptions) ([]byte, error) {
	config, err := m.GetAppConfig(appName)
	if err != nil {
		return nil, err
	}

	bulk := BulkExport{
		Version:    "2.0",
		AppName:    appName,
		ExportedAt: time.Now(),
		Profiles:   make(map[string]ExportedProfile),
	}

	for profileName, profile := range config.Profiles {
		bulk.Profiles[profileName] = cleanProfileForExport(profile, opts)
	}

	switch opts.Format {
	case ExportFormatJSON:
		return json.MarshalIndent(bulk, "", "  ")
	default:
		return yaml.Marshal(bulk)
	}
}

// ImportBulkProfiles imports multiple profiles from a bulk export.
func (m *Manager) ImportBulkProfiles(appName string, data []byte, opts ImportOptions) error {
	var bulk BulkExport
	if err := yaml.Unmarshal(data, &bulk); err != nil {
		if err := json.Unmarshal(data, &bulk); err != nil {
			return fmt.Errorf("failed to parse bulk import data: %w", err)
		}
	}

	config, err := m.GetAppConfig(appName)
	if err != nil {
		return err
	}

	for profileName, exported := range bulk.Profiles {
		// Check if profile exists
		if _, exists := config.Profiles[profileName]; exists && !opts.Overwrite {
			continue // Skip existing profiles if not overwriting
		}

		profile := convertExportedToProfile(exported, profileName)

		if config.Profiles == nil {
			config.Profiles = make(map[string]Profile)
		}
		config.Profiles[profileName] = profile
	}

	config.UpdatedAt = time.Now()

	return m.SaveAppConfig(config)
}
