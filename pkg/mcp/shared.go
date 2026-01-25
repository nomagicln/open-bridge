package mcp

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/nomagicln/open-bridge/pkg/config"
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
