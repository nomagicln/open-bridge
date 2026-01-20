package proptest

import (
	"testing"

	"github.com/leanovate/gopter"
	"github.com/leanovate/gopter/prop"
)

// TestPropertyTestingSetup verifies that gopter is properly set up.
func TestPropertyTestingSetup(t *testing.T) {
	properties := gopter.NewProperties(FastTestParameters())

	// Property: String concatenation is associative
	properties.Property("string concatenation is associative", prop.ForAll(
		func(a, b, c string) bool {
			return (a+b)+c == a+(b+c)
		},
		AlphaString(),
		AlphaString(),
		AlphaString(),
	))

	properties.TestingRun(t)
}

// TestGeneratorSetup tests that basic generators work.
func TestGeneratorSetup(t *testing.T) {
	properties := gopter.NewProperties(FastTestParameters())

	// Test AlphaString generator
	properties.Property("AlphaString generates strings", prop.ForAll(
		func(s string) bool {
			return true // Just verify it generates without error
		},
		AlphaString(),
	))

	// Test Identifier generator
	properties.Property("Identifier generates valid identifiers", prop.ForAll(
		func(id string) bool {
			return len(id) > 0 // Non-empty identifier
		},
		Identifier(),
	))

	// Test IntRange generator
	properties.Property("IntRange generates integers in range", prop.ForAll(
		func(n int) bool {
			return n >= 0 && n <= 100
		},
		IntRange(0, 100),
	))

	// Test Bool generator
	properties.Property("Bool generates boolean values", prop.ForAll(
		func(b bool) bool {
			return b == true || b == false
		},
		Bool(),
	))

	properties.TestingRun(t)
}
