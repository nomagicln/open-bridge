package install

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/nomagicln/open-bridge/pkg/config"
)

func assertNonexistentAppError(t *testing.T, err error) {
	t.Helper()

	if err == nil {
		t.Error("expected error for nonexistent app")
		return
	}

	appNotFoundError := &config.AppNotFoundError{}
	if !errors.As(err, &appNotFoundError) {
		t.Errorf("expected config.AppNotFoundError, got %T", err)
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
