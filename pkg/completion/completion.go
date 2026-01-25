// Package completion provides shell completion support for OpenBridge.
package completion

import (
	"context"
	"sort"
	"strings"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/nomagicln/open-bridge/pkg/config"
	"github.com/nomagicln/open-bridge/pkg/semantic"
	"github.com/nomagicln/open-bridge/pkg/spec"
)

// Provider provides completion suggestions for commands and arguments.
type Provider struct {
	configMgr  *config.Manager
	specParser *spec.Parser
	mapper     *semantic.Mapper
}

// NewProvider creates a new completion provider.
func NewProvider(configMgr *config.Manager, specParser *spec.Parser, mapper *semantic.Mapper) *Provider {
	return &Provider{
		configMgr:  configMgr,
		specParser: specParser,
		mapper:     mapper,
	}
}

// CompleteAppNames returns all installed app names.
func (p *Provider) CompleteAppNames(prefix string) []string {
	apps, err := p.configMgr.ListApps()
	if err != nil {
		return nil
	}

	if prefix == "" {
		return apps
	}

	// Filter apps by prefix
	var matches []string
	for _, app := range apps {
		if strings.HasPrefix(app, prefix) {
			matches = append(matches, app)
		}
	}
	return matches
}

// CompleteVerbs returns available verbs for an app.
func (p *Provider) CompleteVerbs(appName, prefix string) []string {
	specDoc, err := p.loadSpec(appName)
	if err != nil {
		return nil
	}

	tree := p.mapper.BuildCommandTree(specDoc)

	verbs := p.collectVerbs(tree, prefix)

	return verbs
}

// collectVerbs collects unique verbs from the command tree.
func (p *Provider) collectVerbs(tree *semantic.CommandTree, prefix string) []string {
	verbs := make(map[string]bool)
	for _, res := range tree.RootResources {
		for verb := range res.Operations {
			if matchesPrefix(verb, prefix) {
				verbs[verb] = true
			}
		}
	}

	return sortedKeys(verbs)
}

// CompleteResources returns available resources for an app.
func (p *Provider) CompleteResources(appName, prefix string) []string {
	specDoc, err := p.loadSpec(appName)
	if err != nil {
		return nil
	}

	tree := p.mapper.BuildCommandTree(specDoc)

	return p.collectResources(tree, prefix)
}

// collectResources collects resource names from the command tree.
func (p *Provider) collectResources(tree *semantic.CommandTree, prefix string) []string {
	resources := make([]string, 0, len(tree.RootResources))
	for resource := range tree.RootResources {
		if matchesPrefix(resource, prefix) {
			resources = append(resources, resource)
		}
	}
	sort.Strings(resources)
	return resources
}

// CompleteResourcesForVerb returns available resources that support a given verb.
func (p *Provider) CompleteResourcesForVerb(appName, verb, prefix string) []string {
	specDoc, err := p.loadSpec(appName)
	if err != nil {
		return nil
	}

	tree := p.mapper.BuildCommandTree(specDoc)

	return p.collectResourcesWithVerb(tree, verb, prefix)
}

// collectResourcesWithVerb collects resources that support a specific verb.
func (p *Provider) collectResourcesWithVerb(tree *semantic.CommandTree, verb, prefix string) []string {
	var resources []string
	for resourceName, res := range tree.RootResources {
		if hasVerb(res, verb) && matchesPrefix(resourceName, prefix) {
			resources = append(resources, resourceName)
		}
	}
	sort.Strings(resources)
	return resources
}

// hasVerb checks if a resource has a specific verb.
func hasVerb(res *semantic.Resource, verb string) bool {
	_, ok := res.Operations[verb]
	return ok
}

// CompleteVerbsForResource returns available verbs for a given resource.
func (p *Provider) CompleteVerbsForResource(appName, resource, prefix string) []string {
	specDoc, err := p.loadSpec(appName)
	if err != nil {
		return nil
	}

	tree := p.mapper.BuildCommandTree(specDoc)

	return p.collectVerbsForResource(tree, resource, prefix)
}

// collectVerbsForResource collects verbs for a specific resource.
func (p *Provider) collectVerbsForResource(tree *semantic.CommandTree, resource, prefix string) []string {
	res, ok := tree.RootResources[resource]
	if !ok {
		return nil
	}

	verbs := make([]string, 0, len(res.Operations))
	for verb := range res.Operations {
		if matchesPrefix(verb, prefix) {
			verbs = append(verbs, verb)
		}
	}
	sort.Strings(verbs)
	return verbs
}

// getOperationSpecForCommand finds the OpenAPI operation spec for a resource+verb.
func (p *Provider) getOperationSpecForCommand(specDoc *openapi3.T, resource, verb string) *openapi3.Operation {
	tree := p.mapper.BuildCommandTree(specDoc)

	res, ok := tree.RootResources[resource]
	if !ok {
		return nil
	}

	op, ok := res.Operations[verb]
	if !ok {
		return nil
	}

	pathItem := specDoc.Paths.Find(op.Path)
	if pathItem == nil {
		return nil
	}

	return getOperationByMethod(pathItem, op.Method)
}

// getOperationByMethod returns the operation for the given HTTP method.
func getOperationByMethod(pathItem *openapi3.PathItem, method string) *openapi3.Operation {
	switch method {
	case "GET":
		return pathItem.Get
	case "POST":
		return pathItem.Post
	case "PUT":
		return pathItem.Put
	case "PATCH":
		return pathItem.Patch
	case "DELETE":
		return pathItem.Delete
	default:
		return nil
	}
}

// collectParameterFlags collects flag names from operation parameters.
func collectParameterFlags(opSpec *openapi3.Operation, prefix string) []string {
	var flags []string
	for _, paramRef := range opSpec.Parameters {
		if paramRef.Value != nil {
			flagName := paramRef.Value.Name
			if prefix == "" || strings.HasPrefix(flagName, prefix) {
				flags = append(flags, "--"+flagName)
			}
		}
	}
	return flags
}

// collectBodyPropertyFlags collects flag names from request body properties.
func collectBodyPropertyFlags(opSpec *openapi3.Operation, prefix string) []string {
	var flags []string
	if opSpec.RequestBody == nil || opSpec.RequestBody.Value == nil {
		return flags
	}

	for mediaType, content := range opSpec.RequestBody.Value.Content {
		if strings.Contains(mediaType, "json") && content.Schema != nil && content.Schema.Value != nil {
			schema := content.Schema.Value
			if schema.Properties != nil {
				for propName := range schema.Properties {
					if prefix == "" || strings.HasPrefix(propName, prefix) {
						flags = append(flags, "--"+propName)
					}
				}
			}
			break
		}
	}
	return flags
}

// CompleteFlags returns available flag names for a specific resource+verb combination.
func (p *Provider) CompleteFlags(appName, resource, verb, prefix string) []string {
	specDoc, err := p.loadSpec(appName)
	if err != nil {
		return nil
	}

	opSpec := p.getOperationSpecForCommand(specDoc, resource, verb)
	if opSpec == nil {
		return nil
	}

	var flags []string
	flags = append(flags, collectParameterFlags(opSpec, prefix)...)
	flags = append(flags, collectBodyPropertyFlags(opSpec, prefix)...)

	// Add common output flags
	commonFlags := []string{"--json", "--yaml", "--output", "--profile"}
	for _, flag := range commonFlags {
		if prefix == "" || strings.HasPrefix(flag, prefix) {
			flags = append(flags, flag)
		}
	}

	sort.Strings(flags)
	return flags
}

// completeCommonFlagValues handles completion for common flags.
func (p *Provider) completeCommonFlagValues(appName, flagName string) ([]string, bool) {
	switch flagName {
	case "output", "o":
		return []string{"table", "json", "yaml"}, true
	case "profile", "p":
		appConfig, err := p.configMgr.GetAppConfig(appName)
		if err != nil {
			return nil, true
		}
		profiles := make([]string, 0, len(appConfig.Profiles))
		for profileName := range appConfig.Profiles {
			profiles = append(profiles, profileName)
		}
		sort.Strings(profiles)
		return profiles, true
	}
	return nil, false
}

// extractEnumValues extracts string enum values from a slice.
func extractEnumValues(enums []any) []string {
	values := make([]string, 0, len(enums))
	for _, enum := range enums {
		if str, ok := enum.(string); ok {
			values = append(values, str)
		}
	}
	sort.Strings(values)
	return values
}

// findParameterEnumValues finds enum values for a parameter.
func findParameterEnumValues(opSpec *openapi3.Operation, flagName string) []string {
	for _, paramRef := range opSpec.Parameters {
		if paramRef.Value != nil && paramRef.Value.Name == flagName {
			if paramRef.Value.Schema != nil && paramRef.Value.Schema.Value != nil {
				if len(paramRef.Value.Schema.Value.Enum) > 0 {
					return extractEnumValues(paramRef.Value.Schema.Value.Enum)
				}
			}
		}
	}
	return nil
}

// findBodyPropertyEnumValues finds enum values for a body property.
func findBodyPropertyEnumValues(opSpec *openapi3.Operation, flagName string) []string {
	if opSpec.RequestBody == nil || opSpec.RequestBody.Value == nil {
		return nil
	}

	for mediaType, content := range opSpec.RequestBody.Value.Content {
		if !strings.Contains(mediaType, "json") || content.Schema == nil || content.Schema.Value == nil {
			continue
		}
		schema := content.Schema.Value
		if schema.Properties == nil {
			break
		}
		if propSchema, ok := schema.Properties[flagName]; ok && propSchema.Value != nil {
			if len(propSchema.Value.Enum) > 0 {
				return extractEnumValues(propSchema.Value.Enum)
			}
		}
		break
	}
	return nil
}

// CompleteFlagValues returns possible values for a flag.
func (p *Provider) CompleteFlagValues(appName, resource, verb, flagName string) []string {
	cleanFlagName := cleanFlagName(flagName)

	if values, handled := p.completeCommonFlagValues(appName, cleanFlagName); handled {
		return values
	}

	opSpec, err := p.getOperationSpec(appName, resource, verb)
	if err != nil || opSpec == nil {
		return nil
	}

	return findEnumValues(opSpec, cleanFlagName)
}

// cleanFlagName removes -- or - prefix from flag names.
func cleanFlagName(flagName string) string {
	flagName = strings.TrimPrefix(flagName, "--")
	flagName = strings.TrimPrefix(flagName, "-")
	return flagName
}

// getOperationSpec retrieves the operation specification for a command.
func (p *Provider) getOperationSpec(appName, resource, verb string) (*openapi3.Operation, error) {
	specDoc, err := p.loadSpec(appName)
	if err != nil {
		return nil, err
	}

	return p.getOperationSpecForCommand(specDoc, resource, verb), nil
}

// findEnumValues searches for enum values in both parameters and request body.
func findEnumValues(opSpec *openapi3.Operation, flagName string) []string {
	if values := findParameterEnumValues(opSpec, flagName); values != nil {
		return values
	}

	return findBodyPropertyEnumValues(opSpec, flagName)
}

// loadSpec loads and caches the OpenAPI spec for an app.
func (p *Provider) loadSpec(appName string) (*openapi3.T, error) {
	if specDoc, ok := p.getCachedSpec(appName); ok {
		return specDoc, nil
	}

	return p.loadAndCacheSpec(appName)
}

// getCachedSpec retrieves a cached spec if available.
func (p *Provider) getCachedSpec(appName string) (*openapi3.T, bool) {
	return p.specParser.GetCachedSpec(appName)
}

// loadAndCacheSpec loads and caches a spec from the app config.
func (p *Provider) loadAndCacheSpec(appName string) (*openapi3.T, error) {
	appConfig, err := p.configMgr.GetAppConfig(appName)
	if err != nil {
		return nil, err
	}

	ctx := context.Background()
	return p.specParser.LoadSpecWithPersistentCache(ctx, appConfig.SpecSource, appName)
}

// matchesPrefix checks if a string matches the given prefix.
func matchesPrefix(s, prefix string) bool {
	return prefix == "" || strings.HasPrefix(s, prefix)
}

// sortedKeys converts a map's keys to a sorted slice.
func sortedKeys(m map[string]bool) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
