package openapi

import (
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
)

func operationTestParameter(name, in, description string) *openapi3.ParameterRef {
	return &openapi3.ParameterRef{Value: &openapi3.Parameter{
		Name: name, In: in, Description: description,
		Schema: &openapi3.SchemaRef{Value: openapi3.NewStringSchema()},
	}}
}

func TestEffectiveOperation(t *testing.T) {
	pathID := operationTestParameter("id", "path", "inherited")
	queryID := operationTestParameter("id", "query", "different location")
	override := operationTestParameter("id", "path", "operation override")
	item := &openapi3.PathItem{Parameters: openapi3.Parameters{pathID, queryID}}
	op := &openapi3.Operation{OperationID: "getUser", Parameters: openapi3.Parameters{override}}
	view := EffectiveOperation(item, op)
	if view == op {
		t.Fatal("effective view must not alias the source operation")
	}
	if len(view.Parameters) != 2 || view.Parameters[0] != override || view.Parameters[1] != queryID {
		t.Fatalf("parameter inheritance or operation override lost: %#v", view.Parameters)
	}
	view.Parameters[0] = nil
	if len(op.Parameters) != 1 || op.Parameters[0] != override || item.Parameters[0] != pathID {
		t.Fatal("view changed original parameter slices")
	}
}

func TestEffectiveOperationNilAndUnresolved(t *testing.T) {
	if EffectiveOperation(nil, nil) != nil {
		t.Fatal("nil operation must remain nil")
	}
	op := &openapi3.Operation{Parameters: openapi3.Parameters{nil, {Ref: "#/unresolved"}}}
	view := EffectiveOperation(nil, op)
	if view == op || len(view.Parameters) != 2 || view.Parameters[1].Ref != "#/unresolved" {
		t.Fatal("unresolved references must be retained for normal validation/fallback")
	}
}
