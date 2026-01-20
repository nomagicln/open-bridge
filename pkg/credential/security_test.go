package credential

import (
	"bytes"
	"strings"
	"testing"
)

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
				err = &KeyringError{Operation: "test", Cause: nil}
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

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
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
