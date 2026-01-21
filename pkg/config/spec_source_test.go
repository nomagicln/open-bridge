package config

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestNormalizeSpecSource(t *testing.T) {
	tmpDir := t.TempDir()
	specPath := filepath.Join(tmpDir, "spec.yaml")
	specContent := `
openapi: "3.0.0"
info:
  title: Normalize Test API
  version: "1.0.0"
paths: {}
`
	if err := os.WriteFile(specPath, []byte(specContent), 0644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	absSpecPath, err := filepath.Abs(specPath)
	if err != nil {
		t.Fatalf("filepath.Abs failed: %v", err)
	}

	t.Run("relative path is normalized", func(t *testing.T) {
		source, err := normalizeSpecSource(specPath)
		if err != nil {
			t.Fatalf("normalizeSpecSource failed: %v", err)
		}
		if source != absSpecPath {
			t.Errorf("expected %q, got %q", absSpecPath, source)
		}
	})

	t.Run("http URL is preserved", func(t *testing.T) {
		source, err := normalizeSpecSource("https://example.com/spec.yaml")
		if err != nil {
			t.Fatalf("normalizeSpecSource failed: %v", err)
		}
		if source != "https://example.com/spec.yaml" {
			t.Errorf("expected URL to remain unchanged, got %q", source)
		}
	})

	if runtime.GOOS == "windows" {
		t.Run("windows-style path is normalized", func(t *testing.T) {
			source, err := normalizeSpecSource(`C:\specs\petstore.yaml`)
			if err != nil {
				t.Fatalf("normalizeSpecSource failed: %v", err)
			}
			if !filepath.IsAbs(source) {
				t.Errorf("expected absolute path, got %q", source)
			}
		})
	}
}
