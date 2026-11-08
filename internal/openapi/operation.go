// Package openapi provides internal views of resolved OpenAPI operations.
package openapi

import "github.com/getkin/kin-openapi/openapi3"

// EffectiveOperation returns an operation view with inherited path parameters.
// Operation parameters override the same (location, name) pair. The source
// operation and its parameter slice are never modified.
func EffectiveOperation(item *openapi3.PathItem, operation *openapi3.Operation) *openapi3.Operation {
	if operation == nil {
		return nil
	}
	view := *operation
	var inherited openapi3.Parameters
	if item != nil {
		inherited = item.Parameters
	}
	view.Parameters = mergeParameters(inherited, operation.Parameters)
	return &view
}

type parameterKey struct{ location, name string }

func mergeParameters(inherited, operation openapi3.Parameters) openapi3.Parameters {
	parameters := make(openapi3.Parameters, 0, len(inherited)+len(operation))
	positions := make(map[parameterKey]int, cap(parameters))
	for _, source := range []openapi3.Parameters{inherited, operation} {
		for _, ref := range source {
			if ref == nil || ref.Value == nil {
				// Keep unresolved references so existing validators can reject them
				// and the derived index can conservatively fall back.
				parameters = append(parameters, ref)
				continue
			}
			key := parameterKey{location: ref.Value.In, name: ref.Value.Name}
			if position, ok := positions[key]; ok {
				parameters[position] = ref
			} else {
				positions[key] = len(parameters)
				parameters = append(parameters, ref)
			}
		}
	}
	return parameters
}
