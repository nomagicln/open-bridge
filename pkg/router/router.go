// Package router provides command routing for OpenBridge.
package router

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/nomagicln/open-bridge/pkg/cli"
	"github.com/nomagicln/open-bridge/pkg/config"
	"github.com/nomagicln/open-bridge/pkg/mcp"
	"github.com/nomagicln/open-bridge/pkg/spec"
)

// Router determines execution mode and routes to appropriate handlers.
type Router struct {
	cliHandler *cli.Handler
	mcpHandler *mcp.Handler
	configMgr  *config.Manager
	specParser *spec.Parser
}

// NewRouter creates a new command router.
func NewRouter(
	cliHandler *cli.Handler,
	mcpHandler *mcp.Handler,
	configMgr *config.Manager,
	specParser *spec.Parser,
) *Router {
	return &Router{
		cliHandler: cliHandler,
		mcpHandler: mcpHandler,
		configMgr:  configMgr,
		specParser: specParser,
	}
}

// Execute determines the mode and routes to the appropriate handler.
func (r *Router) Execute(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("no command provided")
	}

	// Detect app name from invocation
	appName := r.detectAppName()

	// Handle ob-level commands
	if appName == "ob" {
		return r.handleOBCommand()
	}

	// Load app configuration
	appConfig, err := r.configMgr.GetAppConfig(appName)
	if err != nil {
		return fmt.Errorf("app not found: %s (use 'ob install' to add it)", appName)
	}

	// Check for MCP mode
	if r.IsMCPMode(args) {
		return r.startMCPServer(appConfig, args)
	}

	// Default to CLI mode
	return r.cliHandler.ExecuteCommand(appName, appConfig, args[1:])
}

// detectAppName determines the app name from args or binary name.
func (r *Router) detectAppName() string {
	// Get binary name
	binaryPath := os.Args[0]
	binaryName := filepath.Base(binaryPath)
	binaryName = strings.TrimSuffix(binaryName, filepath.Ext(binaryName))

	// If binary is 'ob', first arg is app name or ob command
	if binaryName == "ob" {
		return "ob"
	}

	// Binary name is the app name (shim mode)
	return binaryName
}

// IsMCPMode checks if we should run in MCP server mode.
func (r *Router) IsMCPMode(args []string) bool {
	return slices.Contains(args, "--mcp")
}

// detectProfile detects the profile name from arguments.
func (r *Router) detectProfile(args []string) string {
	for i, arg := range args {
		if arg == "--profile" || arg == "-p" {
			if i+1 < len(args) {
				return args[i+1]
			}
		}
		if after, ok := strings.CutPrefix(arg, "--profile="); ok {
			return after
		}
	}
	return ""
}

// handleOBCommand handles ob-level commands (install, uninstall, list, etc.)
func (r *Router) handleOBCommand() error {
	// This is handled by cobra in main.go
	// This method is a fallback for direct router usage
	return fmt.Errorf("ob commands should be handled by CLI framework")
}

// startMCPServer starts the MCP server for the given app.
func (r *Router) startMCPServer(appConfig *config.AppConfig, args []string) error {
	// Load spec
	specDoc, err := r.specParser.LoadSpec(appConfig.SpecSource)
	if err != nil {
		return fmt.Errorf("failed to load spec: %w", err)
	}

	// Determine profile
	profileName := r.detectProfile(args)
	transport := "stdio"
	port := "8080"

	for i, arg := range args {
		// Transport
		if arg == "--transport" {
			if i+1 < len(args) {
				transport = args[i+1]
			}
		}
		if after, ok := strings.CutPrefix(arg, "--transport="); ok {
			transport = after
		}

		// Port
		if arg == "--port" {
			if i+1 < len(args) {
				port = args[i+1]
			}
		}
		if after, ok := strings.CutPrefix(arg, "--port="); ok {
			port = after
		}
	}

	if profileName == "" {
		profileName = appConfig.DefaultProfile
	}

	// Configure handler
	r.mcpHandler.SetSpec(specDoc)
	r.mcpHandler.SetAppConfig(appConfig, profileName)

	// Retrieve profile for safety config
	profile, ok := appConfig.GetProfile(profileName)
	if !ok {
		return fmt.Errorf("profile '%s' not found", profileName)
	}

	// Create server using factory
	// Note: We don't have version info here easily, hardcoding "1.0" or using global if accessible.
	// Router doesn't seem to have version field. Using "1.0" for now.
	factory := mcp.NewServerFactory(appConfig.Name, "1.0")
	server := factory.CreateServer()

	// Register tools
	r.mcpHandler.Register(server, &profile.SafetyConfig)

	// Run server
	fmt.Fprintf(os.Stderr, "Starting MCP server for app '%s' (profile: %s) via %s...\n", appConfig.Name, profileName, transport)

	return factory.RunServer(context.Background(), server, transport, port)
}
