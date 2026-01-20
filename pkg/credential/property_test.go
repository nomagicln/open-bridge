package credential

import (
	"testing"
	"time"

	"github.com/leanovate/gopter"
	"github.com/leanovate/gopter/gen"
	"github.com/leanovate/gopter/prop"
	"github.com/nomagicln/open-bridge/internal/proptest"
)

// credentialTypeGen generates random credential types for testing
func credentialTypeGen() gopter.Gen {
	return gen.OneConstOf(
		CredentialTypeBearer,
		CredentialTypeAPIKey,
		CredentialTypeBasic,
		CredentialTypeOAuth2,
	)
}

// TestPropertyCredentialRoundTrip tests that credentials can be stored and retrieved
// Property 4: Credential Keyring Round-Trip
// This test validates that any credential stored in the keyring can be retrieved
// with the same values (Requirements 2.7, 8.1, 8.2, 8.6)
func TestPropertyCredentialRoundTrip(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping property test in short mode")
	}

	if isCI() {
		t.Skip("skipping keyring test in CI environment")
	}

	m, err := NewManager()
	if err != nil {
		t.Skipf("keyring not available: %v", err)
	}

	properties := gopter.NewProperties(proptest.FastTestParameters())

	// Property: Any credential stored can be retrieved with the same values
	properties.Property("credential round-trip preserves values", prop.ForAll(
		func(appName, profileName string, credType CredentialType) bool {
			// Clean up before test
			_ = m.DeleteCredential(appName, profileName)

			var cred *Credential
			switch credType {
			case CredentialTypeBearer:
				cred = NewBearerCredential("test-bearer-token-" + appName)
			case CredentialTypeAPIKey:
				cred = NewAPIKeyCredential("test-api-key-" + appName)
			case CredentialTypeBasic:
				cred = NewBasicCredential("user-"+appName, "pass-"+profileName)
			case CredentialTypeOAuth2:
				cred = NewOAuth2Credential("access-"+appName, "refresh-"+profileName, "Bearer", testTime())
			default:
				cred = NewBearerCredential("default-token")
			}

			// Store credential
			err := m.StoreCredential(appName, profileName, cred)
			if err != nil {
				return false
			}

			// Retrieve credential
			retrieved, err := m.GetCredential(appName, profileName)
			if err != nil {
				return false
			}

			// Verify type matches
			if retrieved.Type != cred.Type {
				return false
			}

			// Verify values match based on type
			valid := false
			switch credType {
			case CredentialTypeBearer:
				valid = retrieved.Token == cred.Token
			case CredentialTypeAPIKey:
				valid = retrieved.Token == cred.Token
			case CredentialTypeBasic:
				valid = retrieved.Username == cred.Username && retrieved.Password == cred.Password
			case CredentialTypeOAuth2:
				valid = retrieved.AccessToken == cred.AccessToken &&
					retrieved.RefreshToken == cred.RefreshToken &&
					retrieved.TokenType == cred.TokenType
			}

			// Clean up after test
			_ = m.DeleteCredential(appName, profileName)

			return valid
		},
		proptest.AppName(),
		proptest.ProfileName(),
		credentialTypeGen(),
	))

	properties.TestingRun(t)
}

// TestPropertyCredentialDeletion tests that deleted credentials cannot be retrieved
func TestPropertyCredentialDeletion(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping property test in short mode")
	}

	if isCI() {
		t.Skip("skipping keyring test in CI environment")
	}

	m, err := NewManager()
	if err != nil {
		t.Skipf("keyring not available: %v", err)
	}

	properties := gopter.NewProperties(proptest.FastTestParameters())

	// Property: Deleted credentials cannot be retrieved
	properties.Property("deleted credentials cannot be retrieved", prop.ForAll(
		func(appName, profileName string) bool {
			// Clean up first
			_ = m.DeleteCredential(appName, profileName)

			// Store a credential
			cred := NewBearerCredential("test-token-" + appName)
			err := m.StoreCredential(appName, profileName, cred)
			if err != nil {
				return false
			}

			// Verify it exists
			if !m.HasCredential(appName, profileName) {
				return false
			}

			// Delete it
			err = m.DeleteCredential(appName, profileName)
			if err != nil {
				return false
			}

			// Verify it's gone
			return !m.HasCredential(appName, profileName)
		},
		proptest.AppName(),
		proptest.ProfileName(),
	))

	properties.TestingRun(t)
}

// TestPropertyCredentialIsolation tests that credentials are isolated by app and profile
func TestPropertyCredentialIsolation(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping property test in short mode")
	}

	if isCI() {
		t.Skip("skipping keyring test in CI environment")
	}

	m, err := NewManager()
	if err != nil {
		t.Skipf("keyring not available: %v", err)
	}

	properties := gopter.NewProperties(proptest.FastTestParameters())

	// Property: Credentials from different apps/profiles don't interfere
	properties.Property("credentials are isolated by app and profile", prop.ForAll(
		func(app1, app2, profile1, profile2 string) bool {
			// Skip if names are the same
			if app1 == app2 && profile1 == profile2 {
				return true
			}

			// Clean up
			_ = m.DeleteCredential(app1, profile1)
			_ = m.DeleteCredential(app2, profile2)

			// Store two different credentials
			cred1 := NewBearerCredential("token1-" + app1)
			cred2 := NewBearerCredential("token2-" + app2)

			err1 := m.StoreCredential(app1, profile1, cred1)
			err2 := m.StoreCredential(app2, profile2, cred2)
			if err1 != nil || err2 != nil {
				return false
			}

			// Retrieve and verify they're different
			retrieved1, err1 := m.GetCredential(app1, profile1)
			retrieved2, err2 := m.GetCredential(app2, profile2)
			if err1 != nil || err2 != nil {
				return false
			}

			result := retrieved1.Token == cred1.Token && retrieved2.Token == cred2.Token

			// Clean up
			_ = m.DeleteCredential(app1, profile1)
			_ = m.DeleteCredential(app2, profile2)

			return result
		},
		proptest.AppName(),
		proptest.AppName(),
		proptest.ProfileName(),
		proptest.ProfileName(),
	))

	properties.TestingRun(t)
}

func testTime() time.Time {
	return time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
}
