// Package semantic provides semantic mapping from OpenAPI operations to CLI commands.
// This file contains HTTP method to verb mapping logic.
package semantic

import (
	"regexp"
	"slices"
	"sort"
	"strings"

	"github.com/getkin/kin-openapi/openapi3"
)

// VerbMapper handles the mapping of HTTP methods to CLI verbs.
type VerbMapper struct {
	// CustomMappings allows overriding default mappings.
	CustomMappings map[string]string

	// PathPatternRules define verb mappings based on path patterns.
	PathPatternRules []PathPatternRule
}

// PathPatternRule defines a verb based on path pattern matching.
type PathPatternRule struct {
	Pattern *regexp.Regexp
	Method  string
	Verb    string
}

// NewVerbMapper creates a new verb mapper with default settings.
func NewVerbMapper() *VerbMapper {
	return &VerbMapper{
		CustomMappings:   make(map[string]string),
		PathPatternRules: defaultPathPatternRules(),
	}
}

// createActionRules creates path pattern rules for action endpoints.
func createActionRules() []PathPatternRule {
	return []PathPatternRule{
		{regexp.MustCompile(`/activate$`), "POST", "activate"},
		{regexp.MustCompile(`/deactivate$`), "POST", "deactivate"},
		{regexp.MustCompile(`/enable$`), "POST", "enable"},
		{regexp.MustCompile(`/disable$`), "POST", "disable"},
		{regexp.MustCompile(`/start$`), "POST", "start"},
		{regexp.MustCompile(`/stop$`), "POST", "stop"},
		{regexp.MustCompile(`/restart$`), "POST", "restart"},
		{regexp.MustCompile(`/cancel$`), "POST", "cancel"},
		{regexp.MustCompile(`/approve$`), "POST", "approve"},
		{regexp.MustCompile(`/reject$`), "POST", "reject"},
		{regexp.MustCompile(`/archive$`), "POST", "archive"},
		{regexp.MustCompile(`/unarchive$`), "POST", "unarchive"},
		{regexp.MustCompile(`/publish$`), "POST", "publish"},
		{regexp.MustCompile(`/unpublish$`), "POST", "unpublish"},
		{regexp.MustCompile(`/lock$`), "POST", "lock"},
		{regexp.MustCompile(`/unlock$`), "POST", "unlock"},
		{regexp.MustCompile(`/sync$`), "POST", "sync"},
		{regexp.MustCompile(`/refresh$`), "POST", "refresh"},
		{regexp.MustCompile(`/validate$`), "POST", "validate"},
		{regexp.MustCompile(`/verify$`), "POST", "verify"},
		{regexp.MustCompile(`/clone$`), "POST", "clone"},
		{regexp.MustCompile(`/copy$`), "POST", "copy"},
		{regexp.MustCompile(`/move$`), "POST", "move"},
		{regexp.MustCompile(`/rename$`), "POST", "rename"},
		{regexp.MustCompile(`/import$`), "POST", "import"},
		{regexp.MustCompile(`/export$`), "POST", "export"},
		{regexp.MustCompile(`/download$`), "GET", "download"},
		{regexp.MustCompile(`/upload$`), "POST", "upload"},
		{regexp.MustCompile(`/send$`), "POST", "send"},
		{regexp.MustCompile(`/resend$`), "POST", "resend"},
		{regexp.MustCompile(`/reset$`), "POST", "reset"},
		{regexp.MustCompile(`/search$`), "POST", "search"},
		{regexp.MustCompile(`/search$`), "GET", "search"},
		{regexp.MustCompile(`/query$`), "POST", "query"},
		{regexp.MustCompile(`/batch$`), "POST", "batch"},
		{regexp.MustCompile(`/bulk$`), "POST", "bulk"},
	}
}

// defaultPathPatternRules returns the default path pattern rules.
func defaultPathPatternRules() []PathPatternRule {
	return createActionRules()
}

// VerbMapping contains the result of verb mapping.
type VerbMapping struct {
	// Verb is the mapped verb.
	Verb string

	// Method is the HTTP method.
	Method string

	// Path is the API path.
	Path string

	// OperationID is the operation ID from the spec.
	OperationID string

	// Source indicates where the verb came from.
	Source VerbSource

	// IsAction indicates if this is an action (not CRUD).
	IsAction bool

	// Qualifier is used when there are conflicts.
	Qualifier string
}

// VerbSource indicates the source of a verb mapping.
type VerbSource string

const (
	// VerbSourceDefault indicates the verb came from default mapping.
	VerbSourceDefault VerbSource = "default"

	// VerbSourceExtension indicates the verb came from x-cli-verb.
	VerbSourceExtension VerbSource = "extension"

	// VerbSourcePattern indicates the verb came from path pattern matching.
	VerbSourcePattern VerbSource = "pattern"

	// VerbSourceOperationID indicates the verb was inferred from operationId.
	VerbSourceOperationID VerbSource = "operationId"
)

// MapVerb maps an HTTP method to a CLI verb.
// checkExtensionVerb checks for x-cli-verb extension.
func checkExtensionVerb(operation *openapi3.Operation) (string, bool) {
	if operation == nil {
		return "", false
	}
	ext, ok := operation.Extensions["x-cli-verb"]
	if !ok {
		return "", false
	}
	verb, ok := ext.(string)
	if !ok {
		return "", false
	}
	return verb, true
}

// checkPathPatternRules checks path pattern rules.
func (m *VerbMapper) checkPathPatternRules(method, path string) (string, bool) {
	for _, rule := range m.PathPatternRules {
		if rule.Method == strings.ToUpper(method) && rule.Pattern.MatchString(path) {
			return rule.Verb, true
		}
	}
	return "", false
}

// checkCustomMapping checks custom mappings.
func (m *VerbMapper) checkCustomMapping(method, path string) (string, bool) {
	key := method + ":" + path
	verb, ok := m.CustomMappings[key]
	if !ok {
		return "", false
	}
	return verb, true
}

// checkOperationID checks operation ID for verb inference.
func (m *VerbMapper) checkOperationID(operation *openapi3.Operation) (string, bool) {
	if operation == nil || operation.OperationID == "" {
		return "", false
	}
	verb := m.inferVerbFromOperationID(operation.OperationID)
	if verb == "" {
		return "", false
	}
	return verb, true
}

func (m *VerbMapper) MapVerb(method, path string, operation *openapi3.Operation) *VerbMapping {
	result := &VerbMapping{
		Method: method,
		Path:   path,
	}

	if operation != nil {
		result.OperationID = operation.OperationID
	}

	// Priority 1: Check for x-cli-verb extension
	if verb, ok := checkExtensionVerb(operation); ok {
		result.Verb = verb
		result.Source = VerbSourceExtension
		return result
	}

	// Priority 2: Check path pattern rules
	if verb, ok := m.checkPathPatternRules(method, path); ok {
		result.Verb = verb
		result.Source = VerbSourcePattern
		result.IsAction = true
		return result
	}

	// Priority 3: Check custom mappings
	if verb, ok := m.checkCustomMapping(method, path); ok {
		result.Verb = verb
		result.Source = VerbSourceDefault
		return result
	}

	// Priority 4: Try to infer from operationId
	if verb, ok := m.checkOperationID(operation); ok {
		result.Verb = verb
		result.Source = VerbSourceOperationID
		return result
	}

	// Priority 5: Default HTTP method mapping
	result.Verb = m.defaultMethodMapping(method, path)
	result.Source = VerbSourceDefault

	return result
}

// defaultMethodMapping returns the default verb for an HTTP method.
func (m *VerbMapper) defaultMethodMapping(method, path string) string {
	// Check if path ends with a parameter (single item operation)
	hasPathParam := regexp.MustCompile(`/\{[^}]+\}$`).MatchString(path)

	switch strings.ToUpper(method) {
	case "GET":
		if hasPathParam {
			return "get"
		}
		return "list"

	case "POST":
		return "create"

	case "PUT":
		return "update"

	case "PATCH":
		return "apply"

	case "DELETE":
		return "delete"

	case "HEAD":
		return "check"

	case "OPTIONS":
		return "options"

	default:
		return strings.ToLower(method)
	}
}

// getVerbMappings returns common verb prefixes in operationIds.
func getVerbMappings() map[string]string {
	return map[string]string{
		"list":     "list",
		"get":      "get",
		"create":   "create",
		"add":      "create",
		"post":     "create",
		"update":   "update",
		"put":      "update",
		"patch":    "apply",
		"modify":   "update",
		"delete":   "delete",
		"remove":   "delete",
		"find":     "find",
		"search":   "search",
		"query":    "query",
		"fetch":    "get",
		"load":     "get",
		"save":     "update",
		"store":    "create",
		"check":    "check",
		"verify":   "verify",
		"validate": "validate",
	}
}

// inferVerbFromOperationID attempts to extract a verb from the operationId.
func (m *VerbMapper) inferVerbFromOperationID(operationID string) string {
	if operationID == "" {
		return ""
	}

	verbMappings := getVerbMappings()
	lower := strings.ToLower(operationID)
	for prefix, verb := range verbMappings {
		if strings.HasPrefix(lower, prefix) {
			return verb
		}
	}

	return ""
}

// VerbConflictResolver handles conflicts when multiple operations map to the same verb.
type VerbConflictResolver struct {
	// Strategy determines how conflicts are resolved.
	Strategy ConflictStrategy
}

// ConflictStrategy determines how verb conflicts are resolved.
type ConflictStrategy string

const (
	// StrategyQualify adds a qualifier to distinguish verbs.
	StrategyQualify ConflictStrategy = "qualify"

	// StrategyOperationID uses the operationId as the verb.
	StrategyOperationID ConflictStrategy = "operationId"

	// StrategyPath uses path segments to qualify.
	StrategyPath ConflictStrategy = "path"
)

// NewVerbConflictResolver creates a new conflict resolver.
func NewVerbConflictResolver(strategy ConflictStrategy) *VerbConflictResolver {
	return &VerbConflictResolver{
		Strategy: strategy,
	}
}

// Resolve resolves a verb conflict by returning a qualified verb.
func (r *VerbConflictResolver) Resolve(mapping *VerbMapping, existingVerbs []string) string {
	if !containsVerb(existingVerbs, mapping.Verb) {
		return mapping.Verb
	}

	switch r.Strategy {
	case StrategyOperationID:
		if mapping.OperationID != "" {
			return toKebabCase(mapping.OperationID)
		}
		return r.qualifyWithPath(mapping)

	case StrategyPath:
		return r.qualifyWithPath(mapping)

	case StrategyQualify:
		fallthrough
	default:
		// Try operationId first, then path
		if mapping.OperationID != "" {
			return mapping.Verb + "-" + toKebabCase(mapping.OperationID)
		}
		return r.qualifyWithPath(mapping)
	}
}

// qualifyWithPath qualifies a verb using path segments.
func (r *VerbConflictResolver) qualifyWithPath(mapping *VerbMapping) string {
	analysis := AnalyzePath(mapping.Path)

	// Use the last resource segment as qualifier
	if len(analysis.ResourceSegments) > 0 {
		lastResource := analysis.ResourceSegments[len(analysis.ResourceSegments)-1]
		return mapping.Verb + "-" + NormalizeName(lastResource)
	}

	// Fallback: append a number
	return mapping.Verb + "-alt"
}

// containsVerb checks if a verb exists in the list.
func containsVerb(verbs []string, verb string) bool {
	return slices.Contains(verbs, verb)
}

// VerbSet manages a set of mapped verbs for conflict detection.
type VerbSet struct {
	verbs    map[string]*VerbMapping
	resolver *VerbConflictResolver
}

// NewVerbSet creates a new verb set.
func NewVerbSet(resolver *VerbConflictResolver) *VerbSet {
	if resolver == nil {
		resolver = NewVerbConflictResolver(StrategyQualify)
	}
	return &VerbSet{
		verbs:    make(map[string]*VerbMapping),
		resolver: resolver,
	}
}

// Add adds a verb mapping, resolving conflicts if necessary.
func (s *VerbSet) Add(mapping *VerbMapping) string {
	originalVerb := mapping.Verb

	// Check for conflict
	existingVerbs := s.AllVerbs()
	if containsVerb(existingVerbs, mapping.Verb) {
		mapping.Verb = s.resolver.Resolve(mapping, existingVerbs)
		mapping.Qualifier = strings.TrimPrefix(mapping.Verb, originalVerb+"-")
	}

	s.verbs[mapping.Verb] = mapping
	return mapping.Verb
}

// Get returns a verb mapping by verb name.
func (s *VerbSet) Get(verb string) *VerbMapping {
	return s.verbs[verb]
}

// AllVerbs returns all registered verbs.
func (s *VerbSet) AllVerbs() []string {
	var verbs []string
	for verb := range s.verbs {
		verbs = append(verbs, verb)
	}
	sort.Strings(verbs)
	return verbs
}

// AllMappings returns all verb mappings.
func (s *VerbSet) AllMappings() []*VerbMapping {
	var mappings []*VerbMapping
	for _, mapping := range s.verbs {
		mappings = append(mappings, mapping)
	}
	// Sort by verb for consistency
	sort.Slice(mappings, func(i, j int) bool {
		return mappings[i].Verb < mappings[j].Verb
	})
	return mappings
}

// Count returns the number of verbs.
func (s *VerbSet) Count() int {
	return len(s.verbs)
}

// toKebabCase converts a string to kebab-case.
func toKebabCase(s string) string {
	if s == "" {
		return ""
	}

	var result strings.Builder
	for i, r := range s {
		if i > 0 && r >= 'A' && r <= 'Z' {
			result.WriteByte('-')
		}
		if r >= 'A' && r <= 'Z' {
			result.WriteByte(byte(r + 32))
		} else {
			result.WriteRune(r)
		}
	}
	return strings.ReplaceAll(result.String(), "_", "-")
}

// StandardVerbs are the common CRUD verbs.
var StandardVerbs = []string{
	"list",
	"get",
	"create",
	"update",
	"apply",
	"delete",
}

// IsStandardVerb checks if a verb is a standard CRUD verb.
func IsStandardVerb(verb string) bool {
	return slices.Contains(StandardVerbs, verb)
}

// ActionVerbs are common action verbs (non-CRUD).
var ActionVerbs = []string{
	"activate", "deactivate",
	"enable", "disable",
	"start", "stop", "restart",
	"cancel", "approve", "reject",
	"archive", "unarchive",
	"publish", "unpublish",
	"lock", "unlock",
	"sync", "refresh",
	"validate", "verify",
	"clone", "copy", "move", "rename",
	"import", "export",
	"download", "upload",
	"send", "resend",
	"reset",
	"search", "query",
	"batch", "bulk",
}

// IsActionVerb checks if a verb is an action verb.
func IsActionVerb(verb string) bool {
	return slices.Contains(ActionVerbs, verb)
}

// VerbDescription returns a human-readable description for a verb.
func VerbDescription(verb string) string {
	descriptions := map[string]string{
		"list":       "List all resources",
		"get":        "Get a single resource",
		"create":     "Create a new resource",
		"update":     "Update an existing resource",
		"apply":      "Apply partial updates to a resource",
		"delete":     "Delete a resource",
		"check":      "Check if a resource exists",
		"options":    "Get available options",
		"activate":   "Activate a resource",
		"deactivate": "Deactivate a resource",
		"enable":     "Enable a resource",
		"disable":    "Disable a resource",
		"start":      "Start a resource",
		"stop":       "Stop a resource",
		"restart":    "Restart a resource",
		"search":     "Search for resources",
		"download":   "Download a resource",
		"upload":     "Upload a resource",
	}

	if desc, ok := descriptions[verb]; ok {
		return desc
	}
	return "Perform " + verb + " operation"
}
