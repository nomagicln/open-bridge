//go:build integration

package integration

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	localmcp "github.com/nomagicln/open-bridge/pkg/mcp"

	"github.com/nomagicln/open-bridge/internal/install"
	"github.com/nomagicln/open-bridge/pkg/request"
)

// recordedCall captures the observable shape of a single outgoing API call.
type recordedCall struct {
	Method string
	Path   string
	Query  url.Values
	Header http.Header
	Body   string
}

// callRecorder records every API call made through the CLI or an MCP server.
type callRecorder struct {
	mu    sync.Mutex
	calls []recordedCall
}

func (r *callRecorder) record(req *http.Request) {
	body := ""
	if req.Body != nil {
		buffered := make([]byte, 0)
		chunk := make([]byte, 512)
		for {
			n, err := req.Body.Read(chunk)
			buffered = append(buffered, chunk[:n]...)
			if err != nil {
				break
			}
		}
		body = string(buffered)
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, recordedCall{
		Method: req.Method,
		Path:   req.URL.Path,
		Query:  req.URL.Query(),
		Header: req.Header.Clone(),
		Body:   body,
	})
}

// last returns the most recent recorded call.
func (r *callRecorder) last(t *testing.T) recordedCall {
	t.Helper()

	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.calls) == 0 {
		t.Fatal("expected at least one recorded API call")
	}
	return r.calls[len(r.calls)-1]
}

// equivalenceSpec builds a spec with one query parameter so the CLI, the
// standard MCP server and the progressive MCP server can be compared on the
// same fixture.
func equivalenceSpec(serverURL string) string {
	return `openapi: "3.0.0"
info:
  title: Equivalence API
  version: "1.0.0"
servers:
  - url: ` + serverURL + `
paths:
  /users:
    get:
      operationId: listUsers
      summary: List all users
      parameters:
        - name: limit
          in: query
          required: false
          schema:
            type: integer
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

// TestInterfaceEquivalence proves the CLI, the standard MCP server and the
// progressive MCP server send the same request for the same fixture, including
// the profile header and query parameter overrides.
func TestInterfaceEquivalence(t *testing.T) {
	recorder := &callRecorder{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		recorder.record(r)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[{"id":1,"name":"Alice"}]`))
	}))
	defer server.Close()

	env := newTestEnv(t)
	defer env.cleanup()

	specPath := filepath.Join(t.TempDir(), "spec.yaml")
	if err := os.WriteFile(specPath, []byte(equivalenceSpec(server.URL)), 0644); err != nil {
		t.Fatalf("failed to write spec: %v", err)
	}

	if _, err := env.installer.InstallApp("testapi", install.InstallOptions{
		SpecSource: specPath,
		CreateShim: false,
	}); err != nil {
		t.Fatalf("InstallApp failed: %v", err)
	}

	appConfig, err := env.configMgr.GetAppConfig("testapi")
	if err != nil {
		t.Fatalf("GetAppConfig failed: %v", err)
	}

	profile, ok := appConfig.GetProfile("default")
	if !ok {
		t.Fatal("expected a default profile")
	}
	profile.Headers = map[string]string{"X-Profile": "staging"}
	profile.QueryParams = map[string]string{"api-version": "v2"}
	appConfig.AddProfile(*profile)
	if err := env.configMgr.SaveAppConfig(appConfig); err != nil {
		t.Fatalf("SaveAppConfig failed: %v", err)
	}

	appConfig, err = env.configMgr.GetAppConfig("testapi")
	if err != nil {
		t.Fatalf("GetAppConfig failed: %v", err)
	}

	cliCall := t.Run("CLI", func(t *testing.T) {
		if err := env.cliHandler.ExecuteCommand("testapi", appConfig, []string{"users", "list", "--limit", "5"}); err != nil {
			t.Fatalf("ExecuteCommand failed: %v", err)
		}
	})
	cliRequest := recorder.last(t)

	standardCall := t.Run("StandardMCP", func(t *testing.T) {
		specDoc, err := env.specParser.LoadSpec(specPath)
		if err != nil {
			t.Fatalf("failed to load spec: %v", err)
		}

		handler := localmcp.NewHandler(env.mapper, env.reqBuilder, server.Client())
		handler.SetSpec(specDoc)
		handler.SetAppConfig(appConfig, "default")

		result, err := handler.HandleCallTool(context.Background(), &mcp.CallToolRequest{
			Params: &mcp.CallToolParamsRaw{
				Name:      "listUsers",
				Arguments: []byte(`{"limit": 5}`),
			},
		})
		if err != nil {
			t.Fatalf("HandleCallTool failed: %v", err)
		}
		if result.IsError {
			t.Fatalf("tool call failed: %s", result.Content[0].(*mcp.TextContent).Text)
		}
	})
	standardRequest := recorder.last(t)

	progressiveCall := t.Run("ProgressiveMCP", func(t *testing.T) {
		specDoc, err := env.specParser.LoadSpec(specPath)
		if err != nil {
			t.Fatalf("failed to load spec: %v", err)
		}

		handler, err := localmcp.NewProgressiveHandler(request.NewBuilder(nil), server.Client(), localmcp.SearchEnginePredicate)
		if err != nil {
			t.Fatalf("NewProgressiveHandler failed: %v", err)
		}
		defer func() { _ = handler.Close() }()

		if err := handler.SetSpec(specDoc, nil); err != nil {
			t.Fatalf("SetSpec failed: %v", err)
		}
		handler.SetAppConfig(appConfig, "default")

		serverTransport, clientTransport := mcp.NewInMemoryTransports()
		mcpServer := mcp.NewServer(&mcp.Implementation{Name: "testapp", Version: "1.0"}, nil)
		handler.Register(mcpServer)

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		if _, err := mcpServer.Connect(ctx, serverTransport, nil); err != nil {
			t.Fatalf("server connect failed: %v", err)
		}

		client := mcp.NewClient(&mcp.Implementation{Name: "testclient", Version: "1.0"}, nil)
		session, err := client.Connect(ctx, clientTransport, nil)
		if err != nil {
			t.Fatalf("client connect failed: %v", err)
		}
		defer func() { _ = session.Close() }()

		// The progressive meta-tools are registered with app-scoped names, so
		// the test discovers them the way a real client does.
		toolList, err := session.ListTools(ctx, nil)
		if err != nil {
			t.Fatalf("ListTools failed: %v", err)
		}
		loadTool, invokeTool := metaToolNames(t, toolList.Tools)

		if _, err := session.CallTool(ctx, &mcp.CallToolParams{
			Name:      loadTool,
			Arguments: map[string]any{"toolId": "listUsers"},
		}); err != nil {
			t.Fatalf("LoadTool failed: %v", err)
		}

		result, err := session.CallTool(ctx, &mcp.CallToolParams{
			Name: invokeTool,
			Arguments: map[string]any{
				"toolId":    "listUsers",
				"arguments": map[string]any{"limit": 5},
			},
		})
		if err != nil {
			t.Fatalf("InvokeTool failed: %v", err)
		}
		if result.IsError {
			t.Fatalf("tool invocation failed: %s", result.Content[0].(*mcp.TextContent).Text)
		}
	})
	progressiveRequest := recorder.last(t)

	if !cliCall || !standardCall || !progressiveCall {
		t.Fatal("expected all three interfaces to run")
	}

	for name, got := range map[string]recordedCall{
		"standard MCP":    standardRequest,
		"progressive MCP": progressiveRequest,
	} {
		if got.Method != cliRequest.Method {
			t.Errorf("%s method = %q, want %q", name, got.Method, cliRequest.Method)
		}
		if got.Path != cliRequest.Path {
			t.Errorf("%s path = %q, want %q", name, got.Path, cliRequest.Path)
		}
		if got.Body != cliRequest.Body {
			t.Errorf("%s body = %q, want %q", name, got.Body, cliRequest.Body)
		}
		for key, want := range cliRequest.Query {
			gotValue := got.Query[key]
			if len(gotValue) != len(want) {
				t.Errorf("%s query %s = %v, want %v", name, key, gotValue, want)
				continue
			}
			for i := range want {
				if gotValue[i] != want[i] {
					t.Errorf("%s query %s = %v, want %v", name, key, gotValue, want)
					break
				}
			}
		}
		if got.Header.Get("X-Profile") != cliRequest.Header.Get("X-Profile") {
			t.Errorf("%s X-Profile header = %q, want %q", name,
				got.Header.Get("X-Profile"), cliRequest.Header.Get("X-Profile"))
		}
	}

	if got := cliRequest.Query.Get("api-version"); got != "v2" {
		t.Errorf("CLI profile query api-version = %q, want v2", got)
	}
	if got := cliRequest.Header.Get("X-Profile"); got != "staging" {
		t.Errorf("CLI profile header X-Profile = %q, want staging", got)
	}
}

// metaToolNames returns the app-scoped load and invoke meta-tool names.
func metaToolNames(t *testing.T, tools []*mcp.Tool) (load, invoke string) {
	t.Helper()

	for _, tool := range tools {
		switch {
		case strings.HasPrefix(tool.Name, "Load"):
			load = tool.Name
		case strings.HasPrefix(tool.Name, "Invoke"):
			invoke = tool.Name
		}
	}
	if load == "" || invoke == "" {
		t.Fatalf("progressive meta-tools not found in %v", tools)
	}
	return load, invoke
}
