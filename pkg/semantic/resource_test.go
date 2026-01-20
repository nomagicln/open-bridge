package semantic

import (
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
)

func TestResourceExtractorExtract(t *testing.T) {
	extractor := NewResourceExtractor()

	tests := []struct {
		name             string
		path             string
		expectedResource string
	}{
		{"simple", "/users", "users"},
		{"with id", "/users/{id}", "users"},
		{"api prefix", "/api/users", "users"},
		{"versioned", "/api/v1/users", "users"},
		{"v2 versioned", "/v2/orders", "orders"},
		{"nested", "/users/{userId}/posts", "posts"},
		{"deeply nested", "/organizations/{orgId}/teams/{teamId}/members", "members"},
		{"with action", "/users/{id}/activate", "activate"},
		{"rest prefix", "/rest/api/products", "products"},
		{"internal prefix", "/internal/admin/settings", "settings"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractor.Extract(tt.path, nil)
			if result.Resource != tt.expectedResource {
				t.Errorf("Extract(%q) = %q, expected %q", tt.path, result.Resource, tt.expectedResource)
			}
		})
	}
}

func TestResourceExtractorWithOverride(t *testing.T) {
	extractor := NewResourceExtractor()

	op := &openapi3.Operation{
		Extensions: map[string]interface{}{
			"x-cli-resource": "custom-resource",
		},
	}

	result := extractor.Extract("/users/{id}", op)

	if result.Resource != "custom-resource" {
		t.Errorf("expected 'custom-resource', got '%s'", result.Resource)
	}
}

func TestResourceExtractorNestedResource(t *testing.T) {
	extractor := NewResourceExtractor()

	result := extractor.Extract("/users/{userId}/posts/{postId}/comments", nil)

	if result.Resource != "comments" {
		t.Errorf("expected resource 'comments', got '%s'", result.Resource)
	}

	if result.ParentResource != "posts" {
		t.Errorf("expected parent resource 'posts', got '%s'", result.ParentResource)
	}

	if !result.IsNested {
		t.Error("expected IsNested to be true")
	}
}

func TestResourceExtractorPathParams(t *testing.T) {
	extractor := NewResourceExtractor()

	result := extractor.Extract("/users/{userId}/posts/{postId}", nil)

	if len(result.PathParams) != 2 {
		t.Fatalf("expected 2 path params, got %d", len(result.PathParams))
	}

	if result.PathParams[0] != "userId" {
		t.Errorf("expected first param 'userId', got '%s'", result.PathParams[0])
	}

	if result.PathParams[1] != "postId" {
		t.Errorf("expected second param 'postId', got '%s'", result.PathParams[1])
	}
}

func TestExtractResourceName(t *testing.T) {
	tests := []struct {
		path     string
		expected string
	}{
		{"/users", "users"},
		{"/api/v1/orders", "orders"},
		{"/products/{id}/reviews", "reviews"},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			result := ExtractResourceName(tt.path, nil)
			if result != tt.expected {
				t.Errorf("ExtractResourceName(%q) = %q, expected %q", tt.path, result, tt.expected)
			}
		})
	}
}

func TestNormalizeName(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"users", "users"},
		{"Users", "users"},
		{"user-profiles", "userprofiles"},
		{"user_profiles", "userprofiles"},
		{"UserProfiles", "userprofiles"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := NormalizeName(tt.input)
			if result != tt.expected {
				t.Errorf("NormalizeName(%q) = %q, expected %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestSingularize(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"users", "user"},
		{"categories", "category"},
		{"people", "person"},
		{"children", "child"},
		{"boxes", "box"},
		{"buses", "bus"},
		{"dishes", "dish"},
		{"wolves", "wolf"},
		{"leaves", "leaf"},
		{"user", "user"},
		{"", ""},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := Singularize(tt.input)
			if result != tt.expected {
				t.Errorf("Singularize(%q) = %q, expected %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestPluralize(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"user", "users"},
		{"category", "categories"},
		{"person", "people"},
		{"child", "children"},
		{"box", "boxes"},
		{"bus", "buses"},
		{"dish", "dishes"},
		{"wolf", "wolves"},
		{"leaf", "leaves"},
		{"", ""},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := Pluralize(tt.input)
			if result != tt.expected {
				t.Errorf("Pluralize(%q) = %q, expected %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestAnalyzePath(t *testing.T) {
	t.Run("collection path", func(t *testing.T) {
		analysis := AnalyzePath("/api/v1/users")

		if !analysis.IsCollection {
			t.Error("expected IsCollection to be true")
		}

		if analysis.IsSingleItem {
			t.Error("expected IsSingleItem to be false")
		}

		if analysis.IsNested {
			t.Error("expected IsNested to be false")
		}
	})

	t.Run("single item path", func(t *testing.T) {
		analysis := AnalyzePath("/api/v1/users/{id}")

		if analysis.IsCollection {
			t.Error("expected IsCollection to be false")
		}

		if !analysis.IsSingleItem {
			t.Error("expected IsSingleItem to be true")
		}

		if len(analysis.ParameterNames) != 1 || analysis.ParameterNames[0] != "id" {
			t.Errorf("expected parameter 'id', got %v", analysis.ParameterNames)
		}
	})

	t.Run("nested path", func(t *testing.T) {
		analysis := AnalyzePath("/organizations/{orgId}/teams/{teamId}/members")

		if !analysis.IsNested {
			t.Error("expected IsNested to be true")
		}

		if analysis.NestingLevel != 2 {
			t.Errorf("expected NestingLevel 2, got %d", analysis.NestingLevel)
		}

		if len(analysis.ResourceSegments) != 3 {
			t.Errorf("expected 3 resource segments, got %d", len(analysis.ResourceSegments))
		}

		if len(analysis.ParameterNames) != 2 {
			t.Errorf("expected 2 parameter names, got %d", len(analysis.ParameterNames))
		}
	})
}

func TestInferResourceFromOperationID(t *testing.T) {
	tests := []struct {
		operationID string
		expected    string
	}{
		{"listUsers", "users"},
		{"getUser", "user"},
		{"createOrder", "order"},
		{"deleteProduct", "product"},
		{"updateUserProfile", "userprofile"},
		{"", ""},
		{"users", "users"},
	}

	for _, tt := range tests {
		t.Run(tt.operationID, func(t *testing.T) {
			result := InferResourceFromOperationID(tt.operationID)
			if result != tt.expected {
				t.Errorf("InferResourceFromOperationID(%q) = %q, expected %q", tt.operationID, result, tt.expected)
			}
		})
	}
}

func TestBuildResourcePath(t *testing.T) {
	tests := []struct {
		resources []string
		expected  string
	}{
		{[]string{"users"}, "users"},
		{[]string{"organizations", "teams"}, "organizations teams"},
		{[]string{"Users", "Posts", "Comments"}, "users posts comments"},
		{[]string{"", "users", ""}, "users"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			result := BuildResourcePath(tt.resources...)
			if result != tt.expected {
				t.Errorf("BuildResourcePath(%v) = %q, expected %q", tt.resources, result, tt.expected)
			}
		})
	}
}

func TestIsVerionSegment(t *testing.T) {
	extractor := NewResourceExtractor()

	tests := []struct {
		segment   string
		isVersion bool
	}{
		{"v1", true},
		{"v2", true},
		{"v10", true},
		{"v1.0", true},
		{"v2.1", true},
		{"V1", true},
		{"users", false},
		{"api", false},
		{"version", false},
	}

	for _, tt := range tests {
		t.Run(tt.segment, func(t *testing.T) {
			result := extractor.isVersionSegment(tt.segment)
			if result != tt.isVersion {
				t.Errorf("isVersionSegment(%q) = %v, expected %v", tt.segment, result, tt.isVersion)
			}
		})
	}
}

func TestIgnoredPrefixes(t *testing.T) {
	extractor := NewResourceExtractor()

	ignored := []string{"api", "apis", "rest", "internal", "external", "public", "private", "admin"}
	notIgnored := []string{"users", "products", "orders", "items"}

	for _, seg := range ignored {
		if !extractor.isIgnoredPrefix(seg) {
			t.Errorf("expected '%s' to be ignored", seg)
		}
	}

	for _, seg := range notIgnored {
		if extractor.isIgnoredPrefix(seg) {
			t.Errorf("expected '%s' to not be ignored", seg)
		}
	}
}

func TestComplexPathExtraction(t *testing.T) {
	extractor := NewResourceExtractor()

	// Complex real-world paths
	tests := []struct {
		path             string
		expectedResource string
		expectedNested   bool
	}{
		{"/api/v2/tenants/{tenantId}/environments/{envId}/deployments", "deployments", true},
		{"/management/api/admin/users/{userId}/permissions", "permissions", true},
		{"/public/api/v1/products", "products", false},
		{"/internal/services/{serviceId}/health", "health", true},
		{"/v3/accounts/{accountId}/transactions/{txId}", "transactions", true},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			result := extractor.Extract(tt.path, nil)

			if result.Resource != tt.expectedResource {
				t.Errorf("Extract(%q).Resource = %q, expected %q", tt.path, result.Resource, tt.expectedResource)
			}

			if result.IsNested != tt.expectedNested {
				t.Errorf("Extract(%q).IsNested = %v, expected %v", tt.path, result.IsNested, tt.expectedNested)
			}
		})
	}
}
