// Package cli provides CLI command handling for OpenBridge.
package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/getkin/kin-openapi/openapi3"
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

// ExecuteCommand parses and executes a CLI command.
func (h *Handler) ExecuteCommand(appName string, appConfig *config.AppConfig, args []string) error {
	// Check for help flag
	if len(args) > 0 && (args[0] == "--help" || args[0] == "-h") {
		return h.showAppHelp(appName, appConfig)
	}

	if len(args) < 2 {
		return h.showInvalidSyntaxError(appName)
	}

	verb := args[0]
	resource := args[1]
	flagArgs := args[2:]

	// Check for help flag for specific command
	for _, arg := range flagArgs {
		if arg == "--help" || arg == "-h" {
			return h.showCommandHelp(appName, verb, resource, appConfig)
		}
	}

	// Load spec
	specDoc, ok := h.specParser.GetCachedSpec(appName)
	if !ok {
		var err error
		specDoc, err = h.specParser.LoadSpec(appConfig.SpecSource)
		if err != nil {
			// Format spec load error with helpful troubleshooting
			return fmt.Errorf("%s", h.errorFormatter.FormatError(fmt.Errorf("failed to load spec: %w", err)))
		}
		h.specParser.CacheSpec(appName, specDoc)
	}

	// Build command tree and find operation
	tree := h.mapper.BuildCommandTree(specDoc)
	res, ok := tree.RootResources[resource]
	if !ok {
		return h.showUnknownResourceError(resource, tree)
	}

	op, ok := res.Operations[verb]
	if !ok {
		return h.showUnknownVerbError(verb, resource, res)
	}

	// Parse flags
	params := h.parseFlags(flagArgs)

	// Get profile
	profile := h.getProfile(appConfig)

	// Get operation from spec
	pathItem := specDoc.Paths.Find(op.Path)
	if pathItem == nil {
		return fmt.Errorf("path not found: %s", op.Path)
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
		return fmt.Errorf("operation not found for %s %s", op.Method, op.Path)
	}

	// Get request body if present
	var requestBody *openapi3.RequestBody
	if opSpec.RequestBody != nil && opSpec.RequestBody.Value != nil {
		requestBody = opSpec.RequestBody.Value
	}

	// Validate parameters before building request
	if err := h.reqBuilder.ValidateParams(params, opSpec.Parameters, requestBody); err != nil {
		// Show detailed parameter error with help
		return h.showParameterValidationError(err, appName, verb, resource, opSpec, opSpec.Parameters)
	}

	// Build request
	req, err := h.reqBuilder.BuildRequest(
		op.Method,
		op.Path,
		profile.BaseURL,
		params,
		opSpec.Parameters,
		requestBody,
	)
	if err != nil {
		return fmt.Errorf("failed to build request: %w", err)
	}

	// Inject auth
	if err := h.reqBuilder.InjectAuth(req, appName, profile.Name, &profile.Auth); err != nil {
		return fmt.Errorf("failed to inject auth: %w", err)
	}

	// Execute request
	resp, err := h.httpClient.Do(req)
	if err != nil {
		// Format network error with helpful troubleshooting
		return fmt.Errorf("%s", h.errorFormatter.FormatError(err))
	}
	defer func() { _ = resp.Body.Close() }()

	// Read response
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read response: %w", err)
	}

	// Check for HTTP errors
	if resp.StatusCode >= 400 {
		return fmt.Errorf("%s", h.errorFormatter.FormatHTTPError(resp, body))
	}

	// Format output
	outputFormat := "table"
	if f, ok := params["output"].(string); ok {
		outputFormat = f
	}
	if _, ok := params["json"]; ok {
		outputFormat = "json"
	}
	if _, ok := params["yaml"]; ok {
		outputFormat = "yaml"
	}

	output, err := h.FormatOutput(body, outputFormat)
	if err != nil {
		return fmt.Errorf("failed to format output: %w", err)
	}

	fmt.Println(output)
	return nil
}

// parseFlags parses command line flags into a map.
func (h *Handler) parseFlags(args []string) map[string]any {
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
			h.addFlagValue(params, key, value)
			continue
		}

		// Check for boolean flag (no value)
		if i+1 >= len(args) || strings.HasPrefix(args[i+1], "--") {
			params[key] = true
			continue
		}

		// Get value
		value := args[i+1]
		i++

		h.addFlagValue(params, key, value)
	}

	return params
}

// addFlagValue adds a flag value to params, handling nested objects and arrays.
func (h *Handler) addFlagValue(params map[string]any, key, value string) {
	// Handle dot notation for nested objects
	if strings.Contains(key, ".") {
		h.setNestedValue(params, key, value)
		return
	}

	// Check if key already exists (array values from repeated flags)
	if existing, ok := params[key]; ok {
		switch v := existing.(type) {
		case []string:
			params[key] = append(v, value)
		case []any:
			params[key] = append(v, value)
		default:
			// Convert to array
			params[key] = []string{fmt.Sprintf("%v", v), value}
		}
	} else {
		params[key] = value
	}
}

// setNestedValue sets a nested value using dot notation.
func (h *Handler) setNestedValue(params map[string]any, key, value string) {
	parts := strings.Split(key, ".")
	current := params

	for i, part := range parts[:len(parts)-1] {
		if _, ok := current[part]; !ok {
			current[part] = make(map[string]any)
		}
		if next, ok := current[part].(map[string]any); ok {
			current = next
		} else {
			// Conflict - can't nest under non-map
			// Fallback to using full key
			params[key] = value
			return
		}
		_ = i
	}

	current[parts[len(parts)-1]] = value
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
		formatted, err := yaml.Marshal(data)
		if err != nil {
			return string(body), nil
		}
		return string(formatted), nil

	case "table":
		// For table format, parse JSON and display as table
		// This is a simplified implementation
		var data any
		if err := json.Unmarshal(body, &data); err != nil {
			return string(body), nil
		}
		return h.formatAsTable(data), nil

	default:
		return string(body), nil
	}
}

// formatAsTable formats data as a simple table.
func (h *Handler) formatAsTable(data any) string {
	// Simple implementation - to be enhanced with tablewriter
	switch v := data.(type) {
	case []any:
		if len(v) == 0 {
			return "No results"
		}
		// Format as list
		var lines []string
		for _, item := range v {
			if m, ok := item.(map[string]any); ok {
				var parts []string
				for key, val := range m {
					parts = append(parts, fmt.Sprintf("%s: %v", key, val))
				}
				lines = append(lines, strings.Join(parts, ", "))
			} else {
				lines = append(lines, fmt.Sprintf("%v", item))
			}
		}
		return strings.Join(lines, "\n")

	case map[string]any:
		var lines []string
		for key, val := range v {
			lines = append(lines, fmt.Sprintf("%s: %v", key, val))
		}
		return strings.Join(lines, "\n")

	default:
		return fmt.Sprintf("%v", data)
	}
}

// showInvalidSyntaxError displays usage help for invalid command syntax.
func (h *Handler) showInvalidSyntaxError(appName string) error {
	var sb strings.Builder
	sb.WriteString("Error: Invalid command syntax.\n\n")
	sb.WriteString(fmt.Sprintf("Usage: %s <verb> <resource> [flags]\n\n", appName))
	sb.WriteString("Examples:\n")
	sb.WriteString(fmt.Sprintf("  %s create user --name \"John\" --email \"john@example.com\"\n", appName))
	sb.WriteString(fmt.Sprintf("  %s list users\n", appName))
	sb.WriteString(fmt.Sprintf("  %s get user --id 123\n\n", appName))
	sb.WriteString("For more information, use:\n")
	sb.WriteString(fmt.Sprintf("  %s --help\n", appName))
	return fmt.Errorf("%s", sb.String())
}

// showAppHelp displays help for the entire app.
func (h *Handler) showAppHelp(appName string, appConfig *config.AppConfig) error {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Usage: %s <verb> <resource> [flags]\n\n", appName))

	if appConfig.Description != "" {
		sb.WriteString(fmt.Sprintf("Description: %s\n\n", appConfig.Description))
	}

	// Load spec to show available resources
	specDoc, ok := h.specParser.GetCachedSpec(appName)
	if !ok {
		var err error
		specDoc, err = h.specParser.LoadSpec(appConfig.SpecSource)
		if err != nil {
			sb.WriteString("(Unable to load API specification)\n")
			fmt.Print(sb.String())
			return nil
		}
		h.specParser.CacheSpec(appName, specDoc)
	}

	tree := h.mapper.BuildCommandTree(specDoc)

	sb.WriteString("Available Resources:\n")
	for resourceName := range tree.RootResources {
		sb.WriteString(fmt.Sprintf("  %s\n", resourceName))
	}

	sb.WriteString("\nCommon Verbs:\n")
	sb.WriteString("  create, list, get, update, delete, apply\n\n")

	sb.WriteString("Global Flags:\n")
	sb.WriteString("  --help, -h       Show help\n")
	sb.WriteString("  --json           Output in JSON format\n")
	sb.WriteString("  --yaml           Output in YAML format\n")
	sb.WriteString("  --output, -o     Output format: table, json, yaml\n")
	sb.WriteString("  --profile, -p    Profile to use\n\n")

	sb.WriteString("Examples:\n")
	sb.WriteString(fmt.Sprintf("  %s create user --name \"John\"\n", appName))
	sb.WriteString(fmt.Sprintf("  %s list users --json\n", appName))
	sb.WriteString(fmt.Sprintf("  %s get user --id 123\n", appName))

	fmt.Print(sb.String())
	return nil
}

// showCommandHelp displays help for a specific command.
func (h *Handler) showCommandHelp(appName, verb, resource string, appConfig *config.AppConfig) error {
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
	res, ok := tree.RootResources[resource]
	if !ok {
		return fmt.Errorf("unknown resource: %s", resource)
	}

	op, ok := res.Operations[verb]
	if !ok {
		return fmt.Errorf("unknown verb '%s' for resource '%s'", verb, resource)
	}

	// Get operation from spec
	pathItem := specDoc.Paths.Find(op.Path)
	if pathItem == nil {
		return fmt.Errorf("path not found: %s", op.Path)
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
		return fmt.Errorf("operation not found for %s %s", op.Method, op.Path)
	}

	// Format and display help
	help := h.errorFormatter.FormatUsageHelp(appName, verb, resource, opSpec, opSpec.Parameters)
	fmt.Print(help)
	return nil
}

// showUnknownResourceError displays error for unknown resource with suggestions.
func (h *Handler) showUnknownResourceError(resource string, tree *semantic.CommandTree) error {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Error: Unknown resource '%s'.\n\n", resource))

	// List available resources
	sb.WriteString("Available resources:\n")
	for resourceName := range tree.RootResources {
		sb.WriteString(fmt.Sprintf("  %s\n", resourceName))
	}

	sb.WriteString("\nFor more information, use:\n")
	sb.WriteString("  <app> --help\n")

	return fmt.Errorf("%s", sb.String())
}

// showUnknownVerbError displays error for unknown verb with available verbs.
func (h *Handler) showUnknownVerbError(verb, resource string, res *semantic.Resource) error {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Error: Unknown verb '%s' for resource '%s'.\n\n", verb, resource))

	// List available verbs for this resource
	sb.WriteString(fmt.Sprintf("Available verbs for '%s':\n", resource))
	for verbName := range res.Operations {
		sb.WriteString(fmt.Sprintf("  %s\n", verbName))
	}

	sb.WriteString("\nFor more information, use:\n")
	sb.WriteString(fmt.Sprintf("  <app> %s --help\n", resource))

	return fmt.Errorf("%s", sb.String())
}

// showParameterValidationError displays detailed parameter validation error with help.
func (h *Handler) showParameterValidationError(
	validationErr error,
	appName, verb, resource string,
	opSpec *openapi3.Operation,
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
				sb.WriteString(fmt.Sprintf("  --%s", param.Name))
				if param.Schema != nil && param.Schema.Value != nil && param.Schema.Value.Type != nil {
					sb.WriteString(fmt.Sprintf(" <%s>", param.Schema.Value.Type.Slice()[0]))
				}
				if param.Description != "" {
					sb.WriteString(fmt.Sprintf(" - %s", param.Description))
				}
				sb.WriteString("\n")
			}
		}
		sb.WriteString("\n")
	}

	sb.WriteString("For complete parameter details, use:\n")
	sb.WriteString(fmt.Sprintf("  %s %s %s --help\n", appName, verb, resource))

	return fmt.Errorf("%s", sb.String())
}
