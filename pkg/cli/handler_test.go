package cli

import (
	"bytes"
	"encoding/json"
	"maps"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/99designs/keyring"
	"github.com/getkin/kin-openapi/openapi3"
	"github.com/nomagicln/open-bridge/pkg/config"
	"github.com/nomagicln/open-bridge/pkg/credential"
	"github.com/nomagicln/open-bridge/pkg/request"
	"github.com/nomagicln/open-bridge/pkg/semantic"
	"github.com/nomagicln/open-bridge/pkg/spec"
)

const cliTestAppName = "testapp"

const cliTestSpecJSON = `{
  "openapi": "3.0.0",
  "info": {"title": "CLI Handler Test API", "version": "1.0.0"},
  "paths": {
    "/pets": {
      "get": {
        "operationId": "listPets",
        "summary": "List pets",
        "parameters": [
          {"name": "limit", "in": "query", "required": false, "schema": {"type": "integer"}}
        ],
        "responses": {
          "200": {
            "description": "OK",
            "content": {"application/json": {"schema": {"type": "object"}}}
          }
        }
      }
    }
  }
}`

const cliTestResponseJSON = `{"pets":[{"id":1,"name":"fluffy"}],"total":1}`

// capturedRequest stores what the fake API server observed.
type capturedRequest struct {
	method string
	path   string
	query  url.Values
	header http.Header
}

// cliTestEnv wires a Handler with real parser/mapper/builder plus an isolated
// file-backed credential manager and an httptest API server.
type cliTestEnv struct {
	handler   *Handler
	stdout    *bytes.Buffer
	server    *httptest.Server
	appConfig *config.AppConfig

	mu       sync.Mutex
	captured []capturedRequest
}

func newCLITestEnv(t *testing.T) *cliTestEnv {
	t.Helper()

	env := &cliTestEnv{stdout: &bytes.Buffer{}}

	// 1. Persist a minimal OpenAPI spec and pre-load it into the parser cache so
	// the handler never touches the persistent disk cache or the network.
	specPath := filepath.Join(t.TempDir(), "openapi.json")
	if err := os.WriteFile(specPath, []byte(cliTestSpecJSON), 0o600); err != nil {
		t.Fatalf("write test spec: %v", err)
	}
	specParser := spec.NewParser()
	specDoc, err := specParser.LoadSpec(specPath)
	if err != nil {
		t.Fatalf("load test spec: %v", err)
	}
	specParser.CacheSpec(cliTestAppName, specDoc)

	// 2. File-backed credential manager (isolated dir + fixed password) so the
	// tests never prompt for, or depend on, the macOS Keychain.
	credMgr, err := credential.NewManager(
		credential.WithAllowedBackends(keyring.FileBackend),
		credential.WithFileBackend(t.TempDir(), func(string) (string, error) {
			return "test-password", nil
		}),
	)
	if err != nil {
		t.Fatalf("create file-backed credential manager: %v", err)
	}
	if err := credMgr.StoreCredential(cliTestAppName, "default", credential.NewBearerCredential("default-token")); err != nil {
		t.Fatalf("store default credential: %v", err)
	}
	if err := credMgr.StoreCredential(cliTestAppName, "staging", credential.NewBearerCredential("staging-token")); err != nil {
		t.Fatalf("store staging credential: %v", err)
	}

	// 3. Fake API server.
	env.server = httptest.NewServer(http.HandlerFunc(env.handleAPI))
	t.Cleanup(env.server.Close)

	// 4. Handler under test, with output captured in a buffer.
	env.handler = NewHandler(specParser, semantic.NewMapper(), request.NewBuilder(credMgr), nil)
	env.handler.stdout = env.stdout

	env.appConfig = &config.AppConfig{
		Name:           cliTestAppName,
		SpecSource:     specPath,
		DefaultProfile: "default",
		Profiles: map[string]config.Profile{
			"default": {
				Name:    "default",
				BaseURL: env.server.URL,
				Auth:    config.AuthConfig{Type: "bearer"},
				Headers: map[string]string{"X-Profile": "default"},
			},
			"staging": {
				Name:        "staging",
				BaseURL:     env.server.URL,
				Auth:        config.AuthConfig{Type: "bearer"},
				Headers:     map[string]string{"X-Profile": "staging"},
				QueryParams: map[string]string{"api-version": "v2"},
			},
		},
	}

	return env
}

func (e *cliTestEnv) handleAPI(w http.ResponseWriter, r *http.Request) {
	e.mu.Lock()
	e.captured = append(e.captured, capturedRequest{
		method: r.Method,
		path:   r.URL.Path,
		query:  r.URL.Query(),
		header: r.Header.Clone(),
	})
	e.mu.Unlock()

	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(cliTestResponseJSON))
}

func (e *cliTestEnv) lastRequest(t *testing.T) capturedRequest {
	t.Helper()
	e.mu.Lock()
	defer e.mu.Unlock()
	if len(e.captured) == 0 {
		t.Fatal("fake API server received no request")
	}
	return e.captured[len(e.captured)-1]
}

func (e *cliTestEnv) reset() {
	e.mu.Lock()
	e.captured = nil
	e.mu.Unlock()
	e.stdout.Reset()
}

// resolveListOperation resolves the "pets/list" operation used by the unit-level
// request-building tests.
func (e *cliTestEnv) resolveListOperation(t *testing.T) (*semantic.Operation, *openapi3.Operation) {
	t.Helper()
	specDoc, ok := e.handler.specParser.GetCachedSpec(cliTestAppName)
	if !ok {
		t.Fatal("test spec was not cached")
	}
	tree := e.handler.mapper.BuildCommandTree(specDoc)
	resource := tree.RootResources["pets"]
	if resource == nil {
		t.Fatal("resource pets not found in command tree")
	}
	op := resource.Operations["list"]
	if op == nil {
		t.Fatalf("verb list not found for resource pets (got %v)", resource.Operations)
	}
	pathItem := specDoc.Paths.Find(op.Path)
	if pathItem == nil {
		t.Fatalf("path %q not found in spec", op.Path)
	}
	opSpec := getOperationSpec(pathItem, op.Method)
	if opSpec == nil {
		t.Fatalf("operation %s %s not found in spec", op.Method, op.Path)
	}
	return op, opSpec
}

// TestNewHandlerConfiguresHTTPClientTimeout pins the A0-6 fix: every CLI request
// must be bounded by a client timeout instead of hanging forever.
func TestNewHandlerConfiguresHTTPClientTimeout(t *testing.T) {
	h := NewHandler(nil, nil, nil, nil)
	if h.httpClient == nil {
		t.Fatal("NewHandler() httpClient is nil")
	}
	if got, want := h.httpClient.Timeout, 30*time.Second; got != want {
		t.Errorf("NewHandler() httpClient.Timeout = %v, want %v", got, want)
	}
	if h.stdout == nil {
		t.Error("NewHandler() stdout is nil, want os.Stdout default")
	}
}

// TestExecuteCommandJSONOutputAndBearerAuth is the end-to-end acceptance test for
// A0-1 (authorization is actually attached) and A0-3 (--json is honoured).
func TestExecuteCommandJSONOutputAndBearerAuth(t *testing.T) {
	env := newCLITestEnv(t)

	if err := env.handler.ExecuteCommand(cliTestAppName, env.appConfig, []string{"pets", "list", "--json"}); err != nil {
		t.Fatalf("ExecuteCommand() error: %v", err)
	}

	req := env.lastRequest(t)
	if got, want := req.header.Get("Authorization"), "Bearer default-token"; got != want {
		t.Errorf("Authorization header = %q, want %q (credential must be looked up with the real app name)", got, want)
	}
	if got, want := req.header.Get("X-Profile"), "default"; got != want {
		t.Errorf("X-Profile header = %q, want %q", got, want)
	}

	out := strings.TrimSpace(env.stdout.String())
	if out == "" {
		t.Fatal("ExecuteCommand() wrote no output to Handler.stdout")
	}
	var decoded any
	if err := json.Unmarshal([]byte(out), &decoded); err != nil {
		t.Errorf("--json output is not valid JSON: %v\noutput:\n%s", err, out)
	}
}

// TestExecuteCommandOutputFormatPriority covers A0-3: explicit format flags beat
// the default, and --output has the highest priority.
func TestExecuteCommandOutputFormatPriority(t *testing.T) {
	tests := []struct {
		name     string
		args     []string
		wantJSON bool
	}{
		{name: "default is yaml", args: []string{"pets", "list"}, wantJSON: false},
		{name: "json shorthand", args: []string{"pets", "list", "--json"}, wantJSON: true},
		{name: "yaml shorthand", args: []string{"pets", "list", "--yaml"}, wantJSON: false},
		{name: "output long flag", args: []string{"pets", "list", "--output", "json"}, wantJSON: true},
		{name: "output short flag", args: []string{"pets", "list", "-o", "json"}, wantJSON: true},
		{name: "json overridden by output yaml", args: []string{"pets", "list", "--json", "--output", "yaml"}, wantJSON: false},
		{name: "output json wins over yaml flag", args: []string{"pets", "list", "--yaml", "--output", "json"}, wantJSON: true},
	}

	env := newCLITestEnv(t)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env.reset()
			if err := env.handler.ExecuteCommand(cliTestAppName, env.appConfig, tt.args); err != nil {
				t.Fatalf("ExecuteCommand(%v) error: %v", tt.args, err)
			}
			out := strings.TrimSpace(env.stdout.String())
			if tt.wantJSON {
				var decoded any
				if err := json.Unmarshal([]byte(out), &decoded); err != nil {
					t.Errorf("expected JSON output, got %q (unmarshal error: %v)", out, err)
				}
				return
			}
			if json.Valid([]byte(out)) {
				t.Errorf("expected YAML output, got valid JSON: %q", out)
			}
			if !strings.Contains(out, "total: 1") {
				t.Errorf("expected YAML output containing %q, got %q", "total: 1", out)
			}
		})
	}
}

// TestExecuteCommandProfileSelection covers A0-3b: --profile selects the profile,
// its headers and query params reach the request, and existing query values survive.
func TestExecuteCommandProfileSelection(t *testing.T) {
	env := newCLITestEnv(t)

	err := env.handler.ExecuteCommand(cliTestAppName, env.appConfig,
		[]string{"pets", "list", "--profile", "staging", "--limit", "5", "--json"})
	if err != nil {
		t.Fatalf("ExecuteCommand() error: %v", err)
	}

	req := env.lastRequest(t)
	if got, want := req.header.Get("Authorization"), "Bearer staging-token"; got != want {
		t.Errorf("Authorization header = %q, want %q", got, want)
	}
	if got, want := req.header.Get("X-Profile"), "staging"; got != want {
		t.Errorf("X-Profile header = %q, want %q", got, want)
	}
	if got := req.query.Get("limit"); got != "5" {
		t.Errorf("query parameter limit = %q, want 5", got)
	}
	if got := req.query.Get("api-version"); got != "v2" {
		t.Errorf("profile query parameter api-version = %q, want v2", got)
	}
	for _, leaked := range []string{"profile", "json", "output"} {
		if req.query.Has(leaked) {
			t.Errorf("CLI-only flag %q leaked into request query: %v", leaked, req.query)
		}
	}
}

// TestExecuteCommandShortProfileFlag covers the -p shorthand.
func TestExecuteCommandShortProfileFlag(t *testing.T) {
	env := newCLITestEnv(t)

	if err := env.handler.ExecuteCommand(cliTestAppName, env.appConfig, []string{"pets", "list", "-p", "staging", "--json"}); err != nil {
		t.Fatalf("ExecuteCommand() error: %v", err)
	}
	if got, want := env.lastRequest(t).header.Get("Authorization"), "Bearer staging-token"; got != want {
		t.Errorf("Authorization header = %q, want %q", got, want)
	}
}

// TestExecuteCommandUnknownProfile verifies the profile validation error names
// the requested profile and lists the available ones.
func TestExecuteCommandUnknownProfile(t *testing.T) {
	env := newCLITestEnv(t)

	err := env.handler.ExecuteCommand(cliTestAppName, env.appConfig, []string{"pets", "list", "--profile", "nope"})
	if err == nil {
		t.Fatal("ExecuteCommand() expected error for unknown profile, got nil")
	}
	for _, want := range []string{`profile "nope"`, "available profiles", "default", "staging"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("ExecuteCommand() error = %q, want it to contain %q", err.Error(), want)
		}
	}
}

// TestBuildRequestInjectsAuthWithRealAppName is the unit-level regression test
// for A0-1: buildRequest must forward the real app name to InjectAuth.
func TestBuildRequestInjectsAuthWithRealAppName(t *testing.T) {
	env := newCLITestEnv(t)
	op, opSpec := env.resolveListOperation(t)

	profile := env.appConfig.Profiles["default"]
	serverURL, err := url.Parse(env.server.URL)
	if err != nil {
		t.Fatalf("parse server URL: %v", err)
	}
	serverURL.User = url.UserPassword("user", "pass")
	profile.BaseURL = serverURL.String()
	profile.QueryParams = map[string]string{"api-version": "v1"}

	req, err := env.handler.buildRequest(cliTestAppName, op, opSpec, map[string]any{"limit": "5"}, &profile)
	if err != nil {
		t.Fatalf("buildRequest() error: %v", err)
	}

	if got, want := req.Header.Get("Authorization"), "Bearer default-token"; got != want {
		t.Errorf("Authorization header = %q, want %q", got, want)
	}
	if got, want := req.Header.Get("X-Profile"), "default"; got != want {
		t.Errorf("X-Profile header = %q, want %q", got, want)
	}
	if req.URL.User == nil || req.URL.User.Username() != "user" {
		t.Errorf("buildRequest() dropped URL userinfo: %v", req.URL.User)
	}
	query := req.URL.Query()
	if got := query.Get("limit"); got != "5" {
		t.Errorf("query parameter limit = %q, want 5", got)
	}
	if got := query.Get("api-version"); got != "v1" {
		t.Errorf("profile query parameter api-version = %q, want v1", got)
	}
}

// withDefaultProfile returns a copy of the test app config whose default profile
// is adjusted by mutate, keeping the original env untouched.
func withDefaultProfile(env *cliTestEnv, mutate func(*config.Profile)) *config.AppConfig {
	appConfig := *env.appConfig
	appConfig.Profiles = make(map[string]config.Profile, len(env.appConfig.Profiles))
	maps.Copy(appConfig.Profiles, env.appConfig.Profiles)
	profile := appConfig.Profiles["default"]
	mutate(&profile)
	appConfig.Profiles["default"] = profile
	return &appConfig
}

// TestExecuteCommandUsesProfileTLS verifies the CLI handler wires profile TLS
// settings into the HTTP client: a self-signed server is accepted only when the
// profile opts into InsecureSkipVerify.
func TestExecuteCommandUsesProfileTLS(t *testing.T) {
	env := newCLITestEnv(t)

	var requests atomic.Int32
	tlsServer := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(cliTestResponseJSON))
	}))
	t.Cleanup(tlsServer.Close)

	t.Run("insecure skip verify accepts self-signed server", func(t *testing.T) {
		env.reset()
		appConfig := withDefaultProfile(env, func(profile *config.Profile) {
			profile.BaseURL = tlsServer.URL
			profile.TLSConfig = config.TLSConfig{InsecureSkipVerify: true}
		})
		if err := env.handler.ExecuteCommand(cliTestAppName, appConfig, []string{"pets", "list", "--json"}); err != nil {
			t.Fatalf("ExecuteCommand() error: %v", err)
		}
		out := strings.TrimSpace(env.stdout.String())
		var decoded any
		if err := json.Unmarshal([]byte(out), &decoded); err != nil {
			t.Errorf("--json output is not valid JSON: %v\noutput:\n%s", err, out)
		}
	})

	t.Run("default verification rejects self-signed server", func(t *testing.T) {
		env.reset()
		appConfig := withDefaultProfile(env, func(profile *config.Profile) {
			profile.BaseURL = tlsServer.URL
			profile.TLSConfig = config.TLSConfig{}
		})
		err := env.handler.ExecuteCommand(cliTestAppName, appConfig, []string{"pets", "list", "--json"})
		if err == nil {
			t.Fatal("ExecuteCommand() expected TLS verification error, got nil")
		}
		if !IsPrintedError(err) {
			t.Errorf("ExecuteCommand() error = %T, want *PrintedError", err)
		}
	})
}

// TestExecuteCommandRetriesTransientFailures verifies the CLI handler uses the
// profile retry policy and eventually returns the successful response.
func TestExecuteCommandRetriesTransientFailures(t *testing.T) {
	env := newCLITestEnv(t)

	var attempts atomic.Int32
	retryServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if attempts.Add(1) <= 2 {
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte(`{"error":"busy"}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(cliTestResponseJSON))
	}))
	t.Cleanup(retryServer.Close)

	appConfig := withDefaultProfile(env, func(profile *config.Profile) {
		profile.BaseURL = retryServer.URL
		profile.RetryConfig = config.RetryConfig{
			MaxRetries:           2,
			InitialDelay:         config.Duration{Duration: time.Millisecond},
			MaxDelay:             config.Duration{Duration: 5 * time.Millisecond},
			RetryableStatusCodes: []int{http.StatusServiceUnavailable},
		}
	})

	if err := env.handler.ExecuteCommand(cliTestAppName, appConfig, []string{"pets", "list", "--json"}); err != nil {
		t.Fatalf("ExecuteCommand() error: %v", err)
	}
	if got := attempts.Load(); got != 3 {
		t.Errorf("server attempts = %d, want 3 (two retries)", got)
	}
	out := strings.TrimSpace(env.stdout.String())
	var decoded any
	if err := json.Unmarshal([]byte(out), &decoded); err != nil {
		t.Errorf("--json output is not valid JSON: %v\noutput:\n%s", err, out)
	}
}

// TestExecuteCommandProfileTimeout verifies a profile timeout is applied to CLI
// requests instead of the 30s handler default.
func TestExecuteCommandProfileTimeout(t *testing.T) {
	env := newCLITestEnv(t)

	slowServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(200 * time.Millisecond)
		_, _ = w.Write([]byte(cliTestResponseJSON))
	}))
	t.Cleanup(slowServer.Close)

	appConfig := withDefaultProfile(env, func(profile *config.Profile) {
		profile.BaseURL = slowServer.URL
		profile.Timeout = config.Duration{Duration: 50 * time.Millisecond}
	})

	start := time.Now()
	err := env.handler.ExecuteCommand(cliTestAppName, appConfig, []string{"pets", "list", "--json"})
	if err == nil {
		t.Fatal("ExecuteCommand() expected timeout error, got nil")
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Errorf("ExecuteCommand() took %v, profile timeout was not applied", elapsed)
	}
	if !IsPrintedError(err) {
		t.Errorf("ExecuteCommand() error = %T, want *PrintedError", err)
	}
}

// TestExecuteCommandInvalidTLSConfiguration verifies invalid profile TLS
// configuration fails before any request is attempted.
func TestExecuteCommandInvalidTLSConfiguration(t *testing.T) {
	env := newCLITestEnv(t)

	appConfig := withDefaultProfile(env, func(profile *config.Profile) {
		profile.TLSConfig = config.TLSConfig{CAFile: filepath.Join(t.TempDir(), "does-not-exist.pem")}
	})

	err := env.handler.ExecuteCommand(cliTestAppName, appConfig, []string{"pets", "list", "--json"})
	if err == nil {
		t.Fatal("ExecuteCommand() expected TLS configuration error, got nil")
	}
	if !IsPrintedError(err) {
		t.Errorf("ExecuteCommand() error = %T, want *PrintedError", err)
	}
	env.mu.Lock()
	defer env.mu.Unlock()
	if len(env.captured) != 0 {
		t.Errorf("server received %d requests, want 0 (invalid TLS config must fail fast)", len(env.captured))
	}
}

// TestClientForProfileFallback pins the Handler.httpClient injection/fallback
// behavior and the fail-fast path for invalid TLS configuration.
func TestClientForProfileFallback(t *testing.T) {
	h := NewHandler(nil, nil, nil, nil)
	injected := &http.Client{}
	h.httpClient = injected

	client, retryConfig, err := h.clientForProfile(nil)
	if err != nil {
		t.Fatalf("clientForProfile(nil) error: %v", err)
	}
	if client != injected {
		t.Errorf("clientForProfile(nil) = %p, want the injected Handler.httpClient %p", client, injected)
	}
	if retryConfig.MaxRetries != 0 {
		t.Errorf("retryConfig.MaxRetries = %d, want 0 for a nil profile", retryConfig.MaxRetries)
	}

	h.httpClient = nil
	client, _, err = h.clientForProfile(nil)
	if err != nil {
		t.Fatalf("clientForProfile(nil) without injected client error: %v", err)
	}
	if client == nil {
		t.Fatal("clientForProfile(nil) returned a nil client without an injected client")
	}

	_, _, err = h.clientForProfile(&config.Profile{Name: "broken", TLSConfig: config.TLSConfig{MinVersion: "1.0"}})
	if err == nil {
		t.Fatal("clientForProfile() expected error for invalid TLS configuration, got nil")
	}
}

// newIndexedHelpHandler builds a Handler whose parser uses a persistent derived
// index cache and whose help output is captured in a buffer.
func newIndexedHelpHandler(t *testing.T, indexDir string) (*Handler, *bytes.Buffer) {
	t.Helper()

	parser := spec.NewParser(spec.WithIndexCache(indexDir))
	handler := NewHandler(parser, semantic.NewMapper(), request.NewBuilder(nil), nil)
	buf := &bytes.Buffer{}
	handler.stdout = buf

	return handler, buf
}

// writeCLIHelpSpec writes a spec file and returns its path.
func writeCLIHelpSpec(t *testing.T) string {
	t.Helper()

	specPath := filepath.Join(t.TempDir(), "index-help.json")
	if err := os.WriteFile(specPath, []byte(cliTestSpecJSON), 0o600); err != nil {
		t.Fatalf("write help spec: %v", err)
	}

	return specPath
}

// corruptCLIHelpSpec replaces the spec content with garbage of the same size and
// restores the original modification time, so the derived-index key still
// matches while parsing the file would fail.
func corruptCLIHelpSpec(t *testing.T, specPath string) {
	t.Helper()

	info, err := os.Stat(specPath)
	if err != nil {
		t.Fatalf("stat spec: %v", err)
	}

	garbage := bytes.Repeat([]byte("x"), int(info.Size()))
	if err := os.WriteFile(specPath, garbage, 0o600); err != nil {
		t.Fatalf("corrupt spec: %v", err)
	}
	if err := os.Chtimes(specPath, info.ModTime(), info.ModTime()); err != nil {
		t.Fatalf("restore spec mtime: %v", err)
	}
}

// helpAppConfig returns an AppConfig for the given spec source.
func helpAppConfig(name, specSource string) *config.AppConfig {
	return &config.AppConfig{
		Name:           name,
		SpecSource:     specSource,
		Description:    "Derived Index Help API",
		DefaultProfile: "default",
		Profiles: map[string]config.Profile{
			"default": {Name: "default", BaseURL: "https://api.example.com"},
		},
	}
}

func TestShowAppHelpUsesDerivedIndexWithoutParsing(t *testing.T) {
	specPath := writeCLIHelpSpec(t)
	indexDir := t.TempDir()
	appConfig := helpAppConfig("idxapp", specPath)

	first, firstOut := newIndexedHelpHandler(t, indexDir)
	if err := first.showAppHelp("idxapp", appConfig); err != nil {
		t.Fatalf("first showAppHelp failed: %v", err)
	}

	firstText := firstOut.String()
	if !strings.Contains(firstText, "Available Resources:") || !strings.Contains(firstText, "pets") {
		t.Fatalf("help output missing resources:\n%s", firstText)
	}
	if _, ok := first.specParser.GetCachedSpec("idxapp"); ok {
		t.Fatal("expected the index path not to populate the in-memory spec cache")
	}

	corruptCLIHelpSpec(t, specPath)

	second, secondOut := newIndexedHelpHandler(t, indexDir)
	if err := second.showAppHelp("idxapp", appConfig); err != nil {
		t.Fatalf("cached showAppHelp failed: %v", err)
	}
	if _, ok := second.specParser.GetCachedSpec("idxapp"); ok {
		t.Error("expected the second help render to use the derived index, not parse the spec")
	}
	if got := secondOut.String(); got != firstText {
		t.Errorf("help text changed when served from the derived index:\nfirst:\n%s\nsecond:\n%s", firstText, got)
	}
}

func TestShowResourceHelpUsesDerivedIndexWithoutParsing(t *testing.T) {
	specPath := writeCLIHelpSpec(t)
	indexDir := t.TempDir()
	appConfig := helpAppConfig("idxres", specPath)

	first, firstOut := newIndexedHelpHandler(t, indexDir)
	if err := first.showResourceHelp("idxres", "pets", appConfig); err != nil {
		t.Fatalf("first showResourceHelp failed: %v", err)
	}

	firstText := firstOut.String()
	if !strings.Contains(firstText, "Resource: pets") || !strings.Contains(firstText, "pets list") {
		t.Fatalf("resource help output unexpected:\n%s", firstText)
	}

	corruptCLIHelpSpec(t, specPath)

	second, secondOut := newIndexedHelpHandler(t, indexDir)
	if err := second.showResourceHelp("idxres", "pets", appConfig); err != nil {
		t.Fatalf("cached showResourceHelp failed: %v", err)
	}
	if got := secondOut.String(); got != firstText {
		t.Errorf("resource help text changed when served from the derived index:\n%s\n---\n%s", firstText, got)
	}
}

func TestShowResourceOrVerbHelpUnknownResourceUsesIndex(t *testing.T) {
	specPath := writeCLIHelpSpec(t)
	indexDir := t.TempDir()
	appConfig := helpAppConfig("idxunknown", specPath)

	first, _ := newIndexedHelpHandler(t, indexDir)
	if err := first.showAppHelp("idxunknown", appConfig); err != nil {
		t.Fatalf("priming showAppHelp failed: %v", err)
	}

	corruptCLIHelpSpec(t, specPath)

	second, _ := newIndexedHelpHandler(t, indexDir)
	err := second.showResourceOrVerbHelp("idxunknown", "nope", appConfig)
	if err == nil {
		t.Fatal("expected an error for an unknown resource")
	}
	// The unknown-resource path renders the invalid-syntax help built from the
	// derived index (the pre-index behavior for a single unknown argument).
	if !strings.Contains(err.Error(), "Invalid command syntax") {
		t.Errorf("unexpected error (likely parsed instead of using the index): %v", err)
	}
	if !strings.Contains(err.Error(), "idxunknown pets list --json") {
		t.Errorf("index-derived examples missing from error: %v", err)
	}
}

func TestShowAppHelpFallsBackWhenIndexDisabled(t *testing.T) {
	specPath := writeCLIHelpSpec(t)
	appConfig := helpAppConfig("fallbackapp", specPath)

	handler, out := newIndexedHelpHandler(t, "")
	if err := handler.showAppHelp("fallbackapp", appConfig); err != nil {
		t.Fatalf("showAppHelp failed: %v", err)
	}
	if !strings.Contains(out.String(), "Available Resources:") {
		t.Errorf("fallback help output missing resources:\n%s", out.String())
	}
}

func TestShowAppHelpSpecLoadFailureFallsBackToMessage(t *testing.T) {
	appConfig := helpAppConfig("missingapp", filepath.Join(t.TempDir(), "does-not-exist.json"))

	handler, out := newIndexedHelpHandler(t, t.TempDir())
	if err := handler.showAppHelp("missingapp", appConfig); err != nil {
		t.Fatalf("showAppHelp should tolerate a missing spec, got: %v", err)
	}
	if !strings.Contains(out.String(), "(Unable to load API specification)") {
		t.Errorf("expected fallback message, got:\n%s", out.String())
	}
}

func TestHelpTextIdenticalWithAndWithoutIndex(t *testing.T) {
	specPath := writeCLIHelpSpec(t)
	appConfig := helpAppConfig("sameapp", specPath)

	cold, coldOut := newIndexedHelpHandler(t, "")
	if err := cold.showAppHelp("sameapp", appConfig); err != nil {
		t.Fatalf("cold showAppHelp failed: %v", err)
	}

	indexDir := t.TempDir()
	warm, warmOut := newIndexedHelpHandler(t, indexDir)
	if err := warm.showAppHelp("sameapp", appConfig); err != nil {
		t.Fatalf("warm showAppHelp failed: %v", err)
	}

	if coldOut.String() != warmOut.String() {
		t.Errorf("help text differs between parse and index paths:\ncold:\n%s\nwarm:\n%s", coldOut.String(), warmOut.String())
	}
}
