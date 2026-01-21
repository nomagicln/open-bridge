package request

import (
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
)

type validateParamsCase struct {
	name     string
	params   map[string]any
	opParams openapi3.Parameters
	wantErr  bool
	errMsg   string
}

func objectSchemaWithStringArray(nameField, arrayField string) *openapi3.Schema {
	return &openapi3.Schema{
		Type: &openapi3.Types{"object"},
		Properties: map[string]*openapi3.SchemaRef{
			nameField: {
				Value: stringSchema(),
			},
			arrayField: {
				Value: &openapi3.Schema{
					Type: &openapi3.Types{"array"},
					Items: &openapi3.SchemaRef{
						Value: stringSchema(),
					},
				},
			},
		},
	}
}

func stringSchema() *openapi3.Schema {
	return &openapi3.Schema{Type: &openapi3.Types{"string"}}
}

func intSchema() *openapi3.Schema {
	return &openapi3.Schema{Type: &openapi3.Types{"integer"}}
}

func paramRef(name, in string, required bool, schema *openapi3.Schema) *openapi3.ParameterRef {
	return &openapi3.ParameterRef{
		Value: &openapi3.Parameter{
			Name:     name,
			In:       in,
			Required: required,
			Schema: &openapi3.SchemaRef{
				Value: schema,
			},
		},
	}
}

func runValidateParamsTests(t *testing.T, b *Builder, tests []validateParamsCase) {
	t.Helper()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := b.ValidateParams(tt.params, tt.opParams, nil)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateParams() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.wantErr && tt.errMsg != "" && err != nil && err.Error() != tt.errMsg {
				t.Errorf("ValidateParams() error message = %v, want %v", err.Error(), tt.errMsg)
			}
		})
	}
}
