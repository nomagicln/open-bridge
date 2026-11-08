//go:build integration

package app

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/nomagicln/open-bridge/internal/install"
	"github.com/nomagicln/open-bridge/pkg/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// entrypointSpec builds a spec whose server URL selects the default profile.
func entrypointSpec(serverURL string) string {
	return `openapi: "3.0.0"
info:
  title: Entrypoint API
  version: "1.0.0"
servers:
  - url: ` + serverURL + `
paths:
  /users:
    get:
      operationId: listUsers
      summary: List all users
      responses:
        "200":
          description: Success
          content:
            application/json:
              schema:
                type: array
                items:
                  type: object
`
}

// TestRootProfileOptionReachesTheApp proves the root-level profile option is
// forwarded to the app handler, and that a profile given with the app
// arguments wins over the root-level one.
func TestRootProfileOptionReachesTheApp(t *testing.T) {
	var defaultCalls, stagingCalls atomic.Int32

	defaultServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defaultCalls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"users": []}`))
	}))
	defer defaultServer.Close()

	stagingServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		stagingCalls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"users": []}`))
	}))
	defer stagingServer.Close()

	specPath := filepath.Join(t.TempDir(), "spec.yaml")
	if err := os.WriteFile(specPath, []byte(entrypointSpec(defaultServer.URL)), 0o644); err != nil {
		t.Fatalf("failed to write spec: %v", err)
	}

	// The CLI writes the response to os.Stdout; send it to a file so the test
	// output stays readable.
	output, err := os.CreateTemp(t.TempDir(), "stdout")
	if err != nil {
		t.Fatalf("failed to create output file: %v", err)
	}
	originalStdout := os.Stdout
	os.Stdout = output
	t.Cleanup(func() {
		os.Stdout = originalStdout
		_ = output.Close()
	})

	deps := newTestDeps(t)

	if _, err := deps.Installer.InstallApp("testapi", install.InstallOptions{
		SpecSource: specPath,
		CreateShim: false,
	}); err != nil {
		t.Fatalf("InstallApp failed: %v", err)
	}

	appConfig, err := deps.Config.GetAppConfig("testapi")
	if err != nil {
		t.Fatalf("GetAppConfig failed: %v", err)
	}
	appConfig.AddProfile(config.NewProfile("staging", stagingServer.URL))
	if err := deps.Config.SaveAppConfig(appConfig); err != nil {
		t.Fatalf("SaveAppConfig failed: %v", err)
	}

	t.Run("root profile is forwarded", func(t *testing.T) {
		if err := deps.RunContext(context.Background(), []string{"ob", "--profile", "staging", "testapi", "users", "list"}); err != nil {
			t.Fatalf("RunContext failed: %v", err)
		}
		if stagingCalls.Load() != 1 {
			t.Errorf("staging server calls = %d, want 1", stagingCalls.Load())
		}
		if defaultCalls.Load() != 0 {
			t.Errorf("default server calls = %d, want 0", defaultCalls.Load())
		}
	})

	t.Run("root profile is forwarded by the run command", func(t *testing.T) {
		stagingBefore := stagingCalls.Load()
		if err := deps.RunContext(context.Background(), []string{"ob", "--profile", "staging", "run", "testapi", "users", "list"}); err != nil {
			t.Fatalf("RunContext failed: %v", err)
		}
		if stagingCalls.Load() != stagingBefore+1 {
			t.Errorf("staging server calls = %d, want %d", stagingCalls.Load(), stagingBefore+1)
		}
	})

	t.Run("app profile wins over the root profile", func(t *testing.T) {
		defaultBefore := defaultCalls.Load()
		stagingBefore := stagingCalls.Load()

		if err := deps.RunContext(context.Background(), []string{"ob", "--profile", "staging", "testapi", "users", "list", "--profile", "default"}); err != nil {
			t.Fatalf("RunContext failed: %v", err)
		}
		if defaultCalls.Load() != defaultBefore+1 {
			t.Errorf("default server calls = %d, want %d", defaultCalls.Load(), defaultBefore+1)
		}
		if stagingCalls.Load() != stagingBefore {
			t.Errorf("staging server calls = %d, want %d", stagingCalls.Load(), stagingBefore)
		}
	})
}

// TestRunCommandReachesTheMCPServer proves the "ob run" entry point uses the
// same execution path as the other entry points, including the MCP server.
func TestRunCommandReachesTheMCPServer(t *testing.T) {
	specPath := filepath.Join(t.TempDir(), "spec.yaml")
	if err := os.WriteFile(specPath, []byte(entrypointSpec("http://127.0.0.1:1")), 0o644); err != nil {
		t.Fatalf("failed to write spec: %v", err)
	}

	deps := newTestDeps(t)

	if _, err := deps.Installer.InstallApp("testapi", install.InstallOptions{
		SpecSource: specPath,
		CreateShim: false,
	}); err != nil {
		t.Fatalf("InstallApp failed: %v", err)
	}

	// An unsupported transport makes the MCP branch fail fast, which proves the
	// run command reached the MCP server.
	err := deps.RunContext(context.Background(), []string{"ob", "run", "testapi", "--mcp", "--transport", "bogus"})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported transport")
}

func TestRootOutputFormatReachesTheApp(t *testing.T) {
	specPath := filepath.Join(t.TempDir(), "spec.yaml")
	if err := os.WriteFile(specPath, []byte(entrypointSpec("http://127.0.0.1:1")), 0o644); err != nil {
		t.Fatalf("failed to write spec: %v", err)
	}

	output, err := os.CreateTemp(t.TempDir(), "stdout")
	if err != nil {
		t.Fatalf("failed to create output file: %v", err)
	}
	originalStdout := os.Stdout
	os.Stdout = output
	t.Cleanup(func() {
		os.Stdout = originalStdout
		_ = output.Close()
	})

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"users": []}`))
	}))
	defer server.Close()

	deps := newTestDeps(t)

	if _, err := deps.Installer.InstallApp("testapi", install.InstallOptions{
		SpecSource: specPath,
		CreateShim: false,
	}); err != nil {
		t.Fatalf("InstallApp failed: %v", err)
	}

	appConfig, err := deps.Config.GetAppConfig("testapi")
	if err != nil {
		t.Fatalf("GetAppConfig failed: %v", err)
	}
	profile, ok := appConfig.GetProfile("default")
	if !ok {
		t.Fatal("expected a default profile")
	}
	profile.BaseURL = server.URL
	appConfig.AddProfile(*profile)
	if err := deps.Config.SaveAppConfig(appConfig); err != nil {
		t.Fatalf("SaveAppConfig failed: %v", err)
	}

	readOutput := func() string {
		content, err := os.ReadFile(output.Name())
		require.NoError(t, err)
		return string(content)
	}
	resetOutput := func() {
		require.NoError(t, output.Truncate(0))
		_, err := output.Seek(0, io.SeekStart)
		require.NoError(t, err)
	}

	t.Run("root output format is forwarded", func(t *testing.T) {
		resetOutput()
		require.NoError(t, deps.RunContext(context.Background(), []string{"ob", "--output", "json", "testapi", "users", "list"}))
		assert.Contains(t, readOutput(), `"users"`)
	})

	t.Run("app output format wins over the root one", func(t *testing.T) {
		resetOutput()
		require.NoError(t, deps.RunContext(context.Background(), []string{"ob", "--output", "yaml", "testapi", "users", "list", "--json"}))
		assert.Contains(t, readOutput(), `"users"`)
	})
}
