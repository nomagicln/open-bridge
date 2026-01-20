// Package integration provides end-to-end integration tests for OpenBridge.
package integration

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nomagicln/open-bridge/pkg/cli"
	"github.com/nomagicln/open-bridge/pkg/config"
	"github.com/nomagicln/open-bridge/pkg/credential"
	"github.com/nomagicln/open-bridge/pkg/mcp"
	"github.com/nomagicln/open-bridge/pkg/request"
	"github.com/nomagicln/open-bridge/pkg/semantic"
	"github.com/nomagicln/open-bridge/pkg/spec"
)

// testEnv provides a complete test environment for integration tests.
type testEnv struct {
	t          *testing.T
	configDir  string
	configMgr  *config.Manager
	specParser *spec.Parser
	credMgr    *credential.Manager
	mapper     *semantic.Mapper
	reqBuilder *request.Builder
	cliHandler *cli.Handler
	mcpHandler *mcp.Handler
	mockServer *httptest.Server
}

// newTestEnv creates a new test environment with all components initialized.
func newTestEnv(t *testing.T) *testEnv {
	t.Helper()

	configDir := t.TempDir()

	configMgr, err := config.NewManager(config.WithConfigDir(configDir))
	if err != nil {
		t.Fatalf("failed to create config manager: %v", err)
	}

	specParser := spec.NewParser()

	credMgr, err := credential.NewManager()
	if err != nil {
		// Use nil if credential manager fails (expected in some test environments)
		credMgr = nil
	}

	mapper := semantic.NewMapper()
	reqBuilder := request.NewBuilder(credMgr)
	cliHandler := cli.NewHandler(specParser, mapper, reqBuilder, configMgr)
	mcpHandler := mcp.NewHandler(mapper, reqBuilder, nil)

	return &testEnv{
		t:          t,
		configDir:  configDir,
		configMgr:  configMgr,
		specParser: specParser,
		credMgr:    credMgr,
		mapper:     mapper,
		reqBuilder: reqBuilder,
		cliHandler: cliHandler,
		mcpHandler: mcpHandler,
	}
}

// createMockServer creates a mock API server.
func (e *testEnv) createMockServer(handlers map[string]http.HandlerFunc) *httptest.Server {
	e.mockServer = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := r.Method + " " + r.URL.Path
		if handler, ok := handlers[key]; ok {
			handler(w, r)
			return
		}
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]string{"error": "not found"})
	}))
	return e.mockServer
}

// cleanup cleans up test resources.
func (e *testEnv) cleanup() {
	if e.mockServer != nil {
		e.mockServer.Close()
	}
}

// writeSpec writes an OpenAPI spec to a temp file.
func (e *testEnv) writeSpec(specContent string) string {
	specPath := filepath.Join(e.t.TempDir(), "spec.yaml")
	if err := os.WriteFile(specPath, []byte(specContent), 0644); err != nil {
		e.t.Fatalf("failed to write spec: %v", err)
	}
	return specPath
}

// TestFullCLIWorkflow tests the complete CLI workflow: install, execute commands, uninstall.
func TestFullCLIWorkflow(t *testing.T) {
	env := newTestEnv(t)
	defer env.cleanup()

	// Create mock API server
	mockServer := env.createMockServer(map[string]http.HandlerFunc{
		"GET /users": func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode([]map[string]any{
				{"id": 1, "name": "Alice", "email": "alice@example.com"},
				{"id": 2, "name": "Bob", "email": "bob@example.com"},
			})
		},
		"GET /users/1": func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]any{
				"id": 1, "name": "Alice", "email": "alice@example.com",
			})
		},
		"POST /users": func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusCreated)
			json.NewEncoder(w).Encode(map[string]any{
				"id": 3, "name": "Charlie", "email": "charlie@example.com",
			})
		},
		"DELETE /users/1": func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNoContent)
		},
	})

	// Create spec with mock server URL
	specContent := createTestSpec(mockServer.URL)
	specPath := env.writeSpec(specContent)

	// Step 1: Install app
	t.Run("Install", func(t *testing.T) {
		result, err := env.configMgr.InstallApp("testapi", config.InstallOptions{
			SpecSource: specPath,
			CreateShim: false,
		})
		if err != nil {
			t.Fatalf("InstallApp failed: %v", err)
		}

		if result.AppName != "testapi" {
			t.Errorf("expected app name 'testapi', got '%s'", result.AppName)
		}

		// Verify app is listed
		apps, err := env.configMgr.ListApps()
		if err != nil {
			t.Fatalf("ListApps failed: %v", err)
		}
		if len(apps) != 1 || apps[0] != "testapi" {
			t.Errorf("expected ['testapi'], got %v", apps)
		}
	})

	// Step 2: Get app config
	var appConfig *config.AppConfig
	t.Run("GetAppConfig", func(t *testing.T) {
		var err error
		appConfig, err = env.configMgr.GetAppConfig("testapi")
		if err != nil {
			t.Fatalf("GetAppConfig failed: %v", err)
		}

		if appConfig.Name != "testapi" {
			t.Errorf("expected name 'testapi', got '%s'", appConfig.Name)
		}
	})

	// Step 3: Execute list command
	t.Run("ExecuteListCommand", func(t *testing.T) {
		// Note: We can't easily capture stdout in this test setup
		// In a real scenario, we'd redirect stdout
		err := env.cliHandler.ExecuteCommand("testapi", appConfig, []string{"list", "users", "--json"})
		if err != nil {
			t.Errorf("ExecuteCommand (list) failed: %v", err)
		}
	})

	// Step 4: Execute get command
	t.Run("ExecuteGetCommand", func(t *testing.T) {
		err := env.cliHandler.ExecuteCommand("testapi", appConfig, []string{"get", "users", "--id", "1", "--json"})
		if err != nil {
			t.Errorf("ExecuteCommand (get) failed: %v", err)
		}
	})

	// Step 5: Uninstall app
	t.Run("Uninstall", func(t *testing.T) {
		err := env.configMgr.UninstallApp("testapi", false)
		if err != nil {
			t.Fatalf("UninstallApp failed: %v", err)
		}

		// Verify app is removed
		apps, err := env.configMgr.ListApps()
		if err != nil {
			t.Fatalf("ListApps failed: %v", err)
		}
		if len(apps) != 0 {
			t.Errorf("expected no apps, got %v", apps)
		}
	})
}

// TestMCPServerWorkflow tests the MCP server workflow: list tools, call tool.
func TestMCPServerWorkflow(t *testing.T) {
	env := newTestEnv(t)
	defer env.cleanup()

	// Create mock API server
	mockServer := env.createMockServer(map[string]http.HandlerFunc{
		"GET /users": func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode([]map[string]any{
				{"id": 1, "name": "Alice"},
			})
		},
	})

	// Create and load spec
	specContent := createTestSpec(mockServer.URL)
	specPath := env.writeSpec(specContent)

	specDoc, err := env.specParser.LoadSpec(specPath)
	if err != nil {
		t.Fatalf("failed to load spec: %v", err)
	}

	// Install app for credentials/config
	_, err = env.configMgr.InstallApp("testapi", config.InstallOptions{
		SpecSource: specPath,
		CreateShim: false,
	})
	if err != nil {
		t.Fatalf("InstallApp failed: %v", err)
	}

	appConfig, _ := env.configMgr.GetAppConfig("testapi")

	// Setup MCP handler
	env.mcpHandler.SetSpec(specDoc)
	env.mcpHandler.SetAppConfig(appConfig, "default")

	// Test 1: Build MCP tools
	t.Run("BuildMCPTools", func(t *testing.T) {
		tools := env.mcpHandler.BuildMCPTools(specDoc, nil)
		if len(tools) == 0 {
			t.Error("expected at least one tool")
		}

		// Verify tool structure
		for _, tool := range tools {
			if tool.Name == "" {
				t.Error("tool name should not be empty")
			}
			if tool.InputSchema == nil {
				t.Error("tool input schema should not be nil")
			}
		}
	})

	// Test 2: Handle tools/list request
	t.Run("HandleToolsList", func(t *testing.T) {
		req := mcp.Request{
			JSONRPC: "2.0",
			ID:      1,
			Method:  "tools/list",
		}
		reqData, _ := json.Marshal(req)

		resp, err := env.mcpHandler.HandleRequest(reqData)
		if err != nil {
			t.Fatalf("HandleRequest failed: %v", err)
		}

		if resp.Error != nil {
			t.Errorf("unexpected error: %v", resp.Error)
		}
	})

	// Test 3: Handle tools/call request
	t.Run("HandleToolsCall", func(t *testing.T) {
		req := mcp.Request{
			JSONRPC: "2.0",
			ID:      2,
			Method:  "tools/call",
			Params: map[string]any{
				"name":      "listUsers",
				"arguments": map[string]any{},
			},
		}
		reqData, _ := json.Marshal(req)

		resp, err := env.mcpHandler.HandleRequest(reqData)
		if err != nil {
			t.Fatalf("HandleRequest failed: %v", err)
		}

		if resp.Error != nil {
			t.Errorf("unexpected error: code=%d message=%s", resp.Error.Code, resp.Error.Message)
		}
	})

	// Test 4: Apply safety controls (read-only mode)
	t.Run("SafetyControls", func(t *testing.T) {
		tools := env.mcpHandler.BuildMCPTools(specDoc, &config.SafetyConfig{
			ReadOnlyMode: true,
		})

		// In read-only mode, only GET operations should be available
		for _, tool := range tools {
			if strings.HasPrefix(tool.Name, "create") || strings.HasPrefix(tool.Name, "delete") {
				t.Errorf("tool '%s' should be filtered in read-only mode", tool.Name)
			}
		}
	})
}

// TestProfileSwitching tests switching between profiles.
func TestProfileSwitching(t *testing.T) {
	env := newTestEnv(t)
	defer env.cleanup()

	// Create spec
	specContent := createTestSpec("http://localhost:8080")
	specPath := env.writeSpec(specContent)

	// Install app
	_, err := env.configMgr.InstallApp("testapi", config.InstallOptions{
		SpecSource: specPath,
		CreateShim: false,
	})
	if err != nil {
		t.Fatalf("InstallApp failed: %v", err)
	}

	// Create additional profiles
	t.Run("CreateProfile", func(t *testing.T) {
		profileMgr, err := env.configMgr.NewProfileManager("testapi")
		if err != nil {
			t.Fatalf("NewProfileManager failed: %v", err)
		}

		err = profileMgr.CreateProfile("staging", config.ProfileOptions{
			BaseURL: "http://staging.example.com",
		})
		if err != nil {
			t.Fatalf("CreateProfile failed: %v", err)
		}

		err = profileMgr.CreateProfile("production", config.ProfileOptions{
			BaseURL: "http://production.example.com",
		})
		if err != nil {
			t.Fatalf("CreateProfile (production) failed: %v", err)
		}
	})

	// List profiles
	t.Run("ListProfiles", func(t *testing.T) {
		appConfig, err := env.configMgr.GetAppConfig("testapi")
		if err != nil {
			t.Fatalf("GetAppConfig failed: %v", err)
		}

		profiles := appConfig.ListProfiles()
		if len(profiles) != 3 {
			t.Errorf("expected 3 profiles, got %d: %v", len(profiles), profiles)
		}
	})

	// Switch default profile
	t.Run("SwitchProfile", func(t *testing.T) {
		appConfig, _ := env.configMgr.GetAppConfig("testapi")
		err := appConfig.SetDefaultProfile("staging")
		if err != nil {
			t.Fatalf("SetDefaultProfile failed: %v", err)
		}

		if err := env.configMgr.SaveAppConfig(appConfig); err != nil {
			t.Fatalf("SaveAppConfig failed: %v", err)
		}

		// Reload and verify
		appConfig, _ = env.configMgr.GetAppConfig("testapi")
		if appConfig.DefaultProfile != "staging" {
			t.Errorf("expected default profile 'staging', got '%s'", appConfig.DefaultProfile)
		}
	})

	// Get specific profile
	t.Run("GetProfile", func(t *testing.T) {
		profileMgr, err := env.configMgr.NewProfileManager("testapi")
		if err != nil {
			t.Fatalf("NewProfileManager failed: %v", err)
		}

		profile, err := profileMgr.GetProfile("production")
		if err != nil {
			t.Fatalf("GetProfile failed: %v", err)
		}

		if profile.BaseURL != "http://production.example.com" {
			t.Errorf("expected base URL 'http://production.example.com', got '%s'", profile.BaseURL)
		}
	})

	// Delete profile
	t.Run("DeleteProfile", func(t *testing.T) {
		appConfig, _ := env.configMgr.GetAppConfig("testapi")
		err := appConfig.DeleteProfile("production")
		if err != nil {
			t.Fatalf("DeleteProfile failed: %v", err)
		}

		if err := env.configMgr.SaveAppConfig(appConfig); err != nil {
			t.Fatalf("SaveAppConfig failed: %v", err)
		}

		profiles := appConfig.ListProfiles()
		for _, p := range profiles {
			if p == "production" {
				t.Error("profile 'production' should have been deleted")
			}
		}
	})
}

// TestErrorScenarios tests various error scenarios.
func TestErrorScenarios(t *testing.T) {
	env := newTestEnv(t)
	defer env.cleanup()

	// Test 1: Install with invalid spec
	t.Run("InstallInvalidSpec", func(t *testing.T) {
		_, err := env.configMgr.InstallApp("badapi", config.InstallOptions{
			SpecSource: "/nonexistent/spec.yaml",
			CreateShim: false,
		})
		if err == nil {
			t.Error("expected error for invalid spec")
		}
	})

	// Test 2: Install duplicate app
	t.Run("InstallDuplicate", func(t *testing.T) {
		specContent := createTestSpec("http://localhost:8080")
		specPath := env.writeSpec(specContent)

		_, err := env.configMgr.InstallApp("dupapi", config.InstallOptions{
			SpecSource: specPath,
			CreateShim: false,
		})
		if err != nil {
			t.Fatalf("first install failed: %v", err)
		}

		_, err = env.configMgr.InstallApp("dupapi", config.InstallOptions{
			SpecSource: specPath,
			CreateShim: false,
		})
		if err == nil {
			t.Error("expected error for duplicate app")
		}
	})

	// Test 3: Uninstall non-existent app
	t.Run("UninstallNonExistent", func(t *testing.T) {
		err := env.configMgr.UninstallApp("nonexistent", false)
		if err == nil {
			t.Error("expected error for non-existent app")
		}
	})

	// Test 4: Get non-existent app config
	t.Run("GetNonExistentConfig", func(t *testing.T) {
		_, err := env.configMgr.GetAppConfig("nonexistent")
		if err == nil {
			t.Error("expected error for non-existent app")
		}
	})

	// Test 5: Invalid JSON-RPC request
	t.Run("InvalidJSONRPC", func(t *testing.T) {
		resp, _ := env.mcpHandler.HandleRequest([]byte("invalid json"))
		if resp.Error == nil {
			t.Error("expected error for invalid JSON")
		}
		if resp.Error.Code != mcp.ErrCodeParseError {
			t.Errorf("expected parse error code, got %d", resp.Error.Code)
		}
	})

	// Test 6: Unknown MCP method
	t.Run("UnknownMCPMethod", func(t *testing.T) {
		req := mcp.Request{
			JSONRPC: "2.0",
			ID:      1,
			Method:  "unknown/method",
		}
		reqData, _ := json.Marshal(req)

		resp, _ := env.mcpHandler.HandleRequest(reqData)
		if resp.Error == nil {
			t.Error("expected error for unknown method")
		}
		if resp.Error.Code != mcp.ErrCodeMethodNotFound {
			t.Errorf("expected method not found error, got %d", resp.Error.Code)
		}
	})
}

// TestMCPServerStartup tests MCP server startup and shutdown.
func TestMCPServerStartup(t *testing.T) {
	env := newTestEnv(t)
	defer env.cleanup()

	// Create a pipe to simulate stdin/stdout
	inReader, inWriter, _ := os.Pipe()
	outReader, outWriter, _ := os.Pipe()

	// Create server
	server := mcp.NewServer(env.mcpHandler, inReader, outWriter)

	// Start server in background
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	errChan := make(chan error, 1)
	go func() {
		errChan <- server.Serve(ctx)
	}()

	// Send a request
	req := mcp.Request{
		JSONRPC: "2.0",
		ID:      1,
		Method:  "tools/list",
	}
	reqData, _ := json.Marshal(req)
	_, _ = inWriter.Write(append(reqData, '\n'))
	_ = inWriter.Close()

	// Read response (with timeout)
	buf := make([]byte, 4096)
	_ = outReader.SetReadDeadline(time.Now().Add(time.Second))
	n, _ := outReader.Read(buf)

	if n > 0 {
		var resp mcp.Response
		if err := json.Unmarshal(buf[:n], &resp); err != nil {
			t.Logf("response was: %s", string(buf[:n]))
		} else {
			if resp.JSONRPC != "2.0" {
				t.Errorf("expected jsonrpc 2.0, got %s", resp.JSONRPC)
			}
		}
	}

	// Cancel and wait for shutdown
	cancel()
	select {
	case err := <-errChan:
		if err != nil && err != context.Canceled {
			t.Logf("server error (expected on shutdown): %v", err)
		}
	case <-time.After(time.Second):
		// Timeout is OK for this test
	}
}

// createTestSpec creates a test OpenAPI spec with the given server URL.
func createTestSpec(serverURL string) string {
	return `openapi: "3.0.0"
info:
  title: Test API
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
    post:
      operationId: createUser
      summary: Create a user
      requestBody:
        required: true
        content:
          application/json:
            schema:
              type: object
              properties:
                name:
                  type: string
                email:
                  type: string
      responses:
        "201":
          description: Created
  /users/{id}:
    get:
      operationId: getUser
      summary: Get a user by ID
      parameters:
        - name: id
          in: path
          required: true
          schema:
            type: string
      responses:
        "200":
          description: Success
    delete:
      operationId: deleteUser
      summary: Delete a user
      parameters:
        - name: id
          in: path
          required: true
          schema:
            type: string
      responses:
        "204":
          description: Deleted
`
}

// TestOutputFormatting tests different output formats.
func TestOutputFormatting(t *testing.T) {
	env := newTestEnv(t)
	defer env.cleanup()

	testCases := []struct {
		name   string
		body   []byte
		format string
		check  func(t *testing.T, output string)
	}{
		{
			name:   "JSON format",
			body:   []byte(`{"id":1,"name":"test"}`),
			format: "json",
			check: func(t *testing.T, output string) {
				if !strings.Contains(output, "\"id\"") {
					t.Error("JSON output should contain 'id' key")
				}
			},
		},
		{
			name:   "YAML format",
			body:   []byte(`{"id":1,"name":"test"}`),
			format: "yaml",
			check: func(t *testing.T, output string) {
				if !strings.Contains(output, "id:") {
					t.Error("YAML output should contain 'id:' key")
				}
			},
		},
		{
			name:   "Table format array",
			body:   []byte(`[{"id":1,"name":"Alice"},{"id":2,"name":"Bob"}]`),
			format: "table",
			check: func(t *testing.T, output string) {
				if !strings.Contains(output, "Alice") || !strings.Contains(output, "Bob") {
					t.Error("Table output should contain data values")
				}
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			output, err := env.cliHandler.FormatOutput(tc.body, tc.format)
			if err != nil {
				t.Fatalf("FormatOutput failed: %v", err)
			}
			tc.check(t, output)
		})
	}
}

// TestProfileImportExport tests profile import/export functionality.
func TestProfileImportExport(t *testing.T) {
	env := newTestEnv(t)
	defer env.cleanup()

	// Create spec and install app
	specContent := createTestSpec("http://localhost:8080")
	specPath := env.writeSpec(specContent)

	_, err := env.configMgr.InstallApp("testapi", config.InstallOptions{
		SpecSource: specPath,
		CreateShim: false,
	})
	if err != nil {
		t.Fatalf("InstallApp failed: %v", err)
	}

	// Update profile with custom settings
	appConfig, _ := env.configMgr.GetAppConfig("testapi")
	profile := appConfig.Profiles["default"]
	profile.Headers = map[string]string{"X-Custom": "value"}
	appConfig.Profiles["default"] = profile
	_ = env.configMgr.SaveAppConfig(appConfig)

	// Export profile
	t.Run("Export", func(t *testing.T) {
		data, err := env.configMgr.ExportProfile("testapi", "default")
		if err != nil {
			t.Fatalf("ExportProfile failed: %v", err)
		}

		if len(data) == 0 {
			t.Error("exported data should not be empty")
		}

		// Note: We just verify export doesn't fail. Credential exclusion is tested
		// in the config package's dedicated export tests.
		_ = data // data used above in len check
	})

	// Import profile
	t.Run("Import", func(t *testing.T) {
		exportData, _ := env.configMgr.ExportProfile("testapi", "default")

		// Create a new config dir for import test
		importDir := t.TempDir()
		importMgr, _ := config.NewManager(config.WithConfigDir(importDir))

		// First install the app in the new manager
		_, err := importMgr.InstallApp("testapi", config.InstallOptions{
			SpecSource: specPath,
			CreateShim: false,
		})
		if err != nil {
			t.Fatalf("InstallApp for import failed: %v", err)
		}

		err = importMgr.ImportProfile("testapi", exportData)
		if err != nil {
			t.Fatalf("ImportProfile failed: %v", err)
		}

		// Verify imported profile
		importedConfig, _ := importMgr.GetAppConfig("testapi")
		if importedConfig.Profiles["default"].Headers["X-Custom"] != "value" {
			t.Error("imported profile should have custom header")
		}
	})
}
