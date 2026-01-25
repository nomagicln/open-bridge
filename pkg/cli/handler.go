// Package cli provides CLI command handling for OpenBridge.
package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"net/http"
	"os"
	"strings"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/nomagicln/open-bridge/pkg/codegen"
	"github.com/nomagicln/open-bridge/pkg/config"
	"github.com/nomagicln/open-bridge/pkg/request"
	"github.com/nomagicln/open-bridge/pkg/semantic"
	"github.com/nomagicln/open-bridge/pkg/spec"
	"gopkg.in/yaml.v3"
)

// Handler processes CLI commands and executes API operations.
type Handler struct {
	specParser     *spec.Parser
	mapper         *semantic.Mapper
	reqBuilder     *request.Builder
	httpClient     *http.Client
	errorFormatter *ErrorFormatter
	configMgr      *config.Manager
}

// NewHandler creates a new CLI handler.
func NewHandler(
	specParser *spec.Parser,
	mapper *semantic.Mapper,
	reqBuilder *request.Builder,
	configMgr *config.Manager,
) *Handler {
	return &Handler{
		specParser:     specParser,
		mapper:         mapper,
		reqBuilder:     reqBuilder,
		httpClient:     &http.Client{},
		errorFormatter: NewErrorFormatter(),
		configMgr:      configMgr,
	}
}

// handleHelpCommands handles help-related command patterns.
// Returns true if help was handled, false otherwise.
func (h *Handler) handleHelpCommands(appName string, appConfig *config.AppConfig, args []string) (bool, error) {
	if len(args) == 0 || args[0] == "--help" || args[0] == "-h" {
		return true, h.showAppHelp(appName, appConfig)
	}

	if len(args) == 1 {
		return true, h.showResourceOrVerbHelp(appName, args[0], appConfig)
	}

	if len(args) == 2 && (args[1] == "--help" || args[1] == "-h") {
		return true, h.showResourceHelp(appName, args[0], appConfig)
	}

	// Check for help flag in remaining args
	for _, arg := range args[2:] {
		if arg == "--help" || arg == "-h" {
			return true, h.showCommandHelp(appName, args[0], args[1], appConfig)
		}
	}

	return false, nil
}

// loadAndCacheSpec loads the spec, using cache if available.
func (h *Handler) loadAndCacheSpec(appName string, appConfig *config.AppConfig) (*openapi3.T, error) {
	// Try to get from memory cache first
	if specDoc, ok := h.specParser.GetCachedSpec(appName); ok {
		return specDoc, nil
	}

	// Try to load from persistent cache (cross-process)
	ctx := context.Background()
	specDoc, err := h.specParser.LoadSpecWithPersistentCache(ctx, appConfig.SpecSource, appName)
	if err != nil {
		return nil, h.printAndWrapError(h.errorFormatter.FormatError(fmt.Errorf("failed to load spec: %w", err)), err)
	}

	// Cache in memory for this process
	h.specParser.CacheSpec(appName, specDoc)
	return specDoc, nil
}

// getOperationSpec retrieves the operation spec for the given method.
func getOperationSpec(pathItem *openapi3.PathItem, method string) *openapi3.Operation {
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

// printAndWrapError prints a formatted error message to stderr and returns a PrintedError
// to prevent double printing when the error bubbles up to main().
func (h *Handler) printAndWrapError(message string, underlying error) error {
	fmt.Fprintln(os.Stderr, message)
	return &PrintedError{Err: underlying}
}

// executeAPIRequest builds and executes the API request.
// buildRequest builds an HTTP request from operation details.
func (h *Handler) buildRequest(op *semantic.Operation, opSpec *openapi3.Operation, params map[string]any, profile *config.Profile) (*http.Request, error) {
	var requestBody *openapi3.RequestBody
	if opSpec.RequestBody != nil && opSpec.RequestBody.Value != nil {
		requestBody = opSpec.RequestBody.Value
	}

	req, err := h.reqBuilder.BuildRequest(op.Method, op.Path, profile.BaseURL, params, opSpec.Parameters, requestBody)
	if err != nil {
		return nil, fmt.Errorf("failed to build request: %w", err)
	}

	if err := h.reqBuilder.InjectAuth(req, "", profile.Name, &profile.Auth); err != nil {
		return nil, fmt.Errorf("failed to inject auth: %w", err)
	}

	return req, nil
}

// readResponse reads and validates the HTTP response.
func (h *Handler) readResponse(resp *http.Response) ([]byte, error) {
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode >= 400 {
		return nil, h.printAndWrapError(h.errorFormatter.FormatHTTPError(resp, body), nil)
	}

	return body, nil
}

// executeAPIRequest executes an API request and returns the response body.
func (h *Handler) executeAPIRequest(_ string, op *semantic.Operation, opSpec *openapi3.Operation, params map[string]any, profile *config.Profile) ([]byte, error) {
	req, err := h.buildRequest(op, opSpec, params, profile)
	if err != nil {
		return nil, err
	}

	resp, err := h.httpClient.Do(req)
	if err != nil {
		return nil, h.printAndWrapError(h.errorFormatter.FormatError(err), err)
	}
	defer func() { _ = resp.Body.Close() }()

	return h.readResponse(resp)
}

// createCodeGenerator creates a code generator with the specified format and options.
func createCodeGenerator(format string, maskSecrets bool) (codegen.Generator, error) {
	if !codegen.ValidateFormat(format) {
		return nil, fmt.Errorf("unsupported code format: %s (valid formats: curl, nodejs, go, python)", format)
	}
	return codegen.NewGenerator(codegen.OutputFormat(format), codegen.Options{MaskSecrets: maskSecrets})
}

// writeCodeOutput writes generated code to stdout or a file.
func writeCodeOutput(code, outputFile string) error {
	if outputFile == "" {
		fmt.Print(code)
		return nil
	}
	if err := os.WriteFile(outputFile, []byte(code), 0600); err != nil {
		return fmt.Errorf("failed to write to file %s: %w", outputFile, err)
	}
	fmt.Printf("Code generated successfully and saved to: %s\n", outputFile)
	return nil
}

// generateCode generates code for the API request and outputs it to stdout or a file.
func (h *Handler) generateCode(_ string, op *semantic.Operation, opSpec *openapi3.Operation, params map[string]any, profile *config.Profile, format string, outputFile string) error {
	req, err := h.buildRequest(op, opSpec, params, profile)
	if err != nil {
		return err
	}

	generator, err := createCodeGenerator(format, profile.ProtectSensitiveInfo)
	if err != nil {
		return err
	}

	code, err := generator.Generate(req)
	if err != nil {
		return fmt.Errorf("failed to generate code: %w", err)
	}

	return writeCodeOutput(code, outputFile)
}

// resolveOperation finds the resource and operation for the given command.
func (h *Handler) resolveOperation(specDoc *openapi3.T, resource, verb string) (*semantic.Resource, *semantic.Operation, error) {
	tree := h.mapper.BuildCommandTree(specDoc)
	res := h.findResource(tree, resource)
	if res == nil {
		return nil, nil, h.showUnknownResourceError(resource, tree)
	}

	op, ok := res.Operations[verb]
	if !ok {
		return nil, nil, h.showUnknownVerbError(verb, resource, res)
	}

	return res, op, nil
}

// getOperationRequestBody extracts the request body from the operation spec if present.
func getOperationRequestBody(opSpec *openapi3.Operation) *openapi3.RequestBody {
	if opSpec.RequestBody != nil && opSpec.RequestBody.Value != nil {
		return opSpec.RequestBody.Value
	}
	return nil
}

// ExecuteCommand parses and executes a CLI command.
// resolveOperationSpec loads spec and resolves operation details.
func (h *Handler) resolveOperationSpec(appName string, appConfig *config.AppConfig, resource, verb string) (*openapi3.PathItem, *openapi3.Operation, *semantic.Operation, error) {
	specDoc, err := h.loadAndCacheSpec(appName, appConfig)
	if err != nil {
		return nil, nil, nil, err
	}

	_, op, err := h.resolveOperation(specDoc, resource, verb)
	if err != nil {
		return nil, nil, nil, err
	}

	pathItem := specDoc.Paths.Find(op.Path)
	if pathItem == nil {
		return nil, nil, nil, fmt.Errorf("path not found: %s", op.Path)
	}

	opSpec := getOperationSpec(pathItem, op.Method)
	if opSpec == nil {
		return nil, nil, nil, fmt.Errorf("operation not found for %s %s", op.Method, op.Path)
	}

	return pathItem, opSpec, op, nil
}

// parseAndMergeParams parses CLI and request arguments and merges them into a unified map.
// The `--` delimiter separates CLI flags (before) from request parameters (after):
//   - With delimiter: args before -- are CLI flags, args after -- are request parameters
//   - Without delimiter: all args are parsed as CLI flags, no request parameters
//
// Returns a merged map containing CLI flags and request parameters for building the API request.
func (h *Handler) parseAndMergeParams(flagArgs []string, opSpec *openapi3.Operation) (map[string]any, error) {
	cliArgs, requestArgs, hasDelimiter := SplitArgs(flagArgs)

	var cliParams map[string]any
	if hasDelimiter {
		// Delimiter mode: split between CLI flags and request parameters
		cliParams = h.parseCLIFlags(cliArgs)
	} else {
		// No delimiter: all arguments are CLI flags
		cliParams = h.parseCLIFlags(flagArgs)
		requestArgs = nil
	}

	requestParams, err := ParseRequestParams(requestArgs, opSpec, hasDelimiter)
	if err != nil {
		return nil, fmt.Errorf("failed to parse request parameters: %w", err)
	}

	return h.mergeRequestParams(cliParams, requestParams), nil
}

// extractCLIFlags extracts CLI-only flags from parameters.
func extractCLIFlags(params map[string]any) (string, string, map[string]any) {
	generateFormat := ""
	generateOutput := ""

	if val, ok := params["generate"]; ok && val != nil {
		if str, ok := val.(string); ok {
			generateFormat = str
		}
	}
	if val, ok := params["generate-output"]; ok && val != nil {
		if str, ok := val.(string); ok {
			generateOutput = str
		}
	}

	// Remove CLI flags from params map
	cleanParams := make(map[string]any, len(params))
	for k, v := range params {
		switch k {
		case "generate", "generate-output", "output", "json", "yaml":
			continue
		default:
			cleanParams[k] = v
		}
	}
	return generateFormat, generateOutput, cleanParams
}

// ExecuteCommand parses and executes a CLI command.
func (h *Handler) ExecuteCommand(appName string, appConfig *config.AppConfig, args []string) error {
	if handled, err := h.handleHelpCommands(appName, appConfig, args); handled {
		return err
	}

	resource, verb, flagArgs := args[0], args[1], args[2:]

	_, opSpec, op, err := h.resolveOperationSpec(appName, appConfig, resource, verb)
	if err != nil {
		return err
	}

	params, err := h.parseAndMergeParams(flagArgs, opSpec)
	if err != nil {
		return err
	}

	generateFormat, generateOutput, cleanParams := extractCLIFlags(params)
	profile := h.getProfile(appConfig)

	// Handle code generation or API request execution
	if generateFormat != "" {
		return h.generateCode(appName, op, opSpec, cleanParams, profile, generateFormat, generateOutput)
	}

	// API request path: validate parameters
	requestBody := getOperationRequestBody(opSpec)
	if err := h.reqBuilder.ValidateParams(cleanParams, opSpec.Parameters, requestBody); err != nil {
		return h.showParameterValidationError(err, appName, resource, verb, opSpec.Parameters)
	}

	body, err := h.executeAPIRequest(appName, op, opSpec, cleanParams, profile)
	if err != nil {
		return err
	}

	return h.formatAndPrintOutput(body, cleanParams)
}

// determineOutputFormat extracts the output format from parameters.
func determineOutputFormat(params map[string]any) string {
	if f, ok := params["output"].(string); ok {
		return f
	}
	if _, ok := params["json"]; ok {
		return "json"
	}
	if _, ok := params["yaml"]; ok {
		return "yaml"
	}
	return "table"
}

// formatAndPrintOutput formats the response body and prints it.
func (h *Handler) formatAndPrintOutput(body []byte, params map[string]any) error {
	output, err := h.FormatOutput(body, determineOutputFormat(params))
	if err != nil {
		return fmt.Errorf("failed to format output: %w", err)
	}
	fmt.Println(output)
	return nil
}

// findResource searches for a resource by name in both root resources and sub-resources.
func (h *Handler) findResource(tree *semantic.CommandTree, name string) *semantic.Resource {
	// Normalize name: replace / with - to support URL-style resource paths
	normalizedName := strings.ReplaceAll(name, "/", "-")

	// First check root resources
	if res, ok := tree.RootResources[normalizedName]; ok {
		return res
	}

	// Search in sub-resources of all root resources
	for _, rootRes := range tree.RootResources {
		if found := h.findResourceRecursive(rootRes, normalizedName); found != nil {
			return found
		}
	}

	return nil
}

// findResourceRecursive searches for a resource in sub-resources recursively.
func (h *Handler) findResourceRecursive(parent *semantic.Resource, name string) *semantic.Resource {
	for subName, subRes := range parent.SubResources {
		if subName == name {
			return subRes
		}
		// Check nested sub-resources
		if found := h.findResourceRecursive(subRes, name); found != nil {
			return found
		}
	}
	return nil
}

// parseCLIFlags parses CLI-only flags (like --generate, --output, etc.)
// These are flags not related to API parameters.
func (h *Handler) parseCLIFlags(args []string) map[string]any {
	params := make(map[string]any)

	for i := 0; i < len(args); i++ {
		arg := args[i]
		if !strings.HasPrefix(arg, "--") {
			continue
		}

		key := strings.TrimPrefix(arg, "--")

		// Check for = separator
		if idx := strings.Index(key, "="); idx != -1 {
			value := key[idx+1:]
			key = key[:idx]
			params[key] = value
			continue
		}

		// Check for boolean flag
		if i+1 >= len(args) || strings.HasPrefix(args[i+1], "--") {
			params[key] = true
			continue
		}

		// Get value
		value := args[i+1]
		i++
		params[key] = value
	}

	return params
}

// mergeRequestParams merges request parameters from RequestParams struct into a unified map.
// This consolidates Headers, Query, Path, and Body into one map for request building.
func (h *Handler) mergeRequestParams(cliParams map[string]any, reqParams *RequestParams) map[string]any {
	merged := cliParams

	// Merge Header, Query, and Path parameters with their values (not prefixed)
	for key, value := range reqParams.Headers {
		merged[key] = value
	}

	for key, value := range reqParams.Query {
		merged[key] = value
	}

	for key, value := range reqParams.Path {
		merged[key] = value
	}

	// Merge Body parameters
	maps.Copy(merged, reqParams.Body)

	return merged
}

// getProfile returns the profile to use for the request.
func (h *Handler) getProfile(appConfig *config.AppConfig) *config.Profile {
	profileName := appConfig.DefaultProfile
	if profileName == "" {
		profileName = "default"
	}

	if profile, ok := appConfig.Profiles[profileName]; ok {
		return &profile
	}

	// Return first profile if default not found
	for _, profile := range appConfig.Profiles {
		return &profile
	}

	// Return empty profile
	return &config.Profile{Name: "default"}
}

// FormatOutput formats the response body according to the specified format.
func (h *Handler) FormatOutput(body []byte, format string) (string, error) {
	switch format {
	case "json":
		var data any
		if err := json.Unmarshal(body, &data); err != nil {
			return string(body), nil
		}
		formatted, err := json.MarshalIndent(data, "", "  ")
		if err != nil {
			return string(body), nil
		}
		return string(formatted), nil

	case "yaml":
		var data any
		if err := json.Unmarshal(body, &data); err != nil {
			return string(body), nil
		}
		// Use encoder to set 2-space indentation
		var buf bytes.Buffer
		enc := yaml.NewEncoder(&buf)
		enc.SetIndent(2)
		if err := enc.Encode(data); err != nil {
			return string(body), nil
		}
		return buf.String(), nil

	default:
		// Default to yaml format
		return h.FormatOutput(body, "yaml")
	}
}

// showInvalidSyntaxError displays usage help for invalid command syntax.
func (h *Handler) showInvalidSyntaxError(appName string, tree *semantic.CommandTree) error {
	var sb strings.Builder
	sb.WriteString("Error: Invalid command syntax.\n\n")
	fmt.Fprintf(&sb, "Usage: %s <resource> <verb> [flags]\n\n", appName)
	sb.WriteString("Examples:\n")
	h.writeResourceExamples(&sb, appName, tree)
	sb.WriteString("\nFor more information, use:\n")
	fmt.Fprintf(&sb, "  %s --help\n", appName)
	return fmt.Errorf("%s", sb.String())
}

// showResourceOrVerbHelp handles single-argument input, determining if it's a resource or verb.
// If it's a valid resource, shows resource help. Otherwise shows an error.
func (h *Handler) showResourceOrVerbHelp(appName, arg string, appConfig *config.AppConfig) error {
	// Load spec
	specDoc, err := h.loadSpec(appName, appConfig)
	if err != nil {
		return err
	}

	tree := h.mapper.BuildCommandTree(specDoc)

	// Check if arg is a resource
	res := h.findResource(tree, arg)
	if res != nil {
		return h.showResourceHelpFromResource(appName, arg, res, appConfig)
	}

	// Not a valid resource, show syntax help
	return h.showInvalidSyntaxError(appName, tree)
}

// showResourceHelp shows help for a specific resource.
func (h *Handler) showResourceHelp(appName, resourceName string, appConfig *config.AppConfig) error {
	// Load spec
	specDoc, err := h.loadSpec(appName, appConfig)
	if err != nil {
		return err
	}

	tree := h.mapper.BuildCommandTree(specDoc)

	// Find resource
	res := h.findResource(tree, resourceName)
	if res == nil {
		return h.showUnknownResourceError(resourceName, tree)
	}

	return h.showResourceHelpFromResource(appName, resourceName, res, appConfig)
}

// showResourceHelpFromResource displays help for a specific resource.
func (h *Handler) showResourceHelpFromResource(appName, resourceName string, res *semantic.Resource, appConfig *config.AppConfig) error {
	var sb strings.Builder
	fmt.Fprintf(&sb, "Resource: %s\n\n", resourceName)

	if appConfig.Description != "" {
		fmt.Fprintf(&sb, "API: %s\n\n", appConfig.Description)
	}

	sb.WriteString("Available Operations:\n")
	if len(res.Operations) == 0 {
		sb.WriteString("  (no operations available)\n")
	} else {
		for verbName, op := range res.Operations {
			fmt.Fprintf(&sb, "  %s %s", resourceName, verbName)
			if op.Summary != "" {
				fmt.Fprintf(&sb, " - %s", op.Summary)
			}
			sb.WriteString("\n")
		}
	}

	// Show sub-resources if any
	if len(res.SubResources) > 0 {
		sb.WriteString("\nSub-resources:\n")
		for subName := range res.SubResources {
			fmt.Fprintf(&sb, "  %s\n", subName)
		}
	}

	sb.WriteString("\nFor detailed command help:\n")
	fmt.Fprintf(&sb, "  %s %s <verb> --help\n", appName, resourceName)

	fmt.Print(sb.String())
	return nil
}

// loadSpec loads and caches the spec for an app.
func (h *Handler) loadSpec(appName string, appConfig *config.AppConfig) (*openapi3.T, error) {
	specDoc, ok := h.specParser.GetCachedSpec(appName)
	if !ok {
		var err error
		specDoc, err = h.specParser.LoadSpec(appConfig.SpecSource)
		if err != nil {
			return nil, h.printAndWrapError(h.errorFormatter.FormatError(fmt.Errorf("failed to load spec: %w", err)), err)
		}
		h.specParser.CacheSpec(appName, specDoc)
	}
	return specDoc, nil
}

// showAppHelp displays help for the entire app.
//
//nolint:unparam // Returns nil for consistency with other help methods that may return errors.
func (h *Handler) showAppHelp(appName string, appConfig *config.AppConfig) error {
	var sb strings.Builder
	fmt.Fprintf(&sb, "Usage: %s <resource> <verb> [flags]\n\n", appName)

	if appConfig.Description != "" {
		fmt.Fprintf(&sb, "Description: %s\n\n", appConfig.Description)
	}

	// Load spec to show available resources
	specDoc, ok := h.specParser.GetCachedSpec(appName)
	if !ok {
		var err error
		specDoc, err = h.specParser.LoadSpec(appConfig.SpecSource)
		if err != nil {
			h.writeHelpWhenSpecLoadFails(&sb, appName)
			return nil
		}
		h.specParser.CacheSpec(appName, specDoc)
	}

	tree := h.mapper.BuildCommandTree(specDoc)

	h.writeResourcesSection(&sb, tree)
	h.writeGlobalFlagsSection(&sb)
	h.writeExamplesSection(&sb, appName, tree)

	fmt.Print(sb.String())
	return nil
}

func (h *Handler) writeHelpWhenSpecLoadFails(sb *strings.Builder, appName string) {
	sb.WriteString("(Unable to load API specification)\n\n")
	sb.WriteString("Examples:\n")
	fmt.Fprintf(sb, "  %s <resource> <verb> [flags]\n", appName)
	fmt.Print(sb.String())
}

func (h *Handler) writeResourcesSection(sb *strings.Builder, tree *semantic.CommandTree) {
	sb.WriteString("Available Resources:\n")
	for resourceName, res := range tree.RootResources {
		fmt.Fprintf(sb, "  %s\n", resourceName)
		// List sub-resources
		for subName := range res.SubResources {
			fmt.Fprintf(sb, "    %s\n", subName)
		}
	}

	sb.WriteString("\nCommon Verbs:\n")
	sb.WriteString("  create, list, get, update, delete, apply\n\n")
}

func (h *Handler) writeGlobalFlagsSection(sb *strings.Builder) {
	sb.WriteString("Global Flags:\n")
	sb.WriteString("  --help, -h       Show help\n")
	sb.WriteString("  --json           Output in JSON format\n")
	sb.WriteString("  --yaml           Output in YAML format (default)\n")
	sb.WriteString("  --output, -o     Output format: json, yaml (default: yaml)\n")
	sb.WriteString("  --profile, -p    Profile to use\n")
	sb.WriteString("  --generate, -g   Generate code instead of sending request\n")
	sb.WriteString("                   Formats: curl, nodejs, go, python\n")
	sb.WriteString("  --generate-output, -O  Save generated code to file (default: stdout)\n\n")

	sb.WriteString("Code Generation Note:\n")
	sb.WriteString("  When using --generate, no actual request is sent. Instead, code is generated\n")
	sb.WriteString("  in the specified language. Sensitive information (API keys, tokens) can be\n")
	sb.WriteString("  masked based on the 'protect_sensitive_info' profile setting.\n\n")
}

func (h *Handler) writeExamplesSection(sb *strings.Builder, appName string, tree *semantic.CommandTree) {
	sb.WriteString("Examples:\n")
	h.writeResourceExamples(sb, appName, tree)
}

// writeResourceExamples writes dynamic examples based on actual resources.
func (h *Handler) writeResourceExamples(sb *strings.Builder, appName string, tree *semantic.CommandTree) {
	// Get first available resource for examples
	var exampleResource string
	var exampleVerbs []string
	for resourceName, res := range tree.RootResources {
		exampleResource = resourceName
		for verbName := range res.Operations {
			exampleVerbs = append(exampleVerbs, verbName)
			if len(exampleVerbs) >= 3 {
				break
			}
		}
		break
	}

	if exampleResource == "" {
		// Fallback to generic examples if no resources found
		fmt.Fprintf(sb, "  %s <resource> <verb> [flags]\n", appName)
		return
	}

	// Generate examples based on actual verbs
	for _, verb := range exampleVerbs {
		switch verb {
		case "create":
			fmt.Fprintf(sb, "  %s %s create --name \"example\"\n", appName, exampleResource)
		case "list":
			fmt.Fprintf(sb, "  %s %s list --json\n", appName, exampleResource)
		case "get":
			fmt.Fprintf(sb, "  %s %s get --id 123\n", appName, exampleResource)
		default:
			fmt.Fprintf(sb, "  %s %s %s\n", appName, exampleResource, verb)
		}
	}

	// If we have less than 2 examples, add some generic ones
	if len(exampleVerbs) < 2 {
		fmt.Fprintf(sb, "  %s %s --help\n", appName, exampleResource)
	}
}

// showCommandHelp displays help for a specific command.
func (h *Handler) showCommandHelp(appName, resource, verb string, appConfig *config.AppConfig) error {
	// Load spec
	specDoc, ok := h.specParser.GetCachedSpec(appName)
	if !ok {
		var err error
		specDoc, err = h.specParser.LoadSpec(appConfig.SpecSource)
		if err != nil {
			return fmt.Errorf("failed to load spec: %w", err)
		}
		h.specParser.CacheSpec(appName, specDoc)
	}

	// Build command tree and find operation
	tree := h.mapper.BuildCommandTree(specDoc)
	res := h.findResource(tree, resource)
	if res == nil {
		return fmt.Errorf("unknown resource: %s", resource)
	}

	op, ok := res.Operations[verb]
	if !ok {
		return fmt.Errorf("unknown verb '%s' for resource '%s'", verb, resource)
	}

	opSpec, err := h.getOperationFromSpec(specDoc, op)
	if err != nil {
		return err
	}

	var requestBody *openapi3.RequestBody
	if opSpec.RequestBody != nil && opSpec.RequestBody.Value != nil {
		requestBody = opSpec.RequestBody.Value
	}

	help := h.errorFormatter.FormatUsageHelpWithBody(appName, resource, verb, opSpec, opSpec.Parameters, requestBody)
	fmt.Print(help)
	return nil
}

// getOperationFromSpec retrieves the operation spec from the spec document.
func (h *Handler) getOperationFromSpec(specDoc *openapi3.T, op *semantic.Operation) (*openapi3.Operation, error) {
	pathItem := specDoc.Paths.Find(op.Path)
	if pathItem == nil {
		return nil, fmt.Errorf("path not found: %s", op.Path)
	}

	opSpec := getOperationSpec(pathItem, op.Method)
	if opSpec == nil {
		return nil, fmt.Errorf("operation not found for %s %s", op.Method, op.Path)
	}

	return opSpec, nil
}

// showUnknownResourceError displays error for unknown resource with suggestions.
func (h *Handler) showUnknownResourceError(resource string, tree *semantic.CommandTree) error {
	var sb strings.Builder
	fmt.Fprintf(&sb, "Error: Unknown resource '%s'.\n\n", resource)

	// List available resources (including sub-resources)
	sb.WriteString("Available resources:\n")
	for resourceName, res := range tree.RootResources {
		fmt.Fprintf(&sb, "  %s\n", resourceName)
		// List sub-resources
		for subName := range res.SubResources {
			fmt.Fprintf(&sb, "    %s\n", subName)
		}
	}

	sb.WriteString("\nFor more information, use:\n")
	sb.WriteString("  <app> --help\n")

	return fmt.Errorf("%s", sb.String())
}

// showUnknownVerbError displays error for unknown verb with available verbs.
func (h *Handler) showUnknownVerbError(verb, resource string, res *semantic.Resource) error {
	var sb strings.Builder
	fmt.Fprintf(&sb, "Error: Unknown verb '%s' for resource '%s'.\n\n", verb, resource)

	// List available verbs for this resource
	fmt.Fprintf(&sb, "Available verbs for '%s':\n", resource)
	for verbName := range res.Operations {
		fmt.Fprintf(&sb, "  %s\n", verbName)
	}

	sb.WriteString("\nFor more information, use:\n")
	fmt.Fprintf(&sb, "  <app> %s --help\n", resource)

	return fmt.Errorf("%s", sb.String())
}

// showParameterValidationError displays detailed parameter validation error with help.
func (h *Handler) showParameterValidationError(
	validationErr error,
	appName, resource, verb string,
	opParams openapi3.Parameters,
) error {
	var sb strings.Builder

	// Show the validation error
	sb.WriteString(h.errorFormatter.FormatError(validationErr))
	sb.WriteString("\n\n")

	// Show required parameters
	var requiredParams []string
	for _, paramRef := range opParams {
		param := paramRef.Value
		if param.Required {
			requiredParams = append(requiredParams, param.Name)
		}
	}

	if len(requiredParams) > 0 {
		sb.WriteString("Required parameters:\n")
		for _, paramRef := range opParams {
			param := paramRef.Value
			if param.Required {
				fmt.Fprintf(&sb, "  --%s", param.Name)
				if param.Schema != nil && param.Schema.Value != nil && param.Schema.Value.Type != nil {
					fmt.Fprintf(&sb, " <%s>", param.Schema.Value.Type.Slice()[0])
				}
				if param.Description != "" {
					fmt.Fprintf(&sb, " - %s", param.Description)
				}
				sb.WriteString("\n")
			}
		}
		sb.WriteString("\n")
	}

	sb.WriteString("For complete parameter details, use:\n")
	fmt.Fprintf(&sb, "  %s %s %s --help\n", appName, resource, verb)

	return fmt.Errorf("%s", sb.String())
}
