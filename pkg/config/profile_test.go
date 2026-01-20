package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func setupTestApp(t *testing.T) (*Manager, *ProfileManager) {
	tmpDir := t.TempDir()
	m, err := NewManager(WithConfigDir(tmpDir))
	if err != nil {
		t.Fatalf("NewManager failed: %v", err)
	}

	// Create a test spec
	specPath := filepath.Join(tmpDir, "spec.yaml")
	specContent := `
openapi: "3.0.0"
info:
  title: Test API
  version: "1.0.0"
servers:
  - url: https://api.example.com
paths: {}
`
	os.WriteFile(specPath, []byte(specContent), 0644)

	// Install app
	_, err = m.InstallApp("testapp", InstallOptions{SpecSource: specPath})
	if err != nil {
		t.Fatalf("InstallApp failed: %v", err)
	}

	pm, err := m.NewProfileManager("testapp")
	if err != nil {
		t.Fatalf("NewProfileManager failed: %v", err)
	}

	return m, pm
}

func TestNewProfileManager(t *testing.T) {
	_, pm := setupTestApp(t)

	if pm == nil {
		t.Fatal("expected non-nil ProfileManager")
	}

	if pm.appName != "testapp" {
		t.Errorf("expected app name 'testapp', got '%s'", pm.appName)
	}
}

func TestNewProfileManagerNotFound(t *testing.T) {
	tmpDir := t.TempDir()
	m, _ := NewManager(WithConfigDir(tmpDir))

	_, err := m.NewProfileManager("nonexistent")
	if err == nil {
		t.Error("expected error for nonexistent app")
	}
}

func TestCreateProfile(t *testing.T) {
	_, pm := setupTestApp(t)

	err := pm.CreateProfile("staging", ProfileOptions{
		BaseURL:     "https://staging.example.com",
		Description: "Staging environment",
		AuthType:    "bearer",
	})

	if err != nil {
		t.Fatalf("CreateProfile failed: %v", err)
	}

	profile, err := pm.GetProfile("staging")
	if err != nil {
		t.Fatalf("GetProfile failed: %v", err)
	}

	if profile.BaseURL != "https://staging.example.com" {
		t.Errorf("expected base URL 'https://staging.example.com', got '%s'", profile.BaseURL)
	}

	if profile.Description != "Staging environment" {
		t.Errorf("expected description 'Staging environment', got '%s'", profile.Description)
	}

	if profile.Auth.Type != "bearer" {
		t.Errorf("expected auth type 'bearer', got '%s'", profile.Auth.Type)
	}
}

func TestCreateProfileAlreadyExists(t *testing.T) {
	_, pm := setupTestApp(t)

	// "default" profile already exists from installation
	err := pm.CreateProfile("default", ProfileOptions{
		BaseURL: "https://another.example.com",
	})

	if err == nil {
		t.Error("expected error for existing profile")
	}

	if _, ok := err.(*ProfileExistsError); !ok {
		t.Errorf("expected ProfileExistsError, got %T", err)
	}
}

func TestCreateProfileSetAsDefault(t *testing.T) {
	_, pm := setupTestApp(t)

	err := pm.CreateProfile("prod", ProfileOptions{
		BaseURL:      "https://prod.example.com",
		SetAsDefault: true,
	})

	if err != nil {
		t.Fatalf("CreateProfile failed: %v", err)
	}

	if pm.GetDefaultProfileName() != "prod" {
		t.Errorf("expected default profile 'prod', got '%s'", pm.GetDefaultProfileName())
	}
}

func TestUpdateProfile(t *testing.T) {
	_, pm := setupTestApp(t)

	// Update the default profile
	err := pm.UpdateProfile("default", ProfileOptions{
		BaseURL:     "https://updated.example.com",
		Description: "Updated description",
		AuthType:    "api_key",
	})

	if err != nil {
		t.Fatalf("UpdateProfile failed: %v", err)
	}

	profile, _ := pm.GetProfile("default")

	if profile.BaseURL != "https://updated.example.com" {
		t.Errorf("expected updated base URL, got '%s'", profile.BaseURL)
	}

	if profile.Description != "Updated description" {
		t.Errorf("expected updated description, got '%s'", profile.Description)
	}

	if profile.Auth.Type != "api_key" {
		t.Errorf("expected auth type 'api_key', got '%s'", profile.Auth.Type)
	}
}

func TestUpdateProfileNotFound(t *testing.T) {
	_, pm := setupTestApp(t)

	err := pm.UpdateProfile("nonexistent", ProfileOptions{
		BaseURL: "https://example.com",
	})

	if err == nil {
		t.Error("expected error for nonexistent profile")
	}

	if _, ok := err.(*ProfileNotFoundError); !ok {
		t.Errorf("expected ProfileNotFoundError, got %T", err)
	}
}

func TestDeleteProfile(t *testing.T) {
	_, pm := setupTestApp(t)

	// Create a second profile first
	pm.CreateProfile("staging", ProfileOptions{
		BaseURL: "https://staging.example.com",
	})

	// Delete the staging profile
	err := pm.DeleteProfile("staging")
	if err != nil {
		t.Fatalf("DeleteProfile failed: %v", err)
	}

	if pm.ProfileExists("staging") {
		t.Error("expected profile to be deleted")
	}
}

func TestDeleteProfileUpdateDefault(t *testing.T) {
	_, pm := setupTestApp(t)

	// Create profiles and set one as default
	pm.CreateProfile("staging", ProfileOptions{BaseURL: "https://staging.example.com"})
	err := pm.SetDefaultProfile("staging")
	require.NoError(t, err)

	// Delete the default profile
	err := pm.DeleteProfile("staging")
	if err != nil {
		t.Fatalf("DeleteProfile failed: %v", err)
	}

	// Default should be updated to another profile
	defaultProfile := pm.GetDefaultProfileName()
	if defaultProfile == "staging" {
		t.Error("default profile should not be the deleted profile")
	}

	if defaultProfile == "" {
		t.Error("default profile should not be empty")
	}
}

func TestDeleteLastProfile(t *testing.T) {
	_, pm := setupTestApp(t)

	// Try to delete the only profile
	err := pm.DeleteProfile("default")

	if err == nil {
		t.Error("expected error when deleting last profile")
	}
}

func TestRenameProfile(t *testing.T) {
	_, pm := setupTestApp(t)

	err := pm.RenameProfile("default", "production")
	if err != nil {
		t.Fatalf("RenameProfile failed: %v", err)
	}

	// Old name should not exist
	if pm.ProfileExists("default") {
		t.Error("old profile name should not exist")
	}

	// New name should exist
	if !pm.ProfileExists("production") {
		t.Error("new profile name should exist")
	}

	// Default should be updated
	if pm.GetDefaultProfileName() != "production" {
		t.Errorf("expected default profile 'production', got '%s'", pm.GetDefaultProfileName())
	}
}

func TestRenameProfileToExisting(t *testing.T) {
	_, pm := setupTestApp(t)

	// Create another profile
	pm.CreateProfile("staging", ProfileOptions{BaseURL: "https://staging.example.com"})

	// Try to rename to existing name
	err := pm.RenameProfile("default", "staging")

	if err == nil {
		t.Error("expected error when renaming to existing profile")
	}

	if _, ok := err.(*ProfileExistsError); !ok {
		t.Errorf("expected ProfileExistsError, got %T", err)
	}
}

func TestCopyProfile(t *testing.T) {
	_, pm := setupTestApp(t)

	// Add some data to the default profile
	pm.SetProfileHeader("default", "X-Custom", "value")

	err := pm.CopyProfile("default", "staging")
	if err != nil {
		t.Fatalf("CopyProfile failed: %v", err)
	}

	staging, _ := pm.GetProfile("staging")
	defaultProfile, _ := pm.GetProfile("default")

	// Verify copied data
	if staging.BaseURL != defaultProfile.BaseURL {
		t.Error("expected base URL to be copied")
	}

	if staging.Headers["X-Custom"] != "value" {
		t.Error("expected headers to be copied")
	}

	// Verify it's a separate copy
	staging.Headers["X-Custom"] = "modified"
	if defaultProfile.Headers["X-Custom"] == "modified" {
		t.Error("modifying copy should not affect original")
	}
}

func TestGetDefaultProfile(t *testing.T) {
	_, pm := setupTestApp(t)

	profile, err := pm.GetDefaultProfile()
	if err != nil {
		t.Fatalf("GetDefaultProfile failed: %v", err)
	}

	if profile.Name != "default" {
		t.Errorf("expected profile name 'default', got '%s'", profile.Name)
	}
}

func TestSetDefaultProfile(t *testing.T) {
	_, pm := setupTestApp(t)

	// Create another profile
	pm.CreateProfile("prod", ProfileOptions{BaseURL: "https://prod.example.com"})

	// Set as default
	err := pm.SetDefaultProfile("prod")
	if err != nil {
		t.Fatalf("SetDefaultProfile failed: %v", err)
	}

	if pm.GetDefaultProfileName() != "prod" {
		t.Errorf("expected default profile 'prod', got '%s'", pm.GetDefaultProfileName())
	}
}

func TestSetDefaultProfileNotFound(t *testing.T) {
	_, pm := setupTestApp(t)

	err := pm.SetDefaultProfile("nonexistent")
	if err == nil {
		t.Error("expected error for nonexistent profile")
	}
}

func TestListProfiles(t *testing.T) {
	_, pm := setupTestApp(t)

	pm.CreateProfile("staging", ProfileOptions{BaseURL: "https://staging.example.com"})
	pm.CreateProfile("prod", ProfileOptions{BaseURL: "https://prod.example.com"})

	profiles := pm.ListProfiles()

	if len(profiles) != 3 {
		t.Errorf("expected 3 profiles, got %d", len(profiles))
	}

	// Should be sorted
	expected := []string{"default", "prod", "staging"}
	for i, name := range profiles {
		if name != expected[i] {
			t.Errorf("expected profile %d to be '%s', got '%s'", i, expected[i], name)
		}
	}
}

func TestListProfilesWithInfo(t *testing.T) {
	_, pm := setupTestApp(t)

	pm.CreateProfile("staging", ProfileOptions{
		BaseURL:     "https://staging.example.com",
		Description: "Staging",
		AuthType:    "bearer",
	})

	profiles := pm.ListProfilesWithInfo()

	if len(profiles) != 2 {
		t.Fatalf("expected 2 profiles, got %d", len(profiles))
	}

	// Find staging
	var staging *ProfileInfo
	for i := range profiles {
		if profiles[i].Name == "staging" {
			staging = &profiles[i]
			break
		}
	}

	if staging == nil {
		t.Fatal("staging profile not found")
	}

	if staging.BaseURL != "https://staging.example.com" {
		t.Errorf("expected base URL 'https://staging.example.com', got '%s'", staging.BaseURL)
	}

	if staging.Description != "Staging" {
		t.Errorf("expected description 'Staging', got '%s'", staging.Description)
	}

	if staging.AuthType != "bearer" {
		t.Errorf("expected auth type 'bearer', got '%s'", staging.AuthType)
	}

	if staging.IsDefault {
		t.Error("staging should not be default")
	}
}

func TestSelectProfile(t *testing.T) {
	_, pm := setupTestApp(t)

	pm.CreateProfile("staging", ProfileOptions{BaseURL: "https://staging.example.com"})
	pm.CreateProfile("prod", ProfileOptions{BaseURL: "https://prod.example.com"})

	// Test explicit selection
	profile, err := pm.SelectProfile("staging")
	if err != nil {
		t.Fatalf("SelectProfile failed: %v", err)
	}
	if profile.Name != "staging" {
		t.Errorf("expected 'staging', got '%s'", profile.Name)
	}

	// Test default selection (empty explicit name)
	profile, err = pm.SelectProfile("")
	if err != nil {
		t.Fatalf("SelectProfile failed: %v", err)
	}
	if profile.Name != pm.GetDefaultProfileName() {
		t.Errorf("expected default profile '%s', got '%s'", pm.GetDefaultProfileName(), profile.Name)
	}
}

func TestSetProfileHeader(t *testing.T) {
	_, pm := setupTestApp(t)

	err := pm.SetProfileHeader("default", "X-Custom-Header", "custom-value")
	if err != nil {
		t.Fatalf("SetProfileHeader failed: %v", err)
	}

	profile, _ := pm.GetProfile("default")
	if profile.Headers["X-Custom-Header"] != "custom-value" {
		t.Errorf("expected header value 'custom-value', got '%s'", profile.Headers["X-Custom-Header"])
	}
}

func TestDeleteProfileHeader(t *testing.T) {
	_, pm := setupTestApp(t)

	pm.SetProfileHeader("default", "X-Custom-Header", "value")

	err := pm.DeleteProfileHeader("default", "X-Custom-Header")
	if err != nil {
		t.Fatalf("DeleteProfileHeader failed: %v", err)
	}

	profile, _ := pm.GetProfile("default")
	if _, exists := profile.Headers["X-Custom-Header"]; exists {
		t.Error("expected header to be deleted")
	}
}

func TestSetProfileQueryParam(t *testing.T) {
	_, pm := setupTestApp(t)

	err := pm.SetProfileQueryParam("default", "api_version", "v2")
	if err != nil {
		t.Fatalf("SetProfileQueryParam failed: %v", err)
	}

	profile, _ := pm.GetProfile("default")
	if profile.QueryParams["api_version"] != "v2" {
		t.Errorf("expected query param value 'v2', got '%s'", profile.QueryParams["api_version"])
	}
}

func TestConfigureSafety(t *testing.T) {
	_, pm := setupTestApp(t)

	safety := SafetyConfig{
		ReadOnlyMode:     true,
		RequireConfirm:   []string{"DELETE", "PUT"},
		DeniedOperations: []string{"deleteAll"},
	}

	err := pm.ConfigureSafety("default", safety)
	if err != nil {
		t.Fatalf("ConfigureSafety failed: %v", err)
	}

	profile, _ := pm.GetProfile("default")

	if !profile.SafetyConfig.ReadOnlyMode {
		t.Error("expected ReadOnlyMode to be true")
	}

	if len(profile.SafetyConfig.RequireConfirm) != 2 {
		t.Errorf("expected 2 require confirm methods, got %d", len(profile.SafetyConfig.RequireConfirm))
	}
}

func TestConfigureTLS(t *testing.T) {
	_, pm := setupTestApp(t)

	tls := TLSConfig{
		InsecureSkipVerify: true,
		CAFile:             "/path/to/ca.crt",
		MinVersion:         "1.2",
	}

	err := pm.ConfigureTLS("default", tls)
	if err != nil {
		t.Fatalf("ConfigureTLS failed: %v", err)
	}

	profile, _ := pm.GetProfile("default")

	if !profile.TLSConfig.InsecureSkipVerify {
		t.Error("expected InsecureSkipVerify to be true")
	}

	if profile.TLSConfig.CAFile != "/path/to/ca.crt" {
		t.Errorf("expected CA file path, got '%s'", profile.TLSConfig.CAFile)
	}
}

func TestConfigureRetry(t *testing.T) {
	_, pm := setupTestApp(t)

	retry := RetryConfig{
		MaxRetries:           3,
		InitialDelay:         Duration{Duration: 100 * time.Millisecond},
		MaxDelay:             Duration{Duration: 5 * time.Second},
		RetryableStatusCodes: []int{502, 503, 504},
	}

	err := pm.ConfigureRetry("default", retry)
	if err != nil {
		t.Fatalf("ConfigureRetry failed: %v", err)
	}

	profile, _ := pm.GetProfile("default")

	if profile.RetryConfig.MaxRetries != 3 {
		t.Errorf("expected max retries 3, got %d", profile.RetryConfig.MaxRetries)
	}

	if len(profile.RetryConfig.RetryableStatusCodes) != 3 {
		t.Errorf("expected 3 retryable status codes, got %d", len(profile.RetryConfig.RetryableStatusCodes))
	}
}

func TestValidateProfileName(t *testing.T) {
	tests := []struct {
		name  string
		valid bool
	}{
		{"default", true},
		{"prod", true},
		{"staging-env", true},
		{"staging_env", true},
		{"Prod123", true},
		{"", false},
		{"name with spaces", false},
		{"name.with.dots", false},
		{"name/with/slashes", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateProfileName(tt.name)
			if tt.valid && err != nil {
				t.Errorf("expected '%s' to be valid, got error: %v", tt.name, err)
			}
			if !tt.valid && err == nil {
				t.Errorf("expected '%s' to be invalid", tt.name)
			}
		})
	}
}

func TestProfileSummary(t *testing.T) {
	profile := &Profile{
		Name:    "test",
		BaseURL: "https://api.example.com",
		Auth: AuthConfig{
			Type: "bearer",
		},
		Headers: map[string]string{
			"X-Custom": "value",
		},
	}

	summary := ProfileSummary(profile)

	if summary == "" {
		t.Error("expected non-empty summary")
	}

	// Should contain base URL
	if !containsString(summary, "https://api.example.com") {
		t.Error("expected summary to contain base URL")
	}

	// Should mention auth type
	if !containsString(summary, "auth:bearer") {
		t.Error("expected summary to mention auth type")
	}

	// Should mention header count
	if !containsString(summary, "1 headers") {
		t.Error("expected summary to mention header count")
	}
}

func TestSelectProfileForApp(t *testing.T) {
	m, _ := setupTestApp(t)

	profile, err := m.SelectProfileForApp("testapp", "")
	if err != nil {
		t.Fatalf("SelectProfileForApp failed: %v", err)
	}

	if profile == nil {
		t.Error("expected non-nil profile")
	}
}

func TestGetProfileNames(t *testing.T) {
	m, pm := setupTestApp(t)

	pm.CreateProfile("staging", ProfileOptions{BaseURL: "https://staging.example.com"})

	names, err := m.GetProfileNames("testapp")
	if err != nil {
		t.Fatalf("GetProfileNames failed: %v", err)
	}

	if len(names) != 2 {
		t.Errorf("expected 2 profile names, got %d", len(names))
	}
}

func TestProfileReload(t *testing.T) {
	m, pm := setupTestApp(t)

	// Modify config directly through manager
	config, _ := m.GetAppConfig("testapp")
	config.Description = "Modified"
	m.SaveAppConfig(config)

	// Reload profile manager
	err := pm.Reload()
	if err != nil {
		t.Fatalf("Reload failed: %v", err)
	}

	// Verify reloaded
	if pm.config.Description != "Modified" {
		t.Error("expected reloaded config to have updated description")
	}
}

// Helper
func containsString(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
