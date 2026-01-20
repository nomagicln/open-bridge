package completion

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/nomagicln/open-bridge/internal/testutil"
	"github.com/nomagicln/open-bridge/pkg/config"
	"github.com/nomagicln/open-bridge/pkg/semantic"
	"github.com/nomagicln/open-bridge/pkg/spec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// setupTestEnv creates a test environment with config manager, spec parser, and mapper
func setupTestEnv(t *testing.T) (*config.Manager, *spec.Parser, *semantic.Mapper, func()) {
	t.Helper()

	configDir := t.TempDir()
	configMgr, err := config.NewManager(config.WithConfigDir(configDir))
	if err != nil {
		t.Fatalf("failed to create config manager: %v", err)
	}

	specParser := spec.NewParser()
	mapper := semantic.NewMapper()

	cleanup := func() {
		// Cleanup handled by t.TempDir()
	}

	return configMgr, specParser, mapper, cleanup
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

func TestCompleteAppNames(t *testing.T) {
	// Setup
	configMgr, specParser, mapper, cleanup := setupTestEnv(t)
	defer cleanup()

	provider := NewProvider(configMgr, specParser, mapper)

	// Install test apps
	testSpec := writeTestSpec(t)
	_, err := configMgr.InstallApp("testapp1", config.InstallOptions{
		SpecSource: testSpec,
		BaseURL:    "https://api.test1.com",
	})
	require.NoError(t, err)

	_, err = configMgr.InstallApp("testapp2", config.InstallOptions{
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

func TestCompleteVerbs(t *testing.T) {
	// Setup
	configMgr, specParser, mapper, cleanup := setupTestEnv(t)
	defer cleanup()

	provider := NewProvider(configMgr, specParser, mapper)

	// Install test app
	testSpec := writeTestSpec(t)
	_, err := configMgr.InstallApp("testapp", config.InstallOptions{
		SpecSource: testSpec,
		BaseURL:    "https://api.test.com",
	})
	require.NoError(t, err)

	// Test completion
	t.Run("List all verbs", func(t *testing.T) {
		verbs := provider.CompleteVerbs("testapp", "")
		assert.NotEmpty(t, verbs)
		// Should contain common verbs like list, get, create
		assert.Contains(t, verbs, "list")
	})

	t.Run("Filter verbs by prefix", func(t *testing.T) {
		verbs := provider.CompleteVerbs("testapp", "l")
		assert.NotEmpty(t, verbs)
		// All returned verbs should start with 'l'
		for _, verb := range verbs {
			assert.True(t, len(verb) > 0 && verb[0] == 'l')
		}
	})
}

func TestCompleteResources(t *testing.T) {
	// Setup
	configMgr, specParser, mapper, cleanup := setupTestEnv(t)
	defer cleanup()

	provider := NewProvider(configMgr, specParser, mapper)

	// Install test app
	testSpec := writeTestSpec(t)
	_, err := configMgr.InstallApp("testapp", config.InstallOptions{
		SpecSource: testSpec,
		BaseURL:    "https://api.test.com",
	})
	require.NoError(t, err)

	// Test completion
	t.Run("List all resources", func(t *testing.T) {
		resources := provider.CompleteResources("testapp", "")
		assert.NotEmpty(t, resources)
		// Should contain 'users' resource from test spec
		assert.Contains(t, resources, "users")
	})

	t.Run("Filter resources by prefix", func(t *testing.T) {
		resources := provider.CompleteResources("testapp", "u")
		assert.NotEmpty(t, resources)
		// All returned resources should start with 'u'
		for _, resource := range resources {
			assert.True(t, len(resource) > 0 && resource[0] == 'u')
		}
	})
}

func TestCompleteResourcesForVerb(t *testing.T) {
	// Setup
	configMgr, specParser, mapper, cleanup := setupTestEnv(t)
	defer cleanup()

	provider := NewProvider(configMgr, specParser, mapper)

	// Install test app
	testSpec := writeTestSpec(t)
	_, err := configMgr.InstallApp("testapp", config.InstallOptions{
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
	configMgr, specParser, mapper, cleanup := setupTestEnv(t)
	defer cleanup()

	provider := NewProvider(configMgr, specParser, mapper)

	// Install test app
	testSpec := writeTestSpec(t)
	_, err := configMgr.InstallApp("testapp", config.InstallOptions{
		SpecSource: testSpec,
		BaseURL:    "https://api.test.com",
	})
	require.NoError(t, err)

	// Test completion
	t.Run("List flags for operation", func(t *testing.T) {
		flags := provider.CompleteFlags("testapp", "get", "users", "")
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
		flags := provider.CompleteFlags("testapp", "get", "users", "--j")
		assert.NotEmpty(t, flags)
		// Should only contain flags starting with --j
		for _, flag := range flags {
			assert.True(t, len(flag) >= 3 && flag[0:3] == "--j")
		}
	})
}

func TestCompleteFlagValues(t *testing.T) {
	// Setup
	configMgr, specParser, mapper, cleanup := setupTestEnv(t)
	defer cleanup()

	provider := NewProvider(configMgr, specParser, mapper)

	// Install test app with Petstore spec that has enums
	petstoreSpec := filepath.Join(t.TempDir(), "petstore.yaml")
	if err := os.WriteFile(petstoreSpec, []byte(testutil.PetstoreOpenAPISpec), 0644); err != nil {
		t.Fatalf("failed to write petstore spec: %v", err)
	}

	result, err := configMgr.InstallApp("testapp", config.InstallOptions{
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
		values := provider.CompleteFlagValues("testapp", "create", "pet", "output")
		assert.Contains(t, values, "table")
		assert.Contains(t, values, "json")
		assert.Contains(t, values, "yaml")
	})

	t.Run("Complete profile flag values", func(t *testing.T) {
		values := provider.CompleteFlagValues("testapp", "create", "pet", "profile")
		assert.NotEmpty(t, values)
		assert.Contains(t, values, "default")
		assert.Contains(t, values, "staging")
	})

	t.Run("Complete enum flag values", func(t *testing.T) {
		values := provider.CompleteFlagValues("testapp", "create", "pet", "status")
		assert.NotEmpty(t, values)
		// The petstore spec has a status enum with these values
		assert.Contains(t, values, "available")
		assert.Contains(t, values, "pending")
		assert.Contains(t, values, "sold")
	})

	t.Run("No values for non-enum flag", func(t *testing.T) {
		values := provider.CompleteFlagValues("testapp", "create", "pet", "name")
		assert.Nil(t, values)
	})
}
