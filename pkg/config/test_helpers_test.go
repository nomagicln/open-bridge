package config

import (
	"errors"
	"testing"
)

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
