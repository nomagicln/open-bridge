package testutil

import "github.com/getkin/kin-openapi/openapi3"

// InheritedParameterSpec is a resolved fixture for interface-equivalence tests.
// The path's required id is inherited and its view parameter is overridden.
func InheritedParameterSpec() *openapi3.T {
	description := "OK"
	id := &openapi3.ParameterRef{Value: &openapi3.Parameter{
		Name: "id", In: "path", Required: true,
		Schema: &openapi3.SchemaRef{Value: openapi3.NewStringSchema()},
	}}
	view := func(values ...any) *openapi3.ParameterRef {
		return &openapi3.ParameterRef{Value: &openapi3.Parameter{
			Name: "view", In: "query",
			Schema: &openapi3.SchemaRef{Value: &openapi3.Schema{Type: &openapi3.Types{"string"}, Enum: values}},
		}}
	}
	doc := &openapi3.T{
		OpenAPI: "3.0.3", Info: &openapi3.Info{Title: "Inherited parameters", Version: "1"},
		Paths: openapi3.NewPaths(),
	}
	doc.Paths.Set("/users/{id}", &openapi3.PathItem{
		Parameters: openapi3.Parameters{id, view("brief")},
		Get: &openapi3.Operation{
			OperationID: "getUser", Parameters: openapi3.Parameters{view("full", "compact")},
			Responses: openapi3.NewResponses(openapi3.WithStatus(200, &openapi3.ResponseRef{
				Value: &openapi3.Response{Description: &description},
			})),
		},
	})
	return doc
}
