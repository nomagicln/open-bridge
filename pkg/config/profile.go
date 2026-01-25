// Package config provides configuration management for OpenBridge.
// This file contains profile management functionality.
package config

import (
	"fmt"
	"maps"
	"sort"
	"strings"
	"time"
)

// ProfileManager provides profile management operations for an app.
type ProfileManager struct {
	manager *Manager
	appName string
	config  *AppConfig
}

// NewProfileManager creates a new profile manager for the specified app.
func (m *Manager) NewProfileManager(appName string) (*ProfileManager, error) {
	config, err := m.GetAppConfig(appName)
	if err != nil {
		return nil, err
	}

	return &ProfileManager{
		manager: m,
		appName: appName,
		config:  config,
	}, nil
}

// Reload refreshes the configuration from disk.
func (pm *ProfileManager) Reload() error {
	config, err := pm.manager.GetAppConfig(pm.appName)
	if err != nil {
		return err
	}
	pm.config = config
	return nil
}

// Save persists the current configuration to disk.
func (pm *ProfileManager) Save() error {
	pm.config.UpdatedAt = time.Now()
	return pm.manager.SaveAppConfig(pm.config)
}

// CreateProfile creates a new profile with the given name and options.
// buildAuthConfig builds AuthConfig from options.
func buildAuthConfig(opts ProfileOptions) AuthConfig {
	if opts.AuthType == "" {
		return AuthConfig{}
	}
	return AuthConfig{
		Type:     opts.AuthType,
		Location: opts.AuthLocation,
		KeyName:  opts.AuthKeyName,
		Scheme:   opts.AuthScheme,
	}
}

// initializeProfileMap initializes the profiles map if it's nil.
func initializeProfileMap(config *AppConfig) {
	if config.Profiles == nil {
		config.Profiles = make(map[string]Profile)
	}
}

func (pm *ProfileManager) CreateProfile(name string, opts ProfileOptions) error {
	if err := validateProfileName(name); err != nil {
		return err
	}

	if _, exists := pm.config.Profiles[name]; exists {
		return &ProfileExistsError{AppName: pm.appName, ProfileName: name}
	}

	profile := Profile{
		Name:        name,
		BaseURL:     opts.BaseURL,
		Description: opts.Description,
		Headers:     opts.Headers,
		QueryParams: opts.QueryParams,
		Auth:        buildAuthConfig(opts),
		Timeout:     Duration{Duration: 30 * time.Second},
	}

	if opts.Timeout > 0 {
		profile.Timeout = Duration{Duration: opts.Timeout}
	}

	initializeProfileMap(pm.config)
	pm.config.Profiles[name] = profile

	if len(pm.config.Profiles) == 1 || opts.SetAsDefault {
		pm.config.DefaultProfile = name
	}

	return pm.Save()
}

// ProfileOptions contains options for creating/updating a profile.
type ProfileOptions struct {
	BaseURL      string
	Description  string
	AuthType     string
	AuthLocation string // "header", "query", "cookie"
	AuthKeyName  string
	AuthScheme   string
	Headers      map[string]string
	QueryParams  map[string]string
	Timeout      time.Duration
	SetAsDefault bool
}

// updateBasicFields updates basic profile fields.
func updateBasicFields(profile *Profile, opts ProfileOptions) {
	if opts.BaseURL != "" {
		profile.BaseURL = opts.BaseURL
	}
	if opts.Description != "" {
		profile.Description = opts.Description
	}
}

// updateAuthFields updates authentication fields.
func updateAuthFields(profile *Profile, opts ProfileOptions) {
	if opts.AuthType != "" {
		profile.Auth.Type = opts.AuthType
	}
	if opts.AuthLocation != "" {
		profile.Auth.Location = opts.AuthLocation
	}
	if opts.AuthKeyName != "" {
		profile.Auth.KeyName = opts.AuthKeyName
	}
	if opts.AuthScheme != "" {
		profile.Auth.Scheme = opts.AuthScheme
	}
}

// updateMaps updates header and query param maps.
func updateMaps(profile *Profile, opts ProfileOptions) {
	if opts.Headers != nil {
		if profile.Headers == nil {
			profile.Headers = make(map[string]string)
		}
		maps.Copy(profile.Headers, opts.Headers)
	}
	if opts.QueryParams != nil {
		if profile.QueryParams == nil {
			profile.QueryParams = make(map[string]string)
		}
		maps.Copy(profile.QueryParams, opts.QueryParams)
	}
}

// UpdateProfile updates an existing profile.
func (pm *ProfileManager) UpdateProfile(name string, opts ProfileOptions) error {
	profile, exists := pm.config.Profiles[name]
	if !exists {
		return &ProfileNotFoundError{AppName: pm.appName, ProfileName: name}
	}

	updateBasicFields(&profile, opts)
	updateAuthFields(&profile, opts)
	updateMaps(&profile, opts)

	if opts.Timeout > 0 {
		profile.Timeout = Duration{Duration: opts.Timeout}
	}

	pm.config.Profiles[name] = profile

	if opts.SetAsDefault {
		pm.config.DefaultProfile = name
	}

	return pm.Save()
}

// updateDefaultProfileIfNeeded updates the default profile if the current default is being deleted.
func updateDefaultProfileIfNeeded(config *AppConfig, deletedProfile string) {
	if config.DefaultProfile == deletedProfile {
		for pname := range config.Profiles {
			config.DefaultProfile = pname
			break
		}
	}
}

// DeleteProfile removes a profile from the configuration.
func (pm *ProfileManager) DeleteProfile(name string) error {
	if _, exists := pm.config.Profiles[name]; !exists {
		return &ProfileNotFoundError{AppName: pm.appName, ProfileName: name}
	}

	if len(pm.config.Profiles) == 1 {
		return fmt.Errorf("cannot delete the last profile; at least one profile is required")
	}

	delete(pm.config.Profiles, name)
	updateDefaultProfileIfNeeded(pm.config, name)

	return pm.Save()
}

// checkProfileExistence checks if a profile exists and validates new name.
func (pm *ProfileManager) checkProfileExistence(oldName, newName string) (*Profile, error) {
	if err := validateProfileName(newName); err != nil {
		return nil, err
	}

	profile, exists := pm.config.Profiles[oldName]
	if !exists {
		return nil, &ProfileNotFoundError{AppName: pm.appName, ProfileName: oldName}
	}

	if _, exists := pm.config.Profiles[newName]; exists {
		return nil, &ProfileExistsError{AppName: pm.appName, ProfileName: newName}
	}

	return &profile, nil
}

// RenameProfile renames a profile.
func (pm *ProfileManager) RenameProfile(oldName, newName string) error {
	profile, err := pm.checkProfileExistence(oldName, newName)
	if err != nil {
		return err
	}

	profile.Name = newName
	pm.config.Profiles[newName] = *profile
	delete(pm.config.Profiles, oldName)

	if pm.config.DefaultProfile == oldName {
		pm.config.DefaultProfile = newName
	}

	return pm.Save()
}

// copyProfileMaps copies header and query param maps from source to destination.
func copyProfileMaps(dest *Profile, source Profile) {
	if source.Headers != nil {
		dest.Headers = make(map[string]string)
		maps.Copy(dest.Headers, source.Headers)
	}
	if source.QueryParams != nil {
		dest.QueryParams = make(map[string]string)
		maps.Copy(dest.QueryParams, source.QueryParams)
	}
}

// checkCopyProfileExistence checks if source exists and destination name is valid.
func (pm *ProfileManager) checkCopyProfileExistence(sourceName, destName string) (Profile, error) {
	if err := validateProfileName(destName); err != nil {
		return Profile{}, err
	}

	source, exists := pm.config.Profiles[sourceName]
	if !exists {
		return Profile{}, &ProfileNotFoundError{AppName: pm.appName, ProfileName: sourceName}
	}

	if _, exists := pm.config.Profiles[destName]; exists {
		return Profile{}, &ProfileExistsError{AppName: pm.appName, ProfileName: destName}
	}

	return source, nil
}

// CopyProfile creates a copy of an existing profile.
func (pm *ProfileManager) CopyProfile(sourceName, destName string) error {
	source, err := pm.checkCopyProfileExistence(sourceName, destName)
	if err != nil {
		return err
	}

	dest := Profile{
		Name:         destName,
		BaseURL:      source.BaseURL,
		Description:  source.Description,
		Auth:         source.Auth,
		TLSConfig:    source.TLSConfig,
		SafetyConfig: source.SafetyConfig,
		Timeout:      source.Timeout,
		RetryConfig:  source.RetryConfig,
	}

	copyProfileMaps(&dest, source)
	pm.config.Profiles[destName] = dest

	return pm.Save()
}

// GetProfile returns a profile by name.
func (pm *ProfileManager) GetProfile(name string) (*Profile, error) {
	profile, exists := pm.config.Profiles[name]
	if !exists {
		return nil, &ProfileNotFoundError{AppName: pm.appName, ProfileName: name}
	}
	return &profile, nil
}

// GetDefaultProfile returns the default profile.
func (pm *ProfileManager) GetDefaultProfile() (*Profile, error) {
	if pm.config.DefaultProfile == "" {
		// Return first profile if no default set
		for _, profile := range pm.config.Profiles {
			return &profile, nil
		}
		return nil, fmt.Errorf("no profiles exist for app '%s'", pm.appName)
	}

	return pm.GetProfile(pm.config.DefaultProfile)
}

// SetDefaultProfile sets the default profile.
func (pm *ProfileManager) SetDefaultProfile(name string) error {
	if _, exists := pm.config.Profiles[name]; !exists {
		return &ProfileNotFoundError{AppName: pm.appName, ProfileName: name}
	}

	pm.config.DefaultProfile = name
	return pm.Save()
}

// GetDefaultProfileName returns the name of the default profile.
func (pm *ProfileManager) GetDefaultProfileName() string {
	return pm.config.DefaultProfile
}

// ListProfiles returns a list of all profile names.
func (pm *ProfileManager) ListProfiles() []string {
	var names []string
	for name := range pm.config.Profiles {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// ListProfilesWithInfo returns detailed information about all profiles.
func (pm *ProfileManager) ListProfilesWithInfo() []ProfileInfo {
	var profiles []ProfileInfo
	for name, profile := range pm.config.Profiles {
		info := ProfileInfo{
			Name:        name,
			BaseURL:     profile.BaseURL,
			Description: profile.Description,
			AuthType:    profile.Auth.Type,
			IsDefault:   name == pm.config.DefaultProfile,
			HeaderCount: len(profile.Headers),
		}
		profiles = append(profiles, info)
	}

	// Sort by name
	sort.Slice(profiles, func(i, j int) bool {
		return profiles[i].Name < profiles[j].Name
	})

	return profiles
}

// ProfileInfo contains summary information about a profile.
type ProfileInfo struct {
	Name        string
	BaseURL     string
	Description string
	AuthType    string
	IsDefault   bool
	HeaderCount int
}

// ProfileExists checks if a profile exists.
func (pm *ProfileManager) ProfileExists(name string) bool {
	_, exists := pm.config.Profiles[name]
	return exists
}

// ProfileCount returns the number of profiles.
func (pm *ProfileManager) ProfileCount() int {
	return len(pm.config.Profiles)
}

func (pm *ProfileManager) setProfileStringMapValue(
	profileName, key, value string,
	getMap func(*Profile) map[string]string,
	setMap func(*Profile, map[string]string),
) error {
	profile, exists := pm.config.Profiles[profileName]
	if !exists {
		return &ProfileNotFoundError{AppName: pm.appName, ProfileName: profileName}
	}

	target := getMap(&profile)
	if target == nil {
		target = make(map[string]string)
		setMap(&profile, target)
	}
	target[key] = value
	pm.config.Profiles[profileName] = profile

	return pm.Save()
}

// SelectProfile selects a profile based on the following priority:
// 1. Explicit profile name if provided
// 2. Profile from environment variable OPENBRIDGE_PROFILE
// 3. Default profile for the app
func (pm *ProfileManager) SelectProfile(explicitName string) (*Profile, error) {
	// Priority 1: Explicit profile name
	if explicitName != "" {
		return pm.GetProfile(explicitName)
	}

	// Priority 2: Environment variable (handled at higher level, but check here too)
	// This would typically be passed as explicitName by the caller

	// Priority 3: Default profile
	return pm.GetDefaultProfile()
}

// SetProfileHeader sets a header for a profile.
func (pm *ProfileManager) SetProfileHeader(profileName, headerName, headerValue string) error {
	return pm.setProfileStringMapValue(
		profileName,
		headerName,
		headerValue,
		func(p *Profile) map[string]string {
			return p.Headers
		},
		func(p *Profile, m map[string]string) {
			p.Headers = m
		},
	)
}

// DeleteProfileHeader removes a header from a profile.
func (pm *ProfileManager) DeleteProfileHeader(profileName, headerName string) error {
	profile, exists := pm.config.Profiles[profileName]
	if !exists {
		return &ProfileNotFoundError{AppName: pm.appName, ProfileName: profileName}
	}

	if profile.Headers != nil {
		delete(profile.Headers, headerName)
		pm.config.Profiles[profileName] = profile
	}

	return pm.Save()
}

// SetProfileQueryParam sets a query parameter for a profile.
func (pm *ProfileManager) SetProfileQueryParam(profileName, paramName, paramValue string) error {
	return pm.setProfileStringMapValue(
		profileName,
		paramName,
		paramValue,
		func(p *Profile) map[string]string {
			return p.QueryParams
		},
		func(p *Profile, m map[string]string) {
			p.QueryParams = m
		},
	)
}

// ConfigureSafety configures AI safety settings for a profile.
func (pm *ProfileManager) ConfigureSafety(profileName string, safety SafetyConfig) error {
	profile, exists := pm.config.Profiles[profileName]
	if !exists {
		return &ProfileNotFoundError{AppName: pm.appName, ProfileName: profileName}
	}

	profile.SafetyConfig = safety
	pm.config.Profiles[profileName] = profile

	return pm.Save()
}

// ConfigureTLS configures TLS settings for a profile.
func (pm *ProfileManager) ConfigureTLS(profileName string, tls TLSConfig) error {
	profile, exists := pm.config.Profiles[profileName]
	if !exists {
		return &ProfileNotFoundError{AppName: pm.appName, ProfileName: profileName}
	}

	profile.TLSConfig = tls
	pm.config.Profiles[profileName] = profile

	return pm.Save()
}

// ConfigureRetry configures retry settings for a profile.
func (pm *ProfileManager) ConfigureRetry(profileName string, retry RetryConfig) error {
	profile, exists := pm.config.Profiles[profileName]
	if !exists {
		return &ProfileNotFoundError{AppName: pm.appName, ProfileName: profileName}
	}

	profile.RetryConfig = retry
	pm.config.Profiles[profileName] = profile

	return pm.Save()
}

// Error types

// ProfileExistsError indicates a profile already exists.
type ProfileExistsError struct {
	AppName     string
	ProfileName string
}

func (e *ProfileExistsError) Error() string {
	return fmt.Sprintf("profile '%s' already exists in app '%s'", e.ProfileName, e.AppName)
}

// validateProfileName validates a profile name.
func validateProfileName(name string) error {
	if name == "" {
		return fmt.Errorf("profile name cannot be empty")
	}

	// Profile names should be simple identifiers
	if len(name) > 64 {
		return fmt.Errorf("profile name cannot exceed 64 characters")
	}

	// Check for invalid characters
	for _, r := range name {
		if (r < 'a' || r > 'z') && (r < 'A' || r > 'Z') &&
			(r < '0' || r > '9') && r != '-' && r != '_' {
			return fmt.Errorf("profile name can only contain letters, numbers, hyphens, and underscores")
		}
	}

	return nil
}

// Helper functions for profile selection from Manager

// SelectProfileForApp selects the appropriate profile for an app.
// Priority: explicit > environment variable > default
func (m *Manager) SelectProfileForApp(appName, explicitProfile string) (*Profile, error) {
	pm, err := m.NewProfileManager(appName)
	if err != nil {
		return nil, err
	}

	return pm.SelectProfile(explicitProfile)
}

// GetProfileNames returns all profile names for an app.
func (m *Manager) GetProfileNames(appName string) ([]string, error) {
	config, err := m.GetAppConfig(appName)
	if err != nil {
		return nil, err
	}

	return config.ListProfiles(), nil
}

// ProfileSummary returns a brief summary string for a profile.
func ProfileSummary(p *Profile) string {
	var parts []string
	parts = append(parts, p.BaseURL)
	if p.Auth.Type != "" && p.Auth.Type != "none" {
		parts = append(parts, fmt.Sprintf("auth:%s", p.Auth.Type))
	}
	if len(p.Headers) > 0 {
		parts = append(parts, fmt.Sprintf("%d headers", len(p.Headers)))
	}
	return strings.Join(parts, ", ")
}
