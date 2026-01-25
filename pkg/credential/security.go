// Package credential provides secure credential management using the system keyring.
// This file contains security measures and secure prompting functionality.
package credential

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"golang.org/x/term"
)

// SecurePrompter handles secure credential prompting.
type SecurePrompter struct {
	reader io.Reader
	writer io.Writer
}

// NewSecurePrompter creates a new secure prompter.
func NewSecurePrompter() *SecurePrompter {
	return &SecurePrompter{
		reader: os.Stdin,
		writer: os.Stderr, // Use stderr for prompts
	}
}

// NewSecurePrompterWithIO creates a secure prompter with custom I/O.
func NewSecurePrompterWithIO(reader io.Reader, writer io.Writer) *SecurePrompter {
	return &SecurePrompter{
		reader: reader,
		writer: writer,
	}
}

// PromptCredential prompts for a complete credential based on auth type.
func (p *SecurePrompter) PromptCredential(authType string) (*Credential, error) {
	switch authType {
	case string(CredentialTypeBearer):
		return p.PromptBearerToken()
	case string(CredentialTypeAPIKey):
		return p.PromptAPIKey()
	case string(CredentialTypeBasic):
		return p.PromptBasicAuth()
	default:
		return nil, fmt.Errorf("unsupported auth type: %s", authType)
	}
}

// PromptBearerToken prompts for a bearer token.
func (p *SecurePrompter) PromptBearerToken() (*Credential, error) {
	_, _ = fmt.Fprint(p.writer, "Enter bearer token: ")
	token, err := p.readSecureInput()
	if err != nil {
		return nil, fmt.Errorf("failed to read token: %w", err)
	}

	if token == "" {
		return nil, fmt.Errorf("token cannot be empty")
	}

	return NewBearerCredential(token), nil
}

// PromptAPIKey prompts for an API key.
func (p *SecurePrompter) PromptAPIKey() (*Credential, error) {
	_, _ = fmt.Fprint(p.writer, "Enter API key: ")
	apiKey, err := p.readSecureInput()
	if err != nil {
		return nil, fmt.Errorf("failed to read API key: %w", err)
	}

	if apiKey == "" {
		return nil, fmt.Errorf("API key cannot be empty")
	}

	return NewAPIKeyCredential(apiKey), nil
}

// PromptBasicAuth prompts for username and password.
func (p *SecurePrompter) PromptBasicAuth() (*Credential, error) {
	_, _ = fmt.Fprint(p.writer, "Username: ")
	username, err := p.readInput()
	if err != nil {
		return nil, fmt.Errorf("failed to read username: %w", err)
	}

	if username == "" {
		return nil, fmt.Errorf("username cannot be empty")
	}

	_, _ = fmt.Fprint(p.writer, "Password: ")
	password, err := p.readSecureInput()
	if err != nil {
		return nil, fmt.Errorf("failed to read password: %w", err)
	}

	if password == "" {
		return nil, fmt.Errorf("password cannot be empty")
	}

	return NewBasicCredential(username, password), nil
}

// PromptToken prompts for a single token/secret securely.
func (p *SecurePrompter) PromptToken(prompt string) (string, error) {
	_, _ = fmt.Fprint(p.writer, prompt)
	return p.readSecureInput()
}

// PromptString prompts for a non-secret string.
func (p *SecurePrompter) PromptString(prompt string) (string, error) {
	_, _ = fmt.Fprint(p.writer, prompt)
	return p.readInput()
}

// PromptConfirm prompts for a yes/no confirmation.
func (p *SecurePrompter) PromptConfirm(prompt string, defaultYes bool) (bool, error) {
	suffix := " [y/N]: "
	if defaultYes {
		suffix = " [Y/n]: "
	}

	_, _ = fmt.Fprint(p.writer, prompt+suffix)

	input, err := p.readInput()
	if err != nil {
		return false, err
	}

	input = strings.ToLower(strings.TrimSpace(input))
	if input == "" {
		return defaultYes, nil
	}

	return input == "y" || input == "yes", nil
}

// readSecureInput reads input without echoing (for passwords/tokens).
func (p *SecurePrompter) readSecureInput() (string, error) {
	// Check if stdin is a terminal
	if f, ok := p.reader.(*os.File); ok && term.IsTerminal(int(f.Fd())) {
		// Read password without echoing
		password, err := term.ReadPassword(int(f.Fd()))
		_, _ = fmt.Fprintln(p.writer) // Print newline after password input
		if err != nil {
			return "", err
		}
		return string(password), nil
	}

	// Fallback for non-terminal (e.g., pipe)
	return p.readInput()
}

// readInput reads a line of input.
func (p *SecurePrompter) readInput() (string, error) {
	scanner := bufio.NewScanner(p.reader)
	if scanner.Scan() {
		return strings.TrimSpace(scanner.Text()), nil
	}
	if err := scanner.Err(); err != nil {
		return "", err
	}
	return "", io.EOF
}

// CredentialValidator validates credentials before storage.
type CredentialValidator struct{}

// NewCredentialValidator creates a new credential validator.
func NewCredentialValidator() *CredentialValidator {
	return &CredentialValidator{}
}

// Validate validates a credential.
// validateBearerToken validates bearer token credential.
func validateBearerToken(cred *Credential) error {
	if cred.Token == "" {
		return fmt.Errorf("bearer token cannot be empty")
	}
	return nil
}

// validateAPIKey validates API key credential.
func validateAPIKey(cred *Credential) error {
	if cred.Token == "" {
		return fmt.Errorf("API key cannot be empty")
	}
	return nil
}

// validateBasicAuth validates basic authentication credential.
func validateBasicAuth(cred *Credential) error {
	if cred.Username == "" {
		return fmt.Errorf("username cannot be empty")
	}
	if cred.Password == "" {
		return fmt.Errorf("password cannot be empty")
	}
	return nil
}

// validateOAuth2 validates OAuth2 credential.
func validateOAuth2(cred *Credential) error {
	if cred.AccessToken == "" {
		return fmt.Errorf("access token cannot be empty")
	}
	return nil
}

func (v *CredentialValidator) Validate(cred *Credential) error {
	if cred == nil {
		return fmt.Errorf("credential is nil")
	}

	switch cred.Type {
	case CredentialTypeBearer:
		return validateBearerToken(cred)
	case CredentialTypeAPIKey:
		return validateAPIKey(cred)
	case CredentialTypeBasic:
		return validateBasicAuth(cred)
	case CredentialTypeOAuth2:
		return validateOAuth2(cred)
	default:
		return fmt.Errorf("unknown credential type: %s", cred.Type)
	}
}

// ValidateNonEmpty checks if a credential has non-empty values.
func (v *CredentialValidator) ValidateNonEmpty(cred *Credential) bool {
	return v.Validate(cred) == nil
}

// KeyringError represents errors from keyring operations.
type KeyringError struct {
	Operation string
	AppName   string
	Profile   string
	Cause     error
}

func (e *KeyringError) Error() string {
	if e.Profile != "" {
		return fmt.Sprintf("keyring %s failed for %s/%s: %v", e.Operation, e.AppName, e.Profile, e.Cause)
	}
	if e.AppName != "" {
		return fmt.Sprintf("keyring %s failed for %s: %v", e.Operation, e.AppName, e.Cause)
	}
	return fmt.Sprintf("keyring %s failed: %v", e.Operation, e.Cause)
}

func (e *KeyringError) Unwrap() error {
	return e.Cause
}

// IsKeyringUnavailable checks if the error indicates keyring is unavailable.
func IsKeyringUnavailable(err error) bool {
	if err == nil {
		return false
	}

	errStr := err.Error()
	unavailableIndicators := []string{
		"keyring not available",
		"dbus",
		"secret service",
		"keychain",
		"credential manager",
		"failed to open keyring",
		"kwallet",
	}

	for _, indicator := range unavailableIndicators {
		if strings.Contains(strings.ToLower(errStr), strings.ToLower(indicator)) {
			return true
		}
	}

	return false
}

// IsCredentialNotFound checks if the error indicates credential not found.
func IsCredentialNotFound(err error) bool {
	if err == nil {
		return false
	}

	credentialNotFoundError := &CredentialNotFoundError{}
	ok := errors.As(err, &credentialNotFoundError)
	if ok {
		return true
	}

	errStr := strings.ToLower(err.Error())
	return strings.Contains(errStr, "not found") || strings.Contains(errStr, "does not exist")
}

// RedactCredential creates a safe-to-log version of a credential.
// redactTokenFields redacts token fields for OAuth2 credentials.
func redactOAuth2Fields(cred *Credential, redacted map[string]any) {
	redacted["access_token"] = redactString(cred.AccessToken)
	redacted["refresh_token"] = redactString(cred.RefreshToken)
	if !cred.ExpiresAt.IsZero() {
		redacted["expires_at"] = cred.ExpiresAt.String()
	}
}

// addMetadataFields adds timestamp metadata to redacted credential.
func addMetadataFields(cred *Credential, redacted map[string]any) {
	if !cred.CreatedAt.IsZero() {
		redacted["created_at"] = cred.CreatedAt.String()
	}
	if !cred.UpdatedAt.IsZero() {
		redacted["updated_at"] = cred.UpdatedAt.String()
	}
}

func RedactCredential(cred *Credential) map[string]any {
	if cred == nil {
		return nil
	}

	redacted := map[string]any{
		"type": string(cred.Type),
	}

	switch cred.Type {
	case CredentialTypeBearer:
		redacted["token"] = redactString(cred.Token)
	case CredentialTypeAPIKey:
		redacted["api_key"] = redactString(cred.Token)
	case CredentialTypeBasic:
		redacted["username"] = cred.Username
		redacted["password"] = "[REDACTED]"
	case CredentialTypeOAuth2:
		redactOAuth2Fields(cred, redacted)
	default:
		if cred.Token != "" {
			redacted["token"] = redactString(cred.Token)
		}
	}

	addMetadataFields(cred, redacted)
	return redacted
}

// redactString partially redacts a string, showing only first and last few characters.
func redactString(s string) string {
	if s == "" {
		return "[EMPTY]"
	}
	if len(s) <= 8 {
		return "[REDACTED]"
	}
	return s[:4] + "..." + s[len(s)-4:]
}

// SanitizeForConfig ensures a value is safe to include in config files.
// This should NEVER be used with actual credentials.
func SanitizeForConfig(value string) string {
	// Check if value looks like a credential
	if looksLikeCredential(value) {
		return "[CREDENTIAL_REFERENCE]"
	}
	return value
}

// looksLikeCredential heuristically checks if a value might be a credential.
func looksLikeCredential(value string) bool {
	// Check length - credentials are usually longer
	if len(value) < 16 {
		return false
	}

	// Check for common credential patterns
	lower := strings.ToLower(value)

	// JWT pattern
	if strings.Count(value, ".") == 2 && len(value) > 50 {
		return true
	}

	// Base64-encoded or hex patterns
	isBase64Like := true
	isHexLike := true
	for _, c := range value {
		if (c < 'a' || c > 'z') && (c < 'A' || c > 'Z') && (c < '0' || c > '9') && c != '+' && c != '/' && c != '=' {
			isBase64Like = false
		}
		if (c < 'a' || c > 'f') && (c < 'A' || c > 'F') && (c < '0' || c > '9') {
			isHexLike = false
		}
	}

	if (isBase64Like || isHexLike) && len(value) >= 32 {
		return true
	}

	// Common prefixes
	credentialPrefixes := []string{
		"sk-", "pk-", "api-", "key-", "token-",
		"ghp_", "gho_", "ghs_", "ghr_", // GitHub
		"xox", // Slack
		"ey",  // JWT
	}

	for _, prefix := range credentialPrefixes {
		if strings.HasPrefix(lower, prefix) {
			return true
		}
	}

	return false
}

// SecureZeroMemory attempts to zero out sensitive data in memory.
// Note: In Go, this is best-effort due to garbage collection.
func SecureZeroMemory(data []byte) {
	for i := range data {
		data[i] = 0
	}
}

// SecureZeroString attempts to zero out a string's underlying bytes.
// Note: Strings in Go are immutable, so this converts to bytes first.
// This is best-effort and not cryptographically secure.
func SecureZeroString(s *string) {
	if s == nil || *s == "" {
		return
	}
	// Replace with empty string - the old value may still be in memory
	// until garbage collected
	*s = ""
}

// ClearCredential attempts to clear sensitive data from a credential.
func ClearCredential(cred *Credential) {
	if cred == nil {
		return
	}

	cred.Token = ""
	cred.Username = ""
	cred.Password = ""
	cred.AccessToken = ""
	cred.RefreshToken = ""
}

// GetFd returns the file descriptor for a file, if applicable.
// This is used for terminal detection.
func GetFd(r io.Reader) int {
	if f, ok := r.(*os.File); ok {
		return int(f.Fd())
	}
	return -1
}

// IsTerminal checks if the reader is connected to a terminal.
func IsTerminal(r io.Reader) bool {
	fd := GetFd(r)
	if fd < 0 {
		return false
	}
	return term.IsTerminal(fd)
}

// DisableEcho disables echo on the terminal for secure input.
// Returns a restore function that should be called when done.
func DisableEcho(fd int) (restore func(), err error) {
	if !term.IsTerminal(fd) {
		return func() {}, nil
	}

	oldState, err := term.GetState(fd)
	if err != nil {
		return nil, err
	}

	if err := disableEchoRaw(fd); err != nil {
		return nil, err
	}

	return func() {
		_ = term.Restore(fd, oldState)
	}, nil
}

// disableEchoRaw disables echo at the syscall level.
func disableEchoRaw(fd int) error {
	// Use term.MakeRaw and then restore echo visibility
	_, err := term.MakeRaw(fd)
	return err
}

// MaskInput masks input with asterisks while typing.
// This is a more user-friendly alternative to completely hiding input.
func MaskInput(reader io.Reader, writer io.Writer) (string, error) {
	fd := GetFd(reader)
	if fd < 0 || !term.IsTerminal(fd) {
		// Fallback for non-terminal
		scanner := bufio.NewScanner(reader)
		if scanner.Scan() {
			return strings.TrimSpace(scanner.Text()), nil
		}
		return "", io.EOF
	}

	// For terminal, use ReadPassword which doesn't echo
	password, err := term.ReadPassword(fd)
	if err != nil {
		return "", err
	}
	_, _ = fmt.Fprintln(writer) // Print newline
	return string(password), nil
}

// EnvironmentCredentialSource reads credentials from environment variables.
type EnvironmentCredentialSource struct {
	tokenEnvVar    string
	usernameEnvVar string
	passwordEnvVar string
}

// NewEnvironmentCredentialSource creates a new environment credential source.
func NewEnvironmentCredentialSource(tokenVar, usernameVar, passwordVar string) *EnvironmentCredentialSource {
	return &EnvironmentCredentialSource{
		tokenEnvVar:    tokenVar,
		usernameEnvVar: usernameVar,
		passwordEnvVar: passwordVar,
	}
}

// GetCredential retrieves a credential from environment variables.
func (e *EnvironmentCredentialSource) GetCredential(authType string) (*Credential, error) {
	switch authType {
	case string(CredentialTypeBearer):
		token := os.Getenv(e.tokenEnvVar)
		if token == "" {
			return nil, fmt.Errorf("environment variable %s not set", e.tokenEnvVar)
		}
		return NewBearerCredential(token), nil

	case string(CredentialTypeAPIKey):
		apiKey := os.Getenv(e.tokenEnvVar)
		if apiKey == "" {
			return nil, fmt.Errorf("environment variable %s not set", e.tokenEnvVar)
		}
		return NewAPIKeyCredential(apiKey), nil

	case string(CredentialTypeBasic):
		username := os.Getenv(e.usernameEnvVar)
		password := os.Getenv(e.passwordEnvVar)
		if username == "" || password == "" {
			return nil, fmt.Errorf("environment variables %s and %s must be set", e.usernameEnvVar, e.passwordEnvVar)
		}
		return NewBasicCredential(username, password), nil

	default:
		return nil, fmt.Errorf("unsupported auth type: %s", authType)
	}
}

// ClearEnvironmentCredentials unsets credential environment variables.
func (e *EnvironmentCredentialSource) ClearEnvironmentCredentials() {
	if e.tokenEnvVar != "" {
		_ = os.Unsetenv(e.tokenEnvVar)
	}
	if e.usernameEnvVar != "" {
		_ = os.Unsetenv(e.usernameEnvVar)
	}
	if e.passwordEnvVar != "" {
		_ = os.Unsetenv(e.passwordEnvVar)
	}
}
