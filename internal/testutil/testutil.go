// Package testutil provides testing utilities for OpenBridge.
package testutil

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

// MockServer creates a mock HTTP server for testing.
type MockServer struct {
	*httptest.Server
	t        *testing.T
	handlers map[string]http.HandlerFunc
}

// NewMockServer creates a new mock server.
func NewMockServer(t *testing.T) *MockServer {
	m := &MockServer{
		t:        t,
		handlers: make(map[string]http.HandlerFunc),
	}

	m.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := r.Method + " " + r.URL.Path
		if handler, ok := m.handlers[key]; ok {
			handler(w, r)
			return
		}
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]string{"error": "not found"})
	}))

	return m
}

// On registers a handler for a specific method and path.
func (m *MockServer) On(method, path string, handler http.HandlerFunc) {
	m.handlers[method+" "+path] = handler
}

// OnJSON registers a handler that returns JSON.
func (m *MockServer) OnJSON(method, path string, statusCode int, response any) {
	m.handlers[method+" "+path] = func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(statusCode)
		json.NewEncoder(w).Encode(response)
	}
}

// URL returns the server URL.
func (m *MockServer) URL() string {
	return m.Server.URL
}

// Close shuts down the mock server.
func (m *MockServer) Close() {
	m.Server.Close()
}

// TempOpenAPISpec creates a temporary OpenAPI spec file for testing.
func TempOpenAPISpec(t *testing.T, spec string) string {
	t.Helper()

	tmpFile := t.TempDir() + "/spec.yaml"
	if err := writeFile(tmpFile, spec); err != nil {
		t.Fatalf("failed to write temp spec: %v", err)
	}

	return tmpFile
}

func writeFile(path, content string) error {
	return os.WriteFile(path, []byte(content), 0644)
}

// MinimalOpenAPISpec returns a minimal valid OpenAPI 3.0 spec.
const MinimalOpenAPISpec = `
openapi: "3.0.0"
info:
  title: Test API
  version: "1.0.0"
paths:
  /users:
    get:
      operationId: listUsers
      summary: List all users
      responses:
        "200":
          description: Success
    post:
      operationId: createUser
      summary: Create a user
      requestBody:
        required: true
        content:
          application/json:
            schema:
              type: object
              properties:
                name:
                  type: string
                email:
                  type: string
      responses:
        "201":
          description: Created
  /users/{id}:
    get:
      operationId: getUser
      summary: Get a user by ID
      parameters:
        - name: id
          in: path
          required: true
          schema:
            type: string
      responses:
        "200":
          description: Success
    delete:
      operationId: deleteUser
      summary: Delete a user
      parameters:
        - name: id
          in: path
          required: true
          schema:
            type: string
      responses:
        "204":
          description: Deleted
`

// PetstoreOpenAPISpec returns the Swagger Petstore spec for integration testing.
const PetstoreOpenAPISpec = `
openapi: "3.0.0"
info:
  title: Petstore API
  version: "1.0.0"
servers:
  - url: https://petstore.swagger.io/v2
paths:
  /pet:
    post:
      operationId: addPet
      summary: Add a new pet to the store
      requestBody:
        required: true
        content:
          application/json:
            schema:
              $ref: '#/components/schemas/Pet'
      responses:
        "200":
          description: successful operation
  /pet/{petId}:
    get:
      operationId: getPetById
      summary: Find pet by ID
      parameters:
        - name: petId
          in: path
          required: true
          schema:
            type: integer
            format: int64
      responses:
        "200":
          description: successful operation
components:
  schemas:
    Pet:
      type: object
      required:
        - name
      properties:
        id:
          type: integer
          format: int64
        name:
          type: string
        status:
          type: string
          enum:
            - available
            - pending
            - sold
`
