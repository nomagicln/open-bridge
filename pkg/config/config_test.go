package config

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func TestNewManager(t *testing.T) {
	// Use temp directory for testing
	tmpDir := t.TempDir()

	m, err := NewManager(WithConfigDir(tmpDir))
	if err != nil {
		t.Fatalf("NewManager failed: %v", err)
	}

	if m.ConfigDir() != tmpDir {
		t.Errorf("expected configDir %s, got %s", tmpDir, m.ConfigDir())
	}

	// Verify apps directory was created
	appsDir := filepath.Join(tmpDir, "apps")
	if _, err := os.Stat(appsDir); os.IsNotExist(err) {
		t.Error("expected apps directory to be created")
	}
}

func TestGetConfigDir(t *testing.T) {
	// Clear any override
	_ = os.Unsetenv("OPENBRIDGE_CONFIG_DIR")

	dir, err := GetConfigDir()
	if err != nil {
		t.Fatalf("GetConfigDir failed: %v", err)
	}

	if dir == "" {
		t.Error("expected non-empty config dir")
	}

	// Verify platform-specific path
	switch runtime.GOOS {
	case "darwin":
		if !contains(dir, "Library/Application Support/openbridge") {
			t.Errorf("macOS config dir should contain Library/Application Support/openbridge, got %s", dir)
		}
	case "windows":
		if !contains(dir, "openbridge") {
			t.Errorf("Windows config dir should contain openbridge, got %s", dir)
		}
	default:
		if !contains(dir, ".config/openbridge") {
			t.Errorf("Linux config dir should contain .config/openbridge, got %s", dir)
		}
	}
}

func TestGetConfigDirWithOverride(t *testing.T) {
	customDir := "/custom/config/dir"
	if err := os.Setenv("OPENBRIDGE_CONFIG_DIR", customDir); err != nil {
		t.Fatalf("os.Setenv failed: %v", err)
	}
	defer func() { _ = os.Unsetenv("OPENBRIDGE_CONFIG_DIR") }()

	dir, err := GetConfigDir()
	if err != nil {
		t.Fatalf("GetConfigDir failed: %v", err)
	}

	if dir != customDir {
		t.Errorf("expected %s, got %s", customDir, dir)
	}
}

func TestValidateAppName(t *testing.T) {
	tests := []struct {
		name  string
		valid bool
	}{
		{"myapp", true},
		{"my-app", true},
		{"my_app", true},
		{"MyApp123", true},
		{"app", true},
		{"a", true},
		{"", false},        // empty
		{"123app", false},  // starts with number
		{"-app", false},    // starts with hyphen
		{"my app", false},  // contains space
		{"my.app", false},  // contains dot
		{"ob", false},      // reserved
		{"install", false}, // reserved
		{"help", false},    // reserved
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateAppName(tt.name)
			if tt.valid && err != nil {
				t.Errorf("expected '%s' to be valid, got error: %v", tt.name, err)
			}
			if !tt.valid && err == nil {
				t.Errorf("expected '%s' to be invalid, but got no error", tt.name)
			}
		})
	}
}

func TestSaveAndGetAppConfig(t *testing.T) {
	tmpDir := t.TempDir()
	m, err := NewManager(WithConfigDir(tmpDir))
	if err != nil {
		t.Fatalf("NewManager failed: %v", err)
	}

	config := NewAppConfig("testapp", "/path/to/spec.yaml")
	config.Description = "Test application"
	config.AddProfile(NewProfile("default", "https://api.example.com"))
	config.AddProfile(NewProfile("staging", "https://staging.example.com"))

	// Save config
	if err := m.SaveAppConfig(config); err != nil {
		t.Fatalf("SaveAppConfig failed: %v", err)
	}

	// Get config
	loaded, err := m.GetAppConfig("testapp")
	if err != nil {
		t.Fatalf("GetAppConfig failed: %v", err)
	}

	if loaded.Name != config.Name {
		t.Errorf("expected name %q, got %q", config.Name, loaded.Name)
	}

	if loaded.SpecSource != config.SpecSource {
		t.Errorf("expected spec source %q, got %q", config.SpecSource, loaded.SpecSource)
	}

	if loaded.Description != config.Description {
		t.Errorf("expected description %q, got %q", config.Description, loaded.Description)
	}

	if len(loaded.Profiles) != 2 {
		t.Errorf("expected 2 profiles, got %d", len(loaded.Profiles))
	}

	profile, ok := loaded.Profiles["default"]
	if !ok {
		t.Fatal("expected 'default' profile to exist")
	}

	if profile.BaseURL != "https://api.example.com" {
		t.Errorf("expected baseURL %q, got %q", "https://api.example.com", profile.BaseURL)
	}
}

func TestAppExists(t *testing.T) {
	tmpDir := t.TempDir()
	m, err := NewManager(WithConfigDir(tmpDir))
	if err != nil {
		t.Fatalf("NewManager failed: %v", err)
	}

	// Should not exist initially
	if m.AppExists("testapp") {
		t.Error("expected app to not exist")
	}

	// Create app
	config := NewAppConfig("testapp", "/path/to/spec.yaml")
	if err := m.SaveAppConfig(config); err != nil {
		t.Fatalf("SaveAppConfig failed: %v", err)
	}

	// Should exist now
	if !m.AppExists("testapp") {
		t.Error("expected app to exist")
	}
}

func TestDeleteAppConfig(t *testing.T) {
	tmpDir := t.TempDir()
	m, err := NewManager(WithConfigDir(tmpDir))
	if err != nil {
		t.Fatalf("NewManager failed: %v", err)
	}

	config := NewAppConfig("testapp", "/path/to/spec.yaml")

	// Save config
	if err := m.SaveAppConfig(config); err != nil {
		t.Fatalf("SaveAppConfig failed: %v", err)
	}

	// Delete config
	if err := m.DeleteAppConfig("testapp"); err != nil {
		t.Fatalf("DeleteAppConfig failed: %v", err)
	}

	// Verify deletion
	_, err = m.GetAppConfig("testapp")
	if err == nil {
		t.Error("expected error after deletion")
	}

	// Check error type
	if _, ok := err.(*AppNotFoundError); !ok {
		t.Errorf("expected AppNotFoundError, got %T", err)
	}
}

func TestDeleteAppConfigNotFound(t *testing.T) {
	tmpDir := t.TempDir()
	m, err := NewManager(WithConfigDir(tmpDir))
	if err != nil {
		t.Fatalf("NewManager failed: %v", err)
	}

	err = m.DeleteAppConfig("nonexistent")
	if err == nil {
		t.Error("expected error for nonexistent app")
	}

	if _, ok := err.(*AppNotFoundError); !ok {
		t.Errorf("expected AppNotFoundError, got %T", err)
	}
}

func TestListApps(t *testing.T) {
	tmpDir := t.TempDir()
	m, err := NewManager(WithConfigDir(tmpDir))
	if err != nil {
		t.Fatalf("NewManager failed: %v", err)
	}

	// Initially empty
	apps, err := m.ListApps()
	if err != nil {
		t.Fatalf("ListApps failed: %v", err)
	}
	if len(apps) != 0 {
		t.Errorf("expected 0 apps, got %d", len(apps))
	}

	// Add some apps
	for _, name := range []string{"zapp", "aapp", "mapp"} {
		config := NewAppConfig(name, "/path/"+name+".yaml")
		if err := m.SaveAppConfig(config); err != nil {
			t.Fatalf("SaveAppConfig failed for %s: %v", name, err)
		}
	}

	// List apps (should be sorted)
	apps, err = m.ListApps()
	if err != nil {
		t.Fatalf("ListApps failed: %v", err)
	}
	if len(apps) != 3 {
		t.Errorf("expected 3 apps, got %d", len(apps))
	}

	// Check sorted order
	expected := []string{"aapp", "mapp", "zapp"}
	for i, name := range apps {
		if name != expected[i] {
			t.Errorf("expected app %d to be %s, got %s", i, expected[i], name)
		}
	}
}

func TestListAppsWithInfo(t *testing.T) {
	tmpDir := t.TempDir()
	m, err := NewManager(WithConfigDir(tmpDir))
	if err != nil {
		t.Fatalf("NewManager failed: %v", err)
	}

	// Add apps with profiles
	config1 := NewAppConfig("app1", "/path/spec1.yaml")
	config1.Description = "First app"
	config1.AddProfile(NewProfile("default", "https://api1.example.com"))
	if err := m.SaveAppConfig(config1); err != nil {
		t.Fatalf("SaveAppConfig failed: %v", err)
	}

	config2 := NewAppConfig("app2", "/path/spec2.yaml")
	config2.AddProfile(NewProfile("default", "https://api2.example.com"))
	config2.AddProfile(NewProfile("prod", "https://prod.example.com"))
	if err := m.SaveAppConfig(config2); err != nil {
		t.Fatalf("SaveAppConfig failed: %v", err)
	}

	apps, err := m.ListAppsWithInfo()
	if err != nil {
		t.Fatalf("ListAppsWithInfo failed: %v", err)
	}

	if len(apps) != 2 {
		t.Fatalf("expected 2 apps, got %d", len(apps))
	}

	// Find app1
	var app1Info *AppInfo
	for i := range apps {
		if apps[i].Name == "app1" {
			app1Info = &apps[i]
			break
		}
	}

	if app1Info == nil {
		t.Fatal("app1 not found")
	}

	if app1Info.Description != "First app" {
		t.Errorf("expected description 'First app', got '%s'", app1Info.Description)
	}

	if app1Info.ProfileCount != 1 {
		t.Errorf("expected 1 profile, got %d", app1Info.ProfileCount)
	}
}

func TestGetAppConfigNotFound(t *testing.T) {
	tmpDir := t.TempDir()
	m, err := NewManager(WithConfigDir(tmpDir))
	if err != nil {
		t.Fatalf("NewManager failed: %v", err)
	}

	_, err = m.GetAppConfig("nonexistent")
	if err == nil {
		t.Error("expected error for nonexistent app")
	}

	if _, ok := err.(*AppNotFoundError); !ok {
		t.Errorf("expected AppNotFoundError, got %T", err)
	}
}

func TestExportProfile(t *testing.T) {
	tmpDir := t.TempDir()
	m, err := NewManager(WithConfigDir(tmpDir))
	if err != nil {
		t.Fatalf("NewManager failed: %v", err)
	}

	config := NewAppConfig("testapp", "/path/to/spec.yaml")
	profile := NewProfile("prod", "https://api.prod.example.com")
	profile.Headers = map[string]string{"X-Custom-Header": "value"}
	profile.Auth = AuthConfig{Type: "bearer"}
	config.AddProfile(profile)

	if err := m.SaveAppConfig(config); err != nil {
		t.Fatalf("SaveAppConfig failed: %v", err)
	}

	data, err := m.ExportProfile("testapp", "prod")
	if err != nil {
		t.Fatalf("ExportProfile failed: %v", err)
	}

	if len(data) == 0 {
		t.Error("expected non-empty export data")
	}

	// Verify export contains expected fields
	exportStr := string(data)
	if !contains(exportStr, "base_url") {
		t.Error("expected export to contain base_url")
	}
	if !contains(exportStr, "profile_name") {
		t.Error("expected export to contain profile_name")
	}
}

func TestExportProfileNotFound(t *testing.T) {
	tmpDir := t.TempDir()
	m, err := NewManager(WithConfigDir(tmpDir))
	if err != nil {
		t.Fatalf("NewManager failed: %v", err)
	}

	config := NewAppConfig("testapp", "/path/to/spec.yaml")
	if err := m.SaveAppConfig(config); err != nil {
		t.Fatalf("SaveAppConfig failed: %v", err)
	}

	_, err = m.ExportProfile("testapp", "nonexistent")
	if err == nil {
		t.Error("expected error for nonexistent profile")
	}

	if _, ok := err.(*ProfileNotFoundError); !ok {
		t.Errorf("expected ProfileNotFoundError, got %T", err)
	}
}

func TestImportProfile(t *testing.T) {
	tmpDir := t.TempDir()
	m, err := NewManager(WithConfigDir(tmpDir))
	if err != nil {
		t.Fatalf("NewManager failed: %v", err)
	}

	// Create app
	config := NewAppConfig("testapp", "/path/to/spec.yaml")
	config.AddProfile(NewProfile("default", "https://api.example.com"))
	if err := m.SaveAppConfig(config); err != nil {
		t.Fatalf("SaveAppConfig failed: %v", err)
	}

	// Export profile
	exportData, err := m.ExportProfile("testapp", "default")
	if err != nil {
		t.Fatalf("ExportProfile failed: %v", err)
	}

	// Create another app
	config2 := NewAppConfig("otherapp", "/path/to/other.yaml")
	if err := m.SaveAppConfig(config2); err != nil {
		t.Fatalf("SaveAppConfig failed: %v", err)
	}

	// Import profile
	if err := m.ImportProfile("otherapp", exportData); err != nil {
		t.Fatalf("ImportProfile failed: %v", err)
	}

	// Verify imported profile
	loaded, _ := m.GetAppConfig("otherapp")
	if len(loaded.Profiles) != 1 {
		t.Errorf("expected 1 profile after import, got %d", len(loaded.Profiles))
	}
}

func TestValidateConfig(t *testing.T) {
	tests := []struct {
		name    string
		config  *AppConfig
		wantErr bool
	}{
		{
			name:    "nil config",
			config:  nil,
			wantErr: true,
		},
		{
			name: "empty name",
			config: &AppConfig{
				Name:       "",
				SpecSource: "/path/spec.yaml",
			},
			wantErr: true,
		},
		{
			name: "empty spec source",
			config: &AppConfig{
				Name:       "testapp",
				SpecSource: "",
			},
			wantErr: true,
		},
		{
			name: "profile without base_url",
			config: &AppConfig{
				Name:       "testapp",
				SpecSource: "/path/spec.yaml",
				Profiles: map[string]Profile{
					"default": {Name: "default"},
				},
			},
			wantErr: true,
		},
		{
			name: "invalid default profile",
			config: &AppConfig{
				Name:           "testapp",
				SpecSource:     "/path/spec.yaml",
				DefaultProfile: "nonexistent",
				Profiles: map[string]Profile{
					"default": {Name: "default", BaseURL: "https://api.example.com"},
				},
			},
			wantErr: true,
		},
		{
			name: "valid config",
			config: &AppConfig{
				Name:           "testapp",
				SpecSource:     "/path/spec.yaml",
				DefaultProfile: "default",
				Profiles: map[string]Profile{
					"default": {Name: "default", BaseURL: "https://api.example.com"},
				},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateConfig(tt.config)
			if tt.wantErr && err == nil {
				t.Error("expected error, got nil")
			}
			if !tt.wantErr && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}

func TestAppConfigProfileMethods(t *testing.T) {
	config := NewAppConfig("testapp", "/path/spec.yaml")

	// Add profiles
	config.AddProfile(NewProfile("default", "https://api.example.com"))
	config.AddProfile(NewProfile("staging", "https://staging.example.com"))
	config.AddProfile(NewProfile("prod", "https://prod.example.com"))

	// Get profile
	profile, ok := config.GetProfile("staging")
	if !ok {
		t.Error("expected to find staging profile")
	}
	if profile.BaseURL != "https://staging.example.com" {
		t.Errorf("unexpected base URL: %s", profile.BaseURL)
	}

	// Get nonexistent profile
	_, ok = config.GetProfile("nonexistent")
	if ok {
		t.Error("expected to not find nonexistent profile")
	}

	// Set default profile
	if err := config.SetDefaultProfile("prod"); err != nil {
		t.Errorf("SetDefaultProfile failed: %v", err)
	}
	if config.DefaultProfile != "prod" {
		t.Errorf("expected default profile 'prod', got '%s'", config.DefaultProfile)
	}

	// Set nonexistent default
	if err := config.SetDefaultProfile("nonexistent"); err == nil {
		t.Error("expected error for nonexistent profile")
	}

	// Get default profile
	defaultProfile, ok := config.GetDefaultProfile()
	if !ok {
		t.Error("expected to get default profile")
	}
	if defaultProfile.Name != "prod" {
		t.Errorf("expected default profile 'prod', got '%s'", defaultProfile.Name)
	}

	// List profiles
	names := config.ListProfiles()
	if len(names) != 3 {
		t.Errorf("expected 3 profiles, got %d", len(names))
	}

	// Delete profile
	if err := config.DeleteProfile("staging"); err != nil {
		t.Errorf("DeleteProfile failed: %v", err)
	}
	if len(config.Profiles) != 2 {
		t.Errorf("expected 2 profiles after delete, got %d", len(config.Profiles))
	}

	// Delete nonexistent profile
	if err := config.DeleteProfile("nonexistent"); err == nil {
		t.Error("expected error for deleting nonexistent profile")
	}

	// Delete default profile
	if err := config.DeleteProfile("prod"); err != nil {
		t.Errorf("DeleteProfile failed: %v", err)
	}
	// Default should be cleared or set to another profile
	if config.DefaultProfile == "prod" {
		t.Error("default profile should not be deleted profile")
	}
}

func TestDuration(t *testing.T) {
	// Test marshaling
	d := Duration{Duration: 30 * time.Second}
	data, err := d.MarshalYAML()
	if err != nil {
		t.Fatalf("MarshalYAML failed: %v", err)
	}
	if data != "30s" {
		t.Errorf("expected '30s', got '%v'", data)
	}
}

func TestNewAppConfig(t *testing.T) {
	config := NewAppConfig("testapp", "/path/spec.yaml")

	if config.Name != "testapp" {
		t.Errorf("expected name 'testapp', got '%s'", config.Name)
	}

	if config.SpecSource != "/path/spec.yaml" {
		t.Errorf("expected spec source '/path/spec.yaml', got '%s'", config.SpecSource)
	}

	if config.DefaultProfile != "default" {
		t.Errorf("expected default profile 'default', got '%s'", config.DefaultProfile)
	}

	if config.Version != "1.0" {
		t.Errorf("expected version '1.0', got '%s'", config.Version)
	}

	if config.CreatedAt.IsZero() {
		t.Error("expected CreatedAt to be set")
	}
}

func TestNewProfile(t *testing.T) {
	profile := NewProfile("myprofile", "https://api.example.com")

	if profile.Name != "myprofile" {
		t.Errorf("expected name 'myprofile', got '%s'", profile.Name)
	}

	if profile.BaseURL != "https://api.example.com" {
		t.Errorf("expected base URL 'https://api.example.com', got '%s'", profile.BaseURL)
	}

	if profile.Timeout.Duration != 30*time.Second {
		t.Errorf("expected timeout 30s, got %v", profile.Timeout.Duration)
	}
}

// Helper function
func contains(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
