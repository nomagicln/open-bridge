// Package spec provides OpenAPI specification parsing and validation.
package spec

import (
	"bytes"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
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
	if isEmptyOrJSON(data) {
		return false
	}

	return hasYAMLIndicators(data)
}

// isEmptyOrJSON checks if data is empty or looks like JSON.
func isEmptyOrJSON(data []byte) bool {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 {
		return true
	}

	return trimmed[0] == '{' || trimmed[0] == '['
}

// hasYAMLIndicators checks for YAML-specific patterns.
func hasYAMLIndicators(data []byte) bool {
	trimmed := bytes.TrimSpace(data)

	return bytes.Contains(trimmed, []byte(": ")) ||
		bytes.Contains(trimmed, []byte(":\n")) ||
		bytes.HasPrefix(trimmed, []byte("---")) ||
		bytes.HasPrefix(trimmed, []byte("-")) ||
		bytes.HasPrefix(trimmed, []byte("#")) ||
		hasOpenAPIIndicators(trimmed)
}

// hasOpenAPIIndicators checks for OpenAPI/Swagger-specific patterns.
func hasOpenAPIIndicators(data []byte) bool {
	content := string(data)

	return bytes.Contains(data, []byte("swagger:")) ||
		bytes.Contains(data, []byte("openapi:")) ||
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
//
// It decodes the first document with gopkg.in/yaml.v3 and normalizes the value
// tree to JSON-compatible data. The conversion intentionally preserves the
// semantics of the archived github.com/ghodss/yaml implementation it replaces:
//   - map keys are stringified (integer, float and boolean keys included);
//   - YAML 1.1 boolean literals (yes/no/on/off/...) become JSON booleans while
//     quoted scalars stay strings;
//   - unquoted timestamps stay strings instead of becoming time.Time values;
//   - duplicate mapping keys keep the last occurrence (lenient like yaml.v2).
func (s *YAMLStrategy) ToJSON(data []byte) ([]byte, error) {
	var root yaml.Node
	if err := yaml.Unmarshal(data, &root); err != nil {
		return nil, err
	}

	if len(root.Content) == 0 {
		return []byte("null"), nil
	}

	normalizeYAMLNode(&root)

	var value any
	if err := root.Content[0].Decode(&value); err != nil {
		return nil, err
	}

	converted, err := toJSONCompatible(value)
	if err != nil {
		return nil, err
	}

	return json.Marshal(converted)
}

// normalizeYAMLNode rewrites a parsed YAML node tree so that decoding it
// produces the same JSON semantics as the previous ghodss/yaml converter.
func normalizeYAMLNode(n *yaml.Node) {
	switch n.Kind {
	case yaml.DocumentNode, yaml.SequenceNode:
		for _, child := range n.Content {
			normalizeYAMLNode(child)
		}
	case yaml.MappingNode:
		dedupeMappingKeys(n)
		for _, child := range n.Content {
			normalizeYAMLNode(child)
		}
	case yaml.ScalarNode:
		normalizeYAMLScalar(n)
	default:
		// Alias and zero nodes need no normalization; decoding resolves them.
	}
}

// yamlKeyID identifies a mapping key the same way yaml.v3's duplicate-key
// detection does (kind plus textual value).
type yamlKeyID struct {
	kind  yaml.Kind
	value string
}

// dedupeMappingKeys keeps only the last occurrence of duplicate mapping keys.
// yaml.v3 rejects duplicates while yaml.v2 (used by the previous ghodss/yaml
// dependency) silently kept the last value; this restores that behavior.
func dedupeMappingKeys(n *yaml.Node) {
	last := make(map[yamlKeyID]int, len(n.Content)/2)
	for i := 0; i+1 < len(n.Content); i += 2 {
		key := n.Content[i]
		last[yamlKeyID{kind: key.Kind, value: key.Value}] = i
	}

	kept := make([]*yaml.Node, 0, len(n.Content))
	for i := 0; i+1 < len(n.Content); i += 2 {
		key := n.Content[i]
		if last[yamlKeyID{kind: key.Kind, value: key.Value}] == i {
			kept = append(kept, key, n.Content[i+1])
		}
	}

	n.Content = kept
}

// legacyYAMLBooleans maps YAML 1.1 boolean spellings that yaml.v3 resolves as
// strings but yaml.v2 (the previous implementation) resolved as booleans.
var legacyYAMLBooleans = map[string]string{
	"y": "true", "Y": "true", "yes": "true", "Yes": "true", "YES": "true",
	"on": "true", "On": "true", "ON": "true",
	"n": "false", "N": "false", "no": "false", "No": "false", "NO": "false",
	"off": "false", "Off": "false", "OFF": "false",
}

// normalizeYAMLScalar rewrites a scalar node so that decoding it matches the
// legacy ghodss/yaml semantics (quoted scalars are always left untouched).
func normalizeYAMLScalar(n *yaml.Node) {
	if n.Style != 0 {
		return
	}

	switch n.Tag {
	case "!!timestamp":
		// yaml.v2 kept unquoted timestamps as strings; yaml.v3 resolves them
		// to time.Time, which would change the JSON output.
		n.Tag = "!!str"
	case "!!str":
		if normalized, ok := legacyYAMLBooleans[n.Value]; ok {
			n.Tag = "!!bool"
			n.Value = normalized
		}
	default:
		// Numeric and native boolean scalars need no normalization.
	}
}

// toJSONCompatible converts a decoded YAML value into data that encoding/json
// can marshal, stringifying non-string map keys like the legacy converter.
func toJSONCompatible(value any) (any, error) {
	switch typed := value.(type) {
	case map[string]any:
		out := make(map[string]any, len(typed))
		for key, item := range typed {
			converted, err := toJSONCompatible(item)
			if err != nil {
				return nil, err
			}
			out[key] = converted
		}
		return out, nil
	case map[any]any:
		out := make(map[string]any, len(typed))
		for key, item := range typed {
			keyString, err := yamlMapKeyString(key)
			if err != nil {
				return nil, err
			}
			converted, err := toJSONCompatible(item)
			if err != nil {
				return nil, err
			}
			out[keyString] = converted
		}
		return out, nil
	case []any:
		out := make([]any, 0, len(typed))
		for _, item := range typed {
			converted, err := toJSONCompatible(item)
			if err != nil {
				return nil, err
			}
			out = append(out, converted)
		}
		return out, nil
	default:
		return value, nil
	}
}

// yamlMapKeyString converts a decoded YAML mapping key to a JSON object key.
func yamlMapKeyString(key any) (string, error) {
	switch typed := key.(type) {
	case string:
		return typed, nil
	case int:
		return strconv.Itoa(typed), nil
	case int64:
		return strconv.FormatInt(typed, 10), nil
	case uint64:
		return strconv.FormatUint(typed, 10), nil
	case float64:
		return formatYAMLFloatKey(typed), nil
	case bool:
		return strconv.FormatBool(typed), nil
	default:
		return "", fmt.Errorf("unsupported map key of type %T", key)
	}
}

// formatYAMLFloatKey formats a float mapping key the same way yaml.v2 did.
func formatYAMLFloatKey(value float64) string {
	switch formatted := strconv.FormatFloat(value, 'g', -1, 32); formatted {
	case "+Inf":
		return ".inf"
	case "-Inf":
		return "-.inf"
	case "NaN":
		return ".nan"
	default:
		return formatted
	}
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
	if strategy := d.GetStrategyForData(data); strategy != nil {
		if err := strategy.Unmarshal(data, v); err == nil {
			return nil
		}
	}

	lastErr := d.tryAllStrategies(data, v, func(s UnmarshalStrategy, data []byte, v any) error {
		return s.Unmarshal(data, v)
	})

	return fmt.Errorf("failed to unmarshal data with any strategy: %w", lastErr)
}

// ToJSONWithFallback converts data to JSON using detected format,
// with fallback to trying all strategies.
func (d *ContentDetector) ToJSONWithFallback(data []byte) ([]byte, error) {
	if strategy := d.GetStrategyForData(data); strategy != nil {
		if jsonData, err := strategy.ToJSON(data); err == nil {
			return jsonData, nil
		}
	}

	lastErr := d.tryAllStrategies(data, nil, func(s UnmarshalStrategy, data []byte, _ any) error {
		var err error
		_, err = s.ToJSON(data)
		return err
	})

	return nil, fmt.Errorf("failed to convert data to JSON with any strategy: %w", lastErr)
}

// tryAllStrategies attempts to process data with all available strategies.
func (d *ContentDetector) tryAllStrategies(data []byte, v any, fn func(UnmarshalStrategy, []byte, any) error) error {
	var lastErr error
	for _, strategy := range d.strategies {
		err := fn(strategy, data, v)
		if err == nil {
			return nil
		}
		lastErr = err
	}
	return lastErr
}
