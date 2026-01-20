// Package integration provides end-to-end integration tests for OpenBridge.
package integration

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/nomagicln/open-bridge/pkg/spec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestPetstoreAPI tests OpenBridge with the Petstore OpenAPI spec
func TestPetstoreAPI(t *testing.T) {
	parser := spec.NewParser()
	ctx := context.Background()

	// Test loading Petstore OpenAPI 3.0 spec from local file
	t.Run("LoadPetstoreSpec", func(t *testing.T) {
		specPath := filepath.Join("testdata", "petstore.json")
		
		loadedSpec, err := parser.LoadSpecWithContext(ctx, specPath)
		require.NoError(t, err, "should load Petstore spec successfully")
		require.NotNil(t, loadedSpec, "loaded spec should not be nil")

		// Validate the spec
		err = parser.ValidateSpec(loadedSpec)
		assert.NoError(t, err, "Petstore spec should be valid")

		// Check basic spec info
		info := spec.GetSpecInfo(loadedSpec, specPath)
		assert.NotNil(t, info, "spec info should not be nil")
		assert.Equal(t, "Swagger Petstore - OpenAPI 3.0", info.Title)
		assert.NotEmpty(t, info.Version, "spec should have a version")
		
		// Check that operations exist
		assert.NotEmpty(t, loadedSpec.Paths.Map(), "should have paths")
		
		// Verify some common Petstore endpoints exist
		paths := loadedSpec.Paths.Map()
		assert.Contains(t, paths, "/pet", "should have /pet endpoint")
		assert.Contains(t, paths, "/pet/findByStatus", "should have /pet/findByStatus endpoint")
		assert.Contains(t, paths, "/store/order", "should have /store/order endpoint")
		
		// Verify operations on paths
		petPath := paths["/pet"]
		assert.NotNil(t, petPath, "/pet path should exist")
		assert.NotNil(t, petPath.Post, "/pet should have POST operation")
		assert.Equal(t, "addPet", petPath.Post.OperationID)
		
		// Verify parameters
		findByStatusPath := paths["/pet/findByStatus"]
		assert.NotNil(t, findByStatusPath, "/pet/findByStatus path should exist")
		assert.NotNil(t, findByStatusPath.Get, "/pet/findByStatus should have GET operation")
		assert.NotEmpty(t, findByStatusPath.Get.Parameters, "findByStatus should have parameters")
		
		// Verify schemas
		assert.NotNil(t, loadedSpec.Components, "should have components")
		assert.NotNil(t, loadedSpec.Components.Schemas, "should have schemas")
		assert.Contains(t, loadedSpec.Components.Schemas, "Pet", "should have Pet schema")
		assert.Contains(t, loadedSpec.Components.Schemas, "Order", "should have Order schema")
	})
}

// TestGitHubAPI tests OpenBridge with a simplified GitHub-like OpenAPI spec
func TestGitHubAPI(t *testing.T) {
	parser := spec.NewParser()

	t.Run("LoadGitHubLikeSpec", func(t *testing.T) {
		// Create a minimal GitHub-like spec for testing
		specContent := `{
  "openapi": "3.0.0",
  "info": {
    "title": "GitHub v3 REST API",
    "version": "1.0.0"
  },
  "servers": [
    {
      "url": "https://api.github.com"
    }
  ],
  "paths": {
    "/repos/{owner}/{repo}": {
      "get": {
        "operationId": "repos/get",
        "summary": "Get a repository",
        "parameters": [
          {
            "name": "owner",
            "in": "path",
            "required": true,
            "schema": {
              "type": "string"
            }
          },
          {
            "name": "repo",
            "in": "path",
            "required": true,
            "schema": {
              "type": "string"
            }
          }
        ],
        "responses": {
          "200": {
            "description": "Response",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "properties": {
                    "name": {
                      "type": "string"
                    },
                    "full_name": {
                      "type": "string"
                    }
                  }
                }
              }
            }
          }
        }
      }
    }
  }
}`
		
		// Parse the spec from the JSON string
		loadedSpec, err := parser.ParseSpecFromJSON([]byte(specContent))
		require.NoError(t, err, "should parse GitHub-like spec successfully")
		require.NotNil(t, loadedSpec, "loaded spec should not be nil")

		// Check basic spec info
		info := spec.GetSpecInfo(loadedSpec, "github-test")
		assert.NotNil(t, info, "spec info should not be nil")
		assert.Contains(t, info.Title, "GitHub", "title should mention GitHub")
		assert.NotEmpty(t, info.Version, "spec should have a version")
		
		// Check that operations exist
		assert.NotEmpty(t, loadedSpec.Paths.Map(), "should have paths")
		
		// Verify GitHub-specific patterns (path parameters)
		paths := loadedSpec.Paths.Map()
		assert.Contains(t, paths, "/repos/{owner}/{repo}", "should have repo endpoint with path parameters")
	})
}

// TestStripeAPI tests OpenBridge with a simplified Stripe-like OpenAPI spec
func TestStripeAPI(t *testing.T) {
	parser := spec.NewParser()

	t.Run("LoadStripeLikeSpec", func(t *testing.T) {
		// Create a minimal Stripe-like spec for testing
		specContent := `{
  "openapi": "3.0.0",
  "info": {
    "title": "Stripe API",
    "version": "2023-10-16"
  },
  "servers": [
    {
      "url": "https://api.stripe.com/v1"
    }
  ],
  "paths": {
    "/customers": {
      "post": {
        "operationId": "PostCustomers",
        "summary": "Create a customer",
        "requestBody": {
          "content": {
            "application/x-www-form-urlencoded": {
              "schema": {
                "type": "object",
                "properties": {
                  "email": {
                    "type": "string"
                  }
                }
              }
            }
          }
        },
        "responses": {
          "200": {
            "description": "Successful response",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "properties": {
                    "id": {
                      "type": "string"
                    },
                    "email": {
                      "type": "string"
                    }
                  }
                }
              }
            }
          }
        }
      },
      "get": {
        "operationId": "GetCustomers",
        "summary": "List all customers",
        "responses": {
          "200": {
            "description": "Successful response",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "properties": {
                    "data": {
                      "type": "array",
                      "items": {
                        "type": "object"
                      }
                    }
                  }
                }
              }
            }
          }
        }
      }
    }
  }
}`
		
		// Parse the spec from the JSON string
		loadedSpec, err := parser.ParseSpecFromJSON([]byte(specContent))
		require.NoError(t, err, "should parse Stripe-like spec successfully")
		require.NotNil(t, loadedSpec, "loaded spec should not be nil")

		// Check basic spec info
		info := spec.GetSpecInfo(loadedSpec, "stripe-test")
		assert.NotNil(t, info, "spec info should not be nil")
		assert.Contains(t, info.Title, "Stripe", "title should mention Stripe")
		assert.NotEmpty(t, info.Version, "spec should have a version")
		
		// Check that operations exist
		assert.NotEmpty(t, loadedSpec.Paths.Map(), "should have paths")
		
		// Verify some Stripe-like patterns
		paths := loadedSpec.Paths.Map()
		assert.Contains(t, paths, "/customers", "should have customers endpoint")
		
		customersPath := paths["/customers"]
		assert.NotNil(t, customersPath.Post, "/customers should have POST operation")
		assert.NotNil(t, customersPath.Get, "/customers should have GET operation")
	})
}
