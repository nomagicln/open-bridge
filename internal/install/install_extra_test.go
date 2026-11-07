package install

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/nomagicln/open-bridge/pkg/config"
)

// extraTestSpec is a minimal OpenAPI document with a server entry, so base URL
// resolution and spec metadata persistence can be exercised.
const extraTestSpec = `openapi: "3.0.0"
info:
  title: Extra Test API
  version: "1.0.0"
servers:
  - url: https://api.extra.example.com
paths:
  /items:
    get:
      operationId: listItems
      summary: List items
      responses:
        "200":
          description: OK
`

// newExtraTestInstaller returns an Installer isolated in temporary config and
// home directories.
func newExtraTestInstaller(t *testing.T) *Installer {
	t.Helper()

	t.Setenv("HOME", t.TempDir())

	mgr, err := config.NewManager(config.WithConfigDir(t.TempDir()))
	if err != nil {
		t.Fatalf("config.NewManager failed: %v", err)
	}

	return New(mgr)
}

// writeExtraSpec writes an OpenAPI document into dir and returns its path.
func writeExtraSpec(t *testing.T, dir, content string) string {
	t.Helper()

	specPath := filepath.Join(dir, "spec.yaml")
	if err := os.WriteFile(specPath, []byte(content), 0o600); err != nil {
		t.Fatalf("write spec: %v", err)
	}

	return specPath
}

// errorReader always fails so prompt error paths can be exercised.
type errorReader struct{}

func (errorReader) Read([]byte) (int, error) {
	return 0, errors.New("reader failed")
}

// lineReader returns one buffered line per Read call. bufio.Scanner (used by
// the prompt helpers) reads ahead, so feeding a strings.Reader would let the
// first prompt consume every answer; lineReader keeps one answer per prompt.
type lineReader struct {
	lines   []string
	index   int
	pending []byte
}

func newLineReader(lines ...string) *lineReader {
	return &lineReader{lines: lines}
}

func (r *lineReader) Read(p []byte) (int, error) {
	if len(r.pending) == 0 {
		if r.index >= len(r.lines) {
			return 0, io.EOF
		}
		r.pending = []byte(r.lines[r.index] + "\n")
		r.index++
	}

	n := copy(p, r.pending)
	r.pending = r.pending[n:]

	return n, nil
}

func TestNormalizeSpecSources(t *testing.T) {
	normalized, err := normalizeSpecSources(nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(normalized) != 0 {
		t.Errorf("expected empty result, got %v", normalized)
	}

	normalized, err = normalizeSpecSources([]string{"spec.yaml", "https://api.example.com/openapi.json"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(normalized) != 2 {
		t.Fatalf("expected 2 sources, got %v", normalized)
	}
	if !filepath.IsAbs(normalized[0]) {
		t.Errorf("expected local source to be absolute, got %q", normalized[0])
	}
	if normalized[1] != "https://api.example.com/openapi.json" {
		t.Errorf("expected URL preserved, got %q", normalized[1])
	}
}

func TestIsValidPath(t *testing.T) {
	dir := t.TempDir()
	filePath := filepath.Join(dir, "readable.txt")
	if err := os.WriteFile(filePath, []byte("data"), 0o600); err != nil {
		t.Fatalf("write file: %v", err)
	}

	tests := []struct {
		name string
		path string
		want bool
	}{
		{name: "regular file", path: filePath, want: true},
		{name: "directory", path: dir, want: false},
		{name: "missing file", path: filepath.Join(dir, "missing.txt"), want: false},
		{name: "empty path", path: "", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsValidPath(tt.path); got != tt.want {
				t.Errorf("IsValidPath(%q) = %v, want %v", tt.path, got, tt.want)
			}
		})
	}
}

func TestResolveBaseURL(t *testing.T) {
	t.Run("explicit option wins", func(t *testing.T) {
		got, err := resolveBaseURL(InstallOptions{BaseURL: "https://explicit.example.com"}, &openapi3.T{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != "https://explicit.example.com" {
			t.Errorf("got %q", got)
		}
	})

	t.Run("spec server fallback", func(t *testing.T) {
		specDoc := &openapi3.T{Servers: openapi3.Servers{{URL: "https://spec.example.com"}}}
		got, err := resolveBaseURL(InstallOptions{}, specDoc)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != "https://spec.example.com" {
			t.Errorf("got %q", got)
		}
	})

	t.Run("interactive prompt accepts default", func(t *testing.T) {
		var out bytes.Buffer
		got, err := resolveBaseURL(InstallOptions{
			Interactive: true,
			Reader:      strings.NewReader("\n"),
			Writer:      &out,
		}, &openapi3.T{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != "http://localhost:8080" {
			t.Errorf("expected prompt default, got %q", got)
		}
		if !strings.Contains(out.String(), "No server URL found in spec") {
			t.Errorf("expected missing-server notice, got %q", out.String())
		}
	})

	t.Run("interactive prompt error", func(t *testing.T) {
		_, err := resolveBaseURL(InstallOptions{
			Interactive: true,
			Reader:      errorReader{},
			Writer:      io.Discard,
		}, &openapi3.T{})
		if err == nil {
			t.Fatal("expected prompt error")
		}
	})

	t.Run("missing everywhere", func(t *testing.T) {
		_, err := resolveBaseURL(InstallOptions{}, &openapi3.T{})
		if err == nil {
			t.Fatal("expected error when no base URL is available")
		}
	})
}

func TestMergeInstallOptions(t *testing.T) {
	t.Run("spec info", func(t *testing.T) {
		existing := &config.AppConfig{SpecSource: "/existing/spec.yaml", Description: "existing"}
		merged := mergeSpecInfo(InstallOptions{}, existing)
		if merged.SpecSource != existing.SpecSource || merged.Description != existing.Description {
			t.Errorf("expected spec info merged, got %+v", merged)
		}

		explicit := mergeSpecInfo(InstallOptions{SpecSource: "/explicit.yaml", Description: "explicit"}, existing)
		if explicit.SpecSource != "/explicit.yaml" || explicit.Description != "explicit" {
			t.Errorf("expected explicit options preserved, got %+v", explicit)
		}
	})

	t.Run("profile info", func(t *testing.T) {
		profile := config.Profile{BaseURL: "https://profile.example.com", Auth: config.AuthConfig{Type: "bearer"}}
		merged := mergeProfileInfo(InstallOptions{}, profile)
		if merged.BaseURL != profile.BaseURL || merged.AuthType != "bearer" {
			t.Errorf("expected profile merged, got %+v", merged)
		}
	})

	t.Run("tls info", func(t *testing.T) {
		profile := config.Profile{TLSConfig: config.TLSConfig{
			CAFile:             "/ca.pem",
			CertFile:           "/cert.pem",
			KeyFile:            "/key.pem",
			InsecureSkipVerify: true,
		}}
		merged := mergeTLSInfo(InstallOptions{}, profile)
		if merged.TLSCACert != "/ca.pem" || merged.TLSClientCert != "/cert.pem" ||
			merged.TLSClientKey != "/key.pem" || !merged.TLSSkipVerify {
			t.Errorf("expected TLS merged, got %+v", merged)
		}
	})

	t.Run("safety info", func(t *testing.T) {
		profile := config.Profile{SafetyConfig: config.SafetyConfig{
			ProgressiveDisclosure: true,
			SearchEngine:          "predicate",
			ReadOnlyMode:          true,
		}}
		merged := mergeSafetyInfo(InstallOptions{}, profile)
		if merged.ProgressiveDisclosure == nil || !*merged.ProgressiveDisclosure ||
			merged.SearchEngine != "predicate" || !merged.ReadOnlyMode {
			t.Errorf("expected safety merged, got %+v", merged)
		}
	})

	t.Run("existing app config", func(t *testing.T) {
		existing := &config.AppConfig{
			SpecSource:     "/existing/spec.yaml",
			Description:    "existing",
			DefaultProfile: "default",
			Profiles: map[string]config.Profile{
				"default": {Name: "default", BaseURL: "https://existing.example.com"},
			},
		}
		merged := mergeExistingAppConfig(InstallOptions{}, existing)
		if merged.SpecSource != "/existing/spec.yaml" || merged.BaseURL != "https://existing.example.com" {
			t.Errorf("expected existing config merged, got %+v", merged)
		}
	})
}

func TestPromptForMissingInfo(t *testing.T) {
	installer := newExtraTestInstaller(t)

	t.Run("fills all missing fields", func(t *testing.T) {
		var out bytes.Buffer
		opts, err := installer.promptForMissingInfo(InstallOptions{
			Reader: newLineReader("spec.yaml", "my description", "bearer", "n"),
			Writer: &out,
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !filepath.IsAbs(opts.SpecSource) {
			t.Errorf("expected absolute spec source, got %q", opts.SpecSource)
		}
		if opts.Description != "my description" || opts.AuthType != "bearer" || opts.CreateShim {
			t.Errorf("unexpected options: %+v", opts)
		}
	})

	t.Run("keeps provided values", func(t *testing.T) {
		opts, err := installer.promptForMissingInfo(InstallOptions{
			SpecSource:  "spec.yaml",
			Description: "given",
			AuthType:    "none",
			Reader:      strings.NewReader("y\n"),
			Writer:      io.Discard,
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if opts.Description != "given" || opts.AuthType != "none" || !opts.CreateShim {
			t.Errorf("unexpected options: %+v", opts)
		}
	})

	t.Run("missing spec source", func(t *testing.T) {
		_, err := installer.promptForMissingInfo(InstallOptions{
			Reader: strings.NewReader("\n"),
			Writer: io.Discard,
		})
		if err == nil {
			t.Fatal("expected error for missing spec source")
		}
	})

	t.Run("invalid auth choice", func(t *testing.T) {
		_, err := installer.promptForMissingInfo(InstallOptions{
			SpecSource:  "spec.yaml",
			Description: "provided",
			Reader:      newLineReader("invalid"),
			Writer:      io.Discard,
		})
		if err == nil {
			t.Fatal("expected error for invalid auth choice")
		}
	})

	t.Run("reader failure", func(t *testing.T) {
		_, err := installer.promptForMissingInfo(InstallOptions{
			Reader: errorReader{},
			Writer: io.Discard,
		})
		if err == nil {
			t.Fatal("expected reader error")
		}
	})
}

func TestPromptAndNormalizeSpecSources(t *testing.T) {
	opts, err := promptAndNormalizeSpec(InstallOptions{
		SpecSources: []string{"a.yaml", "https://api.example.com/openapi.json"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(opts.SpecSources) != 2 || !filepath.IsAbs(opts.SpecSources[0]) {
		t.Errorf("expected normalized spec sources, got %v", opts.SpecSources)
	}
}

func TestCreateShimUsesDefaultDir(t *testing.T) {
	installer := newExtraTestInstaller(t)

	shimPath, err := installer.CreateShim("defshim", "")
	if err != nil {
		t.Fatalf("CreateShim failed: %v", err)
	}
	if filepath.Base(shimPath) != "defshim" {
		t.Errorf("unexpected shim name: %s", shimPath)
	}
	if !strings.Contains(shimPath, filepath.Join(".local", "bin")) {
		t.Errorf("expected default shim dir under ~/.local/bin, got %s", shimPath)
	}
	if !installer.ShimExists("defshim") {
		t.Error("expected shim to exist")
	}

	if err := installer.RemoveShim("defshim"); err != nil {
		t.Fatalf("RemoveShim failed: %v", err)
	}
	if installer.ShimExists("defshim") {
		t.Error("expected shim to be removed")
	}
	// Removing an already removed shim stays a no-op.
	if err := installer.RemoveShim("defshim"); err != nil {
		t.Fatalf("second RemoveShim failed: %v", err)
	}
}

func TestCreateShimInvalidDirectory(t *testing.T) {
	installer := newExtraTestInstaller(t)

	blocker := filepath.Join(t.TempDir(), "blocker")
	if err := os.WriteFile(blocker, []byte("x"), 0o600); err != nil {
		t.Fatalf("write blocker: %v", err)
	}

	if _, err := installer.CreateShim("badshim", blocker); err == nil {
		t.Fatal("expected error when shim directory is a file")
	}
}

func TestGetOBBinaryPath(t *testing.T) {
	t.Run("found in PATH", func(t *testing.T) {
		binDir := t.TempDir()
		fakeOb := filepath.Join(binDir, "ob")
		if err := os.WriteFile(fakeOb, []byte("#!/bin/sh\n"), 0o755); err != nil {
			t.Fatalf("write fake ob: %v", err)
		}
		t.Setenv("PATH", binDir)

		got, err := getOBBinaryPath()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != fakeOb {
			t.Errorf("expected %q, got %q", fakeOb, got)
		}
	})

	t.Run("falls back to current executable", func(t *testing.T) {
		t.Setenv("PATH", "")

		got, err := getOBBinaryPath()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !filepath.IsAbs(got) {
			t.Errorf("expected absolute executable path, got %q", got)
		}
	})
}

func TestCreateShimIfRequested(t *testing.T) {
	installer := newExtraTestInstaller(t)

	t.Run("not requested", func(t *testing.T) {
		var out bytes.Buffer
		result := &InstallResult{}
		installer.createShimIfRequested(InstallOptions{Writer: &out}, "noshim", result)
		if result.ShimPath != "" || out.Len() != 0 {
			t.Errorf("expected no-op, got shim %q output %q", result.ShimPath, out.String())
		}
	})

	t.Run("creates shim and prints path instructions", func(t *testing.T) {
		var out bytes.Buffer
		shimDir := filepath.Join(t.TempDir(), "bin")
		result := &InstallResult{}
		installer.createShimIfRequested(InstallOptions{
			CreateShim: true,
			ShimDir:    shimDir,
			Writer:     &out,
		}, "shimapp", result)

		if result.ShimPath == "" {
			t.Fatal("expected shim path to be set")
		}
		if !strings.Contains(out.String(), "not in your PATH") {
			t.Errorf("expected PATH notice, got %q", out.String())
		}
		if !strings.Contains(out.String(), shimDir) {
			t.Errorf("expected instructions to mention %q, got %q", shimDir, out.String())
		}
	})

	t.Run("no notice when shim dir is in PATH", func(t *testing.T) {
		var out bytes.Buffer
		shimDir := filepath.Join(t.TempDir(), "bin")
		t.Setenv("PATH", shimDir+string(os.PathListSeparator)+"/usr/bin"+string(os.PathListSeparator)+"/bin")
		result := &InstallResult{}
		installer.createShimIfRequested(InstallOptions{
			CreateShim: true,
			ShimDir:    shimDir,
			Writer:     &out,
		}, "pathshim", result)

		if result.ShimPath == "" {
			t.Fatal("expected shim path to be set")
		}
		if out.Len() != 0 {
			t.Errorf("expected no PATH notice, got %q", out.String())
		}
	})

	t.Run("warns when shim creation fails", func(t *testing.T) {
		var out bytes.Buffer
		blocker := filepath.Join(t.TempDir(), "blocker")
		if err := os.WriteFile(blocker, []byte("x"), 0o600); err != nil {
			t.Fatalf("write blocker: %v", err)
		}
		result := &InstallResult{}
		installer.createShimIfRequested(InstallOptions{
			CreateShim: true,
			ShimDir:    blocker,
			Writer:     &out,
		}, "fails", result)

		if result.ShimPath != "" {
			t.Errorf("expected no shim path, got %q", result.ShimPath)
		}
		if !strings.Contains(out.String(), "Warning: failed to create shim") {
			t.Errorf("expected warning, got %q", out.String())
		}
	})
}

func TestUpdateAppBranches(t *testing.T) {
	installer := newExtraTestInstaller(t)
	specPath := writeExtraSpec(t, t.TempDir(), extraTestSpec)

	if _, err := installer.InstallApp("updateapp", InstallOptions{SpecSource: specPath}); err != nil {
		t.Fatalf("InstallApp failed: %v", err)
	}

	t.Run("description only", func(t *testing.T) {
		if err := installer.UpdateApp("updateapp", InstallOptions{Description: "updated description"}); err != nil {
			t.Fatalf("UpdateApp failed: %v", err)
		}
		appConfig, err := installer.manager.GetAppConfig("updateapp")
		if err != nil {
			t.Fatalf("GetAppConfig failed: %v", err)
		}
		if appConfig.Description != "updated description" {
			t.Errorf("expected updated description, got %q", appConfig.Description)
		}
	})

	t.Run("spec sources reload", func(t *testing.T) {
		otherSpec := writeExtraSpec(t, t.TempDir(), extraTestSpec)
		if err := installer.UpdateApp("updateapp", InstallOptions{SpecSources: []string{otherSpec}}); err != nil {
			t.Fatalf("UpdateApp failed: %v", err)
		}
		appConfig, err := installer.manager.GetAppConfig("updateapp")
		if err != nil {
			t.Fatalf("GetAppConfig failed: %v", err)
		}
		if len(appConfig.SpecSources) != 1 || !filepath.IsAbs(appConfig.SpecSources[0]) {
			t.Errorf("expected normalized spec sources, got %v", appConfig.SpecSources)
		}
	})

	t.Run("invalid spec rejected", func(t *testing.T) {
		badSpec := filepath.Join(t.TempDir(), "bad.yaml")
		if err := os.WriteFile(badSpec, []byte("not: [valid"), 0o600); err != nil {
			t.Fatalf("write bad spec: %v", err)
		}
		err := installer.UpdateApp("updateapp", InstallOptions{SpecSource: badSpec})
		if err == nil || !strings.Contains(err.Error(), "failed to load updated spec") {
			t.Fatalf("expected spec load error, got %v", err)
		}
	})

	t.Run("missing app", func(t *testing.T) {
		err := installer.UpdateApp("missingapp", InstallOptions{Description: "x"})
		assertNonexistentAppError(t, err)
	})
}

func TestUninstallAppRemovesDefaultShim(t *testing.T) {
	installer := newExtraTestInstaller(t)
	specPath := writeExtraSpec(t, t.TempDir(), extraTestSpec)

	if _, err := installer.InstallApp("uninstallshim", InstallOptions{SpecSource: specPath}); err != nil {
		t.Fatalf("InstallApp failed: %v", err)
	}
	if _, err := installer.CreateShim("uninstallshim", ""); err != nil {
		t.Fatalf("CreateShim failed: %v", err)
	}
	if !installer.ShimExists("uninstallshim") {
		t.Fatal("expected shim to exist before uninstall")
	}

	if err := installer.UninstallApp("uninstallshim", true); err != nil {
		t.Fatalf("UninstallApp failed: %v", err)
	}
	if installer.ShimExists("uninstallshim") {
		t.Error("expected default shim to be removed")
	}

	if err := installer.UninstallApp("bad name", true); err == nil {
		t.Fatal("expected app name validation error")
	}
}

func TestGetDefaultShimDirHomeError(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("HOME lookup is not used on Windows")
	}
	t.Setenv("HOME", "")

	if _, err := getDefaultShimDir(); err == nil {
		t.Fatal("expected error when HOME is empty")
	}
}

func TestInstallAppForceMergesExistingConfig(t *testing.T) {
	installer := newExtraTestInstaller(t)
	specPath := writeExtraSpec(t, t.TempDir(), extraTestSpec)

	if _, err := installer.InstallApp("forceapp", InstallOptions{
		SpecSource:  specPath,
		Description: "original description",
	}); err != nil {
		t.Fatalf("InstallApp failed: %v", err)
	}

	result, err := installer.InstallApp("forceapp", InstallOptions{
		SpecSource: specPath,
		Force:      true,
	})
	if err != nil {
		t.Fatalf("forced InstallApp failed: %v", err)
	}
	if result.AppName != "forceapp" {
		t.Errorf("unexpected result: %+v", result)
	}

	appConfig, err := installer.manager.GetAppConfig("forceapp")
	if err != nil {
		t.Fatalf("GetAppConfig failed: %v", err)
	}
	if appConfig.Description != "original description" {
		t.Errorf("expected existing description to be merged, got %q", appConfig.Description)
	}
}

func TestInstallAppWithShimWarning(t *testing.T) {
	installer := newExtraTestInstaller(t)
	specPath := writeExtraSpec(t, t.TempDir(), extraTestSpec)
	shimDir := filepath.Join(t.TempDir(), "bin")

	var out bytes.Buffer
	result, err := installer.InstallApp("shimwarn", InstallOptions{
		SpecSource:  specPath,
		CreateShim:  true,
		ShimDir:     shimDir,
		Writer:      &out,
		Description: "shim app",
	})
	if err != nil {
		t.Fatalf("InstallApp failed: %v", err)
	}
	if result.ShimPath == "" {
		t.Fatal("expected shim path in install result")
	}
	if !strings.Contains(out.String(), "not in your PATH") {
		t.Errorf("expected PATH instructions, got %q", out.String())
	}
}
