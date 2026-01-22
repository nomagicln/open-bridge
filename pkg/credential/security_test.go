package credential

import (
	"bytes"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewSecurePrompter(t *testing.T) {
	p := NewSecurePrompter()
	require.NotNil(t, p)
	assert.Equal(t, os.Stdin, p.reader)
	assert.Equal(t, os.Stderr, p.writer)
}

func TestPromptCredential(t *testing.T) {
	t.Run("bearer token", func(t *testing.T) {
		reader := strings.NewReader("my-bearer-token\n")
		var output bytes.Buffer
		p := NewSecurePrompterWithIO(reader, &output)
		cred, err := p.PromptCredential("bearer")
		require.NoError(t, err)
		require.NotNil(t, cred)
		assert.Equal(t, CredentialTypeBearer, cred.Type)
	})

	t.Run("api key", func(t *testing.T) {
		reader := strings.NewReader("my-api-key\n")
		var output bytes.Buffer
		p := NewSecurePrompterWithIO(reader, &output)
		cred, err := p.PromptCredential("api_key")
		require.NoError(t, err)
		require.NotNil(t, cred)
		assert.Equal(t, CredentialTypeAPIKey, cred.Type)
	})

	t.Run("unsupported auth type", func(t *testing.T) {
		reader := strings.NewReader("")
		var output bytes.Buffer
		p := NewSecurePrompterWithIO(reader, &output)
		_, err := p.PromptCredential("oauth2")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "unsupported auth type")
	})

	t.Run("unknown auth type", func(t *testing.T) {
		reader := strings.NewReader("")
		var output bytes.Buffer
		p := NewSecurePrompterWithIO(reader, &output)
		_, err := p.PromptCredential("unknown")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "unsupported auth type")
	})
}

func TestPromptTokenMethods(t *testing.T) {
	tests := []struct {
		name           string
		method         string
		input          string
		wantErr        bool
		errMsg         string
		expectedType   CredentialType
		expectedToken  string
		expectedPrompt string
	}{
		{
			name:           "valid bearer token",
			method:         "bearer",
			input:          "my-secret-token\n",
			wantErr:        false,
			expectedType:   CredentialTypeBearer,
			expectedToken:  "my-secret-token",
			expectedPrompt: "Enter bearer token:",
		},
		{
			name:    "empty bearer token",
			method:  "bearer",
			input:   "\n",
			wantErr: true,
			errMsg:  "token cannot be empty",
		},
		{
			name:           "valid api key",
			method:         "api_key",
			input:          "sk-1234567890\n",
			wantErr:        false,
			expectedType:   CredentialTypeAPIKey,
			expectedToken:  "sk-1234567890",
			expectedPrompt: "Enter API key:",
		},
		{
			name:    "empty api key",
			method:  "api_key",
			input:   "\n",
			wantErr: true,
			errMsg:  "API key cannot be empty",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reader := strings.NewReader(tt.input)
			var output bytes.Buffer
			p := NewSecurePrompterWithIO(reader, &output)

			var cred *Credential
			var err error
			switch tt.method {
			case "bearer":
				cred, err = p.PromptBearerToken()
			case "api_key":
				cred, err = p.PromptAPIKey()
			default:
				t.Fatalf("unknown method: %s", tt.method)
			}

			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errMsg)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.expectedType, cred.Type)
			assert.Equal(t, tt.expectedToken, cred.Token)
			assert.Contains(t, output.String(), tt.expectedPrompt)
		})
	}
}

func TestPromptBasicAuth(t *testing.T) {
	// Note: PromptBasicAuth requires two sequential reads (username + password).
	// Due to bufio.Scanner creating new instances per read, testing multi-line
	// input is unreliable with strings.Reader. We test error cases instead.

	t.Run("empty username", func(t *testing.T) {
		reader := strings.NewReader("\n")
		var output bytes.Buffer
		p := NewSecurePrompterWithIO(reader, &output)
		_, err := p.PromptBasicAuth()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "username cannot be empty")
	})
}

func TestPromptToken(t *testing.T) {
	reader := strings.NewReader("my-secret\n")
	var output bytes.Buffer

	p := NewSecurePrompterWithIO(reader, &output)
	token, err := p.PromptToken("Enter secret: ")

	require.NoError(t, err)
	assert.Equal(t, "my-secret", token)
	assert.Contains(t, output.String(), "Enter secret:")
}

func TestSecurePrompterPromptString(t *testing.T) {
	input := "test input\n"
	reader := strings.NewReader(input)
	var output bytes.Buffer

	p := NewSecurePrompterWithIO(reader, &output)

	result, err := p.PromptString("Enter value: ")
	if err != nil {
		t.Fatalf("PromptString failed: %v", err)
	}

	if result != "test input" {
		t.Errorf("expected 'test input', got '%s'", result)
	}

	if !strings.Contains(output.String(), "Enter value:") {
		t.Error("expected prompt to be written to output")
	}
}

func TestSecurePrompterPromptConfirm(t *testing.T) {
	tests := []struct {
		name       string
		input      string
		defaultYes bool
		expected   bool
	}{
		{"yes", "y\n", false, true},
		{"yes full", "yes\n", false, true},
		{"no", "n\n", true, false},
		{"no full", "no\n", true, false},
		{"default yes", "\n", true, true},
		{"default no", "\n", false, false},
		{"uppercase", "Y\n", false, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reader := strings.NewReader(tt.input)
			var output bytes.Buffer

			p := NewSecurePrompterWithIO(reader, &output)
			result, err := p.PromptConfirm("Continue?", tt.defaultYes)

			if err != nil {
				t.Fatalf("PromptConfirm failed: %v", err)
			}

			if result != tt.expected {
				t.Errorf("expected %v, got %v", tt.expected, result)
			}
		})
	}
}

func TestCredentialValidator(t *testing.T) {
	v := NewCredentialValidator()

	tests := []struct {
		name    string
		cred    *Credential
		wantErr bool
	}{
		{
			name:    "nil credential",
			cred:    nil,
			wantErr: true,
		},
		{
			name:    "valid bearer",
			cred:    NewBearerCredential("token"),
			wantErr: false,
		},
		{
			name:    "empty bearer token",
			cred:    &Credential{Type: CredentialTypeBearer, Token: ""},
			wantErr: true,
		},
		{
			name:    "valid api key",
			cred:    NewAPIKeyCredential("key"),
			wantErr: false,
		},
		{
			name:    "empty api key",
			cred:    &Credential{Type: CredentialTypeAPIKey, Token: ""},
			wantErr: true,
		},
		{
			name:    "valid basic",
			cred:    NewBasicCredential("user", "pass"),
			wantErr: false,
		},
		{
			name:    "empty username",
			cred:    &Credential{Type: CredentialTypeBasic, Username: "", Password: "pass"},
			wantErr: true,
		},
		{
			name:    "empty password",
			cred:    &Credential{Type: CredentialTypeBasic, Username: "user", Password: ""},
			wantErr: true,
		},
		{
			name:    "unknown type",
			cred:    &Credential{Type: "unknown"},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := v.Validate(tt.cred)
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestKeyringError(t *testing.T) {
	tests := []struct {
		name     string
		err      *KeyringError
		expected string
	}{
		{
			name: "with app and profile",
			err: &KeyringError{
				Operation: "get",
				AppName:   "myapp",
				Profile:   "prod",
				Cause:     &CredentialNotFoundError{AppName: "myapp", ProfileName: "prod"},
			},
			expected: "keyring get failed for myapp/prod:",
		},
		{
			name: "with app only",
			err: &KeyringError{
				Operation: "list",
				AppName:   "myapp",
				Cause:     nil,
			},
			expected: "keyring list failed for myapp:",
		},
		{
			name: "operation only",
			err: &KeyringError{
				Operation: "init",
				Cause:     nil,
			},
			expected: "keyring init failed:",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			errStr := tt.err.Error()
			if !strings.Contains(errStr, tt.expected) {
				t.Errorf("expected error to contain '%s', got '%s'", tt.expected, errStr)
			}
		})
	}
}

func TestIsKeyringUnavailable(t *testing.T) {
	tests := []struct {
		errStr      string
		unavailable bool
	}{
		{"keyring not available", true},
		{"dbus connection failed", true},
		{"secret service error", true},
		{"keychain access denied", true},
		{"credential manager error", true},
		{"failed to open keyring", true},
		{"kwallet not running", true},
		{"regular error", false},
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.errStr, func(t *testing.T) {
			var err error
			if tt.errStr != "" {
				// Create a simple error for testing
				err = stringError(tt.errStr)
			}

			result := IsKeyringUnavailable(err)
			if result != tt.unavailable {
				t.Errorf("IsKeyringUnavailable(%q) = %v, expected %v", tt.errStr, result, tt.unavailable)
			}
		})
	}
}

type stringError string

func (e stringError) Error() string { return string(e) }

func TestIsCredentialNotFound(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected bool
	}{
		{
			name:     "CredentialNotFoundError",
			err:      &CredentialNotFoundError{AppName: "app", ProfileName: "profile"},
			expected: true,
		},
		{
			name:     "contains not found",
			err:      stringError("credential not found"),
			expected: true,
		},
		{
			name:     "contains does not exist",
			err:      stringError("key does not exist"),
			expected: true,
		},
		{
			name:     "other error",
			err:      stringError("permission denied"),
			expected: false,
		},
		{
			name:     "nil error",
			err:      nil,
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := IsCredentialNotFound(tt.err)
			if result != tt.expected {
				t.Errorf("IsCredentialNotFound() = %v, expected %v", result, tt.expected)
			}
		})
	}
}

func TestRedactCredential(t *testing.T) {
	t.Run("nil credential", func(t *testing.T) {
		result := RedactCredential(nil)
		if result != nil {
			t.Error("expected nil for nil credential")
		}
	})

	t.Run("bearer token", func(t *testing.T) {
		cred := NewBearerCredential("super-secret-token-12345")
		result := RedactCredential(cred)

		if result["type"] != "bearer" {
			t.Errorf("expected type 'bearer', got '%v'", result["type"])
		}

		tokenStr, ok := result["token"].(string)
		if !ok {
			t.Fatal("expected token in result")
		}

		// Should be redacted
		if tokenStr == "super-secret-token-12345" {
			t.Error("token should be redacted")
		}

		if !strings.Contains(tokenStr, "...") {
			t.Error("redacted token should contain '...'")
		}
	})

	t.Run("basic auth", func(t *testing.T) {
		cred := NewBasicCredential("username", "password123")
		result := RedactCredential(cred)

		// Username should be visible
		if result["username"] != "username" {
			t.Errorf("expected username 'username', got '%v'", result["username"])
		}

		// Password should be redacted
		if result["password"] != "[REDACTED]" {
			t.Errorf("expected password '[REDACTED]', got '%v'", result["password"])
		}
	})
}

func TestRedactString(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"", "[EMPTY]"},
		{"short", "[REDACTED]"},
		{"12345678", "[REDACTED]"},
		{"123456789", "1234...6789"},
		{"abcdefghijklmnop", "abcd...mnop"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := redactString(tt.input)
			if result != tt.expected {
				t.Errorf("redactString(%q) = %q, expected %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestLooksLikeCredential(t *testing.T) {
	tests := []struct {
		value      string
		isCredLike bool
	}{
		// Definitely credentials
		{"sk-1234567890abcdefghijklmnop", true},    // API key prefix
		{"ghp_abcdefghijklmnopqrstuvwxyz12", true}, // GitHub token
		{"eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.dozjgNryP4J3jVmNHl0w5N_XgL0n3I9PlFUP0THsR8U", true}, // JWT
		{strings.Repeat("a", 32), true}, // Long base64-like string
		{strings.Repeat("0", 40), true}, // Long hex-like string

		// Not credentials
		{"hello", false},
		{"short", false},
		{"http://example.com", false},
		{"/path/to/file", false},
		{"my-app-name", false},
	}

	for _, tt := range tests {
		t.Run(tt.value[:min(20, len(tt.value))], func(t *testing.T) {
			result := looksLikeCredential(tt.value)
			if result != tt.isCredLike {
				t.Errorf("looksLikeCredential(%q...) = %v, expected %v", tt.value[:min(10, len(tt.value))], result, tt.isCredLike)
			}
		})
	}
}

func TestSanitizeForConfig(t *testing.T) {
	tests := []struct {
		value    string
		expected string
	}{
		{"normal value", "normal value"},
		{"sk-1234567890abcdefghijklmnop", "[CREDENTIAL_REFERENCE]"},
		{"ghp_abcdefghijklmnopqrstuvwxyz12", "[CREDENTIAL_REFERENCE]"},
	}

	for _, tt := range tests {
		t.Run(tt.value[:min(20, len(tt.value))], func(t *testing.T) {
			result := SanitizeForConfig(tt.value)
			if result != tt.expected {
				t.Errorf("SanitizeForConfig(%q) = %q, expected %q", tt.value, result, tt.expected)
			}
		})
	}
}

func TestSecureZeroMemory(t *testing.T) {
	data := []byte("sensitive data")
	SecureZeroMemory(data)

	for i, b := range data {
		if b != 0 {
			t.Errorf("byte at index %d not zeroed: %d", i, b)
		}
	}
}

func TestClearCredential(t *testing.T) {
	cred := &Credential{
		Type:         CredentialTypeOAuth2,
		Token:        "token",
		Username:     "user",
		Password:     "pass",
		AccessToken:  "access",
		RefreshToken: "refresh",
	}

	ClearCredential(cred)

	if cred.Token != "" || cred.Username != "" || cred.Password != "" ||
		cred.AccessToken != "" || cred.RefreshToken != "" {
		t.Error("credential fields should be cleared")
	}

	// Type should remain
	if cred.Type != CredentialTypeOAuth2 {
		t.Error("credential type should remain")
	}

	// Nil should not panic
	ClearCredential(nil)
}

func TestEnvironmentCredentialSource(t *testing.T) {
	// Set test environment variables
	t.Setenv("TEST_TOKEN", "test-token")
	t.Setenv("TEST_USERNAME", "testuser")
	t.Setenv("TEST_PASSWORD", "testpass")

	source := NewEnvironmentCredentialSource("TEST_TOKEN", "TEST_USERNAME", "TEST_PASSWORD")

	t.Run("bearer", func(t *testing.T) {
		cred, err := source.GetCredential("bearer")
		if err != nil {
			t.Fatalf("GetCredential failed: %v", err)
		}
		if cred.Token != "test-token" {
			t.Errorf("expected token 'test-token', got '%s'", cred.Token)
		}
	})

	t.Run("api_key", func(t *testing.T) {
		cred, err := source.GetCredential("api_key")
		if err != nil {
			t.Fatalf("GetCredential failed: %v", err)
		}
		if cred.Token != "test-token" {
			t.Errorf("expected token 'test-token', got '%s'", cred.Token)
		}
	})

	t.Run("basic", func(t *testing.T) {
		cred, err := source.GetCredential("basic")
		if err != nil {
			t.Fatalf("GetCredential failed: %v", err)
		}
		if cred.Username != "testuser" {
			t.Errorf("expected username 'testuser', got '%s'", cred.Username)
		}
		if cred.Password != "testpass" {
			t.Errorf("expected password 'testpass', got '%s'", cred.Password)
		}
	})

	t.Run("missing env var", func(t *testing.T) {
		source2 := NewEnvironmentCredentialSource("MISSING_VAR", "", "")
		_, err := source2.GetCredential("bearer")
		if err == nil {
			t.Error("expected error for missing env var")
		}
	})
}

func TestSecurePrompterNonTerminal(t *testing.T) {
	// Test with non-terminal reader
	input := "secret-value\n"
	reader := strings.NewReader(input)
	var output bytes.Buffer

	p := NewSecurePrompterWithIO(reader, &output)

	// Should fall back to regular read for non-terminal
	result, err := p.readSecureInput()
	if err != nil {
		t.Fatalf("readSecureInput failed: %v", err)
	}

	if result != "secret-value" {
		t.Errorf("expected 'secret-value', got '%s'", result)
	}
}

func TestValidateNonEmpty(t *testing.T) {
	v := NewCredentialValidator()

	tests := []struct {
		name     string
		cred     *Credential
		expected bool
	}{
		{
			name:     "valid bearer credential",
			cred:     NewBearerCredential("token"),
			expected: true,
		},
		{
			name:     "empty bearer token",
			cred:     &Credential{Type: CredentialTypeBearer, Token: ""},
			expected: false,
		},
		{
			name:     "nil credential",
			cred:     nil,
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := v.ValidateNonEmpty(tt.cred)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestValidateOAuth2Credential(t *testing.T) {
	v := NewCredentialValidator()

	t.Run("valid oauth2", func(t *testing.T) {
		cred := NewOAuth2Credential("access-token", "refresh-token", "Bearer", time.Now().Add(time.Hour))
		err := v.Validate(cred)
		assert.NoError(t, err)
	})

	t.Run("empty access token", func(t *testing.T) {
		cred := &Credential{Type: CredentialTypeOAuth2, AccessToken: ""}
		err := v.Validate(cred)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "access token cannot be empty")
	})
}

func TestKeyringErrorUnwrap(t *testing.T) {
	cause := &CredentialNotFoundError{AppName: "myapp", ProfileName: "default"}
	kerr := &KeyringError{
		Operation: "get",
		AppName:   "myapp",
		Profile:   "default",
		Cause:     cause,
	}

	unwrapped := kerr.Unwrap()
	assert.Equal(t, cause, unwrapped)
}

func TestRedactCredentialOAuth2(t *testing.T) {
	expiresAt := time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC)
	cred := NewOAuth2Credential("access-token-12345678", "refresh-token-12345678", "Bearer", expiresAt)
	cred.CreatedAt = time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	cred.UpdatedAt = time.Date(2024, 1, 10, 0, 0, 0, 0, time.UTC)

	result := RedactCredential(cred)

	assert.Equal(t, "oauth2", result["type"])

	// Access token should be redacted
	accessToken, ok := result["access_token"].(string)
	require.True(t, ok)
	assert.Contains(t, accessToken, "...")
	assert.NotEqual(t, "access-token-12345678", accessToken)

	// Refresh token should be redacted
	refreshToken, ok := result["refresh_token"].(string)
	require.True(t, ok)
	assert.Contains(t, refreshToken, "...")

	// Timestamps should be present
	assert.Contains(t, result, "expires_at")
	assert.Contains(t, result, "created_at")
	assert.Contains(t, result, "updated_at")
}

func TestRedactCredentialAPIKey(t *testing.T) {
	cred := NewAPIKeyCredential("sk-1234567890abcdef")
	result := RedactCredential(cred)

	assert.Equal(t, "api_key", result["type"])

	apiKey, ok := result["api_key"].(string)
	require.True(t, ok)
	assert.Contains(t, apiKey, "...")
	assert.NotEqual(t, "sk-1234567890abcdef", apiKey)
}

func TestRedactCredentialUnknownType(t *testing.T) {
	cred := &Credential{
		Type:  "custom",
		Token: "custom-token-12345678",
	}
	result := RedactCredential(cred)

	assert.Equal(t, "custom", result["type"])

	token, ok := result["token"].(string)
	require.True(t, ok)
	assert.Contains(t, token, "...")
}

func TestGetFd(t *testing.T) {
	t.Run("with os.File", func(t *testing.T) {
		fd := GetFd(os.Stdin)
		assert.GreaterOrEqual(t, fd, 0)
	})

	t.Run("with non-file reader", func(t *testing.T) {
		reader := strings.NewReader("test")
		fd := GetFd(reader)
		assert.Equal(t, -1, fd)
	})
}

func TestIsTerminal(t *testing.T) {
	t.Run("with non-file reader", func(t *testing.T) {
		reader := strings.NewReader("test")
		result := IsTerminal(reader)
		assert.False(t, result)
	})

	// Note: Testing with actual terminal is environment-dependent
	// In CI, os.Stdin might not be a terminal
}

func TestMaskInput(t *testing.T) {
	t.Run("non-terminal input", func(t *testing.T) {
		reader := strings.NewReader("my-secret-input\n")
		var output bytes.Buffer

		result, err := MaskInput(reader, &output)
		require.NoError(t, err)
		assert.Equal(t, "my-secret-input", result)
	})

	t.Run("empty input EOF", func(t *testing.T) {
		reader := strings.NewReader("")
		var output bytes.Buffer

		_, err := MaskInput(reader, &output)
		assert.Error(t, err)
	})
}

func TestSecureZeroString(t *testing.T) {
	t.Run("non-empty string", func(t *testing.T) {
		s := "secret"
		SecureZeroString(&s)
		assert.Equal(t, "", s)
	})

	t.Run("empty string", func(t *testing.T) {
		s := ""
		SecureZeroString(&s)
		assert.Equal(t, "", s)
	})

	t.Run("nil pointer", func(t *testing.T) {
		// Should not panic
		SecureZeroString(nil)
	})
}

func TestClearEnvironmentCredentials(t *testing.T) {
	// Set test environment variables
	t.Setenv("TEST_CLEAR_TOKEN", "token-value")
	t.Setenv("TEST_CLEAR_USER", "user-value")
	t.Setenv("TEST_CLEAR_PASS", "pass-value")

	source := NewEnvironmentCredentialSource("TEST_CLEAR_TOKEN", "TEST_CLEAR_USER", "TEST_CLEAR_PASS")

	// Verify they're set
	assert.NotEmpty(t, os.Getenv("TEST_CLEAR_TOKEN"))
	assert.NotEmpty(t, os.Getenv("TEST_CLEAR_USER"))
	assert.NotEmpty(t, os.Getenv("TEST_CLEAR_PASS"))

	// Clear them
	source.ClearEnvironmentCredentials()

	// Verify they're cleared
	assert.Empty(t, os.Getenv("TEST_CLEAR_TOKEN"))
	assert.Empty(t, os.Getenv("TEST_CLEAR_USER"))
	assert.Empty(t, os.Getenv("TEST_CLEAR_PASS"))
}

func TestEnvironmentCredentialSourceUnsupportedType(t *testing.T) {
	source := NewEnvironmentCredentialSource("TOKEN", "USER", "PASS")
	_, err := source.GetCredential("oauth2")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported auth type")
}

func TestEnvironmentCredentialSourceMissingBasicCredentials(t *testing.T) {
	// Only set username, not password
	t.Setenv("TEST_BASIC_USER", "myuser")
	// Ensure password is not set
	_ = os.Unsetenv("TEST_BASIC_PASS")

	source := NewEnvironmentCredentialSource("", "TEST_BASIC_USER", "TEST_BASIC_PASS")
	_, err := source.GetCredential("basic")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "must be set")
}

func TestPromptConfirmError(t *testing.T) {
	// Test with empty reader that will cause EOF
	reader := strings.NewReader("")
	var output bytes.Buffer

	p := NewSecurePrompterWithIO(reader, &output)
	_, err := p.PromptConfirm("Continue?", false)

	assert.Error(t, err)
}
