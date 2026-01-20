package semantic

import (
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
)

func TestExtractResource(t *testing.T) {
	m := NewMapper()

	tests := []struct {
		path     string
		expected string
	}{
		{"/api/v1/users", "users"},
		{"/api/v1/users/{id}", "users"},
		{"/users", "users"},
		{"/users/{userId}/posts", "posts"},
		{"/users/{userId}/posts/{postId}", "posts"},
		{"/v2/pets", "pets"},
		{"/api/orders/{orderId}/items/{itemId}", "items"},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			op := &openapi3.Operation{}
			result := m.ExtractResource(tt.path, op)
			if result != tt.expected {
				t.Errorf("ExtractResource(%q) = %q, expected %q", tt.path, result, tt.expected)
			}
		})
	}
}

func TestExtractResourceWithOverride(t *testing.T) {
	m := NewMapper()

	op := &openapi3.Operation{
		Extensions: map[string]interface{}{
			"x-cli-resource": "custom-resource",
		},
	}

	result := m.ExtractResource("/api/v1/users", op)
	if result != "custom-resource" {
		t.Errorf("expected 'custom-resource', got '%s'", result)
	}
}

func TestMapVerb(t *testing.T) {
	m := NewMapper()

	tests := []struct {
		method   string
		path     string
		expected string
	}{
		{"GET", "/users", "list"},
		{"GET", "/users/{id}", "get"},
		{"POST", "/users", "create"},
		{"PUT", "/users/{id}", "update"},
		{"PATCH", "/users/{id}", "apply"},
		{"DELETE", "/users/{id}", "delete"},
	}

	for _, tt := range tests {
		t.Run(tt.method+" "+tt.path, func(t *testing.T) {
			op := &openapi3.Operation{}
			result := m.MapVerb(tt.method, tt.path, op)
			if result != tt.expected {
				t.Errorf("MapVerb(%q, %q) = %q, expected %q", tt.method, tt.path, result, tt.expected)
			}
		})
	}
}

func TestMapVerbWithOverride(t *testing.T) {
	m := NewMapper()

	op := &openapi3.Operation{
		Extensions: map[string]interface{}{
			"x-cli-verb": "trigger",
		},
	}

	result := m.MapVerb("POST", "/server/reboot", op)
	if result != "trigger" {
		t.Errorf("expected 'trigger', got '%s'", result)
	}
}
