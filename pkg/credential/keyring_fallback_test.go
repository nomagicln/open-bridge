package credential

import (
	"errors"
	"fmt"
	"io/fs"
	"testing"

	"github.com/99designs/keyring"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestHandleRemoveError_IdempotentForMissingCredentials verifies that deleting a
// nonexistent credential is a no-op for both keyring and filesystem errors.
func TestHandleRemoveError_IdempotentForMissingCredentials(t *testing.T) {
	assert.NoError(t, handleRemoveError(nil))
	assert.NoError(t, handleRemoveError(keyring.ErrKeyNotFound))
	assert.NoError(t, handleRemoveError(&fs.PathError{Op: "remove", Path: "/tmp/missing", Err: fs.ErrNotExist}))
	assert.NoError(t, handleRemoveError(fmt.Errorf("wrapped: %w", fs.ErrNotExist)))

	denied := errors.New("permission denied")
	require.ErrorIs(t, handleRemoveError(denied), denied)
}

// TestGetPasswordFunc_NoImplicitFixedPassword verifies that the manager no longer
// falls back to a hard-coded password when no explicit password source is configured.
func TestGetPasswordFunc_NoImplicitFixedPassword(t *testing.T) {
	t.Setenv(KeyringPasswordEnvVar, "")

	if fn := getPasswordFunc(&managerConfig{}); fn != nil {
		t.Fatal("getPasswordFunc must return nil without an explicit password source")
	}
}

func TestGetPasswordFunc_FromEnv(t *testing.T) {
	t.Setenv(KeyringPasswordEnvVar, "env-secret")

	fn := getPasswordFunc(&managerConfig{})
	require.NotNil(t, fn, "expected an env-backed password function")

	password, err := fn("prompt")
	require.NoError(t, err)
	assert.Equal(t, "env-secret", password)
}

func TestGetPasswordFunc_ExplicitOptionWins(t *testing.T) {
	t.Setenv(KeyringPasswordEnvVar, "env-secret")

	explicit := func(string) (string, error) { return "explicit-secret", nil }
	fn := getPasswordFunc(&managerConfig{filePasswordFn: explicit})
	require.NotNil(t, fn)

	password, err := fn("prompt")
	require.NoError(t, err)
	assert.Equal(t, "explicit-secret", password)
}

// TestNewManager_FileBackendWithoutPassword_ReturnsError verifies that the file
// backend is not usable without an explicit password.
func TestNewManager_FileBackendWithoutPassword_ReturnsError(t *testing.T) {
	t.Setenv(KeyringPasswordEnvVar, "")

	mgr, err := NewManager(WithFileBackend(t.TempDir(), nil))
	require.Error(t, err)
	assert.Nil(t, mgr)
	require.ErrorIs(t, err, ErrKeyringPasswordRequired)
	assert.Contains(t, err.Error(), KeyringPasswordEnvVar)
}

// TestNewManager_AllowedFileBackendWithoutPassword_ReturnsError covers explicitly
// allowing the file backend without a password.
func TestNewManager_AllowedFileBackendWithoutPassword_ReturnsError(t *testing.T) {
	t.Setenv(KeyringPasswordEnvVar, "")

	mgr, err := NewManager(WithAllowedBackends(keyring.FileBackend))
	require.Error(t, err)
	assert.Nil(t, mgr)
	require.ErrorIs(t, err, ErrKeyringPasswordRequired)
}

// TestNewManager_NoAvailableSystemBackend_ReturnsError verifies that an unusable
// system backend produces an explicit error instead of silently using the file backend.
func TestNewManager_NoAvailableSystemBackend_ReturnsError(t *testing.T) {
	t.Setenv(KeyringPasswordEnvVar, "")

	mgr, err := NewManager(WithAllowedBackends(keyring.BackendType("openbridge-missing-test-backend")))
	require.Error(t, err)
	assert.Nil(t, mgr)
	require.ErrorIs(t, err, ErrNoSecureKeyring)
	assert.Contains(t, err.Error(), KeyringPasswordEnvVar)
}

// TestNewManager_EnvPasswordEnablesFileBackend verifies that
// OPENBRIDGE_KEYRING_PASSWORD explicitly selects the encrypted file backend.
func TestNewManager_EnvPasswordEnablesFileBackend(t *testing.T) {
	t.Setenv(KeyringPasswordEnvVar, "test-password")

	keyringDir := t.TempDir()
	mgr, err := NewManager(WithFileBackend(keyringDir, nil))
	require.NoError(t, err)
	require.NotNil(t, mgr)
	assert.Equal(t, BackendFile, mgr.Backend())

	require.NoError(t, mgr.StoreCredential("secureapp", "default", NewBearerCredential("super-secret-token")))

	stored, err := mgr.GetCredential("secureapp", "default")
	require.NoError(t, err)
	assert.Equal(t, "super-secret-token", stored.Token)
}

// TestNewManager_DefaultNeverSilentlyUsesFileBackend verifies that a manager created
// without explicit configuration never reports the file backend.
func TestNewManager_DefaultNeverSilentlyUsesFileBackend(t *testing.T) {
	t.Setenv(KeyringPasswordEnvVar, "")

	mgr, err := NewManager()
	if err != nil {
		t.Skipf("no system keyring backend available in this environment: %v", err)
	}

	assert.NotEqual(t, BackendFile, mgr.Backend(),
		"default manager must not silently fall back to the file backend")
}

// TestManagerBackend_ReflectsRealFileBackend verifies that Backend() reports the
// backend that is actually in use rather than guessing from the platform.
func TestManagerBackend_ReflectsRealFileBackend(t *testing.T) {
	t.Setenv(KeyringPasswordEnvVar, "")

	passwordFn := func(string) (string, error) { return "test-password", nil }
	mgr, err := NewManager(
		WithAllowedBackends(keyring.FileBackend),
		WithFileBackend(t.TempDir(), passwordFn),
	)
	require.NoError(t, err)
	assert.Equal(t, BackendFile, mgr.Backend())
}

// TestNewManager_MissingAllowedBackendDoesNotFallBackToFile verifies that an
// unavailable allowed backend list never silently opens the file backend.
func TestNewManager_MissingAllowedBackendDoesNotFallBackToFile(t *testing.T) {
	t.Setenv(KeyringPasswordEnvVar, "")

	mgr, err := NewManager(WithAllowedBackends(keyring.KeychainBackend))
	if err == nil {
		assert.NotEqual(t, BackendFile, mgr.Backend(),
			"must not fall back to the file backend when it is not allowed")
		t.Skip("system keychain is available on this host; fallback path not exercised")
	}
	require.ErrorIs(t, err, ErrNoSecureKeyring)
}
