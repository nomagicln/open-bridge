package request

import (
	"bytes"
	"encoding/json"
	"io"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/leanovate/gopter"
	"github.com/leanovate/gopter/prop"
	"github.com/nomagicln/open-bridge/internal/proptest"
)

func TestPropertyValidatedRawBodyPreservesTransmittedBytes(t *testing.T) {
	body := &openapi3.RequestBody{Required: true, Content: openapi3.Content{
		"application/json": &openapi3.MediaType{Schema: &openapi3.SchemaRef{Value: &openapi3.Schema{
			Type: &openapi3.Types{"object"}, Required: []string{"name"},
			Properties: openapi3.Schemas{"name": {Value: openapi3.NewStringSchema()}},
		}}},
	}}
	properties := gopter.NewProperties(proptest.TestParameters())
	properties.Property("validating an explicit body cannot replace it with separate flags", prop.ForAll(
		func(name string) bool {
			data, err := json.Marshal(map[string]any{"name": name})
			if err != nil {
				return false
			}
			params := map[string]any{"body": string(data), "name": "ignored"}
			builder := NewBuilder(nil)
			if err := builder.ValidateParams(params, nil, body); err != nil {
				return false
			}
			req, err := builder.BuildRequest("POST", "/users", "https://api.example.test", params, nil, body)
			if err != nil {
				return false
			}
			defer func() { _ = req.Body.Close() }()
			actual, err := io.ReadAll(req.Body)
			return err == nil && bytes.Equal(actual, data)
		}, proptest.AnyString(),
	))
	properties.TestingRun(t)
}
