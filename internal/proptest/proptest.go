// Package proptest provides property-based testing infrastructure and generators.
package proptest

import (
	"github.com/leanovate/gopter"
	"github.com/leanovate/gopter/gen"
)

// TestParameters returns the standard test parameters for property tests.
// Default: 1000 iterations for a good balance between coverage and speed.
func TestParameters() *gopter.TestParameters {
	params := gopter.DefaultTestParameters()
	params.MinSuccessfulTests = 1000
	return params
}

// AlphaString generates random alphabetic strings.
func AlphaString() gopter.Gen {
	return gen.AlphaString()
}

// Identifier generates random valid identifiers (alphanumeric, starting with letter).
func Identifier() gopter.Gen {
	return gen.Identifier()
}

// AnyString generates any string.
func AnyString() gopter.Gen {
	return gen.AnyString()
}

// IntRange generates integers in a range.
func IntRange(min, max int) gopter.Gen {
	return gen.IntRange(min, max)
}

// SliceOf generates slices of elements from the given generator.
func SliceOf(elementGen gopter.Gen) gopter.Gen {
	return gen.SliceOf(elementGen)
}

// MapOf generates maps from the given key and value generators.
func MapOf(keyGen, valueGen gopter.Gen) gopter.Gen {
	return gen.MapOf(keyGen, valueGen)
}

// OneConstOf generates one of the constant values.
func OneConstOf(values ...any) gopter.Gen {
	return gen.OneConstOf(values...)
}

// Bool generates random boolean values.
func Bool() gopter.Gen {
	return gen.Bool()
}
