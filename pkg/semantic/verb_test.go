package semantic

import (
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
)

func TestVerbMapperDefaultMapping(t *testing.T) {
	mapper := NewVerbMapper()

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
		{"HEAD", "/users/{id}", "check"},
		{"OPTIONS", "/users", "options"},
	}

	for _, tt := range tests {
		t.Run(tt.method+" "+tt.path, func(t *testing.T) {
			result := mapper.MapVerb(tt.method, tt.path, nil)
			if result.Verb != tt.expected {
				t.Errorf("MapVerb(%s, %s) = %q, expected %q", tt.method, tt.path, result.Verb, tt.expected)
			}
		})
	}
}

func TestVerbMapperExtensionOverride(t *testing.T) {
	mapper := NewVerbMapper()

	op := &openapi3.Operation{
		Extensions: map[string]interface{}{
			"x-cli-verb": "custom-verb",
		},
	}

	result := mapper.MapVerb("POST", "/users", op)

	if result.Verb != "custom-verb" {
		t.Errorf("expected 'custom-verb', got '%s'", result.Verb)
	}

	if result.Source != VerbSourceExtension {
		t.Errorf("expected source %s, got %s", VerbSourceExtension, result.Source)
	}
}

func TestVerbMapperPathPattern(t *testing.T) {
	mapper := NewVerbMapper()

	tests := []struct {
		method   string
		path     string
		expected string
		isAction bool
	}{
		{"POST", "/users/{id}/activate", "activate", true},
		{"POST", "/users/{id}/deactivate", "deactivate", true},
		{"POST", "/orders/{id}/cancel", "cancel", true},
		{"POST", "/posts/{id}/publish", "publish", true},
		{"GET", "/files/{id}/download", "download", true},
		{"POST", "/files/upload", "upload", true},
		{"POST", "/data/import", "import", true},
		{"POST", "/users/search", "search", true},
	}

	for _, tt := range tests {
		t.Run(tt.method+" "+tt.path, func(t *testing.T) {
			result := mapper.MapVerb(tt.method, tt.path, nil)
			if result.Verb != tt.expected {
				t.Errorf("MapVerb(%s, %s) = %q, expected %q", tt.method, tt.path, result.Verb, tt.expected)
			}
			if result.IsAction != tt.isAction {
				t.Errorf("MapVerb(%s, %s).IsAction = %v, expected %v", tt.method, tt.path, result.IsAction, tt.isAction)
			}
		})
	}
}

func TestVerbMapperInferFromOperationID(t *testing.T) {
	mapper := NewVerbMapper()

	tests := []struct {
		operationID string
		expected    string
	}{
		{"listUsers", "list"},
		{"getUser", "get"},
		{"createOrder", "create"},
		{"updateProduct", "update"},
		{"deleteItem", "delete"},
		{"searchProducts", "search"},
		{"findByEmail", "find"},
	}

	for _, tt := range tests {
		t.Run(tt.operationID, func(t *testing.T) {
			op := &openapi3.Operation{OperationID: tt.operationID}
			result := mapper.MapVerb("GET", "/test", op)
			if result.Verb != tt.expected {
				t.Errorf("MapVerb for operationID %q = %q, expected %q", tt.operationID, result.Verb, tt.expected)
			}
		})
	}
}

func TestVerbConflictResolver(t *testing.T) {
	resolver := NewVerbConflictResolver(StrategyQualify)

	t.Run("no conflict", func(t *testing.T) {
		mapping := &VerbMapping{
			Verb:        "list",
			OperationID: "listUsers",
			Path:        "/users",
		}
		result := resolver.Resolve(mapping, []string{})
		if result != "list" {
			t.Errorf("expected 'list', got '%s'", result)
		}
	})

	t.Run("conflict with operationId", func(t *testing.T) {
		mapping := &VerbMapping{
			Verb:        "list",
			OperationID: "listActiveUsers",
			Path:        "/users/active",
		}
		result := resolver.Resolve(mapping, []string{"list"})
		if result != "list-list-active-users" {
			t.Errorf("expected 'list-list-active-users', got '%s'", result)
		}
	})

	t.Run("conflict without operationId", func(t *testing.T) {
		mapping := &VerbMapping{
			Verb: "list",
			Path: "/users/active",
		}
		result := resolver.Resolve(mapping, []string{"list"})
		if result != "list-active" {
			t.Errorf("expected 'list-active', got '%s'", result)
		}
	})
}

func TestVerbSet(t *testing.T) {
	set := NewVerbSet(nil)

	// Add first verb
	mapping1 := &VerbMapping{Verb: "list", Path: "/users"}
	verb1 := set.Add(mapping1)
	if verb1 != "list" {
		t.Errorf("expected 'list', got '%s'", verb1)
	}

	// Add same verb - should be qualified
	mapping2 := &VerbMapping{
		Verb:        "list",
		Path:        "/orders",
		OperationID: "listOrders",
	}
	verb2 := set.Add(mapping2)
	if verb2 == "list" {
		t.Error("expected qualified verb, got 'list'")
	}

	// Verify count
	if set.Count() != 2 {
		t.Errorf("expected 2 verbs, got %d", set.Count())
	}
}

func TestVerbSetAllVerbs(t *testing.T) {
	set := NewVerbSet(nil)

	set.Add(&VerbMapping{Verb: "create", Path: "/users"})
	set.Add(&VerbMapping{Verb: "list", Path: "/users"})
	set.Add(&VerbMapping{Verb: "get", Path: "/users/{id}"})

	verbs := set.AllVerbs()

	if len(verbs) != 3 {
		t.Fatalf("expected 3 verbs, got %d", len(verbs))
	}

	// Should be sorted alphabetically
	if verbs[0] != "create" || verbs[1] != "get" || verbs[2] != "list" {
		t.Errorf("expected sorted verbs, got %v", verbs)
	}
}

func TestToKebabCase(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"listUsers", "list-users"},
		{"getUserProfile", "get-user-profile"},
		{"createOrder", "create-order"},
		{"DeleteItem", "delete-item"},
		{"simple", "simple"},
		{"", ""},
		{"list_users", "list-users"},
		{"XMLParser", "x-m-l-parser"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := toKebabCase(tt.input)
			if result != tt.expected {
				t.Errorf("toKebabCase(%q) = %q, expected %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestIsStandardVerb(t *testing.T) {
	standard := []string{"list", "get", "create", "update", "apply", "delete"}
	notStandard := []string{"activate", "search", "download", "custom"}

	for _, verb := range standard {
		if !IsStandardVerb(verb) {
			t.Errorf("expected '%s' to be standard", verb)
		}
	}

	for _, verb := range notStandard {
		if IsStandardVerb(verb) {
			t.Errorf("expected '%s' to not be standard", verb)
		}
	}
}

func TestIsActionVerb(t *testing.T) {
	actions := []string{"activate", "deactivate", "start", "stop", "search", "download", "upload"}
	notActions := []string{"list", "get", "create", "update", "delete", "custom"}

	for _, verb := range actions {
		if !IsActionVerb(verb) {
			t.Errorf("expected '%s' to be action", verb)
		}
	}

	for _, verb := range notActions {
		if IsActionVerb(verb) {
			t.Errorf("expected '%s' to not be action", verb)
		}
	}
}

func TestVerbDescription(t *testing.T) {
	tests := []struct {
		verb     string
		expected string
	}{
		{"list", "List all resources"},
		{"get", "Get a single resource"},
		{"create", "Create a new resource"},
		{"delete", "Delete a resource"},
		{"custom", "Perform custom operation"},
	}

	for _, tt := range tests {
		t.Run(tt.verb, func(t *testing.T) {
			result := VerbDescription(tt.verb)
			if result != tt.expected {
				t.Errorf("VerbDescription(%q) = %q, expected %q", tt.verb, result, tt.expected)
			}
		})
	}
}

func TestVerbMappingSource(t *testing.T) {
	mapper := NewVerbMapper()

	t.Run("default source", func(t *testing.T) {
		result := mapper.MapVerb("GET", "/users", nil)
		if result.Source != VerbSourceDefault {
			t.Errorf("expected source %s, got %s", VerbSourceDefault, result.Source)
		}
	})

	t.Run("extension source", func(t *testing.T) {
		op := &openapi3.Operation{
			Extensions: map[string]interface{}{"x-cli-verb": "custom"},
		}
		result := mapper.MapVerb("GET", "/users", op)
		if result.Source != VerbSourceExtension {
			t.Errorf("expected source %s, got %s", VerbSourceExtension, result.Source)
		}
	})

	t.Run("pattern source", func(t *testing.T) {
		result := mapper.MapVerb("POST", "/users/{id}/activate", nil)
		if result.Source != VerbSourcePattern {
			t.Errorf("expected source %s, got %s", VerbSourcePattern, result.Source)
		}
	})
}

func TestConflictStrategies(t *testing.T) {
	t.Run("StrategyOperationID", func(t *testing.T) {
		resolver := NewVerbConflictResolver(StrategyOperationID)
		mapping := &VerbMapping{
			Verb:        "list",
			OperationID: "listActiveUsers",
			Path:        "/users/active",
		}
		result := resolver.Resolve(mapping, []string{"list"})
		if result != "list-active-users" {
			t.Errorf("expected 'list-active-users', got '%s'", result)
		}
	})

	t.Run("StrategyPath", func(t *testing.T) {
		resolver := NewVerbConflictResolver(StrategyPath)
		mapping := &VerbMapping{
			Verb: "list",
			Path: "/users/active",
		}
		result := resolver.Resolve(mapping, []string{"list"})
		if result != "list-active" {
			t.Errorf("expected 'list-active', got '%s'", result)
		}
	})
}

func TestGetListVerbBehavior(t *testing.T) {
	mapper := NewVerbMapper()

	// Collection endpoint should return "list"
	result := mapper.MapVerb("GET", "/api/v1/users", nil)
	if result.Verb != "list" {
		t.Errorf("GET collection should be 'list', got '%s'", result.Verb)
	}

	// Item endpoint should return "get"
	result = mapper.MapVerb("GET", "/api/v1/users/{userId}", nil)
	if result.Verb != "get" {
		t.Errorf("GET item should be 'get', got '%s'", result.Verb)
	}

	// Nested collection should still return "list"
	result = mapper.MapVerb("GET", "/api/v1/users/{userId}/posts", nil)
	if result.Verb != "list" {
		t.Errorf("GET nested collection should be 'list', got '%s'", result.Verb)
	}

	// Nested item should return "get"
	result = mapper.MapVerb("GET", "/api/v1/users/{userId}/posts/{postId}", nil)
	if result.Verb != "get" {
		t.Errorf("GET nested item should be 'get', got '%s'", result.Verb)
	}
}

func TestVerbMappingFields(t *testing.T) {
	mapper := NewVerbMapper()

	op := &openapi3.Operation{
		OperationID: "listUsers",
	}

	result := mapper.MapVerb("GET", "/users", op)

	if result.Method != "GET" {
		t.Errorf("expected Method 'GET', got '%s'", result.Method)
	}

	if result.Path != "/users" {
		t.Errorf("expected Path '/users', got '%s'", result.Path)
	}

	if result.OperationID != "listUsers" {
		t.Errorf("expected OperationID 'listUsers', got '%s'", result.OperationID)
	}
}
