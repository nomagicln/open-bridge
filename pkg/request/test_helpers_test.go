package request

import (
	"testing"

	"github.com/99designs/keyring"
	"github.com/getkin/kin-openapi/openapi3"
	"github.com/nomagicln/open-bridge/pkg/credential"
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

// setupBuilderWithCredentials creates a Builder with a test credential manager and stores a credential.
// Returns the builder and a cleanup function that should be deferred.
func setupBuilderWithCredentials(t *testing.T, appName, profileName string, cred *credential.Credential) (*Builder, func()) {
	t.Helper()

	tmpDir := t.TempDir()

	mgr, err := credential.NewManager(
		credential.WithAllowedBackends(keyring.FileBackend),
		credential.WithFileBackend(tmpDir, keyring.FixedStringPrompt("test-password")),
	)
	if err != nil {
		t.Fatalf("Failed to create credential manager: %v", err)
	}

	if err := mgr.StoreCredential(appName, profileName, cred); err != nil {
		t.Fatalf("Failed to store credential: %v", err)
	}

	builder := NewBuilder(mgr)

	cleanup := func() {
		// Cleanup is handled by t.TempDir()
	}

	return builder, cleanup
}
