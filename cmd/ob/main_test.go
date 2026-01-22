package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/nomagicln/open-bridge/pkg/config"
	"github.com/nomagicln/open-bridge/pkg/credential"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCreateCredentialFromParams(t *testing.T) {
	tests := []struct {
		name     string
		authType string
		params   map[string]string
		want     *credential.Credential
	}{
		{
			name:     "bearer token",
			authType: "bearer",
			params:   map[string]string{"token": "test-token"},
			want:     credential.NewBearerCredential("test-token"),
		},
		{
			name:     "bearer token empty",
			authType: "bearer",
			params:   map[string]string{},
			want:     nil,
		},
		{
			name:     "api_key token",
			authType: "api_key",
			params:   map[string]string{"token": "test-api-key"},
			want:     credential.NewAPIKeyCredential("test-api-key"),
		},
		{
			name:     "api_key token empty",
			authType: "api_key",
			params:   map[string]string{},
			want:     nil,
		},
		{
			name:     "basic auth with both",
			authType: "basic",
			params:   map[string]string{"username": "user", "password": "pass"},
			want:     credential.NewBasicCredential("user", "pass"),
		},
		{
			name:     "basic auth with username only",
			authType: "basic",
			params:   map[string]string{"username": "user"},
			want:     credential.NewBasicCredential("user", ""),
		},
		{
			name:     "basic auth with password only",
			authType: "basic",
			params:   map[string]string{"password": "pass"},
			want:     credential.NewBasicCredential("", "pass"),
		},
		{
			name:     "basic auth empty",
			authType: "basic",
			params:   map[string]string{},
			want:     nil,
		},
		{
			name:     "none auth type",
			authType: "none",
			params:   map[string]string{},
			want:     nil,
		},
		{
			name:     "unknown auth type",
			authType: "unknown",
			params:   map[string]string{},
			want:     nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := createCredentialFromParams(tt.authType, tt.params)
			if tt.want == nil {
				assert.Nil(t, got)
			} else {
				require.NotNil(t, got)
				assert.Equal(t, tt.want.Type, got.Type)
				assert.Equal(t, tt.want.Token, got.Token)
				assert.Equal(t, tt.want.Username, got.Username)
				assert.Equal(t, tt.want.Password, got.Password)
			}
		})
	}
}

func TestMergeExistingConfig(t *testing.T) {
	existing := &config.AppConfig{
		SpecSource:  "existing-spec.yaml",
		SpecSources: []string{"spec1.yaml", "spec2.yaml"},
		Description: "Existing description",
		Profiles: map[string]config.Profile{
			"default": {
				BaseURL: "https://api.example.com",
				Auth: config.AuthConfig{
					Type: "bearer",
				},
			},
		},
		DefaultProfile: "default",
	}

	tests := []struct {
		name     string
		opts     config.InstallOptions
		expected config.InstallOptions
	}{
		{
			name: "merge all empty fields",
			opts: config.InstallOptions{},
			expected: config.InstallOptions{
				SpecSource:  "existing-spec.yaml",
				SpecSources: []string{"spec1.yaml", "spec2.yaml"},
				Description: "Existing description",
				BaseURL:     "https://api.example.com",
				AuthType:    "bearer",
			},
		},
		{
			name: "preserve non-empty fields",
			opts: config.InstallOptions{
				SpecSource:  "new-spec.yaml",
				Description: "New description",
				BaseURL:     "https://new-api.example.com",
				AuthType:    "api_key",
			},
			expected: config.InstallOptions{
				SpecSource:  "new-spec.yaml",
				SpecSources: nil, // When SpecSource is provided, SpecSources is not merged
				Description: "New description",
				BaseURL:     "https://new-api.example.com",
				AuthType:    "api_key",
			},
		},
		{
			name: "merge spec sources when spec source is empty",
			opts: config.InstallOptions{
				SpecSources: []string{"new-spec1.yaml"},
			},
			expected: config.InstallOptions{
				SpecSource:  "",                         // When SpecSources is provided, SpecSource is not merged
				SpecSources: []string{"new-spec1.yaml"}, // Preserved, not merged
				Description: "Existing description",
				BaseURL:     "https://api.example.com",
				AuthType:    "bearer",
			},
		},
		{
			name: "preserve spec sources when spec source is provided",
			opts: config.InstallOptions{
				SpecSource: "new-spec.yaml",
			},
			expected: config.InstallOptions{
				SpecSource:  "new-spec.yaml",
				SpecSources: nil, // When SpecSource is provided, SpecSources is not merged
				Description: "Existing description",
				BaseURL:     "https://api.example.com",
				AuthType:    "bearer",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := mergeExistingConfig(tt.opts, existing)
			assert.Equal(t, tt.expected.SpecSource, result.SpecSource)
			assert.Equal(t, tt.expected.SpecSources, result.SpecSources)
			assert.Equal(t, tt.expected.Description, result.Description)
			assert.Equal(t, tt.expected.BaseURL, result.BaseURL)
			assert.Equal(t, tt.expected.AuthType, result.AuthType)
		})
	}
}

func TestParseMCPServerArgs(t *testing.T) {
	tests := []struct {
		name           string
		args           []string
		defaultProfile string
		expected       mcpServerOptions
	}{
		{
			name:           "default values",
			args:           []string{},
			defaultProfile: "default",
			expected: mcpServerOptions{
				profileName: "default",
				transport:   "stdio",
				port:        "8080",
			},
		},
		{
			name:           "with profile flag",
			args:           []string{"--profile", "prod"},
			defaultProfile: "default",
			expected: mcpServerOptions{
				profileName: "prod",
				transport:   "stdio",
				port:        "8080",
			},
		},
		{
			name:           "with profile short flag",
			args:           []string{"-p", "staging"},
			defaultProfile: "default",
			expected: mcpServerOptions{
				profileName: "staging",
				transport:   "stdio",
				port:        "8080",
			},
		},
		{
			name:           "with transport flag",
			args:           []string{"--transport", "tcp"},
			defaultProfile: "default",
			expected: mcpServerOptions{
				profileName: "default",
				transport:   "tcp",
				port:        "8080",
			},
		},
		{
			name:           "with port flag",
			args:           []string{"--port", "9090"},
			defaultProfile: "default",
			expected: mcpServerOptions{
				profileName: "default",
				transport:   "stdio",
				port:        "9090",
			},
		},
		{
			name:           "with all flags",
			args:           []string{"--profile", "prod", "--transport", "tcp", "--port", "9090"},
			defaultProfile: "default",
			expected: mcpServerOptions{
				profileName: "prod",
				transport:   "tcp",
				port:        "9090",
			},
		},
		{
			name:           "with equals syntax",
			args:           []string{"--profile=prod", "--transport=tcp", "--port=9090"},
			defaultProfile: "default",
			expected: mcpServerOptions{
				profileName: "prod",
				transport:   "tcp",
				port:        "9090",
			},
		},
		{
			name:           "mixed syntax",
			args:           []string{"--profile", "prod", "--transport=tcp", "-p", "staging"},
			defaultProfile: "default",
			expected: mcpServerOptions{
				profileName: "staging", // Last one wins
				transport:   "tcp",
				port:        "8080",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := parseMCPServerArgs(tt.args, tt.defaultProfile)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestParseArgValue(t *testing.T) {
	tests := []struct {
		name      string
		arg       string
		args      []string
		i         int
		longFlag  string
		shortFlag string
		current   string
		expected  string
	}{
		{
			name:      "match long flag with next arg",
			arg:       "--profile",
			args:      []string{"--profile", "prod", "other"},
			i:         0,
			longFlag:  "--profile",
			shortFlag: "",
			current:   "default",
			expected:  "prod",
		},
		{
			name:      "match short flag with next arg",
			arg:       "-p",
			args:      []string{"-p", "prod", "other"},
			i:         0,
			longFlag:  "--profile",
			shortFlag: "-p",
			current:   "default",
			expected:  "prod",
		},
		{
			name:      "match long flag with equals",
			arg:       "--profile=prod",
			args:      []string{"--profile=prod"},
			i:         0,
			longFlag:  "--profile",
			shortFlag: "",
			current:   "default",
			expected:  "prod",
		},
		{
			name:      "no match returns current",
			arg:       "--other",
			args:      []string{"--other", "value"},
			i:         0,
			longFlag:  "--profile",
			shortFlag: "",
			current:   "default",
			expected:  "default",
		},
		{
			name:      "flag at end of args returns current",
			arg:       "--profile",
			args:      []string{"--profile"},
			i:         0,
			longFlag:  "--profile",
			shortFlag: "",
			current:   "default",
			expected:  "default",
		},
		{
			name:      "flag with equals but no value",
			arg:       "--profile=",
			args:      []string{"--profile="},
			i:         0,
			longFlag:  "--profile",
			shortFlag: "",
			current:   "default",
			expected:  "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := parseArgValue(tt.arg, tt.args, tt.i, tt.longFlag, tt.shortFlag, tt.current)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// testCmdWithSingleArg tests a command that requires a single argument
func testCmdWithSingleArg(t *testing.T, cmd *cobra.Command, expectedUse, expectedShort string) {
	require.NotNil(t, cmd)
	assert.Equal(t, expectedUse, cmd.Use)
	assert.Equal(t, expectedShort, cmd.Short)
	assert.NotEmpty(t, cmd.Long)
	assert.NotNil(t, cmd.Args)
	// Test that Args function works correctly
	err := cmd.Args(cmd, []string{"testapp"})
	assert.NoError(t, err)
	err = cmd.Args(cmd, []string{})
	assert.Error(t, err)
}

func TestNewInstallCmd(t *testing.T) {
	cmd := newInstallCmd()
	testCmdWithSingleArg(t, cmd, "install <app-name>", "Install an API as a CLI application")
}

func TestNewUninstallCmd(t *testing.T) {
	cmd := newUninstallCmd()
	testCmdWithSingleArg(t, cmd, "uninstall <app-name>", "Uninstall an API application")
}

func TestNewListCmd(t *testing.T) {
	cmd := newListCmd()
	require.NotNil(t, cmd)
	assert.Equal(t, "list", cmd.Use)
	assert.Equal(t, "List installed API applications", cmd.Short)
	assert.NotEmpty(t, cmd.Long)
	assert.NotNil(t, cmd.Args)
	// Test that Args function works correctly
	err := cmd.Args(cmd, []string{})
	assert.NoError(t, err)
	err = cmd.Args(cmd, []string{"extra"})
	assert.Error(t, err)
}

func TestNewRunCmd(t *testing.T) {
	cmd := newRunCmd()
	require.NotNil(t, cmd)
	assert.Equal(t, "run <app-name> [args...]", cmd.Use)
	assert.Equal(t, "Run commands for an installed API application", cmd.Short)
	assert.NotEmpty(t, cmd.Long)
	assert.NotNil(t, cmd.Args)
	assert.True(t, cmd.DisableFlagParsing)
	// Test that Args function works correctly
	err := cmd.Args(cmd, []string{"testapp"})
	assert.NoError(t, err)
	err = cmd.Args(cmd, []string{})
	assert.Error(t, err)
}

func TestNewCompletionCmd(t *testing.T) {
	cmd := newCompletionCmd()
	require.NotNil(t, cmd)
	assert.Equal(t, "completion [bash|zsh|fish]", cmd.Use)
	assert.Equal(t, "Generate shell completion script", cmd.Short)
	assert.NotEmpty(t, cmd.Long)
	assert.Equal(t, []string{"bash", "zsh", "fish"}, cmd.ValidArgs)
}

func TestUpdateAPIKeyName(t *testing.T) {
	// This test requires a real config manager, so we'll set up a test environment
	tmpDir := t.TempDir()

	// Create a temporary config manager
	mgr, err := config.NewManager(config.WithConfigDir(tmpDir))
	require.NoError(t, err)

	// Create a test app config
	appName := "testapp"
	specPath := filepath.Join(tmpDir, "spec.yaml")
	specContent := `
openapi: "3.0.0"
info:
  title: Test API
  version: "1.0.0"
paths: {}
`
	require.NoError(t, os.WriteFile(specPath, []byte(specContent), 0644))

	_, err = mgr.InstallApp(appName, config.InstallOptions{
		SpecSource: specPath,
		BaseURL:    "https://api.example.com",
		CreateShim: false,
	})
	require.NoError(t, err)

	// Test updating API key name
	params := map[string]string{
		"key_name": "X-API-Key",
	}

	// We need to temporarily replace the global configMgr
	// Since we can't easily do that, we'll test the logic indirectly
	// by checking that the function doesn't panic with valid input
	originalConfigMgr := configMgr
	defer func() {
		configMgr = originalConfigMgr
	}()

	// Temporarily set configMgr for this test
	configMgr = mgr

	// Call the function
	updateAPIKeyName(appName, params)

	// Verify the key name was updated
	appConfig, err := mgr.GetAppConfig(appName)
	require.NoError(t, err)
	profile, ok := appConfig.Profiles["default"]
	require.True(t, ok)
	assert.Equal(t, "X-API-Key", profile.Auth.KeyName)

	// Test with empty key_name (should not update)
	updateAPIKeyName(appName, map[string]string{})
	appConfig, err = mgr.GetAppConfig(appName)
	require.NoError(t, err)
	profile, ok = appConfig.Profiles["default"]
	require.True(t, ok)
	// Should still be "X-API-Key" from previous update
	assert.Equal(t, "X-API-Key", profile.Auth.KeyName)

	// Test with missing key_name
	updateAPIKeyName(appName, map[string]string{"other": "value"})
	appConfig, err = mgr.GetAppConfig(appName)
	require.NoError(t, err)
	profile, ok = appConfig.Profiles["default"]
	require.True(t, ok)
	// Should still be "X-API-Key"
	assert.Equal(t, "X-API-Key", profile.Auth.KeyName)
}
