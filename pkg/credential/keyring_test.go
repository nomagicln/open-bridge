package credential

import (
	"os"
	"runtime"
	"testing"
	"time"

	"github.com/99designs/keyring"
	"github.com/stretchr/testify/require"
)

func TestCredentialType(t *testing.T) {
	tests := []struct {
		credType CredentialType
		expected string
	}{
		{CredentialTypeBearer, "bearer"},
		{CredentialTypeAPIKey, "api_key"},
		{CredentialTypeBasic, "basic"},
		{CredentialTypeOAuth2, "oauth2"},
	}

	for _, tt := range tests {
		if string(tt.credType) != tt.expected {
			t.Errorf("expected %s, got %s", tt.expected, tt.credType)
		}
	}
}

func TestNewBearerCredential(t *testing.T) {
	token := "test-bearer-token"
	cred := NewBearerCredential(token)

	if cred.Type != CredentialTypeBearer {
		t.Errorf("expected type %s, got %s", CredentialTypeBearer, cred.Type)
	}

	if cred.Token != token {
		t.Errorf("expected token %s, got %s", token, cred.Token)
	}
}

func TestNewAPIKeyCredential(t *testing.T) {
	apiKey := "test-api-key"
	cred := NewAPIKeyCredential(apiKey)

	if cred.Type != CredentialTypeAPIKey {
		t.Errorf("expected type %s, got %s", CredentialTypeAPIKey, cred.Type)
	}

	if cred.Token != apiKey {
		t.Errorf("expected token %s, got %s", apiKey, cred.Token)
	}
}

func TestNewBasicCredential(t *testing.T) {
	username := "testuser"
	password := "testpass"
	cred := NewBasicCredential(username, password)

	if cred.Type != CredentialTypeBasic {
		t.Errorf("expected type %s, got %s", CredentialTypeBasic, cred.Type)
	}

	if cred.Username != username {
		t.Errorf("expected username %s, got %s", username, cred.Username)
	}

	if cred.Password != password {
		t.Errorf("expected password %s, got %s", password, cred.Password)
	}
}

func TestNewOAuth2Credential(t *testing.T) {
	accessToken := "access-token"
	refreshToken := "refresh-token"
	tokenType := "Bearer"
	expiresAt := time.Now().Add(1 * time.Hour)

	cred := NewOAuth2Credential(accessToken, refreshToken, tokenType, expiresAt)

	if cred.Type != CredentialTypeOAuth2 {
		t.Errorf("expected type %s, got %s", CredentialTypeOAuth2, cred.Type)
	}

	if cred.AccessToken != accessToken {
		t.Errorf("expected access token %s, got %s", accessToken, cred.AccessToken)
	}

	if cred.RefreshToken != refreshToken {
		t.Errorf("expected refresh token %s, got %s", refreshToken, cred.RefreshToken)
	}

	if cred.TokenType != tokenType {
		t.Errorf("expected token type %s, got %s", tokenType, cred.TokenType)
	}
}

func TestCredentialIsExpired(t *testing.T) {
	// Non-expired credential
	cred := NewOAuth2Credential("token", "", "", time.Now().Add(1*time.Hour))
	if cred.IsExpired() {
		t.Error("expected credential to not be expired")
	}

	// Expired credential
	cred = NewOAuth2Credential("token", "", "", time.Now().Add(-1*time.Hour))
	if !cred.IsExpired() {
		t.Error("expected credential to be expired")
	}

	// No expiration
	cred = NewBearerCredential("token")
	if cred.IsExpired() {
		t.Error("expected credential without expiration to not be expired")
	}
}

func TestCredentialNeedsRefresh(t *testing.T) {
	// Needs refresh (expires in 2 minutes)
	cred := NewOAuth2Credential("token", "refresh", "", time.Now().Add(2*time.Minute))
	if !cred.NeedsRefresh() {
		t.Error("expected credential expiring soon to need refresh")
	}

	// Doesn't need refresh (expires in 10 minutes)
	cred = NewOAuth2Credential("token", "refresh", "", time.Now().Add(10*time.Minute))
	if cred.NeedsRefresh() {
		t.Error("expected credential with time remaining to not need refresh")
	}

	// No refresh token
	cred = NewOAuth2Credential("token", "", "", time.Now().Add(2*time.Minute))
	if cred.NeedsRefresh() {
		t.Error("expected credential without refresh token to not need refresh")
	}
}

func TestCredentialGetAuthValue(t *testing.T) {
	tests := []struct {
		name     string
		cred     *Credential
		expected string
	}{
		{
			name:     "bearer",
			cred:     NewBearerCredential("bearer-token"),
			expected: "bearer-token",
		},
		{
			name:     "api_key",
			cred:     NewAPIKeyCredential("api-key"),
			expected: "api-key",
		},
		{
			name:     "basic",
			cred:     NewBasicCredential("user", "pass"),
			expected: "user:pass",
		},
		{
			name:     "oauth2",
			cred:     NewOAuth2Credential("access", "refresh", "Bearer", time.Time{}),
			expected: "access",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.cred.GetAuthValue(); got != tt.expected {
				t.Errorf("GetAuthValue() = %s, expected %s", got, tt.expected)
			}
		})
	}
}

func TestBuildKey(t *testing.T) {
	key := buildKey("myapp", "prod")
	expected := "ob:myapp:prod"

	if key != expected {
		t.Errorf("expected key %s, got %s", expected, key)
	}
}

func TestParseKey(t *testing.T) {
	tests := []struct {
		key         string
		wantApp     string
		wantProfile string
		wantOK      bool
	}{
		{"ob:myapp:prod", "myapp", "prod", true},
		{"ob:app:profile", "app", "profile", true},
		{"invalid:key", "", "", false},
		{"ob:only", "", "", false},
		{"", "", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.key, func(t *testing.T) {
			app, profile, ok := parseKey(tt.key)
			if ok != tt.wantOK {
				t.Errorf("parseKey(%s) ok = %v, want %v", tt.key, ok, tt.wantOK)
			}
			if ok && (app != tt.wantApp || profile != tt.wantProfile) {
				t.Errorf("parseKey(%s) = (%s, %s), want (%s, %s)", tt.key, app, profile, tt.wantApp, tt.wantProfile)
			}
		})
	}
}

func TestGetPlatformInfo(t *testing.T) {
	info := GetPlatformInfo()

	if info.OS != runtime.GOOS {
		t.Errorf("expected OS %s, got %s", runtime.GOOS, info.OS)
	}

	if info.Arch != runtime.GOARCH {
		t.Errorf("expected Arch %s, got %s", runtime.GOARCH, info.Arch)
	}

	// Platform-specific checks
	switch runtime.GOOS {
	case "darwin":
		if !info.SupportsNativeStore {
			t.Error("expected macOS to support native store")
		}
		if info.NativeStoreName != "macOS Keychain" {
			t.Errorf("expected native store name 'macOS Keychain', got '%s'", info.NativeStoreName)
		}
		if info.RecommendedBackend != BackendKeychain {
			t.Errorf("expected recommended backend %s, got %s", BackendKeychain, info.RecommendedBackend)
		}

	case "windows":
		if !info.SupportsNativeStore {
			t.Error("expected Windows to support native store")
		}
		if info.NativeStoreName != "Windows Credential Manager" {
			t.Errorf("expected native store name 'Windows Credential Manager', got '%s'", info.NativeStoreName)
		}
		if info.RecommendedBackend != BackendWinCred {
			t.Errorf("expected recommended backend %s, got %s", BackendWinCred, info.RecommendedBackend)
		}

	case "linux":
		if !info.SupportsNativeStore {
			t.Error("expected Linux to support native store")
		}
		if info.RecommendedBackend != BackendSecretService {
			t.Errorf("expected recommended backend %s, got %s", BackendSecretService, info.RecommendedBackend)
		}

	default:
		// Other platforms - just verify basic info is populated
		t.Logf("Unsupported platform %s, skipping platform-specific checks", runtime.GOOS)
	}

	// All platforms should have fallback available
	if !info.FallbackAvailable {
		t.Error("expected fallback to be available")
	}
}

func TestCredentialNotFoundError(t *testing.T) {
	err := &CredentialNotFoundError{AppName: "testapp", ProfileName: "prod"}
	expected := "credential not found for testapp/prod"

	if err.Error() != expected {
		t.Errorf("expected error message '%s', got '%s'", expected, err.Error())
	}
}

func TestBackendTypes(t *testing.T) {
	backends := []BackendType{
		BackendKeychain,
		BackendWinCred,
		BackendSecretService,
		BackendKWallet,
		BackendPass,
		BackendFile,
		BackendUnknown,
	}

	for _, backend := range backends {
		if backend == "" {
			t.Errorf("backend type should not be empty")
		}
	}

	// Check specific values
	if BackendKeychain != "keychain" {
		t.Errorf("expected BackendKeychain = 'keychain', got '%s'", BackendKeychain)
	}

	if BackendWinCred != "wincred" {
		t.Errorf("expected BackendWinCred = 'wincred', got '%s'", BackendWinCred)
	}

	if BackendSecretService != "secret-service" {
		t.Errorf("expected BackendSecretService = 'secret-service', got '%s'", BackendSecretService)
	}
}

// Integration tests that require actual keyring access
// These should be skipped in CI environments without keyring access

func TestNewManagerIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	// Skip if running in CI without keyring access
	if isCI() {
		t.Skip("skipping keyring test in CI environment")
	}

	m, err := NewManager()
	if err != nil {
		t.Skipf("keyring not available: %v", err)
	}

	if !m.IsInitialized() {
		t.Error("expected manager to be initialized")
	}

	backend := m.Backend()
	if backend == BackendUnknown {
		t.Log("warning: keyring backend is unknown")
	}
}

func TestStoreAndGetCredentialIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	if isCI() {
		t.Skip("skipping keyring test in CI environment")
	}

	m, err := NewManager()
	if err != nil {
		t.Skipf("keyring not available: %v", err)
	}

	appName := "test-openbridge"
	profileName := "test-profile"
	token := "test-token-12345"

	// Clean up any existing test credential
	_ = m.DeleteCredential(appName, profileName)

	// Store credential
	cred := NewBearerCredential(token)
	if err := m.StoreCredential(appName, profileName, cred); err != nil {
		t.Fatalf("StoreCredential failed: %v", err)
	}

	// Get credential
	retrieved, err := m.GetCredential(appName, profileName)
	if err != nil {
		t.Fatalf("GetCredential failed: %v", err)
	}

	if retrieved.Token != token {
		t.Errorf("expected token %s, got %s", token, retrieved.Token)
	}

	if retrieved.Type != CredentialTypeBearer {
		t.Errorf("expected type %s, got %s", CredentialTypeBearer, retrieved.Type)
	}

	// Check timestamps were set
	if retrieved.CreatedAt.IsZero() {
		t.Error("expected CreatedAt to be set")
	}

	if retrieved.UpdatedAt.IsZero() {
		t.Error("expected UpdatedAt to be set")
	}

	// Clean up
	if err := m.DeleteCredential(appName, profileName); err != nil {
		t.Errorf("DeleteCredential failed: %v", err)
	}
}

func TestDeleteCredentialIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	if isCI() {
		t.Skip("skipping keyring test in CI environment")
	}

	m, err := NewManager()
	if err != nil {
		t.Skipf("keyring not available: %v", err)
	}

	appName := "test-openbridge"
	profileName := "test-delete"

	// Store a credential
	err = m.StoreCredential(appName, profileName, NewBearerCredential("token"))
	require.NoError(t, err)

	// Delete it
	if err := m.DeleteCredential(appName, profileName); err != nil {
		t.Fatalf("DeleteCredential failed: %v", err)
	}

	// Verify it's gone
	if m.HasCredential(appName, profileName) {
		t.Error("expected credential to be deleted")
	}

	// Deleting again should not error
	if err := m.DeleteCredential(appName, profileName); err != nil {
		t.Errorf("second DeleteCredential should not error: %v", err)
	}
}

func TestHasCredentialIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	if isCI() {
		t.Skip("skipping keyring test in CI environment")
	}

	m, err := NewManager()
	if err != nil {
		t.Skipf("keyring not available: %v", err)
	}

	appName := "test-openbridge"
	profileName := "test-has"

	// Should not exist initially
	if m.HasCredential(appName, profileName) {
		// Clean up first
		err = m.DeleteCredential(appName, profileName)
		require.NoError(t, err)
	}

	if m.HasCredential(appName, profileName) {
		t.Error("expected HasCredential to return false")
	}

	// Store credential
	err = m.StoreCredential(appName, profileName, NewBearerCredential("token"))
	require.NoError(t, err)

	// Should exist now
	if !m.HasCredential(appName, profileName) {
		t.Error("expected HasCredential to return true")
	}

	// Clean up
	err = m.DeleteCredential(appName, profileName)
	require.NoError(t, err)
}

func TestListCredentialsIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	if isCI() {
		t.Skip("skipping keyring test in CI environment")
	}

	m, err := NewManager()
	if err != nil {
		t.Skipf("keyring not available: %v", err)
	}

	appName := "test-openbridge-list"

	// Clean up any existing credentials
	profiles, _ := m.ListCredentials(appName)
	for _, p := range profiles {
		err = m.DeleteCredential(appName, p)
		require.NoError(t, err)
	}

	// Store multiple credentials
	err = m.StoreCredential(appName, "profile1", NewBearerCredential("token1"))
	require.NoError(t, err)
	err = m.StoreCredential(appName, "profile2", NewBearerCredential("token2"))
	require.NoError(t, err)

	// List credentials
	profiles, err = m.ListCredentials(appName)
	if err != nil {
		t.Fatalf("ListCredentials failed: %v", err)
	}

	if len(profiles) != 2 {
		t.Errorf("expected 2 profiles, got %d", len(profiles))
	}

	// Clean up
	err = m.DeleteCredential(appName, "profile1")
	require.NoError(t, err)
	err = m.DeleteCredential(appName, "profile2")
	require.NoError(t, err)
}

// Helper to detect CI environment
func isCI() bool {
	ciEnvVars := []string{"CI", "GITHUB_ACTIONS", "TRAVIS", "CIRCLECI", "JENKINS_URL"}
	for _, env := range ciEnvVars {
		if v := os.Getenv(env); v != "" {
			return true
		}
	}
	return false
}

func TestGetDefaultKeyringDir(t *testing.T) {
	// Test that getDefaultKeyringDir returns a valid path without errors
	dir, err := getDefaultKeyringDir()
	require.NoError(t, err)
	require.NotEmpty(t, dir)

	// Verify it's an absolute path
	if !isAbsPath(dir) {
		t.Errorf("expected absolute path, got: %s", dir)
	}
}

// isAbsPath checks if a path is absolute (cross-platform)
func isAbsPath(path string) bool {
	if len(path) == 0 {
		return false
	}
	// Unix absolute paths start with /
	if path[0] == '/' {
		return true
	}
	// Windows absolute paths can start with C:\ or similar
	if len(path) >= 2 && path[1] == ':' {
		return true
	}
	return false
}

func TestDetectBackend(t *testing.T) {
	backend := detectBackend()

	// Verify backend is valid based on platform
	switch runtime.GOOS {
	case "darwin":
		if backend != BackendKeychain {
			t.Errorf("expected BackendKeychain on darwin, got %s", backend)
		}
	case "windows":
		if backend != BackendWinCred {
			t.Errorf("expected BackendWinCred on windows, got %s", backend)
		}
	case "linux":
		if backend != BackendSecretService {
			t.Errorf("expected BackendSecretService on linux, got %s", backend)
		}
	default:
		if backend != BackendFile {
			t.Errorf("expected BackendFile on unknown platform, got %s", backend)
		}
	}
}

func TestWithAllowedBackends(t *testing.T) {
	cfg := &managerConfig{}

	opt := WithAllowedBackends(keyring.KeychainBackend, keyring.FileBackend)
	opt(cfg)

	require.Len(t, cfg.allowedBackends, 2)
}

func TestWithFileBackend(t *testing.T) {
	cfg := &managerConfig{}

	passwordFn := func(s string) (string, error) { return "password", nil }
	opt := WithFileBackend("/tmp/test", passwordFn)
	opt(cfg)

	require.Equal(t, "/tmp/test", cfg.fileDir)
	require.NotNil(t, cfg.filePasswordFn)
}

func TestWithPassBackend(t *testing.T) {
	cfg := &managerConfig{}

	opt := WithPassBackend("/tmp/pass", "pass", "ob")
	opt(cfg)

	require.Equal(t, "/tmp/pass", cfg.passDir)
	require.Equal(t, "pass", cfg.passCmd)
	require.Equal(t, "ob", cfg.passPrefix)
}

func TestBuildKeyringConfig(t *testing.T) {
	cfg := &managerConfig{
		passDir:    "/tmp/pass",
		passCmd:    "pass",
		passPrefix: "ob",
	}

	passwordFn := func(s string) (string, error) { return "test", nil }

	result := buildKeyringConfig(cfg, "/tmp/keyring", passwordFn)

	require.Equal(t, ServiceName, result.ServiceName)
	require.Equal(t, "/tmp/pass", result.PassDir)
	require.Equal(t, "pass", result.PassCmd)
	require.Equal(t, "ob", result.PassPrefix)
	require.Equal(t, "/tmp/keyring", result.FileDir)
	require.NotNil(t, result.FilePasswordFunc)
}

func TestBuildKeyringConfigWithAllowedBackends(t *testing.T) {
	cfg := &managerConfig{
		allowedBackends: []keyring.BackendType{keyring.FileBackend, keyring.KeychainBackend},
	}

	passwordFn := func(s string) (string, error) { return "test", nil }

	result := buildKeyringConfig(cfg, "/tmp/keyring", passwordFn)

	require.Len(t, result.AllowedBackends, 2)
}
