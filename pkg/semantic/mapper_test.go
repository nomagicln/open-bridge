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
		Extensions: map[string]any{
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
		Extensions: map[string]any{
			"x-cli-verb": "trigger",
		},
	}

	result := m.MapVerb("POST", "/server/reboot", op)
	if result != "trigger" {
		t.Errorf("expected 'trigger', got '%s'", result)
	}
}

func TestBuildCommandTree(t *testing.T) {
	spec := &openapi3.T{
		Paths: openapi3.NewPaths(),
	}

	spec.Paths.Set("/users", &openapi3.PathItem{
		Get:  &openapi3.Operation{OperationID: "listUsers", Summary: "List users"},
		Post: &openapi3.Operation{OperationID: "createUser", Summary: "Create user"},
	})
	spec.Paths.Set("/users/{id}", &openapi3.PathItem{
		Get:    &openapi3.Operation{OperationID: "getUser", Summary: "Get user"},
		Put:    &openapi3.Operation{OperationID: "updateUser", Summary: "Update user"},
		Delete: &openapi3.Operation{OperationID: "deleteUser", Summary: "Delete user"},
	})
	spec.Paths.Set("/users/{userId}/posts", &openapi3.PathItem{
		Get:  &openapi3.Operation{OperationID: "listUserPosts", Summary: "List user posts"},
		Post: &openapi3.Operation{OperationID: "createPost", Summary: "Create post"},
	})
	spec.Paths.Set("/orders", &openapi3.PathItem{
		Get: &openapi3.Operation{OperationID: "listOrders", Summary: "List orders"},
	})

	m := NewMapper()
	tree := m.BuildCommandTree(spec)

	// Verify root resources
	if len(tree.RootResources) != 3 {
		t.Errorf("expected 3 root resources, got %d", len(tree.RootResources))
	}

	users, ok := tree.RootResources["users"]
	if !ok {
		t.Fatal("expected 'users' resource")
	}

	_, ok = tree.RootResources["orders"]
	if !ok {
		t.Fatal("expected 'orders' resource")
	}

	posts, ok := tree.RootResources["users-posts"]
	if !ok {
		t.Fatal("expected 'users-posts' resource")
	}

	// Verify User operations
	if len(users.Operations) != 5 {
		t.Errorf("expected 5 user operations, got %d", len(users.Operations))
	}

	expectedOps := []string{"list", "create", "get", "update", "delete"}
	for _, op := range expectedOps {
		if _, ok := users.Operations[op]; !ok {
			t.Errorf("expected op '%s' on users", op)
		}
	}

	// Verify posts operations
	if len(posts.Operations) != 2 {
		t.Errorf("expected 2 operations on posts, got %d", len(posts.Operations))
	}

	if _, ok := posts.Operations["list"]; !ok {
		t.Error("expected 'list' op on posts")
	}
	if _, ok := posts.Operations["create"]; !ok {
		t.Error("expected 'create' op on posts")
	}
}

func TestBuildCommandTreeConflictResolution(t *testing.T) {
	spec := &openapi3.T{
		Paths: openapi3.NewPaths(),
	}

	spec.Paths.Set("/users", &openapi3.PathItem{
		Get: &openapi3.Operation{OperationID: "listUsers"},
	})
	// Force conflict by mapping another path to same resource using extension
	spec.Paths.Set("/admins", &openapi3.PathItem{
		Get: &openapi3.Operation{
			OperationID: "listAdmins",
			Extensions: map[string]any{
				"x-cli-resource": "users",
			},
		},
	})

	m := NewMapper()
	tree := m.BuildCommandTree(spec)

	users := tree.RootResources["users"]

	if _, ok := users.Operations["list"]; !ok {
		t.Error("expected 'list' op")
	}

	// The second list should be qualified
	// Ideally it becomes 'list-admins' based on operationID strategy, or 'list-list-admins'
	found := false
	for k := range users.Operations {
		if k != "list" && k != "get" {
			found = true
			t.Logf("Found qualified verb: %s", k)
		}
	}
	if !found {
		t.Error("expected qualified verb for conflict")
	}
}
