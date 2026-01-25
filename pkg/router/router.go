// Package router provides command routing for OpenBridge.
package router

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/getkin/kin-openapi/openapi3"
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
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

// mcpOptions holds parsed MCP server options from command-line arguments.
type mcpOptions struct {
	profileName string
	transport   string
	port        string
}

// parseMCPOptions extracts MCP-related options from command-line arguments.
func (r *Router) parseMCPOptions(args []string, defaultProfile string) mcpOptions {
	opts := mcpOptions{
		profileName: r.detectProfile(args),
		transport:   "stdio",
		port:        "8080",
	}

	opts.parseTransport(args)
	opts.parsePort(args)
	opts.ensureProfile(defaultProfile)

	return opts
}

// parseTransport parses the transport option from args.
func (opts *mcpOptions) parseTransport(args []string) {
	for i, arg := range args {
		if arg == "--transport" && i+1 < len(args) {
			opts.transport = args[i+1]
		}
		if after, ok := strings.CutPrefix(arg, "--transport="); ok {
			opts.transport = after
		}
	}
}

// parsePort parses the port option from args.
func (opts *mcpOptions) parsePort(args []string) {
	for i, arg := range args {
		if arg == "--port" && i+1 < len(args) {
			opts.port = args[i+1]
		}
		if after, ok := strings.CutPrefix(arg, "--port="); ok {
			opts.port = after
		}
	}
}

// ensureProfile ensures a profile is set, using default if not provided.
func (opts *mcpOptions) ensureProfile(defaultProfile string) {
	if opts.profileName == "" {
		opts.profileName = defaultProfile
	}
}

// registerProgressiveHandler creates and registers a progressive disclosure handler.
func (r *Router) registerProgressiveHandler(
	server *mcpsdk.Server,
	specDoc *openapi3.T,
	appConfig *config.AppConfig,
	profile *config.Profile,
	profileName string,
) (func(), error) {
	engineType := mcp.SearchEnginePredicate
	if profile.SafetyConfig.SearchEngine != "" {
		var err error
		engineType, err = mcp.ParseSearchEngineType(profile.SafetyConfig.SearchEngine)
		if err != nil {
			return nil, fmt.Errorf("invalid search engine type: %w", err)
		}
	}

	progressiveHandler, err := mcp.NewProgressiveHandler(
		r.mcpHandler.GetRequestBuilder(),
		nil,
		engineType,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create progressive handler: %w", err)
	}

	progressiveHandler.SetAppConfig(appConfig, profileName)
	if err := progressiveHandler.SetSpec(specDoc, &profile.SafetyConfig); err != nil {
		_ = progressiveHandler.Close()
		return nil, fmt.Errorf("failed to set spec for progressive handler: %w", err)
	}

	progressiveHandler.Register(server)
	return func() { _ = progressiveHandler.Close() }, nil
}

// startMCPServer starts the MCP server for the given app.
func (r *Router) startMCPServer(appConfig *config.AppConfig, args []string) error {
	ctx := context.Background()
	specDoc, err := r.specParser.LoadSpecWithPersistentCache(ctx, appConfig.SpecSource, appConfig.Name)
	if err != nil {
		return fmt.Errorf("failed to load spec: %w", err)
	}

	opts := r.parseMCPOptions(args, appConfig.DefaultProfile)

	profile, ok := appConfig.GetProfile(opts.profileName)
	if !ok {
		return fmt.Errorf("profile '%s' not found", opts.profileName)
	}

	factory := mcp.NewServerFactory(appConfig.Name, "1.0")
	server := factory.CreateServer()

	if profile.SafetyConfig.ProgressiveDisclosure {
		cleanup, err := r.registerProgressiveHandler(server, specDoc, appConfig, profile, opts.profileName)
		if err != nil {
			return err
		}
		defer cleanup()
		fmt.Fprintf(os.Stderr, "Starting MCP server for app '%s' (profile: %s, progressive: true) via %s...\n",
			appConfig.Name, opts.profileName, opts.transport)
	} else {
		r.mcpHandler.SetSpec(specDoc)
		r.mcpHandler.SetAppConfig(appConfig, opts.profileName)
		r.mcpHandler.Register(server, &profile.SafetyConfig)
		fmt.Fprintf(os.Stderr, "Starting MCP server for app '%s' (profile: %s) via %s...\n",
			appConfig.Name, opts.profileName, opts.transport)
	}

	// Run server
	return factory.RunServer(context.Background(), server, opts.transport, opts.port)
}
