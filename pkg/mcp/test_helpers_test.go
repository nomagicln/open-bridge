//nolint:dupl // This file contains shared test helpers that define consistent test data.
package mcp

import (
	"github.com/getkin/kin-openapi/openapi3"
)

// createTestTools creates a set of test tool metadata for testing search engines.
// This is shared across multiple test files to avoid code duplication.
func createTestTools() []ToolMetadata {
	return []ToolMetadata{
		{
			ID:          "listPets",
			Name:        "listPets",
			Description: "List all pets in the store",
			Method:      "GET",
			Path:        "/pets",
			Tags:        []string{"pets", "store"},
		},
		{
			ID:          "createPet",
			Name:        "createPet",
			Description: "Create a new pet",
			Method:      "POST",
			Path:        "/pets",
			Tags:        []string{"pets"},
		},
		{
			ID:          "getPetById",
			Name:        "getPetById",
			Description: "Get a pet by its ID",
			Method:      "GET",
			Path:        "/pets/{petId}",
			Tags:        []string{"pets"},
		},
		{
			ID:          "deletePet",
			Name:        "deletePet",
			Description: "Delete a pet from the store",
			Method:      "DELETE",
			Path:        "/pets/{petId}",
			Tags:        []string{"pets", "admin"},
		},
		{
			ID:          "listUsers",
			Name:        "listUsers",
			Description: "List all users",
			Method:      "GET",
			Path:        "/users",
			Tags:        []string{"users"},
		},
	}
}

// createTestOpenAPISpec creates a test OpenAPI specification.
// This is shared across multiple test files to avoid code duplication.
//
//nolint:funlen // Test helper function that defines comprehensive test data.
func createTestOpenAPISpec() *openapi3.T {
	stringType := openapi3.Types{"string"}
	integerType := openapi3.Types{"integer"}

	spec := &openapi3.T{
		OpenAPI: "3.0.0",
		Info: &openapi3.Info{
			Title:   "Test API",
			Version: "1.0.0",
		},
		Paths: openapi3.NewPaths(),
	}

	// Add /pets path
	petsPath := &openapi3.PathItem{
		Get: &openapi3.Operation{
			OperationID: "listPets",
			Summary:     "List all pets",
			Tags:        []string{"pets"},
			Parameters: openapi3.Parameters{
				&openapi3.ParameterRef{
					Value: &openapi3.Parameter{
						Name:        "limit",
						In:          "query",
						Description: "Maximum number of pets to return",
						Schema: &openapi3.SchemaRef{
							Value: &openapi3.Schema{
								Type: &integerType,
							},
						},
					},
				},
			},
		},
		Post: &openapi3.Operation{
			OperationID: "createPet",
			Summary:     "Create a new pet",
			Tags:        []string{"pets"},
		},
	}
	spec.Paths.Set("/pets", petsPath)

	// Add /pets/{petId} path
	petByIDPath := &openapi3.PathItem{
		Get: &openapi3.Operation{
			OperationID: "getPetById",
			Summary:     "Get a pet by ID",
			Tags:        []string{"pets"},
			Parameters: openapi3.Parameters{
				&openapi3.ParameterRef{
					Value: &openapi3.Parameter{
						Name:        "petId",
						In:          "path",
						Required:    true,
						Description: "The pet ID",
						Schema: &openapi3.SchemaRef{
							Value: &openapi3.Schema{
								Type: &stringType,
							},
						},
					},
				},
			},
		},
		Delete: &openapi3.Operation{
			OperationID: "deletePet",
			Summary:     "Delete a pet",
			Tags:        []string{"pets", "admin"},
			Parameters: openapi3.Parameters{
				&openapi3.ParameterRef{
					Value: &openapi3.Parameter{
						Name:        "petId",
						In:          "path",
						Required:    true,
						Description: "The pet ID",
						Schema: &openapi3.SchemaRef{
							Value: &openapi3.Schema{
								Type: &stringType,
							},
						},
					},
				},
			},
		},
	}
	spec.Paths.Set("/pets/{petId}", petByIDPath)

	return spec
}
