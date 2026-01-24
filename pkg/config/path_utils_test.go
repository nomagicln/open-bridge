package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// createTempPEMFile creates a temporary PEM file with the given content and returns its path.
// The caller should use defer os.Remove(path) to clean up.
func createTempPEMFile(t *testing.T, prefix, content string) string {
	t.Helper()
	tmpFile, err := os.CreateTemp("", prefix)
	require.NoError(t, err)
	_, err = tmpFile.WriteString(content)
	require.NoError(t, err)
	require.NoError(t, tmpFile.Close())
	return tmpFile.Name()
}

// validateFunc is the signature for TLS validation functions.
type validateFunc func(string) (string, error)

// assertInvalidPEMError tests that a validation function returns a "not PEM encoded" error
// for invalid content.
func assertInvalidPEMError(t *testing.T, fn validateFunc, prefix, content string) {
	t.Helper()
	path := createTempPEMFile(t, prefix, content)
	defer func() { _ = os.Remove(path) }()

	_, err := fn(path)
	require.Error(t, err)
	var pathErr *PathValidationError
	assert.ErrorAs(t, err, &pathErr)
	assert.Contains(t, pathErr.Reason, "not PEM encoded")
}

// assertPathValidationError tests that a validation function returns a PathValidationError
// with the expected reason substring.
func assertPathValidationError(t *testing.T, fn validateFunc, prefix, content, reasonContains string) {
	t.Helper()
	path := createTempPEMFile(t, prefix, content)
	defer func() { _ = os.Remove(path) }()

	_, err := fn(path)
	require.Error(t, err)
	var pathErr *PathValidationError
	assert.ErrorAs(t, err, &pathErr)
	assert.Contains(t, pathErr.Reason, reasonContains)
}

func TestPathValidationError(t *testing.T) {
	t.Run("error with wrapped error", func(t *testing.T) {
		wrapped := os.ErrNotExist
		err := &PathValidationError{
			Path:    "/some/path",
			Reason:  "file not found",
			Wrapped: wrapped,
		}

		assert.Contains(t, err.Error(), "/some/path")
		assert.Contains(t, err.Error(), "file not found")
		assert.Equal(t, wrapped, err.Unwrap())
	})

	t.Run("error without wrapped error", func(t *testing.T) {
		err := &PathValidationError{
			Path:   "/some/path",
			Reason: "is a directory",
		}

		assert.Contains(t, err.Error(), "/some/path")
		assert.Contains(t, err.Error(), "is a directory")
		assert.Nil(t, err.Unwrap())
	})
}

func TestValidateAndResolvePath(t *testing.T) {
	t.Run("valid file path", func(t *testing.T) {
		path := createTempPEMFile(t, "test-*.txt", "test content")
		defer func() { _ = os.Remove(path) }()

		absPath, err := ValidateAndResolvePath(path)
		require.NoError(t, err)
		assert.True(t, filepath.IsAbs(absPath))
	})

	t.Run("relative path resolution", func(t *testing.T) {
		// Create a temp file in current directory
		tmpFile, err := os.CreateTemp(".", "test-*.txt")
		require.NoError(t, err)
		defer func() { _ = os.Remove(tmpFile.Name()) }()
		require.NoError(t, tmpFile.Close())

		relPath := filepath.Base(tmpFile.Name())
		absPath, err := ValidateAndResolvePath(relPath)
		require.NoError(t, err)
		assert.True(t, filepath.IsAbs(absPath))
	})

	t.Run("empty path", func(t *testing.T) {
		_, err := ValidateAndResolvePath("")
		require.Error(t, err)
		var pathErr *PathValidationError
		assert.ErrorAs(t, err, &pathErr)
		assert.Equal(t, "path cannot be empty", pathErr.Reason)
	})

	t.Run("non-existent file", func(t *testing.T) {
		_, err := ValidateAndResolvePath("/nonexistent/path/to/file.txt")
		require.Error(t, err)
		var pathErr *PathValidationError
		assert.ErrorAs(t, err, &pathErr)
	})

	t.Run("directory instead of file", func(t *testing.T) {
		tmpDir, err := os.MkdirTemp("", "test-dir-*")
		require.NoError(t, err)
		defer func() { _ = os.RemoveAll(tmpDir) }()

		_, err = ValidateAndResolvePath(tmpDir)
		require.Error(t, err)
		var pathErr *PathValidationError
		assert.ErrorAs(t, err, &pathErr)
		assert.Contains(t, pathErr.Reason, "directory")
	})

	t.Run("tilde expansion", func(t *testing.T) {
		homeDir, err := os.UserHomeDir()
		require.NoError(t, err)

		// Create a temp file in home directory
		tmpFile, err := os.CreateTemp(homeDir, "test-*.txt")
		if err != nil {
			t.Skip("Cannot create file in home directory")
		}
		defer func() { _ = os.Remove(tmpFile.Name()) }()
		require.NoError(t, tmpFile.Close())

		tildeRelPath := "~/" + filepath.Base(tmpFile.Name())
		absPath, err := ValidateAndResolvePath(tildeRelPath)
		require.NoError(t, err)
		assert.True(t, filepath.IsAbs(absPath))
		assert.Contains(t, absPath, homeDir)
	})
}

func TestValidateTLSCertFile(t *testing.T) {
	t.Run("valid PEM structure with invalid certificate content", func(t *testing.T) {
		// The certificate validation parses the X.509 certificate, so we test
		// that the function correctly identifies invalid PEM content
		assertPathValidationError(t, ValidateTLSCertFile, "cert-*.pem",
			"-----BEGIN CERTIFICATE-----\ntest\n-----END CERTIFICATE-----", "invalid certificate")
	})

	t.Run("invalid PEM certificate", func(t *testing.T) {
		assertInvalidPEMError(t, ValidateTLSCertFile, "invalid-cert-*.pem", "not a valid certificate")
	})
}

func TestValidateTLSKeyFile(t *testing.T) {
	t.Run("valid PEM private key", func(t *testing.T) {
		keyContent := `-----BEGIN PRIVATE KEY-----
MIIEvgIBADANBgkqhkiG9w0BAQEFAASCBKgwggSkAgEAAoIBAQCfake3mP5MAAAA
-----END PRIVATE KEY-----`
		path := createTempPEMFile(t, "key-*.pem", keyContent)
		defer func() { _ = os.Remove(path) }()

		absPath, err := ValidateTLSKeyFile(path)
		require.NoError(t, err)
		assert.True(t, filepath.IsAbs(absPath))
	})

	t.Run("valid RSA private key", func(t *testing.T) {
		keyContent := `-----BEGIN RSA PRIVATE KEY-----
MIIEvgIBADANBgkqhkiG9w0BAQEFAASCBKgwggSkAgEAAoIBAQCfake3mP5MAAAA
-----END RSA PRIVATE KEY-----`
		path := createTempPEMFile(t, "rsa-key-*.pem", keyContent)
		defer func() { _ = os.Remove(path) }()

		absPath, err := ValidateTLSKeyFile(path)
		require.NoError(t, err)
		assert.True(t, filepath.IsAbs(absPath))
	})

	t.Run("valid EC private key", func(t *testing.T) {
		keyContent := `-----BEGIN EC PRIVATE KEY-----
MHQCAQEEICg7E4NN53YkaWuAwpoqjfAofjzKI7Jq1f532dX+5OjAoAcGBSuBBAAK
-----END EC PRIVATE KEY-----`
		path := createTempPEMFile(t, "ec-key-*.pem", keyContent)
		defer func() { _ = os.Remove(path) }()

		absPath, err := ValidateTLSKeyFile(path)
		require.NoError(t, err)
		assert.True(t, filepath.IsAbs(absPath))
	})

	t.Run("invalid key file", func(t *testing.T) {
		assertInvalidPEMError(t, ValidateTLSKeyFile, "invalid-key-*.pem", "not a valid key")
	})
}

func TestValidateTLSCACertFile(t *testing.T) {
	t.Run("invalid CA certificate - malformed PEM content", func(t *testing.T) {
		// x509.NewCertPool().AppendCertsFromPEM returns false for invalid certificates
		assertPathValidationError(t, ValidateTLSCACertFile, "ca-bundle-*.pem",
			"-----BEGIN CERTIFICATE-----\nMIICnDCCAYQCCQDU+pQ6XWh4wDANBgkqhkiG9w0BAQsFADAQMQ4wDAYDVQQDDAVt\n-----END CERTIFICATE-----",
			"valid PEM-encoded certificates")
	})

	t.Run("invalid CA certificate - not PEM encoded", func(t *testing.T) {
		assertPathValidationError(t, ValidateTLSCACertFile, "ca-invalid-*.pem",
			"not a certificate", "valid PEM-encoded certificates")
	})
}

func TestValidateTLSConfigPaths(t *testing.T) {
	t.Run("empty config", func(t *testing.T) {
		result, err := ValidateTLSConfigPaths("", "", "")
		require.NoError(t, err)
		assert.Empty(t, result.CAFile)
		assert.Empty(t, result.CertFile)
		assert.Empty(t, result.KeyFile)
	})

	t.Run("invalid CA file", func(t *testing.T) {
		// Use invalid certificate content to test error handling
		certContent := `-----BEGIN CERTIFICATE-----
invalid
-----END CERTIFICATE-----`
		path := createTempPEMFile(t, "ca-*.pem", certContent)
		defer func() { _ = os.Remove(path) }()

		_, err := ValidateTLSConfigPaths(path, "", "")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "CA certificate")
	})

	t.Run("cert without key", func(t *testing.T) {
		// Create a CERTIFICATE PEM that will pass format check but may fail X.509 parsing
		// The error message should indicate client cert/key mismatch or validation failure
		certContent := `-----BEGIN CERTIFICATE-----
test
-----END CERTIFICATE-----`
		path := createTempPEMFile(t, "cert-*.pem", certContent)
		defer func() { _ = os.Remove(path) }()

		_, err := ValidateTLSConfigPaths("", path, "")
		require.Error(t, err)
		// The error will be from certificate validation or from cert/key mismatch check
		assert.True(t,
			strings.Contains(err.Error(), "client certificate") ||
				strings.Contains(err.Error(), "certificate and key"),
			"expected error about client certificate, got: %v", err)
	})

	t.Run("key without cert", func(t *testing.T) {
		keyContent := `-----BEGIN PRIVATE KEY-----
MIIEvgIBADANBgkqhkiG9w0BAQEFAASCBKgwggSkAgEAAoIBAQCfake3mP5MAAAA
-----END PRIVATE KEY-----`
		path := createTempPEMFile(t, "key-*.pem", keyContent)
		defer func() { _ = os.Remove(path) }()

		_, err := ValidateTLSConfigPaths("", "", path)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "certificate and key must be provided together")
	})
}
