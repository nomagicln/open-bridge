// Package config provides configuration management for OpenBridge.
// This file contains path validation utilities for TLS certificates and other files.
package config

import (
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// PathValidationError represents a path validation error with details.
type PathValidationError struct {
	Path    string
	Reason  string
	Wrapped error
}

func (e *PathValidationError) Error() string {
	if e.Wrapped != nil {
		return fmt.Sprintf("invalid path %q: %s: %v", e.Path, e.Reason, e.Wrapped)
	}
	return fmt.Sprintf("invalid path %q: %s", e.Path, e.Reason)
}

func (e *PathValidationError) Unwrap() error {
	return e.Wrapped
}

// ValidateAndResolvePath validates that a file exists and is readable,
// then returns its absolute path.
func ValidateAndResolvePath(path string) (string, error) {
	if err := validatePathNotEmpty(path); err != nil {
		return "", err
	}

	path = expandHomeDirectory(path)

	absPath, err := resolveAbsolutePath(path)
	if err != nil {
		return "", err
	}

	if err := checkFileExists(absPath); err != nil {
		return "", err
	}

	if err := checkNotDirectory(absPath); err != nil {
		return "", err
	}

	return checkFileReadable(absPath)
}

// validatePathNotEmpty checks that the path is not empty.
func validatePathNotEmpty(path string) error {
	if path == "" {
		return &PathValidationError{Path: path, Reason: "path cannot be empty"}
	}
	return nil
}

// expandHomeDirectory expands ~ in the path to the user's home directory.
func expandHomeDirectory(path string) string {
	if strings.HasPrefix(path, "~/") {
		home, _ := os.UserHomeDir()
		path = filepath.Join(home, path[2:])
	}
	return path
}

// resolveAbsolutePath resolves the path to an absolute path.
func resolveAbsolutePath(path string) (string, error) {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return "", &PathValidationError{Path: path, Reason: "failed to resolve absolute path", Wrapped: err}
	}
	return absPath, nil
}

// checkFileExists checks if the file exists.
func checkFileExists(absPath string) error {
	_, err := os.Stat(absPath)
	if err != nil {
		if os.IsNotExist(err) {
			return &PathValidationError{Path: absPath, Reason: "file does not exist"}
		}
		return &PathValidationError{Path: absPath, Reason: "failed to stat file", Wrapped: err}
	}
	return nil
}

// checkNotDirectory checks if the path is not a directory.
func checkNotDirectory(absPath string) error {
	info, err := os.Stat(absPath)
	if err != nil {
		return err
	}
	if info.IsDir() {
		return &PathValidationError{Path: absPath, Reason: "path is a directory, not a file"}
	}
	return nil
}

// checkFileReadable checks if the file is readable.
func checkFileReadable(absPath string) (string, error) {
	file, err := os.Open(absPath)
	if err != nil {
		return "", &PathValidationError{Path: absPath, Reason: "file is not readable", Wrapped: err}
	}
	_ = file.Close()
	return absPath, nil
}

// ValidateTLSCertFile validates that a file is a valid PEM-encoded certificate.
func ValidateTLSCertFile(path string) (string, error) {
	absPath, err := ValidateAndResolvePath(path)
	if err != nil {
		return "", err
	}

	content, err := os.ReadFile(absPath)
	if err != nil {
		return "", &PathValidationError{Path: path, Reason: "failed to read certificate file", Wrapped: err}
	}

	// Try to decode PEM block
	block, _ := pem.Decode(content)
	if block == nil {
		return "", &PathValidationError{Path: path, Reason: "file is not PEM encoded"}
	}

	// Check for valid certificate types
	validTypes := []string{"CERTIFICATE", "X509 CERTIFICATE", "TRUSTED CERTIFICATE"}
	isValidType := slices.Contains(validTypes, block.Type)

	if !isValidType {
		return "", &PathValidationError{
			Path:   path,
			Reason: fmt.Sprintf("invalid PEM block type %q, expected CERTIFICATE", block.Type),
		}
	}

	// Try to parse the certificate
	_, err = x509.ParseCertificate(block.Bytes)
	if err != nil {
		return "", &PathValidationError{Path: path, Reason: "invalid certificate", Wrapped: err}
	}

	return absPath, nil
}

// ValidateTLSKeyFile validates that a file is a valid PEM-encoded private key.
func ValidateTLSKeyFile(path string) (string, error) {
	absPath, err := ValidateAndResolvePath(path)
	if err != nil {
		return "", err
	}

	content, err := os.ReadFile(absPath)
	if err != nil {
		return "", &PathValidationError{Path: path, Reason: "failed to read key file", Wrapped: err}
	}

	// Try to decode PEM block
	block, _ := pem.Decode(content)
	if block == nil {
		return "", &PathValidationError{Path: path, Reason: "file is not PEM encoded"}
	}

	// Check for valid key types
	validTypes := []string{
		"RSA PRIVATE KEY",
		"EC PRIVATE KEY",
		"PRIVATE KEY", // PKCS#8
		"ENCRYPTED PRIVATE KEY",
	}
	isValidType := slices.Contains(validTypes, block.Type)

	if !isValidType {
		return "", &PathValidationError{
			Path:   path,
			Reason: fmt.Sprintf("invalid PEM block type %q, expected a private key type", block.Type),
		}
	}

	return absPath, nil
}

// ValidateTLSCACertFile validates a CA certificate file (can contain multiple certificates).
func ValidateTLSCACertFile(path string) (string, error) {
	absPath, err := ValidateAndResolvePath(path)
	if err != nil {
		return "", err
	}

	content, err := os.ReadFile(absPath)
	if err != nil {
		return "", &PathValidationError{Path: path, Reason: "failed to read CA certificate file", Wrapped: err}
	}

	// Try to parse as certificate pool
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(content) {
		return "", &PathValidationError{Path: path, Reason: "file does not contain valid PEM-encoded certificates"}
	}

	return absPath, nil
}

// ValidateTLSConfig validates all TLS configuration paths and returns resolved absolute paths.
type TLSConfigValidation struct {
	CAFile   string
	CertFile string
	KeyFile  string
}

// ValidateTLSConfigPaths validates TLS configuration file paths.
func ValidateTLSConfigPaths(caFile, certFile, keyFile string) (*TLSConfigValidation, error) {
	result := &TLSConfigValidation{}

	if caFile != "" {
		resolved, err := ValidateTLSCACertFile(caFile)
		if err != nil {
			return nil, fmt.Errorf("CA certificate: %w", err)
		}
		result.CAFile = resolved
	}

	if certFile != "" {
		resolved, err := ValidateTLSCertFile(certFile)
		if err != nil {
			return nil, fmt.Errorf("client certificate: %w", err)
		}
		result.CertFile = resolved
	}

	if keyFile != "" {
		resolved, err := ValidateTLSKeyFile(keyFile)
		if err != nil {
			return nil, fmt.Errorf("client key: %w", err)
		}
		result.KeyFile = resolved
	}

	// Check for mismatched cert/key configuration
	if (certFile != "" && keyFile == "") || (certFile == "" && keyFile != "") {
		return nil, fmt.Errorf("client certificate and key must be provided together")
	}

	return result, nil
}

// IsValidPath checks if a path is valid without returning detailed error.
func IsValidPath(path string) bool {
	_, err := ValidateAndResolvePath(path)
	return err == nil
}
