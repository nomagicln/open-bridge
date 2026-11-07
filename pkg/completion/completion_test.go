package completion

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/nomagicln/open-bridge/internal/install"
	"github.com/nomagicln/open-bridge/internal/testutil"
	"github.com/nomagicln/open-bridge/pkg/config"
	"github.com/nomagicln/open-bridge/pkg/semantic"
	"github.com/nomagicln/open-bridge/pkg/spec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// setupTestEnv creates a test environment with config manager, spec parser, and mapper
func setupTestEnv(t *testing.T) (*config.Manager, *spec.Parser, *semantic.Mapper) {
	t.Helper()

	configDir := t.TempDir()
	configMgr, err := config.NewManager(config.WithConfigDir(configDir))
	if err != nil {
		t.Fatalf("failed to create config manager: %v", err)
	}

	specParser := spec.NewParser()
	mapper := semantic.NewMapper()

	return configMgr, specParser, mapper
}

// writeTestSpec writes the test spec to a temp file
func writeTestSpec(t *testing.T) string {
	t.Helper()
	specPath := filepath.Join(t.TempDir(), "spec.yaml")
	if err := os.WriteFile(specPath, []byte(testutil.MinimalOpenAPISpec), 0644); err != nil {
		t.Fatalf("failed to write spec: %v", err)
	}
	return specPath
}

func setupProviderWithApp(t *testing.T) *Provider {
	t.Helper()

	configMgr, specParser, mapper := setupTestEnv(t)
	provider := NewProvider(configMgr, specParser, mapper)

	testSpec := writeTestSpec(t)
	_, err := install.New(configMgr).InstallApp("testapp", install.InstallOptions{
		SpecSource: testSpec,
		BaseURL:    "https://api.test.com",
	})
	require.NoError(t, err)

	return provider
}

func TestCompleteAppNames(t *testing.T) {
	// Setup
	configMgr, specParser, mapper := setupTestEnv(t)

	provider := NewProvider(configMgr, specParser, mapper)

	// Install test apps
	testSpec := writeTestSpec(t)
	_, err := install.New(configMgr).InstallApp("testapp1", install.InstallOptions{
		SpecSource: testSpec,
		BaseURL:    "https://api.test1.com",
	})
	require.NoError(t, err)

	_, err = install.New(configMgr).InstallApp("testapp2", install.InstallOptions{
		SpecSource: testSpec,
		BaseURL:    "https://api.test2.com",
	})
	require.NoError(t, err)

	// Test completion
	t.Run("List all apps with empty prefix", func(t *testing.T) {
		apps := provider.CompleteAppNames("")
		assert.Contains(t, apps, "testapp1")
		assert.Contains(t, apps, "testapp2")
	})

	t.Run("Filter apps by prefix", func(t *testing.T) {
		apps := provider.CompleteAppNames("testapp1")
		assert.Contains(t, apps, "testapp1")
		assert.NotContains(t, apps, "testapp2")
	})

	t.Run("No matches for invalid prefix", func(t *testing.T) {
		apps := provider.CompleteAppNames("nonexistent")
		assert.Empty(t, apps)
	})
}

func TestCompleteVerbsAndResources(t *testing.T) {
	provider := setupProviderWithApp(t)

	type testCase struct {
		name         string
		listFn       func(prefix string) []string
		mustContain  string
		prefix       string
		prefixLetter byte
	}

	cases := []testCase{
		{
			name:         "verbs",
			listFn:       func(prefix string) []string { return provider.CompleteVerbs("testapp", prefix) },
			mustContain:  "list",
			prefix:       "l",
			prefixLetter: 'l',
		},
		{
			name:         "resources",
			listFn:       func(prefix string) []string { return provider.CompleteResources("testapp", prefix) },
			mustContain:  "users",
			prefix:       "u",
			prefixLetter: 'u',
		},
	}

	for _, tc := range cases {
		t.Run("List_"+tc.name, func(t *testing.T) {
			items := tc.listFn("")
			assert.NotEmpty(t, items)
			assert.Contains(t, items, tc.mustContain)
		})

		t.Run("Filter_"+tc.name, func(t *testing.T) {
			items := tc.listFn(tc.prefix)
			assert.NotEmpty(t, items)
			for _, item := range items {
				assert.True(t, len(item) > 0 && item[0] == tc.prefixLetter)
			}
		})
	}
}

func TestCompleteResourcesForVerb(t *testing.T) {
	// Setup
	configMgr, specParser, mapper := setupTestEnv(t)

	provider := NewProvider(configMgr, specParser, mapper)

	// Install test app
	testSpec := writeTestSpec(t)
	_, err := install.New(configMgr).InstallApp("testapp", install.InstallOptions{
		SpecSource: testSpec,
		BaseURL:    "https://api.test.com",
	})
	require.NoError(t, err)

	// Test completion
	t.Run("List resources for list verb", func(t *testing.T) {
		resources := provider.CompleteResourcesForVerb("testapp", "list", "")
		assert.NotEmpty(t, resources)
		// Users should support list operation
		assert.Contains(t, resources, "users")
	})

	t.Run("Filter resources by prefix for verb", func(t *testing.T) {
		resources := provider.CompleteResourcesForVerb("testapp", "list", "u")
		assert.NotEmpty(t, resources)
		for _, resource := range resources {
			assert.True(t, len(resource) > 0 && resource[0] == 'u')
		}
	})
}

func TestCompleteFlags(t *testing.T) {
	// Setup
	configMgr, specParser, mapper := setupTestEnv(t)

	provider := NewProvider(configMgr, specParser, mapper)

	// Install test app
	testSpec := writeTestSpec(t)
	_, err := install.New(configMgr).InstallApp("testapp", install.InstallOptions{
		SpecSource: testSpec,
		BaseURL:    "https://api.test.com",
	})
	require.NoError(t, err)

	// Test completion
	t.Run("List flags for operation", func(t *testing.T) {
		flags := provider.CompleteFlags("testapp", "users", "get", "")
		assert.NotEmpty(t, flags)
		// Should contain common flags and operation-specific flags
		assert.Contains(t, flags, "--json")
		assert.Contains(t, flags, "--yaml")
		assert.Contains(t, flags, "--output")
		assert.Contains(t, flags, "--profile")
		// Should contain operation parameter (id for get user)
		assert.Contains(t, flags, "--id")
	})

	t.Run("Filter flags by prefix", func(t *testing.T) {
		flags := provider.CompleteFlags("testapp", "users", "get", "--j")
		assert.NotEmpty(t, flags)
		// Should only contain flags starting with --j
		for _, flag := range flags {
			assert.True(t, len(flag) >= 3 && flag[0:3] == "--j")
		}
	})
}

func TestCompleteFlagValues(t *testing.T) {
	// Setup
	configMgr, specParser, mapper := setupTestEnv(t)

	provider := NewProvider(configMgr, specParser, mapper)

	// Install test app with Petstore spec that has enums
	petstoreSpec := filepath.Join(t.TempDir(), "petstore.yaml")
	if err := os.WriteFile(petstoreSpec, []byte(testutil.PetstoreOpenAPISpec), 0644); err != nil {
		t.Fatalf("failed to write petstore spec: %v", err)
	}

	result, err := install.New(configMgr).InstallApp("testapp", install.InstallOptions{
		SpecSource: petstoreSpec,
		BaseURL:    "https://api.test.com",
	})
	require.NoError(t, err)

	// Create a profile for testing profile completion
	appConfig, err := configMgr.GetAppConfig(result.AppName)
	require.NoError(t, err)
	appConfig.Profiles["staging"] = config.Profile{
		Name:    "staging",
		BaseURL: "https://api.staging.test.com",
	}
	err = configMgr.SaveAppConfig(appConfig)
	require.NoError(t, err)

	// Test completion
	t.Run("Complete output flag values", func(t *testing.T) {
		values := provider.CompleteFlagValues("testapp", "pet", "create", "output")
		assert.Contains(t, values, "table")
		assert.Contains(t, values, "json")
		assert.Contains(t, values, "yaml")
	})

	t.Run("Complete profile flag values", func(t *testing.T) {
		values := provider.CompleteFlagValues("testapp", "pet", "create", "profile")
		assert.NotEmpty(t, values)
		assert.Contains(t, values, "default")
		assert.Contains(t, values, "staging")
	})

	t.Run("Complete enum flag values", func(t *testing.T) {
		values := provider.CompleteFlagValues("testapp", "pet", "create", "status")
		assert.NotEmpty(t, values)
		// The petstore spec has a status enum with these values
		assert.Contains(t, values, "available")
		assert.Contains(t, values, "pending")
		assert.Contains(t, values, "sold")
	})

	t.Run("No values for non-enum flag", func(t *testing.T) {
		values := provider.CompleteFlagValues("testapp", "pet", "create", "name")
		assert.Nil(t, values)
	})
}

// completionIndexSpec is a two-resource spec used by the derived-index
// completion tests.
const completionIndexSpec = `{
  "openapi": "3.0.0",
  "info": {"title": "Completion Index API", "version": "1.0.0"},
  "paths": {
    "/pets": {
      "get": {"operationId": "listPets", "summary": "List pets", "responses": {"200": {"description": "OK"}}},
      "post": {"operationId": "createPet", "summary": "Create pet", "responses": {"201": {"description": "Created"}}}
    },
    "/toys": {
      "get": {"operationId": "listToys", "summary": "List toys", "responses": {"200": {"description": "OK"}}}
    }
  }
}`

// writeCompletionIndexSpec writes the shared spec and returns its path.
func writeCompletionIndexSpec(t *testing.T) string {
	t.Helper()

	specPath := filepath.Join(t.TempDir(), "completion-index.json")
	if err := os.WriteFile(specPath, []byte(completionIndexSpec), 0o600); err != nil {
		t.Fatalf("write completion spec: %v", err)
	}

	return specPath
}

// corruptCompletionSpec rewrites the spec with garbage of the same size and
// restores its modification time, so the derived-index key still matches while
// parsing the file would fail.
func corruptCompletionSpec(t *testing.T, specPath string) {
	t.Helper()

	info, err := os.Stat(specPath)
	if err != nil {
		t.Fatalf("stat spec: %v", err)
	}

	garbage := make([]byte, int(info.Size()))
	for i := range garbage {
		garbage[i] = 'x'
	}
	if err := os.WriteFile(specPath, garbage, 0o600); err != nil {
		t.Fatalf("corrupt spec: %v", err)
	}
	if err := os.Chtimes(specPath, info.ModTime(), info.ModTime()); err != nil {
		t.Fatalf("restore spec mtime: %v", err)
	}
}

// saveCompletionIndexApp persists an app config pointing at specPath.
func saveCompletionIndexApp(t *testing.T, configMgr *config.Manager, appName, specPath string) {
	t.Helper()

	appConfig := &config.AppConfig{
		Name:           appName,
		SpecSource:     specPath,
		DefaultProfile: "default",
		Profiles: map[string]config.Profile{
			"default": {Name: "default", BaseURL: "https://api.example.com"},
		},
	}
	require.NoError(t, configMgr.SaveAppConfig(appConfig))
}

// newIndexedProvider builds a completion provider whose parser uses a persistent
// derived index cache.
func newIndexedProvider(configMgr *config.Manager, indexDir string) *Provider {
	return NewProvider(configMgr, spec.NewParser(spec.WithIndexCache(indexDir)), semantic.NewMapper())
}

func TestCompleteResourcesUsesDerivedIndexWithoutParsing(t *testing.T) {
	configMgr, _, mapper := setupTestEnv(t)
	specPath := writeCompletionIndexSpec(t)
	saveCompletionIndexApp(t, configMgr, "idxcomplete", specPath)
	indexDir := t.TempDir()

	cold := NewProvider(configMgr, spec.NewParser(spec.WithIndexCache(indexDir)), mapper)
	coldResources := cold.CompleteResources("idxcomplete", "")
	require.NotEmpty(t, coldResources)
	assert.Contains(t, coldResources, "pets")
	assert.Contains(t, coldResources, "toys")

	corruptCompletionSpec(t, specPath)

	warm := newIndexedProvider(configMgr, indexDir)
	warmResources := warm.CompleteResources("idxcomplete", "")
	assert.Equal(t, coldResources, warmResources,
		"completion should be served from the derived index after the spec becomes unparseable")
}

func TestCompleteVerbsForResourceUsesDerivedIndexWithoutParsing(t *testing.T) {
	configMgr, _, mapper := setupTestEnv(t)
	specPath := writeCompletionIndexSpec(t)
	saveCompletionIndexApp(t, configMgr, "idxverbs", specPath)
	indexDir := t.TempDir()

	cold := NewProvider(configMgr, spec.NewParser(spec.WithIndexCache(indexDir)), mapper)
	coldResources := cold.CompleteResources("idxverbs", "")
	require.NotEmpty(t, coldResources)
	coldVerbs := cold.CompleteVerbsForResource("idxverbs", "pets", "")
	require.NotEmpty(t, coldVerbs)
	assert.Contains(t, coldVerbs, "list")
	assert.Contains(t, coldVerbs, "create")

	corruptCompletionSpec(t, specPath)

	warm := newIndexedProvider(configMgr, indexDir)
	warmVerbs := warm.CompleteVerbsForResource("idxverbs", "pets", "")
	assert.Equal(t, coldVerbs, warmVerbs,
		"verb completion should be served from the derived index after the spec becomes unparseable")
}

func TestCompleteVerbsUsesDerivedIndexWithoutParsing(t *testing.T) {
	configMgr, _, mapper := setupTestEnv(t)
	specPath := writeCompletionIndexSpec(t)
	saveCompletionIndexApp(t, configMgr, "idxallverbs", specPath)
	indexDir := t.TempDir()

	cold := NewProvider(configMgr, spec.NewParser(spec.WithIndexCache(indexDir)), mapper)
	coldVerbs := cold.CompleteVerbs("idxallverbs", "")
	require.NotEmpty(t, coldVerbs)

	corruptCompletionSpec(t, specPath)

	warm := newIndexedProvider(configMgr, indexDir)
	assert.Equal(t, coldVerbs, warm.CompleteVerbs("idxallverbs", ""))
}

func TestCompleteResourcesWithFilterUsesDerivedIndex(t *testing.T) {
	configMgr, _, mapper := setupTestEnv(t)
	indexDir := t.TempDir()
	specPath := writeCompletionIndexSpec(t)
	saveCompletionIndexApp(t, configMgr, "idxfilter", specPath)

	cold := NewProvider(configMgr, spec.NewParser(spec.WithIndexCache(indexDir)), mapper)
	filtered := cold.CompleteResources("idxfilter", "pe")
	assert.Equal(t, []string{"pets"}, filtered)

	corruptCompletionSpec(t, specPath)

	warm := newIndexedProvider(configMgr, indexDir)
	assert.Equal(t, []string{"pets"}, warm.CompleteResources("idxfilter", "pe"))
}

func TestCompletionFallsBackWhenIndexDisabled(t *testing.T) {
	configMgr, specParser, mapper := setupTestEnv(t)
	specPath := writeCompletionIndexSpec(t)
	saveCompletionIndexApp(t, configMgr, "noindex", specPath)

	provider := NewProvider(configMgr, specParser, mapper)
	assert.Contains(t, provider.CompleteResources("noindex", ""), "pets")
	assert.Contains(t, provider.CompleteVerbsForResource("noindex", "pets", ""), "list")
}

// completionCommandSpec describes a command with query parameters (including an
// enum) and a required request body, so flag completion can be compared between
// the parsing and derived-index paths.
const completionCommandSpec = `{
  "openapi": "3.0.0",
  "info": {"title": "Completion Command API", "version": "1.0.0"},
  "paths": {
    "/pets": {
      "post": {
        "operationId": "createPet",
        "summary": "Create pet",
        "parameters": [
          {"name": "limit", "in": "query", "required": false, "schema": {"type": "integer"}},
          {"name": "status", "in": "query", "required": true,
           "schema": {"type": "string", "enum": ["available", "pending"]}}
        ],
        "requestBody": {
          "required": true,
          "content": {"application/json": {"schema": {
            "type": "object",
            "required": ["name"],
            "properties": {
              "name": {"type": "string"},
              "kind": {"type": "string", "enum": ["cat", "dog"]}
            }
          }}}
        },
        "responses": {"201": {"description": "Created"}}
      }
    }
  }
}`

// writeCompletionCommandSpec writes the command fixture spec.
func writeCompletionCommandSpec(t *testing.T) string {
	t.Helper()

	specPath := filepath.Join(t.TempDir(), "completion-command.json")
	if err := os.WriteFile(specPath, []byte(completionCommandSpec), 0o600); err != nil {
		t.Fatalf("write completion command spec: %v", err)
	}

	return specPath
}

func TestCompleteFlagsUsesDerivedIndexWithoutParsing(t *testing.T) {
	configMgr, _, mapper := setupTestEnv(t)
	specPath := writeCompletionCommandSpec(t)
	saveCompletionIndexApp(t, configMgr, "idxflags", specPath)
	indexDir := t.TempDir()

	cold := NewProvider(configMgr, spec.NewParser(), mapper)
	coldFlags := cold.CompleteFlags("idxflags", "pets", "create", "")
	require.NotEmpty(t, coldFlags)
	assert.Contains(t, coldFlags, "--limit")
	assert.Contains(t, coldFlags, "--status")
	assert.Contains(t, coldFlags, "--name")
	assert.Contains(t, coldFlags, "--kind")
	assert.Contains(t, coldFlags, "--json")

	// Prime the index, then make the spec unparseable: a matching warm result
	// proves the index path served the completion without parsing.
	primer := newIndexedProvider(configMgr, indexDir)
	require.NotEmpty(t, primer.CompleteFlags("idxflags", "pets", "create", ""))
	corruptCompletionSpec(t, specPath)

	warm := newIndexedProvider(configMgr, indexDir)
	warmFlags := warm.CompleteFlags("idxflags", "pets", "create", "")
	assert.Equal(t, coldFlags, warmFlags)
	if _, ok := warm.specParser.GetCachedSpec("idxflags"); ok {
		t.Error("expected flag completion to come from the derived index, not parsing")
	}
}

func TestCompleteFlagValuesUsesDerivedIndexWithoutParsing(t *testing.T) {
	configMgr, _, mapper := setupTestEnv(t)
	specPath := writeCompletionCommandSpec(t)
	saveCompletionIndexApp(t, configMgr, "idxvalues", specPath)
	indexDir := t.TempDir()

	cold := NewProvider(configMgr, spec.NewParser(), mapper)
	coldStatus := cold.CompleteFlagValues("idxvalues", "pets", "create", "--status")
	coldKind := cold.CompleteFlagValues("idxvalues", "pets", "create", "--kind")
	require.Equal(t, []string{"available", "pending"}, coldStatus)
	require.Equal(t, []string{"cat", "dog"}, coldKind)

	primer := newIndexedProvider(configMgr, indexDir)
	require.NotEmpty(t, primer.CompleteFlagValues("idxvalues", "pets", "create", "--status"))
	corruptCompletionSpec(t, specPath)

	warm := newIndexedProvider(configMgr, indexDir)
	assert.Equal(t, coldStatus, warm.CompleteFlagValues("idxvalues", "pets", "create", "--status"))
	assert.Equal(t, coldKind, warm.CompleteFlagValues("idxvalues", "pets", "create", "--kind"))
	if _, ok := warm.specParser.GetCachedSpec("idxvalues"); ok {
		t.Error("expected flag-value completion to come from the derived index, not parsing")
	}
}

func TestCompleteFlagValuesFallsBackWhenIndexDisabled(t *testing.T) {
	configMgr, specParser, mapper := setupTestEnv(t)
	specPath := writeCompletionCommandSpec(t)
	saveCompletionIndexApp(t, configMgr, "noindexvalues", specPath)

	provider := NewProvider(configMgr, specParser, mapper)
	assert.Equal(t, []string{"available", "pending"}, provider.CompleteFlagValues("noindexvalues", "pets", "create", "--status"))
	assert.Equal(t, []string{"cat", "dog"}, provider.CompleteFlagValues("noindexvalues", "pets", "create", "--kind"))
	assert.Contains(t, provider.CompleteFlags("noindexvalues", "pets", "create", ""), "--limit")
}
