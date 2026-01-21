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
	ExpiresAt    time.Time `json:"expires_at,omitempty"`

	// Metadata
	CreatedAt time.Time `json:"created_at,omitempty"`
	UpdatedAt time.Time `json:"updated_at,omitempty"`
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
	case CredentialTypeBearer:
		return c.Token
	case CredentialTypeAPIKey:
		return c.Token
	case CredentialTypeBasic:
		return c.Username + ":" + c.Password
	case CredentialTypeOAuth2:
		return c.AccessToken
	default:
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
func NewManager(opts ...ManagerOption) (*Manager, error) {
	cfg := &managerConfig{}
	for _, opt := range opts {
		opt(cfg)
	}

	fileDir := cfg.fileDir
	if fileDir == "" {
		var err error
		fileDir, err = getDefaultKeyringDir()
		if err != nil {
			return nil, fmt.Errorf("failed to get keyring directory: %w", err)
		}
	}

	passwordFn := cfg.filePasswordFn
	if passwordFn == nil {
		passwordFn = keyring.FixedStringPrompt(ServiceName)
	}

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
func getDefaultKeyringDir() (string, error) {
	switch runtime.GOOS {
	case "darwin":
		homeDir, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(homeDir, "Library", "Application Support", "openbridge", "keyring"), nil

	case "windows":
		appData := os.Getenv("LOCALAPPDATA")
		if appData == "" {
			homeDir, err := os.UserHomeDir()
			if err != nil {
				return "", err
			}
			appData = filepath.Join(homeDir, "AppData", "Local")
		}
		return filepath.Join(appData, "OpenBridge", "keyring"), nil

	default:
		// Linux/Unix: XDG data directory
		xdgData := os.Getenv("XDG_DATA_HOME")
		if xdgData == "" {
			homeDir, err := os.UserHomeDir()
			if err != nil {
				return "", err
			}
			xdgData = filepath.Join(homeDir, ".local", "share")
		}
		return filepath.Join(xdgData, "openbridge", "keyring"), nil
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
func buildKey(appName, profileName string) string {
	return fmt.Sprintf("%s:%s:%s", KeyringPrefix, appName, profileName)
}

// parseKey extracts app and profile names from a keyring key.
func parseKey(key string) (appName, profileName string, ok bool) {
	parts := strings.SplitN(key, ":", 3)
	if len(parts) != 3 || parts[0] != KeyringPrefix {
		return "", "", false
	}
	return parts[1], parts[2], true
}

// StoreCredential stores a credential in the keyring.
func (m *Manager) StoreCredential(appName, profileName string, cred *Credential) error {
	if cred == nil {
		return fmt.Errorf("credential cannot be nil")
	}

	// Update timestamps
	now := time.Now()
	if cred.CreatedAt.IsZero() {
		cred.CreatedAt = now
	}
	cred.UpdatedAt = now

	data, err := json.Marshal(cred)
	if err != nil {
		return fmt.Errorf("failed to serialize credential: %w", err)
	}

	key := buildKey(appName, profileName)
	err = m.ring.Set(keyring.Item{
		Key:         key,
		Data:        data,
		Label:       fmt.Sprintf("OpenBridge: %s/%s", appName, profileName),
		Description: fmt.Sprintf("API credentials for %s profile %s", appName, profileName),
	})
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

// DeleteCredential removes a credential from the keyring.
func (m *Manager) DeleteCredential(appName, profileName string) error {
	key := buildKey(appName, profileName)

	err := m.ring.Remove(key)
	if err != nil {
		if errors.Is(err, keyring.ErrKeyNotFound) {
			return nil // Already deleted
		}
		return fmt.Errorf("failed to delete credential: %w", err)
	}

	return nil
}

// UpdateCredential updates an existing credential or creates a new one.
func (m *Manager) UpdateCredential(appName, profileName string, updateFn func(*Credential) error) error {
	// Try to get existing credential
	cred, err := m.GetCredential(appName, profileName)
	if err != nil {
		// Create new if not found
		credentialNotFoundError := &CredentialNotFoundError{}
		if errors.As(err, &credentialNotFoundError) {
			cred = &Credential{}
		} else {
			return err
		}
	}

	// Apply update function
	if err := updateFn(cred); err != nil {
		return err
	}

	// Store updated credential
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

		creds = append(creds, CredentialInfo{
			AppName:     appName,
			ProfileName: profileName,
			Type:        cred.Type,
			CreatedAt:   cred.CreatedAt,
			UpdatedAt:   cred.UpdatedAt,
			IsExpired:   cred.IsExpired(),
		})
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

// CopyCredential copies a credential from one profile to another.
func (m *Manager) CopyCredential(appName, fromProfile, toProfile string) error {
	cred, err := m.GetCredential(appName, fromProfile)
	if err != nil {
		return err
	}

	// Create a copy with new timestamps
	newCred := *cred
	newCred.CreatedAt = time.Time{}
	newCred.UpdatedAt = time.Time{}

	return m.StoreCredential(appName, toProfile, &newCred)
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

// GetPlatformInfo returns information about the current platform's keyring support.
func GetPlatformInfo() PlatformInfo {
	info := PlatformInfo{
		OS:                  runtime.GOOS,
		Arch:                runtime.GOARCH,
		SupportsNativeStore: false,
		NativeStoreName:     "",
		FallbackAvailable:   true,
	}

	switch runtime.GOOS {
	case "darwin":
		info.SupportsNativeStore = true
		info.NativeStoreName = "macOS Keychain"
		info.RecommendedBackend = BackendKeychain
	case "windows":
		info.SupportsNativeStore = true
		info.NativeStoreName = "Windows Credential Manager"
		info.RecommendedBackend = BackendWinCred
	case "linux":
		info.SupportsNativeStore = true // D-Bus Secret Service
		info.NativeStoreName = "Secret Service (GNOME Keyring/KWallet)"
		info.RecommendedBackend = BackendSecretService
	default:
		info.RecommendedBackend = BackendFile
	}

	return info
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
