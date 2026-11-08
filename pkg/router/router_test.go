package router

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/nomagicln/open-bridge/pkg/cli"
	"github.com/nomagicln/open-bridge/pkg/config"
	"github.com/nomagicln/open-bridge/pkg/mcp"
	"github.com/nomagicln/open-bridge/pkg/request"
	"github.com/nomagicln/open-bridge/pkg/semantic"
	"github.com/nomagicln/open-bridge/pkg/spec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const testSpec = `openapi: "3.0.0"
info:
  title: Router Test API
  version: "1.0.0"
paths:
  /pets:
    get:
      operationId: listPets
      responses:
        "200":
          description: ok
`

// newTestRouter builds a Router with real components in an isolated config dir.
func newTestRouter(t *testing.T) (*Router, *config.Manager) {
	t.Helper()

	// Must be set before config.NewManager reads the environment.
	t.Setenv("OPENBRIDGE_CONFIG_DIR", t.TempDir())

	configMgr, err := config.NewManager()
	require.NoError(t, err)

	specParser := spec.NewParser()
	mapper := semantic.NewMapper()
	// A nil credential manager is supported by the request builder and keeps
	// these tests independent of the host keyring.
	reqBuilder := request.NewBuilder(nil)

	cliHandler := cli.NewHandler(specParser, mapper, reqBuilder, configMgr)
	mcpHandler := mcp.NewHandler(mapper, reqBuilder, nil)

	return NewRouter(cliHandler, mcpHandler, configMgr, specParser), configMgr
}

// installTestApp writes a minimal spec and persists an app config for it.
func installTestApp(t *testing.T, configMgr *config.Manager, appName string) {
	t.Helper()

	specPath := filepath.Join(t.TempDir(), "openapi.yaml")
	require.NoError(t, os.WriteFile(specPath, []byte(testSpec), 0o644))

	appConfig := config.NewAppConfig(appName, specPath)
	appConfig.AddProfile(config.NewProfile("default", "http://127.0.0.1:1"))
	require.NoError(t, configMgr.SaveAppConfig(appConfig))
}

func TestRouterExecuteAppUnknownApp(t *testing.T) {
	r, _ := newTestRouter(t)

	err := r.ExecuteApp("missing", []string{"--help"})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "app not found")
	assert.Contains(t, err.Error(), "missing")

	var notFound *config.AppNotFoundError
	assert.True(t, errors.As(err, &notFound), "app not found error should be wrapped with %%w")
}

func TestRouterExecuteAppHelp(t *testing.T) {
	r, configMgr := newTestRouter(t)
	installTestApp(t, configMgr, "demo")

	require.NoError(t, r.ExecuteApp("demo", []string{"--help"}))
}

func TestRouterExecuteAppResourceHelp(t *testing.T) {
	r, configMgr := newTestRouter(t)
	installTestApp(t, configMgr, "demo")

	// "ob <app> <resource> --help" must resolve through the CLI handler.
	require.NoError(t, r.ExecuteApp("demo", []string{"pets", "--help"}))
}

func TestRouterIsMCPMode(t *testing.T) {
	r, _ := newTestRouter(t)

	tests := []struct {
		name string
		args []string
		want bool
	}{
		{name: "bare flag", args: []string{"--mcp"}, want: true},
		{name: "flag after subcommand", args: []string{"list", "pets", "--mcp"}, want: true},
		{name: "no flag", args: []string{"list", "pets"}, want: false},
		{name: "empty args", args: nil, want: false},
		{name: "flag with value syntax is not recognized", args: []string{"--mcp=true"}, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, r.IsMCPMode(tt.args))
		})
	}
}

func TestRouterExecuteAppMCPModeUnknownApp(t *testing.T) {
	r, _ := newTestRouter(t)

	// The app lookup must fail before any MCP server is started.
	err := r.ExecuteApp("missing", []string{"--mcp"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "app not found")
}

func TestRouterExecuteShimMode(t *testing.T) {
	r, configMgr := newTestRouter(t)
	installTestApp(t, configMgr, "demo")

	// Shim invocations pass the full argv including the binary path.
	require.NoError(t, r.Execute([]string{"/usr/local/bin/demo", "--help"}))
}

func TestRouterExecuteNoArgs(t *testing.T) {
	r, _ := newTestRouter(t)

	err := r.Execute(nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no command")
}

// TestRouterParseMCPOptions proves the MCP server options are parsed from the
// application arguments, with the last occurrence of a repeated option winning.
func TestRouterParseMCPOptions(t *testing.T) {
	r, _ := newTestRouter(t)

	tests := []struct {
		name           string
		args           []string
		defaultProfile string
		expected       mcpOptions
	}{
		{
			name:           "default values",
			args:           []string{},
			defaultProfile: "default",
			expected: mcpOptions{
				profileName: "default",
				transport:   "stdio",
				port:        "8080",
			},
		},
		{
			name:           "with profile flag",
			args:           []string{"--profile", "prod"},
			defaultProfile: "default",
			expected: mcpOptions{
				profileName: "prod",
				transport:   "stdio",
				port:        "8080",
			},
		},
		{
			name:           "with profile short flag",
			args:           []string{"-p", "staging"},
			defaultProfile: "default",
			expected: mcpOptions{
				profileName: "staging",
				transport:   "stdio",
				port:        "8080",
			},
		},
		{
			name:           "with transport flag",
			args:           []string{"--transport", "sse"},
			defaultProfile: "default",
			expected: mcpOptions{
				profileName: "default",
				transport:   "sse",
				port:        "8080",
			},
		},
		{
			name:           "with port flag",
			args:           []string{"--port", "9090"},
			defaultProfile: "default",
			expected: mcpOptions{
				profileName: "default",
				transport:   "stdio",
				port:        "9090",
			},
		},
		{
			name:           "with all flags",
			args:           []string{"--profile", "prod", "--transport", "sse", "--port", "9090"},
			defaultProfile: "default",
			expected: mcpOptions{
				profileName: "prod",
				transport:   "sse",
				port:        "9090",
			},
		},
		{
			name:           "with equals syntax",
			args:           []string{"--profile=prod", "--transport=sse", "--port=9090"},
			defaultProfile: "default",
			expected: mcpOptions{
				profileName: "prod",
				transport:   "sse",
				port:        "9090",
			},
		},
		{
			name:           "mixed syntax keeps the last profile",
			args:           []string{"--profile", "prod", "--transport=sse", "-p", "staging"},
			defaultProfile: "default",
			expected: mcpOptions{
				profileName: "staging",
				transport:   "sse",
				port:        "8080",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, r.parseMCPOptions(tt.args, tt.defaultProfile))
		})
	}
}

// TestRouterDetectProfileLastWins proves a repeated profile option resolves to
// the last occurrence, matching the CLI convention.
func TestRouterDetectProfileLastWins(t *testing.T) {
	assert.Equal(t, "staging", detectProfile([]string{"--profile", "prod", "-p", "staging"}))
	assert.Equal(t, "prod", detectProfile([]string{"--profile=prod"}))
	assert.Equal(t, "", detectProfile([]string{"--profile"}))
	assert.Equal(t, "", detectProfile(nil))
}

// TestRouterSetServerVersion proves the injected version is used for the MCP
// server and that an empty version falls back to the default.
func TestRouterSetServerVersion(t *testing.T) {
	r, _ := newTestRouter(t)

	assert.Equal(t, defaultServerVersion, r.version)

	r.SetServerVersion("2.1.0")
	assert.Equal(t, "2.1.0", r.version)

	r.SetServerVersion("")
	assert.Equal(t, defaultServerVersion, r.version)
}

// TestRouterExecuteAppContextMCPModeOption proves the forwarded MCP option
// starts the MCP server even when the arguments do not request it.
func TestRouterExecuteAppContextMCPModeOption(t *testing.T) {
	r, configMgr := newTestRouter(t)
	installTestApp(t, configMgr, "demo")

	// An unsupported transport makes the MCP branch fail fast, which proves the
	// MCP server was selected.
	err := r.ExecuteAppContext(context.Background(), "demo", []string{"--transport", "bogus"}, AppOptions{MCPMode: true})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported transport")
}

// TestRouterExecuteAppContextProfileOption proves the forwarded profile is
// used when the arguments do not select one.
func TestRouterExecuteAppContextProfileOption(t *testing.T) {
	r, configMgr := newTestRouter(t)
	installTestApp(t, configMgr, "demo")

	err := r.ExecuteAppContext(context.Background(), "demo", []string{"--mcp"}, AppOptions{ProfileName: "missing"})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "profile 'missing' not found")
}
