package app

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/nomagicln/open-bridge/pkg/cli"
	"github.com/nomagicln/open-bridge/pkg/completion"
	"github.com/nomagicln/open-bridge/pkg/config"
	"github.com/nomagicln/open-bridge/pkg/credential"
	"github.com/nomagicln/open-bridge/pkg/mcp"
	"github.com/nomagicln/open-bridge/pkg/request"
	"github.com/nomagicln/open-bridge/pkg/router"
	"github.com/nomagicln/open-bridge/pkg/semantic"
	"github.com/nomagicln/open-bridge/pkg/spec"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newTestDeps builds a fully wired dependency graph inside an isolated config
// directory so tests never touch the real user configuration or keyring.
func newTestDeps(t *testing.T) *Deps {
	t.Helper()

	configDir := t.TempDir()
	t.Setenv("OPENBRIDGE_CONFIG_DIR", configDir)
	t.Setenv("HOME", t.TempDir())

	configMgr, err := config.NewManager()
	require.NoError(t, err)

	specParser := spec.NewParser()
	mapper := semantic.NewMapper()
	reqBuilder := request.NewBuilder(nil)
	cliHandler := cli.NewHandler(specParser, mapper, reqBuilder, configMgr)
	mcpHandler := mcp.NewHandler(mapper, reqBuilder, nil)
	appRouter := router.NewRouter(cliHandler, mcpHandler, configMgr, specParser)
	completionProvider := completion.NewProvider(configMgr, specParser, mapper)

	return &Deps{
		Config:     configMgr,
		Parser:     specParser,
		Credential: nil,
		Mapper:     mapper,
		Builder:    reqBuilder,
		CLI:        cliHandler,
		MCP:        mcpHandler,
		Router:     appRouter,
		Completion: completionProvider,
	}
}

func TestCreateCredentialFromParams(t *testing.T) {
	deps := newTestDeps(t)
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
			got := deps.createCredentialFromParams(tt.authType, tt.params)
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
	deps := newTestDeps(t)
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
			result := deps.mergeExistingConfig(tt.opts, existing)
			assert.Equal(t, tt.expected.SpecSource, result.SpecSource)
			assert.Equal(t, tt.expected.SpecSources, result.SpecSources)
			assert.Equal(t, tt.expected.Description, result.Description)
			assert.Equal(t, tt.expected.BaseURL, result.BaseURL)
			assert.Equal(t, tt.expected.AuthType, result.AuthType)
		})
	}
}

func TestParseMCPServerArgs(t *testing.T) {
	deps := newTestDeps(t)
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
			result := deps.parseMCPServerArgs(tt.args, tt.defaultProfile)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestParseArgValue(t *testing.T) {
	deps := newTestDeps(t)
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
			result := deps.parseArgValue(tt.arg, tt.args, tt.i, tt.longFlag, tt.shortFlag, tt.current)
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
	deps := newTestDeps(t)
	cmd := deps.newInstallCmd()
	testCmdWithSingleArg(t, cmd, "install <app-name>", "Install an API as a CLI application")
}

func TestNewUninstallCmd(t *testing.T) {
	deps := newTestDeps(t)
	cmd := deps.newUninstallCmd()
	testCmdWithSingleArg(t, cmd, "uninstall <app-name>", "Uninstall an API application")
}

func TestNewListCmd(t *testing.T) {
	deps := newTestDeps(t)
	cmd := deps.newListCmd()
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
	deps := newTestDeps(t)
	cmd := deps.newRunCmd()
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
	deps := newTestDeps(t)
	cmd := deps.newCompletionCmd()
	require.NotNil(t, cmd)
	assert.Equal(t, "completion [bash|zsh|fish]", cmd.Use)
	assert.Equal(t, "Generate shell completion script", cmd.Short)
	assert.NotEmpty(t, cmd.Long)
	assert.Equal(t, []string{"bash", "zsh", "fish"}, cmd.ValidArgs)
}

func TestUpdateAPIKeyName(t *testing.T) {
	deps := newTestDeps(t)
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

	// Inject the isolated config manager through Deps (no global state).
	deps.Config = mgr
	deps.updateAPIKeyName(appName, params)

	// Verify the key name was updated
	appConfig, err := mgr.GetAppConfig(appName)
	require.NoError(t, err)
	profile, ok := appConfig.Profiles["default"]
	require.True(t, ok)
	assert.Equal(t, "X-API-Key", profile.Auth.KeyName)

	// Test with empty key_name (should not update)
	deps.updateAPIKeyName(appName, map[string]string{})
	appConfig, err = mgr.GetAppConfig(appName)
	require.NoError(t, err)
	profile, ok = appConfig.Profiles["default"]
	require.True(t, ok)
	// Should still be "X-API-Key" from previous update
	assert.Equal(t, "X-API-Key", profile.Auth.KeyName)

	// Test with missing key_name
	deps.updateAPIKeyName(appName, map[string]string{"other": "value"})
	appConfig, err = mgr.GetAppConfig(appName)
	require.NoError(t, err)
	profile, ok = appConfig.Profiles["default"]
	require.True(t, ok)
	// Should still be "X-API-Key"
	assert.Equal(t, "X-API-Key", profile.Auth.KeyName)
}

// testAppConfigWithHeaders builds an AppConfig containing both regular and
// spec-fetch headers so redaction can be exercised on both fields.
func testAppConfigWithHeaders() *config.AppConfig {
	return &config.AppConfig{
		Name: "demo",
		Profiles: map[string]config.Profile{
			"default": {
				Name: "default",
				Headers: map[string]string{
					"Authorization": "Bearer super-secret-token",
					"X-Custom":      "public-value",
				},
				SpecFetchHeaders: map[string]string{
					"X-API-Key": "sk-live-1234567890",
					"Accept":    "application/json",
				},
			},
			"prod": {
				Name:    "prod",
				Headers: map[string]string{"X-Auth-Token": "prod-token-abcdef"},
			},
		},
	}
}

func TestRedactAppConfig(t *testing.T) {
	deps := newTestDeps(t)
	cfg := testAppConfigWithHeaders()

	redacted := deps.redactAppConfig(cfg)
	require.NotNil(t, redacted)

	// Sensitive header values must be masked.
	assert.NotEqual(t, "Bearer super-secret-token", redacted.Profiles["default"].Headers["Authorization"])
	assert.Contains(t, redacted.Profiles["default"].Headers["Authorization"], "***")
	assert.NotEqual(t, "sk-live-1234567890", redacted.Profiles["default"].SpecFetchHeaders["X-API-Key"])
	assert.NotEqual(t, "prod-token-abcdef", redacted.Profiles["prod"].Headers["X-Auth-Token"])

	// Non-sensitive header values must be preserved.
	assert.Equal(t, "public-value", redacted.Profiles["default"].Headers["X-Custom"])
	assert.Equal(t, "application/json", redacted.Profiles["default"].SpecFetchHeaders["Accept"])

	// The original config must not be mutated.
	assert.Equal(t, "Bearer super-secret-token", cfg.Profiles["default"].Headers["Authorization"])
	assert.Equal(t, "sk-live-1234567890", cfg.Profiles["default"].SpecFetchHeaders["X-API-Key"])

	// The copy must be deep: mutating the redacted headers must not leak back.
	redacted.Profiles["default"].Headers["X-Custom"] = "mutated"
	assert.Equal(t, "public-value", cfg.Profiles["default"].Headers["X-Custom"])

	// Nil-safe.
	assert.Nil(t, deps.redactAppConfig(nil))
}

func TestPrintAppConfigJSONRedactsHeaders(t *testing.T) {
	deps := newTestDeps(t)
	cfg := testAppConfigWithHeaders()

	var buf bytes.Buffer
	require.NoError(t, deps.printAppConfigJSON(&buf, cfg))

	out := buf.String()
	assert.NotContains(t, out, "super-secret-token")
	assert.NotContains(t, out, "sk-live-1234567890")
	assert.NotContains(t, out, "prod-token-abcdef")
	assert.Contains(t, out, "public-value")

	var decoded map[string]any
	require.NoError(t, json.Unmarshal(buf.Bytes(), &decoded))
	profiles, ok := decoded["Profiles"].(map[string]any)
	require.True(t, ok, "JSON output should contain a Profiles object")
	def, ok := profiles["default"].(map[string]any)
	require.True(t, ok)
	headers, ok := def["Headers"].(map[string]any)
	require.True(t, ok)
	assert.NotEqual(t, "Bearer super-secret-token", headers["Authorization"])
	assert.Equal(t, "public-value", headers["X-Custom"])
}

func TestPrintAppConfigYAMLRedactsHeaders(t *testing.T) {
	deps := newTestDeps(t)
	cfg := testAppConfigWithHeaders()

	var buf bytes.Buffer
	require.NoError(t, deps.printAppConfigYAML(&buf, cfg))

	out := buf.String()
	assert.NotContains(t, out, "super-secret-token")
	assert.NotContains(t, out, "sk-live-1234567890")
	assert.NotContains(t, out, "prod-token-abcdef")
	assert.Contains(t, out, "public-value")

	// The original config is still intact after serialization.
	assert.Equal(t, "Bearer super-secret-token", cfg.Profiles["default"].Headers["Authorization"])
}

func TestNewRootCmd(t *testing.T) {
	deps := newTestDeps(t)
	cmd := deps.newRootCmd()
	require.NotNil(t, cmd)
	assert.Equal(t, "ob", cmd.Use)
	assert.NotNil(t, cmd.Args)
	assert.NotNil(t, cmd.RunE)
	assert.NotEmpty(t, cmd.Long)
	assert.NotEmpty(t, cmd.Example)

	subcommands := make([]string, 0, len(cmd.Commands()))
	for _, sub := range cmd.Commands() {
		subcommands = append(subcommands, sub.Name())
	}
	assert.Subset(t, subcommands, []string{"install", "uninstall", "list", "info", "run", "completion"})
}

func TestRunRootCommandUnknownApp(t *testing.T) {
	deps := newTestDeps(t)
	cmd := deps.newRootCmd()

	err := deps.runRootCommand(cmd, []string{"definitely-not-an-app"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown command or app")
}

func TestPrintAppConfigTextShowsSpecMetadata(t *testing.T) {
	deps := newTestDeps(t)
	cfg := &config.AppConfig{Name: "demo", SpecTitle: "Pet API", SpecVersion: "7.8.9"}

	read, write, err := os.Pipe()
	require.NoError(t, err)
	originalStdout := os.Stdout
	os.Stdout = write
	deps.printAppConfigText(cfg)
	require.NoError(t, write.Close())
	os.Stdout = originalStdout

	data, err := io.ReadAll(read)
	require.NoError(t, err)
	out := string(data)
	assert.Contains(t, out, "Pet API")
	assert.Contains(t, out, "7.8.9")
}
