// Package semantic provides semantic mapping from OpenAPI operations to CLI commands.
package semantic

import (
	"sort"

	"github.com/getkin/kin-openapi/openapi3"
)

// Mapper handles the conversion of OpenAPI operations to semantic CLI commands.
type Mapper struct {
	extractor  *ResourceExtractor
	verbMapper *VerbMapper
}

// NewMapper creates a new semantic mapper.
func NewMapper() *Mapper {
	return &Mapper{
		extractor:  NewResourceExtractor(),
		verbMapper: NewVerbMapper(),
	}
}

// CommandTree represents the hierarchical structure of CLI commands.
type CommandTree struct {
	RootResources map[string]*Resource
}

// Resource represents a resource with its operations.
type Resource struct {
	Name         string
	Description  string
	Operations   map[string]*Operation // verb -> operation
	SubResources map[string]*Resource  // name -> resource
	Parent       *Resource             // pointer to parent

	// Internal helper for conflict resolution
	verbSet *VerbSet
}

// Operation represents a single API operation.
type Operation struct {
	Name        string // The verb (command name)
	Method      string
	Path        string
	OperationID string
	Summary     string
	Description string
	Resource    string
	Aliases     []string
}

// getSortedPaths returns the sorted paths from the spec for deterministic behavior.
func getSortedPaths(spec *openapi3.T) []string {
	paths := make([]string, 0, len(spec.Paths.Map()))
	for path := range spec.Paths.Map() {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	return paths
}

// getPathOperations returns a map of method to operation for the given path item.
func getPathOperations(pathItem *openapi3.PathItem) map[string]*openapi3.Operation {
	return map[string]*openapi3.Operation{
		"GET":    pathItem.Get,
		"POST":   pathItem.Post,
		"PUT":    pathItem.Put,
		"PATCH":  pathItem.Patch,
		"DELETE": pathItem.Delete,
	}
}

// processOperation processes a single operation and adds it to the tree.
func (m *Mapper) processOperation(tree *CommandTree, allResources map[string]*Resource, path, method string, op *openapi3.Operation) {
	extractResult := m.extractor.Extract(path, op)
	verbMapping := m.verbMapper.MapVerb(method, path, op)
	resource := m.getOrCreateResource(tree, allResources, extractResult.Resource, extractResult.ParentResource)
	finalVerb := resource.verbSet.Add(verbMapping)

	resource.Operations[finalVerb] = &Operation{
		Name:        finalVerb,
		Method:      method,
		Path:        path,
		OperationID: op.OperationID,
		Summary:     op.Summary,
		Description: op.Description,
		Resource:    extractResult.Resource,
	}
}

// BuildCommandTree builds a command tree from an OpenAPI specification.
func (m *Mapper) BuildCommandTree(spec *openapi3.T) *CommandTree {
	tree := &CommandTree{RootResources: make(map[string]*Resource)}
	allResources := make(map[string]*Resource)
	methods := []string{"GET", "POST", "PUT", "PATCH", "DELETE"}

	for _, path := range getSortedPaths(spec) {
		pathItem := spec.Paths.Find(path)
		if pathItem == nil {
			continue
		}

		operations := getPathOperations(pathItem)
		for _, method := range methods {
			if op := operations[method]; op != nil {
				m.processOperation(tree, allResources, path, method, op)
			}
		}
	}

	return tree
}

// getOrCreateResource creates a resource with a compound name if it has a parent.
// For example, /store/order becomes "storeorder" as a root resource.
func (m *Mapper) getOrCreateResource(tree *CommandTree, allResources map[string]*Resource, name string, parentName string) *Resource {
	// Build compound resource name
	var resourceName string
	var key string

	if parentName != "" {
		// Create compound name: parentName-name (e.g., "store-order")
		resourceName = parentName + "-" + name
		key = parentName + "." + name
	} else {
		resourceName = name
		key = name
	}

	// Check if resource already exists
	if res, ok := allResources[key]; ok {
		return res
	}

	// Create new resource as a root resource (flattened)
	res := &Resource{
		Name:         resourceName,
		Operations:   make(map[string]*Operation),
		SubResources: make(map[string]*Resource),
		Parent:       nil, // No parent hierarchy - all resources are flat
		verbSet:      NewVerbSet(nil),
	}

	allResources[key] = res
	tree.RootResources[resourceName] = res

	return res
}

// Wrapper methods for backward compatibility with tests

// ExtractResource extracts the resource name from an API path.
func (m *Mapper) ExtractResource(path string, operation *openapi3.Operation) string {
	return m.extractor.Extract(path, operation).Resource
}

// MapVerb maps an HTTP method to a semantic verb.
func (m *Mapper) MapVerb(method, path string, operation *openapi3.Operation) string {
	return m.verbMapper.MapVerb(method, path, operation).Verb
}
