package router

import (
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
