package main

import (
	"testing"

	"github.com/nomagicln/open-bridge/internal/app"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestAppNewInIsolatedEnvironment verifies that the thin entry point can build
// the full dependency graph without relying on package-level globals. The
// config directory and HOME are isolated so the test never touches user state.
func TestAppNewInIsolatedEnvironment(t *testing.T) {
	t.Setenv("OPENBRIDGE_CONFIG_DIR", t.TempDir())
	t.Setenv("HOME", t.TempDir())

	deps, err := app.New()
	require.NoError(t, err)
	require.NotNil(t, deps)

	assert.NotNil(t, deps.Config)
	assert.NotNil(t, deps.Parser)
	assert.NotNil(t, deps.Mapper)
	assert.NotNil(t, deps.Builder)
	assert.NotNil(t, deps.CLI)
	assert.NotNil(t, deps.MCP)
	assert.NotNil(t, deps.Router)
	assert.NotNil(t, deps.Completion)
}

// TestBuildInfoWiring verifies that linker-injected build information reaches
// the application package (the variables themselves are set via ldflags).
func TestBuildInfoWiring(t *testing.T) {
	app.SetBuildInfo(version, commit, date)
	deps, err := app.New()
	require.NoError(t, err)
	require.NotNil(t, deps)
}
