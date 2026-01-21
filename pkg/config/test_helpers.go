package config

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

type credentialTestCase struct {
	name         string
	isCredential bool
}

func assertNonexistentAppError(t *testing.T, err error) {
	t.Helper()

	if err == nil {
		t.Error("expected error for nonexistent app")
		return
	}

	appNotFoundError := &AppNotFoundError{}
	if !errors.As(err, &appNotFoundError) {
		t.Errorf("expected AppNotFoundError, got %T", err)
	}
}

func assertUnixShimSymlink(t *testing.T, shimPath, expectedName string) {
	t.Helper()

	info, err := os.Lstat(shimPath)
	if err != nil {
		t.Fatalf("failed to lstat shim: %v", err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		t.Error("Unix shim should be a symbolic link")
	}

	target, err := os.Readlink(shimPath)
	if err != nil {
		t.Fatalf("failed to readlink shim: %v", err)
	}
	if target == "" {
		t.Error("Unix shim symlink target should not be empty")
	}

	if filepath.Base(shimPath) != expectedName {
		t.Errorf("shim file name should be '%s', got: %s", expectedName, filepath.Base(shimPath))
	}
}

func runCredentialDetectorTests(t *testing.T, label string, cases []credentialTestCase, fn func(string) bool) {
	t.Helper()

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			result := fn(tc.name)
			if result != tc.isCredential {
				t.Errorf("%s(%q) = %v, expected %v", label, tc.name, result, tc.isCredential)
			}
		})
	}
}

// containsAny checks if the string contains any of the given substrings.
func containsAny(s string, substrs ...string) bool {
	for _, substr := range substrs {
		if len(substr) > 0 && len(s) > 0 {
			for i := 0; i <= len(s)-len(substr); i++ {
				if s[i:i+len(substr)] == substr {
					return true
				}
			}
		}
	}
	return false
}
