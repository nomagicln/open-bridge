// Package completion provides shell completion support for OpenBridge.
package completion

import (
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

	// Collect unique verbs
	verbs := make(map[string]bool)
	for _, res := range tree.RootResources {
		for verb := range res.Operations {
			if prefix == "" || strings.HasPrefix(verb, prefix) {
				verbs[verb] = true
			}
		}
	}

	// Convert to sorted slice
	result := make([]string, 0, len(verbs))
	for verb := range verbs {
		result = append(result, verb)
	}
	sort.Strings(result)

	return result
}

// CompleteResources returns available resources for an app.
func (p *Provider) CompleteResources(appName, prefix string) []string {
	specDoc, err := p.loadSpec(appName)
	if err != nil {
		return nil
	}

	tree := p.mapper.BuildCommandTree(specDoc)

	// Collect resources
	resources := make([]string, 0, len(tree.RootResources))
	for resource := range tree.RootResources {
		if prefix == "" || strings.HasPrefix(resource, prefix) {
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

	// Collect resources that have this verb
	var resources []string
	for resourceName, res := range tree.RootResources {
		if _, ok := res.Operations[verb]; ok {
			if prefix == "" || strings.HasPrefix(resourceName, prefix) {
				resources = append(resources, resourceName)
			}
		}
	}
	sort.Strings(resources)

	return resources
}

// CompleteFlags returns available flag names for a specific verb+resource combination.
func (p *Provider) CompleteFlags(appName, verb, resource, prefix string) []string {
	specDoc, err := p.loadSpec(appName)
	if err != nil {
		return nil
	}

	tree := p.mapper.BuildCommandTree(specDoc)

	// Find the resource and operation
	res, ok := tree.RootResources[resource]
	if !ok {
		return nil
	}

	op, ok := res.Operations[verb]
	if !ok {
		return nil
	}

	// Get operation spec
	pathItem := specDoc.Paths.Find(op.Path)
	if pathItem == nil {
		return nil
	}

	var opSpec *openapi3.Operation
	switch op.Method {
	case "GET":
		opSpec = pathItem.Get
	case "POST":
		opSpec = pathItem.Post
	case "PUT":
		opSpec = pathItem.Put
	case "PATCH":
		opSpec = pathItem.Patch
	case "DELETE":
		opSpec = pathItem.Delete
	}

	if opSpec == nil {
		return nil
	}

	// Collect flag names from parameters
	var flags []string

	// Add parameters
	for _, paramRef := range opSpec.Parameters {
		if paramRef.Value != nil {
			flagName := paramRef.Value.Name
			if prefix == "" || strings.HasPrefix(flagName, prefix) {
				flags = append(flags, "--"+flagName)
			}
		}
	}

	// Add request body properties if present
	if opSpec.RequestBody != nil && opSpec.RequestBody.Value != nil {
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
				// Only use the first JSON media type
				break
			}
		}
	}

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

// CompleteFlagValues returns possible values for a flag.
func (p *Provider) CompleteFlagValues(appName, verb, resource, flagName string) []string {
	specDoc, err := p.loadSpec(appName)
	if err != nil {
		return nil
	}

	tree := p.mapper.BuildCommandTree(specDoc)

	// Find the resource and operation
	res, ok := tree.RootResources[resource]
	if !ok {
		return nil
	}

	op, ok := res.Operations[verb]
	if !ok {
		return nil
	}

	// Get operation spec
	pathItem := specDoc.Paths.Find(op.Path)
	if pathItem == nil {
		return nil
	}

	var opSpec *openapi3.Operation
	switch op.Method {
	case "GET":
		opSpec = pathItem.Get
	case "POST":
		opSpec = pathItem.Post
	case "PUT":
		opSpec = pathItem.Put
	case "PATCH":
		opSpec = pathItem.Patch
	case "DELETE":
		opSpec = pathItem.Delete
	}

	if opSpec == nil {
		return nil
	}

	// Remove -- or - prefix if present
	flagName = strings.TrimPrefix(flagName, "--")
	flagName = strings.TrimPrefix(flagName, "-")

	// Special handling for common flags
	switch flagName {
	case "output", "o":
		return []string{"table", "json", "yaml"}
	case "profile", "p":
		// Get profiles for the app
		appConfig, err := p.configMgr.GetAppConfig(appName)
		if err != nil {
			return nil
		}
		profiles := make([]string, 0, len(appConfig.Profiles))
		for profileName := range appConfig.Profiles {
			profiles = append(profiles, profileName)
		}
		sort.Strings(profiles)
		return profiles
	}

	// Check parameters for enum values
	for _, paramRef := range opSpec.Parameters {
		if paramRef.Value != nil && paramRef.Value.Name == flagName {
			if paramRef.Value.Schema != nil && paramRef.Value.Schema.Value != nil {
				schema := paramRef.Value.Schema.Value
				if len(schema.Enum) > 0 {
					values := make([]string, 0, len(schema.Enum))
					for _, enum := range schema.Enum {
						if str, ok := enum.(string); ok {
							values = append(values, str)
						}
					}
					sort.Strings(values)
					return values
				}
			}
		}
	}

	// Check request body schema for enum values
	if opSpec.RequestBody != nil && opSpec.RequestBody.Value != nil {
		for mediaType, content := range opSpec.RequestBody.Value.Content {
			if strings.Contains(mediaType, "json") && content.Schema != nil && content.Schema.Value != nil {
				schema := content.Schema.Value
				if schema.Properties != nil {
					if propSchema, ok := schema.Properties[flagName]; ok && propSchema.Value != nil {
						if len(propSchema.Value.Enum) > 0 {
							values := make([]string, 0, len(propSchema.Value.Enum))
							for _, enum := range propSchema.Value.Enum {
								if str, ok := enum.(string); ok {
									values = append(values, str)
								}
							}
							sort.Strings(values)
							return values
						}
					}
				}
				break
			}
		}
	}

	return nil
}

// loadSpec loads and caches the OpenAPI spec for an app.
func (p *Provider) loadSpec(appName string) (*openapi3.T, error) {
	// Try to get cached spec
	if specDoc, ok := p.specParser.GetCachedSpec(appName); ok {
		return specDoc, nil
	}

	// Load app config
	appConfig, err := p.configMgr.GetAppConfig(appName)
	if err != nil {
		return nil, err
	}

	// Load and cache spec
	specDoc, err := p.specParser.LoadSpec(appConfig.SpecSource)
	if err != nil {
		return nil, err
	}

	p.specParser.CacheSpec(appName, specDoc)
	return specDoc, nil
}
