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

// defaultServerVersion is reported by the MCP server when no build version was
// injected through SetServerVersion.
const defaultServerVersion = "1.0"

// AppOptions carries the root-level options forwarded to an application.
//
// An option explicitly present in the application arguments takes precedence
// over the forwarded default.
type AppOptions struct {
	// ProfileName is the profile to use when the arguments do not select one.
	ProfileName string

	// OutputFormat is the output format to use when the arguments do not
	// select one.
	OutputFormat string

	// MCPMode starts the MCP server even when the arguments do not request it.
	MCPMode bool
}

// Router determines execution mode and routes to appropriate handlers.
type Router struct {
	cliHandler *cli.Handler
	mcpHandler *mcp.Handler
	configMgr  *config.Manager
	specParser *spec.Parser
	version    string
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
		version:    defaultServerVersion,
	}
}

// SetServerVersion records the version reported by the MCP server.
func (r *Router) SetServerVersion(version string) {
	if version == "" {
		version = defaultServerVersion
	}
	r.version = version
}

// ServerVersion returns the version reported by the MCP server.
func (r *Router) ServerVersion() string {
	return r.version
}

// Execute determines the mode and routes to the appropriate handler.
// It is used by shim-mode invocations where the application is identified by
// the binary name (argv[0]).
func (r *Router) Execute(args []string) error {
	return r.ExecuteContext(context.Background(), args)
}

// ExecuteContext determines the mode and routes to the appropriate handler,
// using the given context for the executed request or server.
func (r *Router) ExecuteContext(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("no command provided")
	}

	appName, appArgs := r.detectAppName(args)
	return r.ExecuteAppContext(ctx, appName, appArgs, AppOptions{})
}

// ExecuteApp executes a command for the explicitly named application.
// It validates that the app exists, then dispatches to either the MCP server
// (when args contain --mcp) or the semantic CLI. args must not include the app
// name itself.
func (r *Router) ExecuteApp(appName string, args []string) error {
	return r.ExecuteAppContext(context.Background(), appName, args, AppOptions{})
}

// ExecuteAppContext executes a command for the explicitly named application,
// using the given context for the executed request or server.
//
// The forwarded options act as defaults: an option present in args wins.
func (r *Router) ExecuteAppContext(ctx context.Context, appName string, args []string, options AppOptions) error {
	if appName == "" {
		return fmt.Errorf("no app name provided")
	}

	// Load app configuration
	appConfig, err := r.configMgr.GetAppConfig(appName)
	if err != nil {
		return fmt.Errorf("app not found: %s (use 'ob install' to add it): %w", appName, err)
	}

	// An explicit profile in the app arguments wins over the forwarded default,
	// which in turn wins over the profile configured for the app.
	profileName := options.ProfileName
	if detected := detectProfile(args); detected != "" {
		profileName = detected
	}
	if profileName == "" {
		profileName = appConfig.DefaultProfile
	}

	// Check for MCP mode
	if r.IsMCPMode(args) || options.MCPMode {
		return r.startMCPServer(ctx, appConfig, args, profileName)
	}

	// Default to CLI mode
	return r.cliHandler.ExecuteCommandWithOptions(appName, appConfig, args, cli.Options{
		OutputFormat: options.OutputFormat,
		ProfileName:  profileName,
	})
}

// detectAppName determines the app name from the shim binary name.
func (r *Router) detectAppName(args []string) (string, []string) {
	binaryPath := args[0]
	binaryName := filepath.Base(binaryPath)
	binaryName = strings.TrimSuffix(binaryName, filepath.Ext(binaryName))
	return binaryName, args[1:]
}

// IsMCPMode checks if we should run in MCP server mode.
func (r *Router) IsMCPMode(args []string) bool {
	return slices.Contains(args, "--mcp")
}

// detectProfile detects the profile name from arguments.
//
// The last occurrence wins, matching the CLI convention for repeated options.
func detectProfile(args []string) string {
	profile := ""
	for i, arg := range args {
		switch arg {
		case "--profile", "-p":
			if i+1 < len(args) {
				profile = args[i+1]
			}
		default:
			if after, ok := strings.CutPrefix(arg, "--profile="); ok {
				profile = after
			}
		}
	}
	return profile
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
		profileName: detectProfile(args),
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
func (r *Router) startMCPServer(ctx context.Context, appConfig *config.AppConfig, args []string, profileName string) error {
	specDoc, err := r.specParser.LoadSpecWithPersistentCache(ctx, appConfig.SpecSource, appConfig.Name)
	if err != nil {
		return &cli.SpecLoadError{Source: appConfig.SpecSource, Err: err}
	}

	opts := r.parseMCPOptions(args, profileName)

	profile, ok := appConfig.GetProfile(opts.profileName)
	if !ok {
		return fmt.Errorf("profile '%s' not found", opts.profileName)
	}

	// Fail fast on invalid safety configuration (e.g. an uncompilable
	// dangerous_operation_patterns regex) instead of failing on first tool call.
	if err := mcp.ValidateSafetyConfig(&profile.SafetyConfig); err != nil {
		return fmt.Errorf("invalid MCP safety configuration for profile %q: %w", opts.profileName, err)
	}

	factory := mcp.NewServerFactory(appConfig.Name, r.version)
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
	return factory.RunServer(ctx, server, opts.transport, opts.port)
}
