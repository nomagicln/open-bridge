package semantic

import (
	"testing"
)

type stringMappingCase struct {
	input    string
	expected string
}

func runStringMappingTests(t *testing.T, label string, tests []stringMappingCase, fn func(string) string) {
	t.Helper()

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := fn(tt.input)
			if result != tt.expected {
				t.Errorf("%s(%q) = %q, expected %q", label, tt.input, result, tt.expected)
			}
		})
	}
}
