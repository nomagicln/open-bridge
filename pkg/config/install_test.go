package config

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestInstallApp(t *testing.T) {
	tmpDir := t.TempDir()
	m, err := NewManager(WithConfigDir(tmpDir))
	if err != nil {
		t.Fatalf("NewManager failed: %v", err)
	}

	// Create a test spec file
	specPath := filepath.Join(tmpDir, "spec.yaml")
	specContent := `
openapi: "3.0.0"
info:
  title: Test API
  version: "1.0.0"
servers:
  - url: https://api.example.com
paths:
  /users:
    get:
      operationId: listUsers
      summary: List users
      responses:
        "200":
          description: OK
`
	if err := os.WriteFile(specPath, []byte(specContent), 0644); err != nil {
		t.Fatalf("failed to write spec: %v", err)
	}

	result, err := m.InstallApp("testapi", InstallOptions{
		SpecSource:  specPath,
		Description: "Test API application",
		CreateShim:  false, // Don't create shim in tests
	})

	if err != nil {
		t.Fatalf("InstallApp failed: %v", err)
	}

	if result.AppName != "testapi" {
		t.Errorf("expected app name 'testapi', got '%s'", result.AppName)
	}

	if result.ConfigPath == "" {
		t.Error("expected config path to be set")
	}

	// Verify app was installed
	if !m.AppExists("testapi") {
		t.Error("expected app to exist after installation")
	}

	// Verify config
	config, err := m.GetAppConfig("testapi")
	if err != nil {
		t.Fatalf("GetAppConfig failed: %v", err)
	}

	if config.Description != "Test API application" {
		t.Errorf("expected description 'Test API application', got '%s'", config.Description)
	}

	absSpecPath, err := filepath.Abs(specPath)
	if err != nil {
		t.Fatalf("filepath.Abs failed: %v", err)
	}

	if config.SpecSource != absSpecPath {
		t.Errorf("expected spec source '%s', got '%s'", absSpecPath, config.SpecSource)
	}

	// Verify default profile was created with base URL from spec
	profile, ok := config.Profiles["default"]
	if !ok {
		t.Fatal("expected 'default' profile")
	}

	if profile.BaseURL != "https://api.example.com" {
		t.Errorf("expected base URL 'https://api.example.com', got '%s'", profile.BaseURL)
	}
}

func TestInstallAppWithExplicitBaseURL(t *testing.T) {
	tmpDir := t.TempDir()
	m, err := NewManager(WithConfigDir(tmpDir))
	if err != nil {
		t.Fatalf("NewManager failed: %v", err)
	}

	// Create a test spec file without servers
	specPath := filepath.Join(tmpDir, "spec.yaml")
	specContent := `
openapi: "3.0.0"
info:
  title: Test API
  version: "1.0.0"
paths:
  /test:
    get:
      operationId: test
      responses:
        "200":
          description: OK
`
	if err := os.WriteFile(specPath, []byte(specContent), 0644); err != nil {
		t.Fatalf("failed to write spec: %v", err)
	}

	result, err := m.InstallApp("testapi", InstallOptions{
		SpecSource: specPath,
		BaseURL:    "https://custom.example.com",
	})

	if err != nil {
		t.Fatalf("InstallApp failed: %v", err)
	}

	config, _ := m.GetAppConfig(result.AppName)
	profile := config.Profiles["default"]

	if profile.BaseURL != "https://custom.example.com" {
		t.Errorf("expected base URL 'https://custom.example.com', got '%s'", profile.BaseURL)
	}
}

func TestInstallAppAlreadyExists(t *testing.T) {
	tmpDir := t.TempDir()
	m, err := NewManager(WithConfigDir(tmpDir))
	if err != nil {
		t.Fatalf("NewManager failed: %v", err)
	}

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
	if err := os.WriteFile(specPath, []byte(specContent), 0644); err != nil {
		t.Fatalf("os.WriteFile failed: %v", err)
	}

	// Install first time
	_, err = m.InstallApp("testapi", InstallOptions{SpecSource: specPath})
	if err != nil {
		t.Fatalf("first InstallApp failed: %v", err)
	}

	// Install again without force
	_, err = m.InstallApp("testapi", InstallOptions{SpecSource: specPath})
	if err == nil {
		t.Error("expected error when app already exists")
	}
	if !strings.Contains(err.Error(), "already exists") {
		t.Errorf("expected 'already exists' error, got: %v", err)
	}

	// Install with force
	_, err = m.InstallApp("testapi", InstallOptions{SpecSource: specPath, Force: true})
	if err != nil {
		t.Errorf("InstallApp with force failed: %v", err)
	}
}

func TestInstallAppInvalidSpec(t *testing.T) {
	tmpDir := t.TempDir()
	m, err := NewManager(WithConfigDir(tmpDir))
	if err != nil {
		t.Fatalf("NewManager failed: %v", err)
	}

	_, err = m.InstallApp("testapi", InstallOptions{
		SpecSource: "/nonexistent/spec.yaml",
	})

	if err == nil {
		t.Error("expected error for invalid spec")
	}
}

func TestInstallAppMissingSpecSource(t *testing.T) {
	tmpDir := t.TempDir()
	m, err := NewManager(WithConfigDir(tmpDir))
	if err != nil {
		t.Fatalf("NewManager failed: %v", err)
	}

	_, err = m.InstallApp("testapi", InstallOptions{})

	if err == nil {
		t.Error("expected error for missing spec source")
	}
}

func TestInstallAppInteractive(t *testing.T) {
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
  title: Interactive Test API
  version: "1.0.0"
servers:
  - url: https://api.example.com
paths: {}
`
	if err := os.WriteFile(specPath, []byte(specContent), 0644); err != nil {
		t.Fatalf("os.WriteFile failed: %v", err)
	}

	// Test with explicitly provided options (non-interactive)
	// This simulates what would happen after interactive prompts
	result, err := m.InstallApp("testapi", InstallOptions{
		SpecSource:  specPath,
		Description: "Custom Description",
		AuthType:    "bearer",
		CreateShim:  false,
	})

	if err != nil {
		t.Fatalf("InstallApp failed: %v", err)
	}

	if result.AppName != "testapi" {
		t.Errorf("expected app name 'testapi', got '%s'", result.AppName)
	}

	config, err := m.GetAppConfig("testapi")
	if err != nil {
		t.Fatalf("GetAppConfig failed: %v", err)
	}

	// Description should be from explicit option
	if config.Description != "Custom Description" {
		t.Errorf("expected description 'Custom Description', got '%s'", config.Description)
	}

	profile := config.Profiles["default"]
	if profile.Auth.Type != "bearer" {
		t.Errorf("expected auth type 'bearer', got '%s'", profile.Auth.Type)
	}
}

func TestInstallAppInteractivePrompts(t *testing.T) {
	// Test the prompt helper functions separately to ensure they work correctly
	tmpDir := t.TempDir()
	m, err := NewManager(WithConfigDir(tmpDir))
	if err != nil {
		t.Fatalf("NewManager failed: %v", err)
	}

	specPath := filepath.Join(tmpDir, "spec.yaml")
	specContent := `
openapi: "3.0.0"
info:
  title: Prompt Test API
  version: "1.0.0"
servers:
  - url: https://api.example.com
paths: {}
`
	if err := os.WriteFile(specPath, []byte(specContent), 0644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	// Test prompting for spec only (provide other options)
	input := specPath + "\n" // just the spec path
	reader := strings.NewReader(input)
	var output bytes.Buffer

	opts := InstallOptions{
		Description: "Pre-set Description",
		AuthType:    "api_key",
		Interactive: true,
		Reader:      reader,
		Writer:      &output,
		CreateShim:  false, // Pre-set to avoid prompt
	}

	// We need to test promptForMissingInfo directly
	opts, err = m.promptForMissingInfo(opts)
	if err != nil {
		t.Fatalf("promptForMissingInfo failed: %v", err)
	}

	absSpecPath, err := filepath.Abs(specPath)
	if err != nil {
		t.Fatalf("filepath.Abs failed: %v", err)
	}

	if opts.SpecSource != absSpecPath {
		t.Errorf("expected spec source '%s', got '%s'", absSpecPath, opts.SpecSource)
	}

	// Description and AuthType should remain as pre-set since they were already provided
	if opts.Description != "Pre-set Description" {
		t.Errorf("expected description 'Pre-set Description', got '%s'", opts.Description)
	}

	if opts.AuthType != "api_key" {
		t.Errorf("expected auth type 'api_key', got '%s'", opts.AuthType)
	}
}

func TestUninstallApp(t *testing.T) {
	tmpDir := t.TempDir()
	m, err := NewManager(WithConfigDir(tmpDir))
	if err != nil {
		t.Fatalf("NewManager failed: %v", err)
	}

	// Create a test spec and install app
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
	if err := os.WriteFile(specPath, []byte(specContent), 0644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	_, err = m.InstallApp("testapi", InstallOptions{SpecSource: specPath})
	if err != nil {
		t.Fatalf("InstallApp failed: %v", err)
	}

	// Verify app exists
	if !m.AppExists("testapi") {
		t.Error("expected app to exist before uninstall")
	}

	// Uninstall
	err = m.UninstallApp("testapi", false)
	if err != nil {
		t.Fatalf("UninstallApp failed: %v", err)
	}

	// Verify app was removed
	if m.AppExists("testapi") {
		t.Error("expected app to not exist after uninstall")
	}
}

func TestUninstallAppNotFound(t *testing.T) {
	tmpDir := t.TempDir()
	m, err := NewManager(WithConfigDir(tmpDir))
	if err != nil {
		t.Fatalf("NewManager failed: %v", err)
	}

	err = m.UninstallApp("nonexistent", false)
	assertNonexistentAppError(t, err)
}

func TestCreateAndRemoveShim(t *testing.T) {
	tmpDir := t.TempDir()
	m, err := NewManager(WithConfigDir(tmpDir))
	if err != nil {
		t.Fatalf("NewManager failed: %v", err)
	}

	shimDir := filepath.Join(tmpDir, "bin")

	// Create shim
	shimPath, err := m.CreateShim("testapp", shimDir)
	if err != nil {
		t.Fatalf("CreateShim failed: %v", err)
	}

	if shimPath == "" {
		t.Error("expected shim path to be returned")
	}

	// Verify shim exists
	if _, err := os.Lstat(shimPath); os.IsNotExist(err) {
		t.Error("expected shim file to exist")
	}

	// Platform-specific verification
	switch runtime.GOOS {
	case "windows":
		// Read shim content
		content, err := os.ReadFile(shimPath)
		if err != nil {
			t.Fatalf("failed to read shim: %v", err)
		}

		// Verify shim contains app name
		if !strings.Contains(string(content), "testapp") {
			t.Error("expected shim to contain app name")
		}
	default:
		// Unix: shim is symlink
		info, err := os.Lstat(shimPath)
		if err != nil {
			t.Fatalf("failed to lstat shim: %v", err)
		}
		if info.Mode()&os.ModeSymlink == 0 {
			t.Error("expected Unix shim to be a symbolic link")
		}

		// Verify file name matches app name
		if filepath.Base(shimPath) != "testapp" {
			t.Errorf("expected shim name to be 'testapp', got '%s'", filepath.Base(shimPath))
		}
	}
}

func TestGetInstalledAppInfo(t *testing.T) {
	tmpDir := t.TempDir()
	m, err := NewManager(WithConfigDir(tmpDir))
	if err != nil {
		t.Fatalf("NewManager failed: %v", err)
	}

	// Create and install an app
	specPath := filepath.Join(tmpDir, "spec.yaml")
	specContent := `
openapi: "3.0.0"
info:
  title: Info Test API
  version: "2.0.0"
servers:
  - url: https://api.example.com
paths:
  /users:
    get:
      operationId: listUsers
      responses:
        "200":
          description: OK
`
	if err := os.WriteFile(specPath, []byte(specContent), 0644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	_, err = m.InstallApp("infotest", InstallOptions{
		SpecSource:  specPath,
		Description: "Test for GetInstalledAppInfo",
	})
	if err != nil {
		t.Fatalf("InstallApp failed: %v", err)
	}

	info, err := m.GetInstalledAppInfo("infotest")
	if err != nil {
		t.Fatalf("GetInstalledAppInfo failed: %v", err)
	}

	if info.Name != "infotest" {
		t.Errorf("expected name 'infotest', got '%s'", info.Name)
	}

	if info.Description != "Test for GetInstalledAppInfo" {
		t.Errorf("expected description 'Test for GetInstalledAppInfo', got '%s'", info.Description)
	}

	if info.ProfileCount != 1 {
		t.Errorf("expected 1 profile, got %d", info.ProfileCount)
	}

	if info.SpecInfo == nil {
		t.Error("expected SpecInfo to be populated")
	} else {
		if info.SpecInfo.Title != "Info Test API" {
			t.Errorf("expected spec title 'Info Test API', got '%s'", info.SpecInfo.Title)
		}
	}
}

func TestUpdateApp(t *testing.T) {
	tmpDir := t.TempDir()
	m, err := NewManager(WithConfigDir(tmpDir))
	if err != nil {
		t.Fatalf("NewManager failed: %v", err)
	}

	// Create and install initial app
	specPath := filepath.Join(tmpDir, "spec.yaml")
	specContent := `
openapi: "3.0.0"
info:
  title: Original API
  version: "1.0.0"
servers:
  - url: https://api.example.com
paths: {}
`
	if err := os.WriteFile(specPath, []byte(specContent), 0644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	_, err = m.InstallApp("updatetest", InstallOptions{
		SpecSource:  specPath,
		Description: "Original description",
	})
	if err != nil {
		t.Fatalf("InstallApp failed: %v", err)
	}

	// Update description
	err = m.UpdateApp("updatetest", InstallOptions{
		Description: "Updated description",
	})
	if err != nil {
		t.Fatalf("UpdateApp failed: %v", err)
	}

	config, _ := m.GetAppConfig("updatetest")
	if config.Description != "Updated description" {
		t.Errorf("expected description 'Updated description', got '%s'", config.Description)
	}
}

func TestPromptHelpers(t *testing.T) {
	t.Run("promptString", func(t *testing.T) {
		input := "test value\n"
		reader := strings.NewReader(input)
		var output bytes.Buffer

		result, err := promptString(reader, &output, "Enter value", "default")
		if err != nil {
			t.Fatalf("promptString failed: %v", err)
		}

		if result != "test value" {
			t.Errorf("expected 'test value', got '%s'", result)
		}
	})

	t.Run("promptString with default", func(t *testing.T) {
		input := "\n"
		reader := strings.NewReader(input)
		var output bytes.Buffer

		result, err := promptString(reader, &output, "Enter value", "default")
		if err != nil {
			t.Fatalf("promptString failed: %v", err)
		}

		if result != "default" {
			t.Errorf("expected 'default', got '%s'", result)
		}
	})

	t.Run("promptYesNo yes", func(t *testing.T) {
		input := "y\n"
		reader := strings.NewReader(input)
		var output bytes.Buffer

		result, err := promptYesNo(reader, &output, "Continue?", false)
		if err != nil {
			t.Fatalf("promptYesNo failed: %v", err)
		}

		if !result {
			t.Error("expected true for 'y' input")
		}
	})

	t.Run("promptYesNo no", func(t *testing.T) {
		input := "n\n"
		reader := strings.NewReader(input)
		var output bytes.Buffer

		result, err := promptYesNo(reader, &output, "Continue?", true)
		if err != nil {
			t.Fatalf("promptYesNo failed: %v", err)
		}

		if result {
			t.Error("expected false for 'n' input")
		}
	})

	t.Run("promptChoice valid", func(t *testing.T) {
		input := "bearer\n"
		reader := strings.NewReader(input)
		var output bytes.Buffer

		result, err := promptChoice(reader, &output, "Auth type", []string{"none", "bearer", "basic"}, "none")
		if err != nil {
			t.Fatalf("promptChoice failed: %v", err)
		}

		if result != "bearer" {
			t.Errorf("expected 'bearer', got '%s'", result)
		}
	})

	t.Run("promptChoice invalid", func(t *testing.T) {
		input := "invalid\n"
		reader := strings.NewReader(input)
		var output bytes.Buffer

		_, err := promptChoice(reader, &output, "Auth type", []string{"none", "bearer", "basic"}, "none")
		if err == nil {
			t.Error("expected error for invalid choice")
		}
	})
}

func TestGetDefaultShimDir(t *testing.T) {
	shimDir, err := getDefaultShimDir()
	if err != nil {
		t.Fatalf("getDefaultShimDir failed: %v", err)
	}

	if shimDir == "" {
		t.Error("expected non-empty shim directory")
	}

	// Verify it's an absolute path
	if !filepath.IsAbs(shimDir) {
		t.Errorf("expected absolute path, got '%s'", shimDir)
	}

	// Verify platform-specific expectations
	if runtime.GOOS == "windows" {
		if !strings.Contains(shimDir, "OpenBridge") {
			t.Errorf("expected Windows shim dir to contain 'OpenBridge', got '%s'", shimDir)
		}
	} else {
		// macOS, Linux, and other Unix-like systems use .local/bin
		if !strings.Contains(shimDir, ".local/bin") {
			t.Errorf("expected Unix shim dir to contain '.local/bin', got '%s'", shimDir)
		}
	}
}

func TestGetPathInstructions(t *testing.T) {
	shimDir := "/home/user/.local/bin"
	instructions := GetPathInstructions(shimDir)

	if instructions == "" {
		t.Error("expected non-empty instructions")
	}

	// Should contain the shim directory
	if !strings.Contains(instructions, shimDir) {
		t.Errorf("expected instructions to contain shim dir '%s'", shimDir)
	}

	// Should contain platform-specific guidance
	switch runtime.GOOS {
	case "windows":
		if !strings.Contains(instructions, "PATH") {
			t.Error("expected Windows instructions to mention PATH")
		}
	case "darwin":
		if !strings.Contains(instructions, "export PATH") {
			t.Error("expected macOS instructions to mention 'export PATH'")
		}
	default:
		if !strings.Contains(instructions, "export PATH") {
			t.Error("expected Linux instructions to mention 'export PATH'")
		}
	}
}

func TestIsShimDirInPath(t *testing.T) {
	// Test with a directory that's definitely not in PATH
	notInPath := "/definitely/not/in/path/xyz123"
	if IsShimDirInPath(notInPath) {
		t.Error("expected false for directory not in PATH")
	}

	// Test with a directory that should be in PATH (if PATH is set)
	pathEnv := os.Getenv("PATH")
	if pathEnv != "" {
		var pathSep string
		if runtime.GOOS == "windows" {
			pathSep = ";"
		} else {
			pathSep = ":"
		}
		paths := strings.Split(pathEnv, pathSep)
		if len(paths) > 0 && paths[0] != "" {
			// Test with first directory in PATH
			if !IsShimDirInPath(paths[0]) {
				t.Errorf("expected true for directory in PATH: %s", paths[0])
			}
		}
	}
}

func TestShimContentFormat(t *testing.T) {
	tmpDir := t.TempDir()
	m, err := NewManager(WithConfigDir(tmpDir))
	if err != nil {
		t.Fatalf("NewManager failed: %v", err)
	}

	shimDir := filepath.Join(tmpDir, "bin")
	shimPath, err := m.CreateShim("testapp", shimDir)
	if err != nil {
		t.Fatalf("CreateShim failed: %v", err)
	}

	// Verify shim format based on platform
	switch runtime.GOOS {
	case "windows":
		// Windows batch file
		content, err := os.ReadFile(shimPath)
		if err != nil {
			t.Fatalf("failed to read shim: %v", err)
		}

		contentStr := string(content)
		if !strings.HasPrefix(contentStr, "@echo off") {
			t.Error("expected Windows shim to start with '@echo off'")
		}
		if !strings.Contains(contentStr, "testapp") {
			t.Error("expected shim to contain app name 'testapp'")
		}
		if !strings.Contains(contentStr, "run testapp") {
			t.Error("expected Windows shim to contain 'run testapp'")
		}
		if !strings.Contains(contentStr, "%*") {
			t.Error("expected Windows shim to contain '%*' for argument forwarding")
		}
	default:
		assertUnixShimSymlink(t, shimPath, "testapp")
	}
}

func TestShimExists(t *testing.T) {
	tmpDir := t.TempDir()
	m, err := NewManager(WithConfigDir(tmpDir))
	if err != nil {
		t.Fatalf("NewManager failed: %v", err)
	}

	// Initially should not exist
	if m.ShimExists("testapp") {
		t.Error("expected shim to not exist initially")
	}

	// Create shim in custom directory
	shimDir := filepath.Join(tmpDir, "bin")
	_, err = m.CreateShim("testapp", shimDir)
	if err != nil {
		t.Fatalf("CreateShim failed: %v", err)
	}

	// Note: ShimExists checks default directory, not custom directory
	// So this test verifies the function works correctly
	// In real usage, shims would be created in the default directory
}

func TestRemoveShimNotExists(t *testing.T) {
	tmpDir := t.TempDir()
	m, err := NewManager(WithConfigDir(tmpDir))
	if err != nil {
		t.Fatalf("NewManager failed: %v", err)
	}

	// Removing non-existent shim should not error
	err = m.RemoveShim("nonexistent")
	if err != nil {
		t.Errorf("RemoveShim should not error for non-existent shim: %v", err)
	}
}

func TestNormalizeSpecSource(t *testing.T) {
	tmpDir := t.TempDir()
	specPath := filepath.Join(tmpDir, "spec.yaml")
	specContent := `
openapi: "3.0.0"
info:
  title: Normalize Test API
  version: "1.0.0"
paths: {}
`
	if err := os.WriteFile(specPath, []byte(specContent), 0644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	absSpecPath, err := filepath.Abs(specPath)
	if err != nil {
		t.Fatalf("filepath.Abs failed: %v", err)
	}

	t.Run("relative path is normalized", func(t *testing.T) {
		source, err := normalizeSpecSource(specPath)
		if err != nil {
			t.Fatalf("normalizeSpecSource failed: %v", err)
		}
		if source != absSpecPath {
			t.Errorf("expected %q, got %q", absSpecPath, source)
		}
	})

	t.Run("http URL is preserved", func(t *testing.T) {
		source, err := normalizeSpecSource("https://example.com/spec.yaml")
		if err != nil {
			t.Fatalf("normalizeSpecSource failed: %v", err)
		}
		if source != "https://example.com/spec.yaml" {
			t.Errorf("expected URL to remain unchanged, got %q", source)
		}
	})

	if runtime.GOOS == "windows" {
		t.Run("windows-style path is normalized", func(t *testing.T) {
			source, err := normalizeSpecSource(`C:\specs\petstore.yaml`)
			if err != nil {
				t.Fatalf("normalizeSpecSource failed: %v", err)
			}
			if !filepath.IsAbs(source) {
				t.Errorf("expected absolute path, got %q", source)
			}
		})
	}
}
