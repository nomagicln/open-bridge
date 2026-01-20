package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gopkg.in/yaml.v3"
)

func TestExportProfileWithOptions(t *testing.T) {
	m, pm := setupTestApp(t)

	// Set up a profile with various settings
	if err := pm.SetProfileHeader("default", "X-Custom-Header", "custom-value"); err != nil {
		t.Fatalf("SetProfileHeader failed: %v", err)
	}
	if err := pm.SetProfileQueryParam("default", "version", "v2"); err != nil {
		t.Fatalf("SetProfileQueryParam failed: %v", err)
	}

	data, err := m.ExportProfileWithOptions("testapp", "default", DefaultExportOptions())
	if err != nil {
		t.Fatalf("ExportProfileWithOptions failed: %v", err)
	}

	if len(data) == 0 {
		t.Error("expected non-empty export data")
	}

	// Parse and verify
	var exported ProfileExportV2
	if err := yaml.Unmarshal(data, &exported); err != nil {
		t.Fatalf("failed to parse export: %v", err)
	}

	if exported.Version != "2.0" {
		t.Errorf("expected version '2.0', got '%s'", exported.Version)
	}

	if exported.AppName != "testapp" {
		t.Errorf("expected app name 'testapp', got '%s'", exported.AppName)
	}

	if exported.ProfileName != "default" {
		t.Errorf("expected profile name 'default', got '%s'", exported.ProfileName)
	}

	if exported.Profile.Headers["X-Custom-Header"] != "custom-value" {
		t.Error("expected custom header to be exported")
	}

	if exported.Profile.QueryParams["version"] != "v2" {
		t.Error("expected query param to be exported")
	}
}

func TestExportProfileJSON(t *testing.T) {
	m, _ := setupTestApp(t)

	opts := DefaultExportOptions()
	opts.Format = ExportFormatJSON

	data, err := m.ExportProfileWithOptions("testapp", "default", opts)
	if err != nil {
		t.Fatalf("ExportProfileWithOptions failed: %v", err)
	}

	// Verify it's valid JSON
	var exported ProfileExportV2
	if err := json.Unmarshal(data, &exported); err != nil {
		t.Fatalf("failed to parse JSON export: %v", err)
	}

	if exported.Version != "2.0" {
		t.Errorf("expected version '2.0', got '%s'", exported.Version)
	}
}

func TestExportProfileExcludesCredentials(t *testing.T) {
	m, pm := setupTestApp(t)

	// Add credential-like headers
	if err := pm.SetProfileHeader("default", "Authorization", "Bearer secret-token"); err != nil {
		t.Fatalf("SetProfileHeader failed: %v", err)
	}
	if err := pm.SetProfileHeader("default", "X-API-Key", "secret-api-key"); err != nil {
		t.Fatalf("SetProfileHeader failed: %v", err)
	}
	if err := pm.SetProfileHeader("default", "X-Custom-Header", "safe-value"); err != nil {
		t.Fatalf("SetProfileHeader failed: %v", err)
	}

	// Add credential-like query params
	if err := pm.SetProfileQueryParam("default", "api_key", "secret"); err != nil {
		t.Fatalf("SetProfileQueryParam failed: %v", err)
	}
	if err := pm.SetProfileQueryParam("default", "version", "v2"); err != nil {
		t.Fatalf("SetProfileQueryParam failed: %v", err)
	}

	data, err := m.ExportProfileWithOptions("testapp", "default", DefaultExportOptions())
	if err != nil {
		t.Fatalf("ExportProfileWithOptions failed: %v", err)
	}

	var exported ProfileExportV2
	if err := yaml.Unmarshal(data, &exported); err != nil {
		t.Fatalf("yaml.Unmarshal failed: %v", err)
	}

	// Credential headers should be excluded
	if _, exists := exported.Profile.Headers["Authorization"]; exists {
		t.Error("Authorization header should be excluded from export")
	}

	if _, exists := exported.Profile.Headers["X-API-Key"]; exists {
		t.Error("X-API-Key header should be excluded from export")
	}

	// Safe headers should be included
	if exported.Profile.Headers["X-Custom-Header"] != "safe-value" {
		t.Error("safe headers should be included in export")
	}

	// Credential query params should be excluded
	if _, exists := exported.Profile.QueryParams["api_key"]; exists {
		t.Error("api_key query param should be excluded from export")
	}

	// Safe query params should be included
	if exported.Profile.QueryParams["version"] != "v2" {
		t.Error("safe query params should be included in export")
	}
}

func TestExportProfileWithoutOptionalConfigs(t *testing.T) {
	m, pm := setupTestApp(t)

	// Add safety config
	if err := pm.ConfigureSafety("default", SafetyConfig{ReadOnlyMode: true}); err != nil {
		t.Fatalf("ConfigureSafety failed: %v", err)
	}

	opts := ExportOptions{
		Format:        ExportFormatYAML,
		IncludeSafety: false,
		IncludeTLS:    false,
		IncludeRetry:  false,
	}

	data, err := m.ExportProfileWithOptions("testapp", "default", opts)
	if err != nil {
		t.Fatalf("ExportProfileWithOptions failed: %v", err)
	}

	var exported ProfileExportV2
	if err := yaml.Unmarshal(data, &exported); err != nil {
		t.Fatalf("yaml.Unmarshal failed: %v", err)
	}

	if exported.Profile.SafetyConfig != nil {
		t.Error("safety config should be excluded when IncludeSafety is false")
	}
}

func TestValidateImport(t *testing.T) {
	m, _ := setupTestApp(t)

	// Valid import data
	validData := `
version: "2.0"
app_name: testapp
profile_name: imported
profile:
  name: imported
  base_url: https://imported.example.com
  auth:
    type: bearer
`

	result, err := m.ValidateImport("testapp", []byte(validData))
	if err != nil {
		t.Fatalf("ValidateImport failed: %v", err)
	}

	if !result.Valid {
		t.Errorf("expected valid result, got errors: %v", result.Errors)
	}
}

func TestValidateImportMissingBaseURL(t *testing.T) {
	m, _ := setupTestApp(t)

	invalidData := `
version: "2.0"
app_name: testapp
profile_name: imported
profile:
  name: imported
`

	result, _ := m.ValidateImport("testapp", []byte(invalidData))

	if result.Valid {
		t.Error("expected invalid result for missing base URL")
	}

	hasBaseURLError := false
	for _, err := range result.Errors {
		if strings.Contains(err, "base URL") {
			hasBaseURLError = true
			break
		}
	}

	if !hasBaseURLError {
		t.Error("expected error about missing base URL")
	}
}

func TestValidateImportExistingProfile(t *testing.T) {
	m, _ := setupTestApp(t)

	// Import data for existing profile
	data := `
version: "2.0"
profile_name: default
profile:
  name: default
  base_url: https://example.com
`

	result, _ := m.ValidateImport("testapp", []byte(data))

	// Should have a warning about existing profile
	hasWarning := false
	for _, warn := range result.Warnings {
		if strings.Contains(warn, "already exists") {
			hasWarning = true
			break
		}
	}

	if !hasWarning {
		t.Error("expected warning about existing profile")
	}
}

func TestImportProfileWithOptions(t *testing.T) {
	m, _ := setupTestApp(t)

	importData := `
version: "2.0"
profile_name: imported
profile:
  name: imported
  base_url: https://imported.example.com
  description: Imported profile
  auth:
    type: api_key
    location: header
    key_name: X-API-Key
  headers:
    X-Custom: value
`

	err := m.ImportProfileWithOptions("testapp", []byte(importData), ImportOptions{})
	if err != nil {
		t.Fatalf("ImportProfileWithOptions failed: %v", err)
	}

	// Verify the imported profile
	config, _ := m.GetAppConfig("testapp")
	profile, exists := config.Profiles["imported"]

	if !exists {
		t.Fatal("expected imported profile to exist")
	}

	if profile.BaseURL != "https://imported.example.com" {
		t.Errorf("expected base URL 'https://imported.example.com', got '%s'", profile.BaseURL)
	}

	if profile.Description != "Imported profile" {
		t.Errorf("expected description 'Imported profile', got '%s'", profile.Description)
	}

	if profile.Auth.Type != "api_key" {
		t.Errorf("expected auth type 'api_key', got '%s'", profile.Auth.Type)
	}

	if profile.Headers["X-Custom"] != "value" {
		t.Error("expected custom header to be imported")
	}
}

func TestImportProfileWithTargetName(t *testing.T) {
	m, _ := setupTestApp(t)

	importData := `
version: "2.0"
profile_name: original
profile:
  base_url: https://example.com
`

	err := m.ImportProfileWithOptions("testapp", []byte(importData), ImportOptions{
		TargetProfileName: "renamed",
	})
	if err != nil {
		t.Fatalf("ImportProfileWithOptions failed: %v", err)
	}

	config, _ := m.GetAppConfig("testapp")

	if _, exists := config.Profiles["original"]; exists {
		t.Error("profile should not exist with original name")
	}

	if _, exists := config.Profiles["renamed"]; !exists {
		t.Error("profile should exist with renamed name")
	}
}

func TestImportProfileOverwrite(t *testing.T) {
	m, _ := setupTestApp(t)

	// First import
	importData := `
version: "2.0"
profile_name: staging
profile:
  base_url: https://staging1.example.com
`

	if err := m.ImportProfileWithOptions("testapp", []byte(importData), ImportOptions{}); err != nil {
		t.Fatalf("ImportProfileWithOptions failed: %v", err)
	}

	// Second import without overwrite should fail
	importData2 := `
version: "2.0"
profile_name: staging
profile:
  base_url: https://staging2.example.com
`

	err := m.ImportProfileWithOptions("testapp", []byte(importData2), ImportOptions{})
	if err == nil {
		t.Error("expected error when importing existing profile without overwrite")
	}

	// With overwrite should succeed
	err = m.ImportProfileWithOptions("testapp", []byte(importData2), ImportOptions{Overwrite: true})
	if err != nil {
		t.Fatalf("ImportProfileWithOptions with overwrite failed: %v", err)
	}

	config, _ := m.GetAppConfig("testapp")
	profile := config.Profiles["staging"]

	if profile.BaseURL != "https://staging2.example.com" {
		t.Error("profile should be overwritten with new base URL")
	}
}

func TestImportProfileSetAsDefault(t *testing.T) {
	m, _ := setupTestApp(t)

	importData := `
version: "2.0"
profile_name: newdefault
profile:
  base_url: https://example.com
`

	err := m.ImportProfileWithOptions("testapp", []byte(importData), ImportOptions{
		SetAsDefault: true,
	})
	if err != nil {
		t.Fatalf("ImportProfileWithOptions failed: %v", err)
	}

	config, _ := m.GetAppConfig("testapp")

	if config.DefaultProfile != "newdefault" {
		t.Errorf("expected default profile 'newdefault', got '%s'", config.DefaultProfile)
	}
}

func TestImportLegacyFormat(t *testing.T) {
	m, _ := setupTestApp(t)

	// Legacy format (v1)
	legacyData := `
profile_name: legacy
base_url: https://legacy.example.com
auth_type: bearer
headers:
  X-Legacy: value
`

	err := m.ImportProfileWithOptions("testapp", []byte(legacyData), ImportOptions{})
	if err != nil {
		t.Fatalf("ImportProfileWithOptions failed: %v", err)
	}

	config, _ := m.GetAppConfig("testapp")
	profile, exists := config.Profiles["legacy"]

	if !exists {
		t.Fatal("expected legacy profile to be imported")
	}

	if profile.BaseURL != "https://legacy.example.com" {
		t.Errorf("expected base URL 'https://legacy.example.com', got '%s'", profile.BaseURL)
	}

	if profile.Auth.Type != "bearer" {
		t.Errorf("expected auth type 'bearer', got '%s'", profile.Auth.Type)
	}
}

func TestExportProfileToFile(t *testing.T) {
	m, _ := setupTestApp(t)

	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "profile.yaml")

	err := m.ExportProfileToFile("testapp", "default", filePath, DefaultExportOptions())
	if err != nil {
		t.Fatalf("ExportProfileToFile failed: %v", err)
	}

	// Verify file exists
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		t.Error("expected file to be created")
	}

	// Read and verify content
	data, _ := os.ReadFile(filePath)
	var exported ProfileExportV2
	if err := yaml.Unmarshal(data, &exported); err != nil {
		t.Fatalf("yaml.Unmarshal failed: %v", err)
	}

	if exported.ProfileName != "default" {
		t.Error("expected exported profile name to be 'default'")
	}
}

func TestImportProfileFromFile(t *testing.T) {
	m, _ := setupTestApp(t)

	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "import.yaml")

	// Create import file
	importData := `
version: "2.0"
profile_name: fromfile
profile:
  base_url: https://fromfile.example.com
`
	if err := os.WriteFile(filePath, []byte(importData), 0644); err != nil {
		t.Fatalf("os.WriteFile failed: %v", err)
	}

	err := m.ImportProfileFromFile("testapp", filePath, ImportOptions{})
	if err != nil {
		t.Fatalf("ImportProfileFromFile failed: %v", err)
	}

	config, _ := m.GetAppConfig("testapp")
	if _, exists := config.Profiles["fromfile"]; !exists {
		t.Error("expected profile to be imported from file")
	}
}

func TestExportAllProfiles(t *testing.T) {
	m, pm := setupTestApp(t)

	// Create additional profiles
	if err := pm.CreateProfile("staging", ProfileOptions{BaseURL: "https://staging.example.com"}); err != nil {
		t.Fatalf("CreateProfile failed: %v", err)
	}
	if err := pm.CreateProfile("prod", ProfileOptions{BaseURL: "https://prod.example.com"}); err != nil {
		t.Fatalf("CreateProfile failed: %v", err)
	}

	exports, err := m.ExportAllProfiles("testapp", DefaultExportOptions())
	if err != nil {
		t.Fatalf("ExportAllProfiles failed: %v", err)
	}

	if len(exports) != 3 {
		t.Errorf("expected 3 exports, got %d", len(exports))
	}

	if _, exists := exports["default"]; !exists {
		t.Error("expected 'default' profile export")
	}

	if _, exists := exports["staging"]; !exists {
		t.Error("expected 'staging' profile export")
	}

	if _, exists := exports["prod"]; !exists {
		t.Error("expected 'prod' profile export")
	}
}

func TestExportAllProfilesAsSingle(t *testing.T) {
	m, pm := setupTestApp(t)

	if err := pm.CreateProfile("staging", ProfileOptions{BaseURL: "https://staging.example.com"}); err != nil {
		t.Fatalf("CreateProfile failed: %v", err)
	}

	data, err := m.ExportAllProfilesAsSingle("testapp", DefaultExportOptions())
	if err != nil {
		t.Fatalf("ExportAllProfilesAsSingle failed: %v", err)
	}

	var bulk BulkExport
	if err := yaml.Unmarshal(data, &bulk); err != nil {
		t.Fatalf("yaml.Unmarshal failed: %v", err)
	}

	if bulk.Version != "2.0" {
		t.Errorf("expected version '2.0', got '%s'", bulk.Version)
	}

	if len(bulk.Profiles) != 2 {
		t.Errorf("expected 2 profiles in bulk export, got %d", len(bulk.Profiles))
	}
}

func TestImportBulkProfiles(t *testing.T) {
	m, _ := setupTestApp(t)

	bulkData := `
version: "2.0"
app_name: testapp
profiles:
  staging:
    name: staging
    base_url: https://staging.example.com
  prod:
    name: prod
    base_url: https://prod.example.com
`

	err := m.ImportBulkProfiles("testapp", []byte(bulkData), ImportOptions{})
	if err != nil {
		t.Fatalf("ImportBulkProfiles failed: %v", err)
	}

	config, _ := m.GetAppConfig("testapp")

	if _, exists := config.Profiles["staging"]; !exists {
		t.Error("expected 'staging' profile to be imported")
	}

	if _, exists := config.Profiles["prod"]; !exists {
		t.Error("expected 'prod' profile to be imported")
	}
}

func TestIsCredentialHeader(t *testing.T) {
	tests := []struct {
		header       string
		isCredential bool
	}{
		{"Authorization", true},
		{"X-API-Key", true},
		{"X-Auth-Token", true},
		{"X-Custom-Header", false},
		{"Content-Type", false},
		{"Accept", false},
		{"X-Secret-Header", true},
	}

	for _, tt := range tests {
		t.Run(tt.header, func(t *testing.T) {
			result := isCredentialHeader(tt.header)
			if result != tt.isCredential {
				t.Errorf("isCredentialHeader(%q) = %v, expected %v", tt.header, result, tt.isCredential)
			}
		})
	}
}

func TestIsCredentialParam(t *testing.T) {
	tests := []struct {
		param        string
		isCredential bool
	}{
		{"api_key", true},
		{"token", true},
		{"access_token", true},
		{"version", false},
		{"page", false},
		{"limit", false},
		{"secret", true},
	}

	for _, tt := range tests {
		t.Run(tt.param, func(t *testing.T) {
			result := isCredentialParam(tt.param)
			if result != tt.isCredential {
				t.Errorf("isCredentialParam(%q) = %v, expected %v", tt.param, result, tt.isCredential)
			}
		})
	}
}

func TestExportWithTimeout(t *testing.T) {
	m, pm := setupTestApp(t)

	// Update profile with timeout
	if err := pm.UpdateProfile("default", ProfileOptions{
		Timeout: 60 * time.Second,
	}); err != nil {
		t.Fatalf("UpdateProfile failed: %v", err)
	}

	data, _ := m.ExportProfileWithOptions("testapp", "default", DefaultExportOptions())

	var exported ProfileExportV2
	if err := yaml.Unmarshal(data, &exported); err != nil {
		t.Fatalf("yaml.Unmarshal failed: %v", err)
	}

	if exported.Profile.Timeout != "1m0s" {
		t.Errorf("expected timeout '1m0s', got '%s'", exported.Profile.Timeout)
	}
}

func TestImportWithTimeout(t *testing.T) {
	m, _ := setupTestApp(t)

	importData := `
version: "2.0"
profile_name: withtimeout
profile:
  base_url: https://example.com
  timeout: "2m30s"
`

	if err := m.ImportProfileWithOptions("testapp", []byte(importData), ImportOptions{}); err != nil {
		t.Fatalf("ImportProfileWithOptions failed: %v", err)
	}

	config, _ := m.GetAppConfig("testapp")
	profile := config.Profiles["withtimeout"]

	expected := 2*time.Minute + 30*time.Second
	if profile.Timeout.Duration != expected {
		t.Errorf("expected timeout %v, got %v", expected, profile.Timeout.Duration)
	}
}
