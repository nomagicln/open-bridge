package app

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/nomagicln/open-bridge/pkg/cli"
	"github.com/nomagicln/open-bridge/pkg/completion"
	"github.com/nomagicln/open-bridge/pkg/config"
	"github.com/nomagicln/open-bridge/pkg/mcp"
	"github.com/nomagicln/open-bridge/pkg/request"
	"github.com/nomagicln/open-bridge/pkg/router"
	"github.com/nomagicln/open-bridge/pkg/semantic"
	"github.com/nomagicln/open-bridge/pkg/spec"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// completionFlagSpec is a spec with enum parameters and a body enum so flag
// value completion can be exercised end to end.
const completionFlagSpec = `{
  "openapi": "3.0.0",
  "info": {"title": "Completion Flag API", "version": "1.0.0"},
  "paths": {
    "/pets": {
      "get": {
        "operationId": "listPets",
        "summary": "List pets",
        "parameters": [
          {"name": "status", "in": "query", "required": true,
           "schema": {"type": "string", "enum": ["available", "pending"]}},
          {"name": "sort", "in": "query",
           "schema": {"type": "string", "enum": ["asc", "desc"]}},
          {"name": "limit", "in": "query", "schema": {"type": "integer"}}
        ],
        "responses": {"200": {"description": "OK"}}
      }
    }
  }
}`

// writeCompletionFlagSpec writes the completion fixture spec.
func writeCompletionFlagSpec(t *testing.T) string {
	t.Helper()

	specPath := filepath.Join(t.TempDir(), "completion-flags.json")
	if err := os.WriteFile(specPath, []byte(completionFlagSpec), 0o600); err != nil {
		t.Fatalf("write completion flag spec: %v", err)
	}

	return specPath
}

// newTestDepsWithParser builds a wired Deps graph around an existing parser,
// isolated in a temporary config directory.
func newTestDepsWithParser(t *testing.T, parser *spec.Parser) *Deps {
	t.Helper()

	configDir := t.TempDir()
	t.Setenv("OPENBRIDGE_CONFIG_DIR", configDir)
	t.Setenv("HOME", t.TempDir())

	configMgr, err := config.NewManager()
	require.NoError(t, err)

	mapper := semantic.NewMapper()
	reqBuilder := request.NewBuilder(nil)
	cliHandler := cli.NewHandler(parser, mapper, reqBuilder, configMgr)
	mcpHandler := mcp.NewHandler(mapper, reqBuilder, nil)
	appRouter := router.NewRouter(cliHandler, mcpHandler, configMgr, parser)

	return &Deps{
		Config:     configMgr,
		Parser:     parser,
		Mapper:     mapper,
		Builder:    reqBuilder,
		CLI:        cliHandler,
		MCP:        mcpHandler,
		Router:     appRouter,
		Completion: completion.NewProvider(configMgr, parser, mapper),
	}
}

// installCompletionApp persists an app config pointing at specPath.
func installCompletionApp(t *testing.T, deps *Deps, appName, specPath string) {
	t.Helper()

	appConfig := &config.AppConfig{
		Name:           appName,
		SpecSource:     specPath,
		DefaultProfile: "default",
		Profiles: map[string]config.Profile{
			"default": {Name: "default", BaseURL: "https://api.example.com"},
		},
	}
	require.NoError(t, deps.Config.SaveAppConfig(appConfig))
}

// corruptSpecPreservingStat rewrites specPath with garbage of the same size and
// restores its modification time so the derived-index key still matches while
// parsing the file would fail.
func corruptSpecPreservingStat(t *testing.T, specPath string) {
	t.Helper()

	info, err := os.Stat(specPath)
	require.NoError(t, err)

	garbage := bytes.Repeat([]byte("x"), int(info.Size()))
	require.NoError(t, os.WriteFile(specPath, garbage, 0o600))
	require.NoError(t, os.Chtimes(specPath, info.ModTime(), info.ModTime()))
}

func TestCompleteRunArgsFlagValueEnum(t *testing.T) {
	deps := newTestDeps(t)
	specPath := writeCompletionFlagSpec(t)
	installCompletionApp(t, deps, "cameras", specPath)

	args := []string{"cameras", "pets", "list", "--status"}

	got, directive := deps.completeRunArgs(nil, args, "")
	assert.Equal(t, []string{"available", "pending"}, got)
	assert.Equal(t, cobra.ShellCompDirectiveNoFileComp, directive)

	// A second typed flag value is filtered the same way.
	got, directive = deps.completeRunArgs(nil, []string{"cameras", "pets", "list"}, "--sort=d")
	assert.Equal(t, []string{"--sort=desc"}, got)
	assert.Equal(t, cobra.ShellCompDirectiveNoFileComp, directive)
}

func TestCompleteRunArgsFlagValueEqualsForm(t *testing.T) {
	deps := newTestDeps(t)
	specPath := writeCompletionFlagSpec(t)
	installCompletionApp(t, deps, "eqapp", specPath)

	got, directive := deps.completeRunArgs(nil, []string{"eqapp", "pets", "list"}, "--status=")
	assert.Equal(t, []string{"--status=available", "--status=pending"}, got)
	assert.Equal(t, cobra.ShellCompDirectiveNoFileComp, directive)

	got, directive = deps.completeRunArgs(nil, []string{"eqapp", "pets", "list"}, "--status=a")
	assert.Equal(t, []string{"--status=available"}, got)
	assert.Equal(t, cobra.ShellCompDirectiveNoFileComp, directive)

	got, directive = deps.completeRunArgs(nil, []string{"eqapp", "pets", "list"}, "--status=zzz")
	assert.Empty(t, got)
	assert.Equal(t, cobra.ShellCompDirectiveNoFileComp, directive)

	// Unknown flag in "flag=" form: no values, file completion stays disabled.
	got, directive = deps.completeRunArgs(nil, []string{"eqapp", "pets", "list"}, "--bogus=x")
	assert.Empty(t, got)
	assert.Equal(t, cobra.ShellCompDirectiveNoFileComp, directive)
}

func TestCompleteRunArgsFlagValueCommonOutput(t *testing.T) {
	deps := newTestDeps(t)
	specPath := writeCompletionFlagSpec(t)
	installCompletionApp(t, deps, "outputapp", specPath)

	got, directive := deps.completeRunArgs(nil, []string{"outputapp", "pets", "list", "--output"}, "")
	assert.Contains(t, got, "json")
	assert.Contains(t, got, "yaml")
	assert.NotContains(t, got, "--json")
	assert.Equal(t, cobra.ShellCompDirectiveNoFileComp, directive)

	got, directive = deps.completeRunArgs(nil, []string{"outputapp", "pets", "list"}, "--output=js")
	assert.Equal(t, []string{"--output=json"}, got)
	assert.Equal(t, cobra.ShellCompDirectiveNoFileComp, directive)
}

func TestCompleteRunArgsUnknownFlagFallsBackToFlagNames(t *testing.T) {
	deps := newTestDeps(t)
	specPath := writeCompletionFlagSpec(t)
	installCompletionApp(t, deps, "fallbackapp", specPath)

	got, directive := deps.completeRunArgs(nil, []string{"fallbackapp", "pets", "list", "--nope"}, "")
	assert.Contains(t, got, "--json")
	assert.Contains(t, got, "--status")
	assert.NotContains(t, got, "available")
	assert.Equal(t, cobra.ShellCompDirectiveNoFileComp, directive)
}

func TestCompleteRunArgsFlagNameContextUnchanged(t *testing.T) {
	indexedDeps := newTestDepsWithParser(t, spec.NewParser(spec.WithIndexCache(t.TempDir())))
	specPath := writeCompletionFlagSpec(t)
	installCompletionApp(t, indexedDeps, "flagsapp", specPath)

	// Flag-name completion keeps using the existing completeFlags path.
	got, directive := indexedDeps.completeRunArgs(nil, []string{"flagsapp", "pets", "list"}, "--st")
	assert.Equal(t, []string{"--status"}, got)
	assert.Equal(t, cobra.ShellCompDirectiveNoFileComp, directive)

	// Bare completion after the verb still lists flag names.
	got, directive = indexedDeps.completeRunArgs(nil, []string{"flagsapp", "pets", "list"}, "")
	assert.Contains(t, got, "--json")
	assert.Contains(t, got, "--status")
	assert.Equal(t, cobra.ShellCompDirectiveNoFileComp, directive)
}

func TestCompleteRunArgsRejectsUnknownApp(t *testing.T) {
	deps := newTestDeps(t)

	got, directive := deps.completeRunArgs(nil, []string{"missing-app", "pets"}, "")
	assert.Nil(t, got)
	assert.Equal(t, cobra.ShellCompDirectiveError, directive)
}

func TestCompleteRunArgsFlagValueIndexMatchesFallback(t *testing.T) {
	// Fallback path: no persistent index cache.
	plainDeps := newTestDeps(t)
	specPath := writeCompletionFlagSpec(t)
	installCompletionApp(t, plainDeps, "indexapp", specPath)
	want, wantDirective := plainDeps.completeRunArgs(nil, []string{"indexapp", "pets", "list", "--status"}, "")

	// Index path: a second graph with the persistent derived index enabled.
	indexedDeps := newTestDepsWithParser(t, spec.NewParser(spec.WithIndexCache(t.TempDir())))
	installCompletionApp(t, indexedDeps, "indexapp", specPath)

	got, directive := indexedDeps.completeRunArgs(nil, []string{"indexapp", "pets", "list", "--status"}, "")
	assert.Equal(t, want, got)
	assert.Equal(t, wantDirective, directive)

	// Corrupting the spec (same size/mtime) proves the second lookup is served
	// from the derived index rather than by parsing the spec again.
	corruptSpecPreservingStat(t, specPath)

	got, directive = indexedDeps.completeRunArgs(nil, []string{"indexapp", "pets", "list", "--status"}, "")
	assert.Equal(t, want, got)
	assert.Equal(t, wantDirective, directive)
}

func TestFlagValueCompletionContext(t *testing.T) {
	tests := []struct {
		name       string
		args       []string
		toComplete string
		flagName   string
		prefix     string
		equalsForm bool
		ok         bool
	}{
		{"empty value after flag", []string{"a", "res", "verb", "--status"}, "", "--status", "", false, true},
		{"equals empty prefix", []string{"a", "res", "verb"}, "--status=", "--status", "", true, true},
		{"equals prefix", []string{"a", "res", "verb"}, "--status=av", "--status", "av", true, true},
		{"flag name completion", []string{"a", "res", "verb"}, "--st", "", "", false, false},
		{"bare completion", []string{"a", "res", "verb"}, "", "", "", false, false},
		{"value word only", []string{"a", "res", "verb", "--status"}, "av", "", "", false, false},
		{"non-flag last arg", []string{"a", "res", "verb", "--status", "available"}, "", "", "", false, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			flagName, prefix, equalsForm, ok := flagValueCompletionContext(tt.args, tt.toComplete)
			assert.Equal(t, tt.flagName, flagName)
			assert.Equal(t, tt.prefix, prefix)
			assert.Equal(t, tt.equalsForm, equalsForm)
			assert.Equal(t, tt.ok, ok)
		})
	}
}
