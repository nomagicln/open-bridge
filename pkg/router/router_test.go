package router

import (
	"path/filepath"
	"runtime"
	"testing"

	"github.com/nomagicln/open-bridge/pkg/cli"
	"github.com/nomagicln/open-bridge/pkg/config"
	"github.com/nomagicln/open-bridge/pkg/mcp"
	"github.com/nomagicln/open-bridge/pkg/request"
	"github.com/nomagicln/open-bridge/pkg/semantic"
	"github.com/nomagicln/open-bridge/pkg/spec"
)

func TestDetectAppName(t *testing.T) {
	r := &Router{}

	tests := []struct {
		name     string
		args     []string
		expected string
	}{
		{
			name:     "shimWithObBinary",
			args:     []string{"/usr/local/bin/ob", "petstore", "list"},
			expected: "petstore",
		},
		{
			name:     "shimBinaryName",
			args:     []string{"/usr/local/bin/petstore", "list"},
			expected: "petstore",
		},
		{
			name:     "obNoArgs",
			args:     []string{"/usr/local/bin/ob"},
			expected: "ob",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := r.detectAppName(tt.args); got != tt.expected {
				t.Fatalf("detectAppName(%v) = %q, want %q", tt.args, got, tt.expected)
			}
		})
	}
}

func TestExecuteRoutesObArgs(t *testing.T) {
	tmpDir := t.TempDir()
	mgr, err := config.NewManager(config.WithConfigDir(tmpDir))
	if err != nil {
		t.Fatalf("NewManager failed: %v", err)
	}

	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("failed to resolve current test file path")
	}

	repoRoot := filepath.Clean(filepath.Join(filepath.Dir(currentFile), "..", ".."))
	specPath := filepath.Join(repoRoot, "internal", "integration", "testdata", "petstore.json")
	absSpecPath, err := filepath.Abs(specPath)
	if err != nil {
		t.Fatalf("failed to resolve spec path: %v", err)
	}

	appConfig := config.NewAppConfig("petstore", absSpecPath)
	if err := mgr.SaveAppConfig(appConfig); err != nil {
		t.Fatalf("SaveAppConfig failed: %v", err)
	}

	specParser := spec.NewParser()
	mapper := semantic.NewMapper()
	reqBuilder := request.NewBuilder(nil)
	cliHandler := cli.NewHandler(specParser, mapper, reqBuilder, mgr)
	mcpHandler := mcp.NewHandler(mapper, reqBuilder, nil)
	router := NewRouter(cliHandler, mcpHandler, mgr, specParser)

	tests := [][]string{
		{"/usr/local/bin/ob", "petstore", "--help"},
		{"/usr/local/bin/petstore", "--help"},
	}

	for _, args := range tests {
		if err := router.Execute(args); err != nil {
			t.Fatalf("Execute(%v) returned error: %v", args, err)
		}
	}
}
