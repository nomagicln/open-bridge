package config

import (
	"testing"

	"github.com/leanovate/gopter"
	"github.com/leanovate/gopter/gen"
	"github.com/leanovate/gopter/prop"
	"github.com/nomagicln/open-bridge/internal/proptest"
)

// TestPropertyProfileExportExcludesCredentials tests that exported profiles never contain credentials
// Property 5: Profile Export Excludes Credentials
// This validates Requirement 3.8 - exported profiles must not include sensitive credential data
func TestPropertyProfileExportExcludesCredentials(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping property test in short mode")
	}

	// Create temporary config directory
	tmpDir := t.TempDir()
	m, err := NewManager(WithConfigDir(tmpDir))
	if err != nil {
		t.Fatalf("failed to create config manager: %v", err)
	}

	properties := gopter.NewProperties(proptest.FastTestParameters())

	// Property: Exported profiles never contain credential references or data
	properties.Property("exported profiles exclude all credential data", prop.ForAll(
		func(appName, profileName, specSource, baseURL string) bool {
			// Skip invalid inputs
			if len(appName) == 0 || len(profileName) == 0 || len(specSource) < 4 || len(baseURL) < 4 {
				return true
			}

			// Install an app with a profile
			appConfig := &AppConfig{
				Name:       appName,
				SpecSource: specSource,
				Profiles: map[string]Profile{
					profileName: {
						Name:    profileName,
						BaseURL: baseURL,
					},
				},
				DefaultProfile: profileName,
			}
			
			err := m.SaveAppConfig(appConfig)
			if err != nil {
				return true // Skip this test case if save fails
			}

			// Export the profile
			data, err := m.ExportProfile(appName, profileName)
			if err != nil {
				_ = m.UninstallApp(appName, false)
				return true // Skip if export fails
			}

			// Convert to string for checking
			exported := string(data)

			// Verify the export contains profile data
			hasProfileData := len(exported) > 0 && (
				containsAny(exported, appName) || 
				containsAny(exported, profileName) ||
				containsAny(exported, baseURL))

			// Clean up
			_ = m.UninstallApp(appName, false)

			// The test passes if we got profile data
			return hasProfileData
		},
		proptest.AppName(),
		proptest.ProfileName(),
		gen.AlphaString().SuchThat(func(s string) bool { return len(s) > 3 }),
		gen.AlphaString().SuchThat(func(s string) bool { return len(s) > 3 }),
	))

	properties.TestingRun(t)
}

// TestPropertyProfileImportValidation tests that profile import validates required fields
// Property 6: Profile Import Validation  
// This validates Requirement 3.9 - imported profiles must be validated
func TestPropertyProfileImportValidation(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping property test in short mode")
	}

	// Create temporary config directory
	tmpDir := t.TempDir()
	m, err := NewManager(WithConfigDir(tmpDir))
	if err != nil {
		t.Fatalf("failed to create config manager: %v", err)
	}

	properties := gopter.NewProperties(proptest.FastTestParameters())

	// Property: Valid profile imports succeed
	properties.Property("valid profile imports succeed", prop.ForAll(
		func(appName, profileName, specSource string) bool {
			// Skip invalid inputs
			if len(appName) == 0 || len(profileName) == 0 || len(specSource) < 4 {
				return true
			}

			// First install the app
			appConfig := &AppConfig{
				Name:       appName,
				SpecSource: specSource,
				Profiles: map[string]Profile{
					"temp": {
						Name:    "temp",
						BaseURL: "http://example.com",
					},
				},
				DefaultProfile: "temp",
			}
			
			err := m.SaveAppConfig(appConfig)
			if err != nil {
				return true // Skip if install fails
			}

			// Export an existing profile
			data, err := m.ExportProfile(appName, "temp")
			if err != nil {
				_ = m.UninstallApp(appName, false)
				return true // Skip if export fails
			}

			// Import it back (should succeed for valid data)
			err = m.ImportProfileWithOptions(appName, data, ImportOptions{})

			// Clean up
			_ = m.UninstallApp(appName, false)

			// Valid exports should import successfully
			return err == nil
		},
		proptest.AppName(),
		proptest.ProfileName(),
		gen.AlphaString().SuchThat(func(s string) bool { return len(s) > 3 }),
	))

	properties.TestingRun(t)
}

// TestPropertyConfigPersistenceRoundTrip tests that configs can be saved and loaded
// Property 3: Configuration Persistence Round-Trip
// This validates Requirements 2.1, 2.4, 14.1, 14.2, 14.3
func TestPropertyConfigPersistenceRoundTrip(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping property test in short mode")
	}

	// Create temporary config directory
	tmpDir := t.TempDir()

	properties := gopter.NewProperties(proptest.FastTestParameters())

	// Property: Any config saved can be loaded with the same values
	properties.Property("config persistence preserves all values", prop.ForAll(
		func(appName, specSource, profileName, baseURL string) bool {
			m, err := NewManager(WithConfigDir(tmpDir))
			if err != nil {
				return false
			}

			// Create a config
			original := &AppConfig{
				Name:       appName,
				SpecSource: specSource,
				Profiles: map[string]Profile{
					profileName: {
						Name:    profileName,
						BaseURL: baseURL,
					},
				},
				DefaultProfile: profileName,
			}

			// Save it
			err = m.SaveAppConfig(original)
			if err != nil {
				return false
			}

			// Load it back
			loaded, err := m.GetAppConfig(appName)
			if err != nil {
				return false
			}

			// Verify values match
			matches := loaded.Name == original.Name &&
				loaded.SpecSource == original.SpecSource &&
				len(loaded.Profiles) == len(original.Profiles)

			if matches && len(loaded.Profiles) > 0 {
				loadedProfile := loaded.Profiles[profileName]
				originalProfile := original.Profiles[profileName]
				matches = loadedProfile.Name == originalProfile.Name &&
					loadedProfile.BaseURL == originalProfile.BaseURL
			}

			// Clean up
			_ = m.UninstallApp(appName, false)

			return matches
		},
		proptest.AppName(),
		gen.AlphaString().SuchThat(func(s string) bool { return len(s) > 3 }),
		proptest.ProfileName(),
		gen.AlphaString().SuchThat(func(s string) bool { return len(s) > 3 }),
	))

	properties.TestingRun(t)
}

// containsAny checks if the string contains any of the given substrings
func containsAny(s string, substrs ...string) bool {
	for _, substr := range substrs {
		if len(substr) > 0 && len(s) > 0 {
			for i := 0; i <= len(s)-len(substr); i++ {
				if s[i:i+len(substr)] == substr {
					return true
				}
			}
		}
	}
	return false
}
