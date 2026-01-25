package mcp

import (
	"fmt"
	"strings"
	"sync"

	"github.com/vulcand/predicate"
)

// toolPredicate is a function that takes a ToolMetadata and returns a boolean.
// This is our custom predicate type that operates on tool metadata.
type toolPredicate func(ToolMetadata) bool

// PredicateSearchEngine implements ToolSearchEngine using vulcand-predicate expressions.
// It provides powerful filtering with a domain-specific expression language:
// - Logical operators: && (and), || (or), ! (not)
// - Field matchers: MethodIs, PathContains, NameStartsWith, HasTag, etc.
type PredicateSearchEngine struct {
	tools []ToolMetadata
	mu    sync.RWMutex
}

// NewPredicateSearchEngine creates a new predicate-based search engine.
func NewPredicateSearchEngine() (*PredicateSearchEngine, error) {
	return &PredicateSearchEngine{
		tools: make([]ToolMetadata, 0),
	}, nil
}

// createMatcher creates a predicate matcher function for a given expression.
// The predicate language supports:
// - MethodIs("GET"): exact match on HTTP method
// - PathContains("/pets"): substring match on path
// - PathStartsWith("/api"): prefix match on path
// - PathEndsWith("/list"): suffix match on path
// - NameContains("user"): substring match on tool name
// - NameStartsWith("list"): prefix match on tool name
// - DescriptionContains("create"): substring match on description
// - HasTag("admin"): check if tool has a specific tag
// - IDIs("GET_/pets"): exact match on tool ID
// - Logical operators: && (and), || (or), ! (not)
func (e *PredicateSearchEngine) createMatcher(query string) (func(ToolMetadata) bool, error) {
	parser, err := predicate.NewParser(predicate.Def{
		Functions: e.createPredicateFunctions(),
		Operators: e.createPredicateOperators(),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create parser: %w", err)
	}

	pred, err := parser.Parse(query)
	if err != nil {
		return nil, fmt.Errorf("invalid predicate expression: %w", err)
	}

	fn, ok := pred.(toolPredicate)
	if !ok {
		return nil, fmt.Errorf("predicate must evaluate to boolean, got %T", pred)
	}

	return fn, nil
}

// createPredicateFunctions creates all predicate function definitions.
func (e *PredicateSearchEngine) createPredicateFunctions() map[string]any {
	return map[string]any{
		"MethodIs":            e.createMethodIsPredicate(),
		"PathContains":        e.createPathContainsPredicate(),
		"PathStartsWith":      e.createPathStartsWithPredicate(),
		"PathEndsWith":        e.createPathEndsWithPredicate(),
		"PathIs":              e.createPathIsPredicate(),
		"NameContains":        e.createNameContainsPredicate(),
		"NameStartsWith":      e.createNameStartsWithPredicate(),
		"NameEndsWith":        e.createNameEndsWithPredicate(),
		"NameIs":              e.createNameIsPredicate(),
		"DescriptionContains": e.createDescriptionContainsPredicate(),
		"HasTag":              e.createHasTagPredicate(),
	}
}

// createPredicateOperators creates all logical operator definitions.
func (e *PredicateSearchEngine) createPredicateOperators() predicate.Operators {
	return predicate.Operators{
		AND: e.createAndOperator(),
		OR:  e.createOrOperator(),
		NOT: e.createNotOperator(),
	}
}

// Method predicates
func (e *PredicateSearchEngine) createMethodIsPredicate() func(string) toolPredicate {
	return func(method string) toolPredicate {
		return func(t ToolMetadata) bool {
			return strings.EqualFold(t.Method, method)
		}
	}
}

// Path predicates
func (e *PredicateSearchEngine) createPathContainsPredicate() func(string) toolPredicate {
	return func(substr string) toolPredicate {
		return func(t ToolMetadata) bool {
			return strings.Contains(strings.ToLower(t.Path), strings.ToLower(substr))
		}
	}
}

func (e *PredicateSearchEngine) createPathStartsWithPredicate() func(string) toolPredicate {
	return func(prefix string) toolPredicate {
		return func(t ToolMetadata) bool {
			return strings.HasPrefix(strings.ToLower(t.Path), strings.ToLower(prefix))
		}
	}
}

func (e *PredicateSearchEngine) createPathEndsWithPredicate() func(string) toolPredicate {
	return func(suffix string) toolPredicate {
		return func(t ToolMetadata) bool {
			return strings.HasSuffix(strings.ToLower(t.Path), strings.ToLower(suffix))
		}
	}
}

func (e *PredicateSearchEngine) createPathIsPredicate() func(string) toolPredicate {
	return func(path string) toolPredicate {
		return func(t ToolMetadata) bool {
			return strings.EqualFold(t.Path, path)
		}
	}
}

// Name predicates
func (e *PredicateSearchEngine) createNameContainsPredicate() func(string) toolPredicate {
	return func(substr string) toolPredicate {
		return func(t ToolMetadata) bool {
			return strings.Contains(strings.ToLower(t.Name), strings.ToLower(substr))
		}
	}
}

func (e *PredicateSearchEngine) createNameStartsWithPredicate() func(string) toolPredicate {
	return func(prefix string) toolPredicate {
		return func(t ToolMetadata) bool {
			return strings.HasPrefix(strings.ToLower(t.Name), strings.ToLower(prefix))
		}
	}
}

func (e *PredicateSearchEngine) createNameEndsWithPredicate() func(string) toolPredicate {
	return func(suffix string) toolPredicate {
		return func(t ToolMetadata) bool {
			return strings.HasSuffix(strings.ToLower(t.Name), strings.ToLower(suffix))
		}
	}
}

func (e *PredicateSearchEngine) createNameIsPredicate() func(string) toolPredicate {
	return func(name string) toolPredicate {
		return func(t ToolMetadata) bool {
			return strings.EqualFold(t.Name, name)
		}
	}
}

// Description predicates
func (e *PredicateSearchEngine) createDescriptionContainsPredicate() func(string) toolPredicate {
	return func(substr string) toolPredicate {
		return func(t ToolMetadata) bool {
			return strings.Contains(strings.ToLower(t.Description), strings.ToLower(substr))
		}
	}
}

// Tag predicates
func (e *PredicateSearchEngine) createHasTagPredicate() func(string) toolPredicate {
	return func(tag string) toolPredicate {
		return func(t ToolMetadata) bool {
			for _, tt := range t.Tags {
				if strings.EqualFold(tt, tag) {
					return true
				}
			}
			return false
		}
	}
}

// Logical operators
func (e *PredicateSearchEngine) createAndOperator() func(toolPredicate, toolPredicate) toolPredicate {
	return func(a, b toolPredicate) toolPredicate {
		return func(t ToolMetadata) bool {
			return a(t) && b(t)
		}
	}
}

func (e *PredicateSearchEngine) createOrOperator() func(toolPredicate, toolPredicate) toolPredicate {
	return func(a, b toolPredicate) toolPredicate {
		return func(t ToolMetadata) bool {
			return a(t) || b(t)
		}
	}
}

func (e *PredicateSearchEngine) createNotOperator() func(toolPredicate) toolPredicate {
	return func(a toolPredicate) toolPredicate {
		return func(t ToolMetadata) bool {
			return !a(t)
		}
	}
}

// Search finds tools matching the predicate expression.
// Expression syntax:
// - MethodIs("GET"): exact match on HTTP method
// - PathContains("/pets"): substring match on path
// - PathStartsWith("/api"): prefix match on path
// - NameContains("user"): substring match on tool name
// - NameStartsWith("list"): prefix match on tool name
// - DescriptionContains("create"): substring match on description
// - HasTag("admin"): check if tool has a specific tag
// - Logical operators: && (and), || (or), ! (not)
//
// Examples:
// - MethodIs("GET")
// - MethodIs("POST") && PathContains("/users")
// - HasTag("pet") || HasTag("store")
// - DescriptionContains("create") && !HasTag("admin")
func (e *PredicateSearchEngine) Search(query string) ([]ToolMetadata, error) {
	e.mu.RLock()
	defer e.mu.RUnlock()

	if query == "" {
		return e.getAllTools(), nil
	}

	matcher, err := e.createMatcher(query)
	if err != nil {
		return nil, err
	}

	var results []ToolMetadata
	for _, tool := range e.tools {
		if matcher(tool) {
			results = append(results, tool)
		}
	}

	return results, nil
}

// getAllTools returns all indexed tools.
func (e *PredicateSearchEngine) getAllTools() []ToolMetadata {
	return e.tools
}

// GetDescription returns a description of the predicate search engine.
func (e *PredicateSearchEngine) GetDescription() string {
	return `Use predicate expressions to find tools precisely:
- Method: MethodIs("GET"), MethodIs("POST"), MethodIs("PUT"), MethodIs("DELETE")
- Path: PathContains("/users"), PathStartsWith("/api"), PathEndsWith("/list"), PathIs("/exact")
- Name: NameContains("user"), NameStartsWith("list"), NameEndsWith("Pet"), NameIs("exact")
- Description: DescriptionContains("create")
- Tags: HasTag("tagname")
- Logical operators: && (and), || (or), ! (not)
- Parentheses for grouping: (expr1 || expr2) && expr3`
}

// GetQueryExample returns an example query.
func (e *PredicateSearchEngine) GetQueryExample() string {
	return `MethodIs("GET") && PathContains("/users")`
}

// GetBestPractices returns usage guidance for the predicate engine.
func (e *PredicateSearchEngine) GetBestPractices() string {
	return `- Use MethodIs("GET") to find read operations, MethodIs("POST") for create operations
- Use PathContains("/resource") to find APIs related to specific resources
- Use HasTag("tagname") to filter by OpenAPI tags
- Combine expressions with && (and) or || (or) for precision
- Always prefer specific queries over listing all tools`
}

// GetExamples returns multiple example queries.
func (e *PredicateSearchEngine) GetExamples() []string {
	return []string{
		`MethodIs("GET") && PathContains("/users")`,
		`HasTag("pets") || HasTag("store")`,
		`MethodIs("POST") && !PathContains("/admin")`,
	}
}

// Index adds or updates tools in the search index.
func (e *PredicateSearchEngine) Index(tools []ToolMetadata) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	e.tools = make([]ToolMetadata, len(tools))
	copy(e.tools, tools)

	return nil
}

// Close releases any resources (no-op for predicate engine).
func (e *PredicateSearchEngine) Close() error {
	return nil
}
