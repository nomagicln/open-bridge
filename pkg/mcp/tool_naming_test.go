package mcp

import (
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/stretchr/testify/assert"
)

func TestGenerateToolName(t *testing.T) {
	tests := []struct {
		name        string
		method      string
		path        string
		operationID string
		expected    string
	}{
		{
			name:        "clean operationId without redundancy",
			method:      "GET",
			path:        "/api/users",
			operationID: "listUsers",
			expected:    "listUsers",
		},
		{
			name:        "FastAPI pattern - list todos",
			method:      "GET",
			path:        "/todos",
			operationID: "list_todos_todos_get",
			expected:    "todos_list",
		},
		{
			name:        "FastAPI pattern - create todo",
			method:      "POST",
			path:        "/todos",
			operationID: "create_todo_todos_post",
			expected:    "todos_create",
		},
		{
			name:        "FastAPI pattern - get todo with id",
			method:      "GET",
			path:        "/todos/{todo_id}",
			operationID: "get_todo_todos__todo_id__get",
			expected:    "todos_get",
		},
		{
			name:        "FastAPI pattern - update todo",
			method:      "PUT",
			path:        "/todos/{todo_id}",
			operationID: "update_todo_todos__todo_id__put",
			expected:    "todos_update",
		},
		{
			name:        "FastAPI pattern - delete todo",
			method:      "DELETE",
			path:        "/todos/{todo_id}",
			operationID: "delete_todo_todos__todo_id__delete",
			expected:    "todos_delete",
		},
		{
			name:        "empty operationId - generate from path",
			method:      "GET",
			path:        "/api/v1/users",
			operationID: "",
			expected:    "users_list",
		},
		{
			name:        "nested resource",
			method:      "GET",
			path:        "/users/{userId}/posts",
			operationID: "list_posts_users__userid__posts_get",
			expected:    "users_posts", // Nested resource: users_posts is more clear than posts_list
		},
		{
			name:        "stats endpoint",
			method:      "GET",
			path:        "/todos/stats",
			operationID: "get_stats_todos_stats_get",
			expected:    "todos_stats", // Subresource: todos_stats is clearer than stats_get
		},
		{
			name:        "batch operation",
			method:      "POST",
			path:        "/todos/batch",
			operationID: "batch_create_todos_todos_batch_post",
			expected:    "todos_batch_create",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			op := &openapi3.Operation{
				OperationID: tt.operationID,
			}
			result := GenerateToolName(tt.method, tt.path, op)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestHasRedundancy(t *testing.T) {
	tests := []struct {
		name        string
		operationID string
		expected    bool
	}{
		{
			name:        "FastAPI pattern with redundancy",
			operationID: "list_todos_todos_get",
			expected:    true,
		},
		{
			name:        "clean operationId",
			operationID: "listUsers",
			expected:    false,
		},
		{
			name:        "camelCase without redundancy",
			operationID: "getUserById",
			expected:    false,
		},
		{
			name:        "snake_case without redundancy",
			operationID: "list_users",
			expected:    false,
		},
		{
			name:        "repeated resource name",
			operationID: "create_todo_todos_post",
			expected:    true,
		},
		{
			name:        "with path parameter",
			operationID: "get_todo_todos__todo_id__get",
			expected:    true,
		},
		{
			name:        "empty string",
			operationID: "",
			expected:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := hasRedundancy(tt.operationID)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestIsFastAPIPattern(t *testing.T) {
	tests := []struct {
		name        string
		operationID string
		expected    bool
	}{
		{
			name:        "FastAPI GET pattern",
			operationID: "list_todos_todos_get",
			expected:    true,
		},
		{
			name:        "FastAPI POST pattern",
			operationID: "create_todo_todos_post",
			expected:    true,
		},
		{
			name:        "FastAPI PUT pattern",
			operationID: "update_todo_todos__id__put",
			expected:    true,
		},
		{
			name:        "FastAPI DELETE pattern",
			operationID: "delete_todo_todos__id__delete",
			expected:    true,
		},
		{
			name:        "FastAPI PATCH pattern",
			operationID: "patch_todo_todos__id__patch",
			expected:    true,
		},
		{
			name:        "camelCase operationId",
			operationID: "listUsers",
			expected:    false,
		},
		{
			name:        "simple snake_case",
			operationID: "list_users",
			expected:    false,
		},
		{
			name:        "custom naming",
			operationID: "getUserProfile",
			expected:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isFastAPIPattern(tt.operationID)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestIsHTTPVerbOrMethod(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected bool
	}{
		{name: "get", input: "get", expected: true},
		{name: "POST uppercase", input: "POST", expected: true},
		{name: "patch", input: "patch", expected: true},
		{name: "id", input: "id", expected: true},
		{name: "users", input: "users", expected: false},
		{name: "custom", input: "custom", expected: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isHTTPVerbOrMethod(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestNormalizeOperationID(t *testing.T) {
	tests := []struct {
		name        string
		operationID string
		expected    string
	}{
		{
			name:        "FastAPI pattern",
			operationID: "list_todos_todos_get",
			expected:    "list_todos",
		},
		{
			name:        "clean operationId",
			operationID: "listUsers",
			expected:    "listUsers",
		},
		{
			name:        "empty string",
			operationID: "",
			expected:    "",
		},
		{
			name:        "no redundancy",
			operationID: "list_users",
			expected:    "list_users",
		},
		{
			name:        "with path parameter",
			operationID: "get_todo_todos__id__get",
			expected:    "todo_todos", // Note: GenerateToolName is the primary method, this is just fallback
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := NormalizeOperationID(tt.operationID)
			assert.Equal(t, tt.expected, result)
		})
	}
}
