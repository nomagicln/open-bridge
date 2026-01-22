//go:build integration

package credential

import (
	"testing"

	"github.com/99designs/keyring"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// setupTestManager creates a Manager with file backend for testing.
func setupTestManager(t *testing.T) *Manager {
	t.Helper()

	tmpDir := t.TempDir()

	mgr, err := NewManager(
		WithAllowedBackends(keyring.FileBackend),
		WithFileBackend(tmpDir, keyring.FixedStringPrompt("test-password")),
	)
	require.NoError(t, err)
	require.NotNil(t, mgr)

	return mgr
}

func TestNewManager(t *testing.T) {
	mgr := setupTestManager(t)

	assert.True(t, mgr.IsInitialized())
	assert.NotEmpty(t, mgr.Backend())
}

func TestManagerStoreAndGetCredential(t *testing.T) {
	mgr := setupTestManager(t)

	cred := NewBearerCredential("test-token-12345")

	// Store credential
	err := mgr.StoreCredential("testapp", "default", cred)
	require.NoError(t, err)

	// Get credential
	retrieved, err := mgr.GetCredential("testapp", "default")
	require.NoError(t, err)
	require.NotNil(t, retrieved)

	assert.Equal(t, CredentialTypeBearer, retrieved.Type)
	assert.Equal(t, "test-token-12345", retrieved.Token)
	assert.False(t, retrieved.CreatedAt.IsZero())
	assert.False(t, retrieved.UpdatedAt.IsZero())
}

func TestManagerGetCredentialNotFound(t *testing.T) {
	mgr := setupTestManager(t)

	_, err := mgr.GetCredential("nonexistent", "profile")
	require.Error(t, err)
	assert.True(t, IsCredentialNotFound(err))
}

func TestManagerDeleteCredential(t *testing.T) {
	mgr := setupTestManager(t)

	cred := NewAPIKeyCredential("api-key-12345")

	// Store credential
	err := mgr.StoreCredential("testapp", "default", cred)
	require.NoError(t, err)

	// Verify it exists
	assert.True(t, mgr.HasCredential("testapp", "default"))

	// Delete credential
	err = mgr.DeleteCredential("testapp", "default")
	require.NoError(t, err)

	// Verify it's gone
	assert.False(t, mgr.HasCredential("testapp", "default"))
}

func TestManagerDeleteCredentialNotExists(t *testing.T) {
	mgr := setupTestManager(t)

	// Should not error when deleting non-existent credential
	err := mgr.DeleteCredential("nonexistent", "profile")
	assert.NoError(t, err)
}

func TestManagerUpdateCredential(t *testing.T) {
	mgr := setupTestManager(t)

	// Store initial credential
	err := mgr.StoreCredential("testapp", "default", NewBearerCredential("old-token"))
	require.NoError(t, err)

	// Update credential
	err = mgr.UpdateCredential("testapp", "default", func(cred *Credential) error {
		cred.Token = "new-token"
		return nil
	})
	require.NoError(t, err)

	// Verify update
	retrieved, err := mgr.GetCredential("testapp", "default")
	require.NoError(t, err)
	assert.Equal(t, "new-token", retrieved.Token)
}

func TestManagerUpdateCredentialCreate(t *testing.T) {
	mgr := setupTestManager(t)

	// Update non-existent credential (should create it)
	err := mgr.UpdateCredential("testapp", "newprofile", func(cred *Credential) error {
		cred.Type = CredentialTypeBearer
		cred.Token = "created-token"
		return nil
	})
	require.NoError(t, err)

	// Verify creation
	retrieved, err := mgr.GetCredential("testapp", "newprofile")
	require.NoError(t, err)
	assert.Equal(t, "created-token", retrieved.Token)
}

func TestManagerListCredentials(t *testing.T) {
	mgr := setupTestManager(t)

	// Store multiple credentials for same app
	err := mgr.StoreCredential("testapp", "prod", NewBearerCredential("token1"))
	require.NoError(t, err)

	err = mgr.StoreCredential("testapp", "dev", NewBearerCredential("token2"))
	require.NoError(t, err)

	err = mgr.StoreCredential("otherapp", "default", NewBearerCredential("token3"))
	require.NoError(t, err)

	// List credentials for testapp
	profiles, err := mgr.ListCredentials("testapp")
	require.NoError(t, err)
	assert.Len(t, profiles, 2)
	assert.Contains(t, profiles, "prod")
	assert.Contains(t, profiles, "dev")
}

func TestManagerListAllCredentials(t *testing.T) {
	mgr := setupTestManager(t)

	// Store credentials
	err := mgr.StoreCredential("app1", "default", NewBearerCredential("token1"))
	require.NoError(t, err)

	err = mgr.StoreCredential("app2", "prod", NewBasicCredential("user", "pass"))
	require.NoError(t, err)

	// List all credentials
	creds, err := mgr.ListAllCredentials()
	require.NoError(t, err)
	assert.Len(t, creds, 2)

	// Verify credential info
	for _, info := range creds {
		assert.NotEmpty(t, info.AppName)
		assert.NotEmpty(t, info.ProfileName)
		assert.NotEmpty(t, info.Type)
	}
}

func TestManagerHasCredential(t *testing.T) {
	mgr := setupTestManager(t)

	// Initially should not have credential
	assert.False(t, mgr.HasCredential("testapp", "default"))

	// Store credential
	err := mgr.StoreCredential("testapp", "default", NewBearerCredential("token"))
	require.NoError(t, err)

	// Now should have credential
	assert.True(t, mgr.HasCredential("testapp", "default"))
}

func TestManagerDeleteAllCredentials(t *testing.T) {
	mgr := setupTestManager(t)

	// Store multiple credentials for same app
	err := mgr.StoreCredential("testapp", "prod", NewBearerCredential("token1"))
	require.NoError(t, err)

	err = mgr.StoreCredential("testapp", "dev", NewBearerCredential("token2"))
	require.NoError(t, err)

	// Delete all credentials for testapp
	err = mgr.DeleteAllCredentials("testapp")
	require.NoError(t, err)

	// Verify all are gone
	profiles, err := mgr.ListCredentials("testapp")
	require.NoError(t, err)
	assert.Empty(t, profiles)
}

func TestManagerCopyCredential(t *testing.T) {
	mgr := setupTestManager(t)

	// Store original credential
	err := mgr.StoreCredential("testapp", "source", NewBearerCredential("original-token"))
	require.NoError(t, err)

	// Copy credential
	err = mgr.CopyCredential("testapp", "source", "destination")
	require.NoError(t, err)

	// Verify copy exists
	copied, err := mgr.GetCredential("testapp", "destination")
	require.NoError(t, err)
	assert.Equal(t, "original-token", copied.Token)

	// Verify original still exists
	original, err := mgr.GetCredential("testapp", "source")
	require.NoError(t, err)
	assert.Equal(t, "original-token", original.Token)
}

func TestManagerCopyCredentialNotFound(t *testing.T) {
	mgr := setupTestManager(t)

	err := mgr.CopyCredential("testapp", "nonexistent", "destination")
	require.Error(t, err)
}

func TestManagerStoreCredentialNil(t *testing.T) {
	mgr := setupTestManager(t)

	err := mgr.StoreCredential("testapp", "default", nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "credential cannot be nil")
}

func TestManagerBackend(t *testing.T) {
	mgr := setupTestManager(t)

	backend := mgr.Backend()
	// File backend is used in tests
	assert.NotEmpty(t, backend)
}

func TestGetPlatformInfo(t *testing.T) {
	info := GetPlatformInfo()

	assert.NotEmpty(t, info.OS)
	assert.NotEmpty(t, info.Arch)
	assert.NotEmpty(t, info.RecommendedBackend)
	assert.True(t, info.FallbackAvailable)
}

func TestWithPassBackend(t *testing.T) {
	// Just test that the option can be created without panic
	opt := WithPassBackend("/tmp/pass", "pass", "ob")
	assert.NotNil(t, opt)
}
