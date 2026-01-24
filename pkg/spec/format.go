// Package spec provides OpenAPI specification parsing and validation.
package spec

import (
	"bytes"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	ghodssyaml "github.com/ghodss/yaml"
)

// =============================================================================
// Content Format Detection and Unmarshaling Strategy
// =============================================================================

// ContentFormat represents the detected format of spec data.
type ContentFormat string

const (
	// FormatUnknown indicates the format could not be determined.
	FormatUnknown ContentFormat = "unknown"
	// FormatJSON indicates JSON format.
	FormatJSON ContentFormat = "json"
	// FormatYAML indicates YAML format.
	FormatYAML ContentFormat = "yaml"
)

// UnmarshalStrategy defines the interface for content unmarshaling strategies.
type UnmarshalStrategy interface {
	// Format returns the content format this strategy handles.
	Format() ContentFormat

	// CanHandle returns true if this strategy can likely handle the given data.
	// This is a quick heuristic check, not full validation.
	CanHandle(data []byte) bool

	// Unmarshal deserializes the data into the target object.
	// For Swagger 2.0 specs, this should produce JSON-compatible output
	// that can be unmarshaled into openapi2.T.
	Unmarshal(data []byte, v any) error

	// ToJSON converts the data to JSON format.
	// This is useful for libraries that only accept JSON input.
	ToJSON(data []byte) ([]byte, error)
}

// =============================================================================
// JSON Strategy
// =============================================================================

// JSONStrategy implements UnmarshalStrategy for JSON format.
type JSONStrategy struct{}

// Format returns FormatJSON.
func (s *JSONStrategy) Format() ContentFormat {
	return FormatJSON
}

// CanHandle checks if data appears to be JSON.
func (s *JSONStrategy) CanHandle(data []byte) bool {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 {
		return false
	}
	// JSON typically starts with { or [
	return trimmed[0] == '{' || trimmed[0] == '['
}

// Unmarshal deserializes JSON data.
func (s *JSONStrategy) Unmarshal(data []byte, v any) error {
	return json.Unmarshal(data, v)
}

// ToJSON returns the data as-is (it's already JSON).
func (s *JSONStrategy) ToJSON(data []byte) ([]byte, error) {
	// Validate it's actually JSON
	var js json.RawMessage
	if err := json.Unmarshal(data, &js); err != nil {
		return nil, err
	}
	return data, nil
}

// =============================================================================
// YAML Strategy
// =============================================================================

// YAMLStrategy implements UnmarshalStrategy for YAML format.
type YAMLStrategy struct{}

// Format returns FormatYAML.
func (s *YAMLStrategy) Format() ContentFormat {
	return FormatYAML
}

// CanHandle checks if data appears to be YAML (and not JSON).
// YAML is a superset of JSON, so we check for YAML-specific patterns.
func (s *YAMLStrategy) CanHandle(data []byte) bool {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 {
		return false
	}

	// If it starts with { or [, it's more likely JSON
	if trimmed[0] == '{' || trimmed[0] == '[' {
		return false
	}

	// Common YAML indicators:
	// - Lines with key: value pattern
	// - Lines starting with - (list items)
	// - Document separator ---
	// - Comments starting with #
	content := string(trimmed)
	return bytes.Contains(trimmed, []byte(": ")) ||
		bytes.Contains(trimmed, []byte(":\n")) ||
		bytes.HasPrefix(trimmed, []byte("---")) ||
		bytes.HasPrefix(trimmed, []byte("-")) ||
		bytes.HasPrefix(trimmed, []byte("#")) ||
		// Also check for common OpenAPI/Swagger YAML patterns
		bytes.Contains(trimmed, []byte("swagger:")) ||
		bytes.Contains(trimmed, []byte("openapi:")) ||
		// Check for quoted version strings common in YAML
		bytes.Contains([]byte(content), []byte("'2.0'")) ||
		bytes.Contains([]byte(content), []byte("'3.0"))
}

// Unmarshal deserializes YAML data by first converting to JSON.
// This is necessary because some nested types in libraries (e.g., openapi3.Types
// in kin-openapi) only implement UnmarshalJSON but not UnmarshalYAML,
// causing direct YAML unmarshaling to fail with type errors.
func (s *YAMLStrategy) Unmarshal(data []byte, v any) error {
	jsonData, err := s.ToJSON(data)
	if err != nil {
		return err
	}
	return json.Unmarshal(jsonData, v)
}

// ToJSON converts YAML to JSON.
func (s *YAMLStrategy) ToJSON(data []byte) ([]byte, error) {
	return ghodssyaml.YAMLToJSON(data)
}

// =============================================================================
// Content Detector
// =============================================================================

// ContentDetector detects content format and selects appropriate strategy.
type ContentDetector struct {
	strategies []UnmarshalStrategy
}

// NewContentDetector creates a new ContentDetector with default strategies.
func NewContentDetector() *ContentDetector {
	return &ContentDetector{
		strategies: []UnmarshalStrategy{
			&JSONStrategy{}, // JSON first (more specific)
			&YAMLStrategy{}, // YAML as fallback (superset of JSON)
		},
	}
}

// DetectFormat detects the content format of the given data.
func (d *ContentDetector) DetectFormat(data []byte) ContentFormat {
	for _, strategy := range d.strategies {
		if strategy.CanHandle(data) {
			return strategy.Format()
		}
	}
	return FormatUnknown
}

// DetectFormatFromContentType detects format from HTTP Content-Type header.
func (d *ContentDetector) DetectFormatFromContentType(contentType string) ContentFormat {
	ct := strings.ToLower(contentType)

	switch {
	case strings.Contains(ct, "application/json"):
		return FormatJSON
	case strings.Contains(ct, "text/json"):
		return FormatJSON
	case strings.Contains(ct, "application/yaml"):
		return FormatYAML
	case strings.Contains(ct, "text/yaml"):
		return FormatYAML
	case strings.Contains(ct, "application/x-yaml"):
		return FormatYAML
	case strings.Contains(ct, "text/x-yaml"):
		return FormatYAML
	default:
		return FormatUnknown
	}
}

// DetectFormatFromExtension detects format from file extension.
func (d *ContentDetector) DetectFormatFromExtension(filename string) ContentFormat {
	ext := strings.ToLower(filepath.Ext(filename))

	switch ext {
	case ".json":
		return FormatJSON
	case ".yaml", ".yml":
		return FormatYAML
	default:
		return FormatUnknown
	}
}

// GetStrategy returns the appropriate strategy for the given format.
func (d *ContentDetector) GetStrategy(format ContentFormat) UnmarshalStrategy {
	for _, strategy := range d.strategies {
		if strategy.Format() == format {
			return strategy
		}
	}
	return nil
}

// GetStrategyForData detects the format and returns the appropriate strategy.
func (d *ContentDetector) GetStrategyForData(data []byte) UnmarshalStrategy {
	format := d.DetectFormat(data)
	if format == FormatUnknown {
		// Default to YAML strategy as it can handle both YAML and JSON
		return &YAMLStrategy{}
	}
	return d.GetStrategy(format)
}

// UnmarshalWithFallback attempts to unmarshal data using detected format,
// with fallback to trying all strategies if detection fails.
func (d *ContentDetector) UnmarshalWithFallback(data []byte, v any) error {
	// First, try the detected strategy
	strategy := d.GetStrategyForData(data)
	if strategy != nil {
		if err := strategy.Unmarshal(data, v); err == nil {
			return nil
		}
	}

	// Fallback: try all strategies in order
	var lastErr error
	for _, s := range d.strategies {
		if err := s.Unmarshal(data, v); err == nil {
			return nil
		} else {
			lastErr = err
		}
	}

	return fmt.Errorf("failed to unmarshal data with any strategy: %w", lastErr)
}

// ToJSONWithFallback converts data to JSON using detected format,
// with fallback to trying all strategies.
func (d *ContentDetector) ToJSONWithFallback(data []byte) ([]byte, error) {
	// First, try the detected strategy
	strategy := d.GetStrategyForData(data)
	if strategy != nil {
		if jsonData, err := strategy.ToJSON(data); err == nil {
			return jsonData, nil
		}
	}

	// Fallback: try all strategies in order
	var lastErr error
	for _, s := range d.strategies {
		if jsonData, err := s.ToJSON(data); err == nil {
			return jsonData, nil
		} else {
			lastErr = err
		}
	}

	return nil, fmt.Errorf("failed to convert data to JSON with any strategy: %w", lastErr)
}
