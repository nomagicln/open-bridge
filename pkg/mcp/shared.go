package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/nomagicln/open-bridge/internal/requestpolicy"
	"github.com/nomagicln/open-bridge/pkg/config"
	"github.com/nomagicln/open-bridge/pkg/request"
)

// Common errors for the mcp package.
var errAppConfigNotSet = errors.New("app configuration not set")

// newProfileNotFoundError creates an error for a profile that was not found.
func newProfileNotFoundError(profileName string) error {
	return fmt.Errorf("profile '%s' not found", profileName)
}

// getActiveProfile is a shared helper function to get the active profile configuration.
// It accepts the app configuration, an optional profile name override, and returns
// the resolved profile name, profile configuration, and any error.
func getActiveProfile(appConfig *config.AppConfig, profileNameOverride string) (string, *config.Profile, error) {
	if appConfig == nil {
		return "", nil, errAppConfigNotSet
	}

	profileName := profileNameOverride
	if profileName == "" {
		profileName = appConfig.DefaultProfile
	}

	profile, ok := appConfig.GetProfile(profileName)
	if !ok {
		return "", nil, newProfileNotFoundError(profileName)
	}

	return profileName, profile, nil
}

// formatMCPResult formats an API response into MCP result format.
// This is shared between Handler and ProgressiveHandler.
func formatMCPResult(statusCode int, bodyBytes []byte) *mcp.CallToolResult {
	isError := statusCode >= 400

	if jsonData, ok := parseJSON(bodyBytes); ok {
		return formatJSONResult(jsonData, isError)
	}

	return formatTextResult(bodyBytes, isError)
}

// parseJSON attempts to parse body bytes as JSON.
func parseJSON(bodyBytes []byte) (any, bool) {
	var bodyJSON any
	if err := json.Unmarshal(bodyBytes, &bodyJSON); err != nil {
		return nil, false
	}
	return bodyJSON, true
}

// formatJSONResult formats JSON data into an MCP result.
func formatJSONResult(bodyJSON any, isError bool) *mcp.CallToolResult {
	prettyJSON, err := json.MarshalIndent(bodyJSON, "", "  ")
	if err != nil {
		prettyJSON = []byte{}
	}

	return &mcp.CallToolResult{
		Content: []mcp.Content{
			&mcp.TextContent{Text: string(prettyJSON)},
		},
		IsError: isError,
	}
}

// formatTextResult formats plain text into an MCP result.
func formatTextResult(bodyBytes []byte, isError bool) *mcp.CallToolResult {
	return &mcp.CallToolResult{
		Content: []mcp.Content{
			&mcp.TextContent{Text: string(bodyBytes)},
		},
		IsError: isError,
	}
}

// errorResult creates an MCP error result with the given message.
func errorResult(format string, args ...any) *mcp.CallToolResult {
	return &mcp.CallToolResult{
		Content: []mcp.Content{
			&mcp.TextContent{Text: fmt.Sprintf(format, args...)},
		},
		IsError: true,
	}
}

// apiExecutor performs the API calls of the MCP handlers.
//
// The standard and the progressive handler embed it, so both resolve the active
// profile, pick the HTTP client and apply the request policy in exactly the
// same way.
type apiExecutor struct {
	requestBuilder *request.Builder
	appConfig      *config.AppConfig
	profileName    string

	// httpClient is either the explicitly injected client or the default fallback.
	// clientInjected records whether the caller supplied it: an injected client is
	// always used as-is, while a nil injection enables per-profile clients.
	httpClient     *http.Client
	clientInjected bool
	clients        *profileClientCache
}

// newAPIExecutor builds the executor shared by the MCP handlers.
func newAPIExecutor(requestBuilder *request.Builder, httpClient *http.Client) apiExecutor {
	executor := apiExecutor{
		requestBuilder: requestBuilder,
		clients:        newProfileClientCache(),
	}
	if httpClient != nil {
		executor.httpClient = httpClient
		executor.clientInjected = true
	} else {
		executor.httpClient = &http.Client{Timeout: defaultHTTPTimeout}
	}
	return executor
}

// httpClientForProfile returns the client used for a profile. An explicitly
// injected client always wins; otherwise the per-profile client is built from
// the profile timeout/TLS settings and cached by profile name.
func (e *apiExecutor) httpClientForProfile(profileName string, profile *config.Profile) (*http.Client, error) {
	if e.clientInjected {
		return e.httpClient, nil
	}
	return e.clients.get(profileName, profile)
}

// getActiveProfile returns the active profile name and configuration.
func (e *apiExecutor) getActiveProfile() (string, *config.Profile, error) {
	return getActiveProfile(e.appConfig, e.profileName)
}

// buildAndExecuteRequest validates the arguments, builds the request and
// executes the API call.
func (e *apiExecutor) buildAndExecuteRequest(
	ctx context.Context,
	operation *openapi3.Operation,
	method, path string,
	arguments map[string]any,
	profileName string,
	profile *config.Profile,
) (*mcp.CallToolResult, error) {
	return executeOperation(
		ctx,
		e.requestBuilder,
		func() (*http.Client, error) {
			return e.httpClientForProfile(profileName, profile)
		},
		e.appConfig.Name,
		profileName,
		profile,
		operation,
		method,
		path,
		arguments,
	)
}

// executeOperation validates the tool arguments, builds the request, applies the
// shared request policy and executes the API call.
//
// Standard and progressive MCP modes both route through this function, so the
// MCP surface applies the same parameter validation, profile overrides and retry
// policy as the CLI. The call context is bound to the outgoing request, so a
// cancelled tool call stops the request instead of running it to completion.
func executeOperation(
	ctx context.Context,
	builder *request.Builder,
	resolveClient func() (*http.Client, error),
	appName, profileName string,
	profile *config.Profile,
	operation *openapi3.Operation,
	method, path string,
	arguments map[string]any,
) (*mcp.CallToolResult, error) {
	requestBody, parameters := operationParts(operation)

	if err := builder.ValidateParams(arguments, parameters, requestBody); err != nil {
		return errorResult("Invalid parameters: %v", err), nil
	}

	httpReq, err := builder.BuildRequest(method, path, profile.BaseURL, arguments, parameters, requestBody)
	if err != nil {
		return errorResult("Failed to build request: %v", err), nil
	}

	if err := builder.InjectAuth(httpReq, appName, profileName, &profile.Auth); err != nil {
		return nil, fmt.Errorf("failed to inject authentication: %w", err)
	}

	requestpolicy.ApplyProfile(httpReq, profile)

	if ctx == nil {
		ctx = context.Background()
	}
	httpReq = httpReq.WithContext(ctx)

	client, err := resolveClient()
	if err != nil {
		return errorResult("Failed to create HTTP client for profile %q: %v", profileName, err), nil
	}

	httpResp, err := request.DoWithRetry(client, httpReq, retryConfigFor(profile))
	if err != nil {
		return errorResult("Failed to execute API call: %v", err), nil
	}
	defer func() { _ = httpResp.Body.Close() }()

	bodyBytes, err := io.ReadAll(httpResp.Body)
	if err != nil {
		return errorResult("Failed to read response body: %v", err), nil
	}

	return formatMCPResult(httpResp.StatusCode, bodyBytes), nil
}

// retryConfigFor returns the retry policy of a profile.
func retryConfigFor(profile *config.Profile) config.RetryConfig {
	if profile == nil {
		return config.RetryConfig{}
	}
	return profile.RetryConfig
}

// operationParts returns the request body and the parameters of an operation.
func operationParts(operation *openapi3.Operation) (*openapi3.RequestBody, openapi3.Parameters) {
	if operation == nil {
		return nil, nil
	}

	var requestBody *openapi3.RequestBody
	if operation.RequestBody != nil {
		requestBody = operation.RequestBody.Value
	}
	return requestBody, operation.Parameters
}
