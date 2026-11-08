package openapi

import (
	"reflect"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/leanovate/gopter"
	"github.com/leanovate/gopter/prop"
	"github.com/nomagicln/open-bridge/internal/proptest"
)

func TestPropertyEffectiveOperationIsIdempotent(t *testing.T) {
	properties := gopter.NewProperties(proptest.TestParameters())
	properties.Property("merging twice preserves overrides and distinct locations", prop.ForAll(
		func(name, inherited, replacement string) bool {
			path := &openapi3.PathItem{Parameters: openapi3.Parameters{
				operationTestParameter(name, "path", inherited),
				operationTestParameter(name, "query", inherited),
			}}
			op := &openapi3.Operation{Parameters: openapi3.Parameters{
				operationTestParameter(name, "path", replacement),
			}}
			first := EffectiveOperation(path, op)
			second := EffectiveOperation(path, first)
			return len(first.Parameters) == 2 &&
				first.Parameters[0].Value.Description == replacement &&
				first.Parameters[1].Value.In == "query" && reflect.DeepEqual(first, second) &&
				path.Parameters[0].Value.Description == inherited && len(op.Parameters) == 1
		}, proptest.Identifier(), proptest.AnyString(), proptest.AnyString(),
	))
	properties.TestingRun(t)
}
