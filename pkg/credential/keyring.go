// Package credential provides secure credential management using the system keyring.
package credential

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/99designs/keyring"
)

const (
	// ServiceName is the name used for the keyring service.
	ServiceName = "openbridge"

	// KeyringPrefix is the prefix for all keyring entries.
	KeyringPrefix = "ob"
)

// CredentialType represents the type of credential.
type CredentialType string

const (
	// CredentialTypeBearer represents a bearer token.
	CredentialTypeBearer CredentialType = "bearer"

	// CredentialTypeAPIKey represents an API key.
	CredentialTypeAPIKey CredentialType = "api_key"

	// CredentialTypeBasic represents basic auth (username/password).
	CredentialTypeBasic CredentialType = "basic"

	// CredentialTypeOAuth2 represents OAuth2 credentials.
	CredentialTypeOAuth2 CredentialType = "oauth2"
)

// Credential represents stored authentication credentials.
type Credential struct {
	// Type is the credential type.
	Type CredentialType `json:"type"`

	// Token is used for bearer and API key authentication.
	Token string `json:"token,omitempty"`

	// Username is used for basic auth.
	Username string `json:"username,omitempty"`

	// Password is used for basic auth.
	Password string `json:"password,omitempty"`

	// OAuth2 tokens
	AccessToken  string    `json:"access_token,omitempty"`
	RefreshToken string    `json:"refresh_token,omitempty"`
	TokenType    string    `json:"token_type,omitempty"`
	ExpiresAt    time.Time `json:"expires_at,omitzero"`

	// Metadata
	CreatedAt time.Time `json:"created_at,omitzero"`
	UpdatedAt time.Time `json:"updated_at,omitzero"`
}

// IsExpired checks if the credential has expired (for OAuth2).
func (c *Credential) IsExpired() bool {
	if c.ExpiresAt.IsZero() {
		return false
	}
	return time.Now().After(c.ExpiresAt)
}

// NeedsRefresh checks if the credential needs to be refreshed.
func (c *Credential) NeedsRefresh() bool {
	if c.ExpiresAt.IsZero() || c.RefreshToken == "" {
		return false
	}
	// Refresh if expiring within 5 minutes
	return time.Now().Add(5 * time.Minute).After(c.ExpiresAt)
}

// GetAuthValue returns the appropriate auth value based on credential type.
func (c *Credential) GetAuthValue() string {
	switch c.Type {
	case CredentialTypeBasic:
		return c.Username + ":" + c.Password
	case CredentialTypeOAuth2:
		return c.AccessToken
	default:
		// Bearer, APIKey, and other types use Token field
		return c.Token
	}
}

// Manager handles credential storage and retrieval using the system keyring.
type Manager struct {
	ring        keyring.Keyring
	backend     BackendType
	initialized bool
}

// BackendType represents the keyring backend being used.
type BackendType string

const (
	// BackendKeychain is the macOS Keychain.
	BackendKeychain BackendType = "keychain"

	// BackendWinCred is the Windows Credential Manager.
	BackendWinCred BackendType = "wincred"

	// BackendSecretService is the Linux Secret Service (D-Bus).
	BackendSecretService BackendType = "secret-service"

	// BackendKWallet is the KDE Wallet.
	BackendKWallet BackendType = "kwallet"

	// BackendPass is the pass password manager.
	BackendPass BackendType = "pass"

	// BackendFile is the encrypted file backend.
	BackendFile BackendType = "file"

	// BackendUnknown is an unknown backend.
	BackendUnknown BackendType = "unknown"
)

// ManagerOption is a function that configures a Manager.
type ManagerOption func(*managerConfig)

type managerConfig struct {
	allowedBackends []keyring.BackendType
	passDir         string
	passCmd         string
	passPrefix      string
	fileDir         string
	filePasswordFn  func(string) (string, error)
}

// WithAllowedBackends sets the allowed keyring backends.
func WithAllowedBackends(backends ...keyring.BackendType) ManagerOption {
	return func(c *managerConfig) {
		c.allowedBackends = backends
	}
}

// WithFileBackend configures the file backend.
func WithFileBackend(dir string, passwordFn func(string) (string, error)) ManagerOption {
	return func(c *managerConfig) {
		c.fileDir = dir
		c.filePasswordFn = passwordFn
	}
}

// WithPassBackend configures the pass backend.
func WithPassBackend(dir, cmd, prefix string) ManagerOption {
	return func(c *managerConfig) {
		c.passDir = dir
		c.passCmd = cmd
		c.passPrefix = prefix
	}
}

// buildKeyringConfig creates the keyring configuration from manager config.
func buildKeyringConfig(cfg *managerConfig, fileDir string, passwordFn func(string) (string, error)) keyring.Config {
	config := keyring.Config{
		ServiceName:                    ServiceName,
		KeychainTrustApplication:       true,
		KeychainSynchronizable:         false,
		KeychainAccessibleWhenUnlocked: true,
		LibSecretCollectionName:        ServiceName,
		KWalletAppID:                   ServiceName,
		KWalletFolder:                  ServiceName,
		PassDir:                        cfg.passDir,
		PassCmd:                        cfg.passCmd,
		PassPrefix:                     cfg.passPrefix,
		FileDir:                        fileDir,
		FilePasswordFunc:               passwordFn,
	}

	if len(cfg.allowedBackends) > 0 {
		config.AllowedBackends = cfg.allowedBackends
	}

	return config
}

// NewManager creates a new credential manager.
// getPasswordFunc returns the password function to use for file backend.
func getPasswordFunc(cfg *managerConfig) func(string) (string, error) {
	if cfg.filePasswordFn != nil {
		return cfg.filePasswordFn
	}
	return keyring.FixedStringPrompt(ServiceName)
}

// getFileDir returns the file directory to use for file backend.
func getFileDir(cfg *managerConfig) (string, error) {
	if cfg.fileDir != "" {
		return cfg.fileDir, nil
	}
	return getDefaultKeyringDir()
}

func NewManager(opts ...ManagerOption) (*Manager, error) {
	cfg := &managerConfig{}
	for _, opt := range opts {
		opt(cfg)
	}

	fileDir, err := getFileDir(cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to get keyring directory: %w", err)
	}

	passwordFn := getPasswordFunc(cfg)
	config := buildKeyringConfig(cfg, fileDir, passwordFn)

	ring, err := keyring.Open(config)
	if err != nil {
		return nil, fmt.Errorf("failed to open keyring: %w", err)
	}

	return &Manager{
		ring:        ring,
		backend:     detectBackend(),
		initialized: true,
	}, nil
}

// getDefaultKeyringDir returns the default directory for the file-based keyring.
// getHomeDir returns the user's home directory.
func getHomeDir() (string, error) {
	return os.UserHomeDir()
}

// getDarwinKeyringDir returns the keyring directory for macOS.
func getDarwinKeyringDir() (string, error) {
	homeDir, err := getHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(homeDir, "Library", "Application Support", "openbridge", "keyring"), nil
}

// getWindowsKeyringDir returns the keyring directory for Windows.
func getWindowsKeyringDir() (string, error) {
	appData := os.Getenv("LOCALAPPDATA")
	if appData == "" {
		homeDir, err := getHomeDir()
		if err != nil {
			return "", err
		}
		appData = filepath.Join(homeDir, "AppData", "Local")
	}
	return filepath.Join(appData, "OpenBridge", "keyring"), nil
}

// getLinuxKeyringDir returns the keyring directory for Linux/Unix.
func getLinuxKeyringDir() (string, error) {
	xdgData := os.Getenv("XDG_DATA_HOME")
	if xdgData == "" {
		homeDir, err := getHomeDir()
		if err != nil {
			return "", err
		}
		xdgData = filepath.Join(homeDir, ".local", "share")
	}
	return filepath.Join(xdgData, "openbridge", "keyring"), nil
}

func getDefaultKeyringDir() (string, error) {
	switch runtime.GOOS {
	case "darwin":
		return getDarwinKeyringDir()
	case "windows":
		return getWindowsKeyringDir()
	default:
		return getLinuxKeyringDir()
	}
}

// detectBackend attempts to detect which keyring backend is being used.
func detectBackend() BackendType {
	// The keyring library doesn't expose which backend is in use,
	// so we detect based on platform
	switch runtime.GOOS {
	case "darwin":
		return BackendKeychain
	case "windows":
		return BackendWinCred
	case "linux":
		// Could be Secret Service, KWallet, or file
		// We'd need to actually check, but default to Secret Service
		return BackendSecretService
	default:
		return BackendFile
	}
}

// Backend returns the detected keyring backend type.
func (m *Manager) Backend() BackendType {
	return m.backend
}

// IsInitialized returns whether the manager was successfully initialized.
func (m *Manager) IsInitialized() bool {
	return m.initialized
}

// buildKey constructs the keyring key from app and profile names.
// Uses double underscore as separator for cross-platform compatibility
// (Windows doesn't allow colons in filenames when using file backend).
func buildKey(appName, profileName string) string {
	return fmt.Sprintf("%s__%s__%s", KeyringPrefix, appName, profileName)
}

// parseKey extracts app and profile names from a keyring key.
// Supports both old format (with colons) and new format (with double underscores)
// for backward compatibility.
func parseKey(key string) (appName, profileName string, ok bool) {
	// Try new format first (double underscore)
	parts := strings.SplitN(key, "__", 3)
	if len(parts) == 3 && parts[0] == KeyringPrefix {
		return parts[1], parts[2], true
	}

	// Fall back to old format (colon) for backward compatibility
	parts = strings.SplitN(key, ":", 3)
	if len(parts) == 3 && parts[0] == KeyringPrefix {
		return parts[1], parts[2], true
	}

	return "", "", false
}

// updateTimestamps updates the created and updated timestamps on a credential.
func updateTimestamps(cred *Credential) {
	now := time.Now()
	if cred.CreatedAt.IsZero() {
		cred.CreatedAt = now
	}
	cred.UpdatedAt = now
}

// createKeyringItem creates a keyring item from a credential.
func createKeyringItem(key, appName, profileName string, data []byte) keyring.Item {
	return keyring.Item{
		Key:         key,
		Data:        data,
		Label:       fmt.Sprintf("OpenBridge: %s/%s", appName, profileName),
		Description: fmt.Sprintf("API credentials for %s profile %s", appName, profileName),
	}
}

// StoreCredential stores a credential in the keyring.
func (m *Manager) StoreCredential(appName, profileName string, cred *Credential) error {
	if cred == nil {
		return fmt.Errorf("credential cannot be nil")
	}

	updateTimestamps(cred)

	data, err := json.Marshal(cred)
	if err != nil {
		return fmt.Errorf("failed to serialize credential: %w", err)
	}

	key := buildKey(appName, profileName)
	item := createKeyringItem(key, appName, profileName, data)
	err = m.ring.Set(item)
	if err != nil {
		return fmt.Errorf("failed to store credential: %w", err)
	}

	return nil
}

// GetCredential retrieves a credential from the keyring.
func (m *Manager) GetCredential(appName, profileName string) (*Credential, error) {
	key := buildKey(appName, profileName)

	item, err := m.ring.Get(key)
	if err != nil {
		if errors.Is(err, keyring.ErrKeyNotFound) {
			return nil, &CredentialNotFoundError{AppName: appName, ProfileName: profileName}
		}
		return nil, fmt.Errorf("failed to get credential: %w", err)
	}

	var cred Credential
	if err := json.Unmarshal(item.Data, &cred); err != nil {
		return nil, fmt.Errorf("failed to parse credential: %w", err)
	}

	return &cred, nil
}

// handleRemoveError handles errors from ring.Remove().
func handleRemoveError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, keyring.ErrKeyNotFound) {
		return nil
	}
	return fmt.Errorf("failed to delete credential: %w", err)
}

// DeleteCredential removes a credential from the keyring.
func (m *Manager) DeleteCredential(appName, profileName string) error {
	key := buildKey(appName, profileName)
	return handleRemoveError(m.ring.Remove(key))
}

// getOrCreateCredential gets an existing credential or creates a new one.
func (m *Manager) getOrCreateCredential(appName, profileName string) (*Credential, error) {
	cred, err := m.GetCredential(appName, profileName)
	if err == nil {
		return cred, nil
	}

	// Check if error is CredentialNotFoundError
	credentialNotFoundError := &CredentialNotFoundError{}
	if errors.As(err, &credentialNotFoundError) {
		return &Credential{}, nil
	}

	return nil, err
}

// UpdateCredential updates an existing credential or creates a new one.
func (m *Manager) UpdateCredential(appName, profileName string, updateFn func(*Credential) error) error {
	cred, err := m.getOrCreateCredential(appName, profileName)
	if err != nil {
		return err
	}

	if err := updateFn(cred); err != nil {
		return err
	}

	return m.StoreCredential(appName, profileName, cred)
}

// ListCredentials returns a list of all profile names with stored credentials for an app.
func (m *Manager) ListCredentials(appName string) ([]string, error) {
	keys, err := m.ring.Keys()
	if err != nil {
		return nil, fmt.Errorf("failed to list credentials: %w", err)
	}

	var profiles []string
	for _, key := range keys {
		app, profile, ok := parseKey(key)
		if ok && app == appName {
			profiles = append(profiles, profile)
		}
	}

	return profiles, nil
}

// ListAllCredentials returns all stored credentials.
// buildCredentialInfo creates a CredentialInfo from a credential.
func buildCredentialInfo(appName, profileName string, cred *Credential) CredentialInfo {
	return CredentialInfo{
		AppName:     appName,
		ProfileName: profileName,
		Type:        cred.Type,
		CreatedAt:   cred.CreatedAt,
		UpdatedAt:   cred.UpdatedAt,
		IsExpired:   cred.IsExpired(),
	}
}

func (m *Manager) ListAllCredentials() ([]CredentialInfo, error) {
	keys, err := m.ring.Keys()
	if err != nil {
		return nil, fmt.Errorf("failed to list credentials: %w", err)
	}

	var creds []CredentialInfo
	for _, key := range keys {
		appName, profileName, ok := parseKey(key)
		if !ok {
			continue
		}

		cred, err := m.GetCredential(appName, profileName)
		if err != nil {
			continue
		}

		creds = append(creds, buildCredentialInfo(appName, profileName, cred))
	}

	return creds, nil
}

// CredentialInfo contains summary information about a credential.
type CredentialInfo struct {
	AppName     string
	ProfileName string
	Type        CredentialType
	CreatedAt   time.Time
	UpdatedAt   time.Time
	IsExpired   bool
}

// HasCredential checks if a credential exists for the given app/profile.
func (m *Manager) HasCredential(appName, profileName string) bool {
	_, err := m.GetCredential(appName, profileName)
	return err == nil
}

// DeleteAllCredentials removes all credentials for an app.
func (m *Manager) DeleteAllCredentials(appName string) error {
	profiles, err := m.ListCredentials(appName)
	if err != nil {
		return err
	}

	for _, profile := range profiles {
		if err := m.DeleteCredential(appName, profile); err != nil {
			return err
		}
	}

	return nil
}

// createCredentialCopy creates a copy of a credential with reset timestamps.
func createCredentialCopy(cred *Credential) *Credential {
	newCred := *cred
	newCred.CreatedAt = time.Time{}
	newCred.UpdatedAt = time.Time{}
	return &newCred
}

// CopyCredential copies a credential from one profile to another.
func (m *Manager) CopyCredential(appName, fromProfile, toProfile string) error {
	cred, err := m.GetCredential(appName, fromProfile)
	if err != nil {
		return err
	}

	return m.StoreCredential(appName, toProfile, createCredentialCopy(cred))
}

// Error types

// CredentialNotFoundError indicates a credential was not found.
type CredentialNotFoundError struct {
	AppName     string
	ProfileName string
}

func (e *CredentialNotFoundError) Error() string {
	return fmt.Sprintf("credential not found for %s/%s", e.AppName, e.ProfileName)
}

// Helper functions for creating credentials

// NewBearerCredential creates a new bearer token credential.
func NewBearerCredential(token string) *Credential {
	return &Credential{
		Type:  CredentialTypeBearer,
		Token: token,
	}
}

// NewAPIKeyCredential creates a new API key credential.
func NewAPIKeyCredential(apiKey string) *Credential {
	return &Credential{
		Type:  CredentialTypeAPIKey,
		Token: apiKey,
	}
}

// NewBasicCredential creates a new basic auth credential.
func NewBasicCredential(username, password string) *Credential {
	return &Credential{
		Type:     CredentialTypeBasic,
		Username: username,
		Password: password,
	}
}

// NewOAuth2Credential creates a new OAuth2 credential.
func NewOAuth2Credential(accessToken, refreshToken, tokenType string, expiresAt time.Time) *Credential {
	return &Credential{
		Type:         CredentialTypeOAuth2,
		AccessToken:  accessToken,
		RefreshToken: refreshToken,
		TokenType:    tokenType,
		ExpiresAt:    expiresAt,
	}
}

// getPlatformBackend returns platform-specific backend information.
func getPlatformBackend(os string) (bool, string, BackendType) {
	switch os {
	case "darwin":
		return true, "macOS Keychain", BackendKeychain
	case "windows":
		return true, "Windows Credential Manager", BackendWinCred
	case "linux":
		return true, "Secret Service (GNOME Keyring/KWallet)", BackendSecretService
	default:
		return false, "", BackendFile
	}
}

// GetPlatformInfo returns information about the current platform's keyring support.
func GetPlatformInfo() PlatformInfo {
	supportsNative, nativeStore, backend := getPlatformBackend(runtime.GOOS)

	return PlatformInfo{
		OS:                  runtime.GOOS,
		Arch:                runtime.GOARCH,
		SupportsNativeStore: supportsNative,
		NativeStoreName:     nativeStore,
		FallbackAvailable:   true,
		RecommendedBackend:  backend,
	}
}

// PlatformInfo contains information about the platform's keyring support.
type PlatformInfo struct {
	OS                  string
	Arch                string
	SupportsNativeStore bool
	NativeStoreName     string
	FallbackAvailable   bool
	RecommendedBackend  BackendType
}
