// Code moved from commands.go during the batch-5 app split.
// Behavior is intentionally unchanged; only the file layout differs.
package app

import (
	"context"
	"fmt"
	"os"
	"slices"
	"strings"

	"github.com/nomagicln/open-bridge/pkg/cli"
	"github.com/nomagicln/open-bridge/pkg/config"
	"github.com/nomagicln/open-bridge/pkg/mcp"
	"github.com/spf13/cobra"
)

func (d *Deps) newRunCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "run <app-name> [args...]",
		Short: "Run commands for an installed API application",
		Long: `Run commands for an installed API application.

Example:
  ob run petstore pets list
  ob run petstore pets create --name "Fluffy" --status available
  ob run petstore --mcp  # Start MCP server`,
		Args:               cobra.MinimumNArgs(1),
		ValidArgsFunction:  d.completeRunArgs,
		DisableFlagParsing: true, // Pass all flags to the app handler
		RunE: func(cmd *cobra.Command, args []string) error {
			return d.runCommand(args)
		},
	}

	return cmd
}

func (d *Deps) runCommand(args []string) error {
	appName := args[0]

	if !d.validateAppExists(appName) {
		return fmt.Errorf("app not found: %s", appName)
	}

	appConfig, err := d.loadAppConfig(appName)
	if err != nil {
		return err
	}

	return d.executeRunMode(args, appConfig)
}

func (d *Deps) validateAppExists(appName string) bool {
	if d.Config.AppExists(appName) {
		return true
	}

	apps, _ := d.Config.ListApps()
	if len(apps) > 0 {
		fmt.Fprintf(os.Stderr, "App '%s' not found.\n\nInstalled apps:\n", appName)
		for _, app := range apps {
			fmt.Fprintf(os.Stderr, "  %s\n", app)
		}
	} else {
		fmt.Fprintf(os.Stderr, "App '%s' not found. No apps are currently installed.\n", appName)
	}
	return false
}

func (d *Deps) loadAppConfig(appName string) (*config.AppConfig, error) {
	appConfig, err := d.Config.GetAppConfig(appName)
	if err != nil {
		return nil, fmt.Errorf("failed to load app config: %w", err)
	}
	return appConfig, nil
}

func (d *Deps) executeRunMode(args []string, appConfig *config.AppConfig) error {
	isMCPMode := slices.Contains(args[1:], "--mcp")

	if isMCPMode {
		return d.startMCPServer(appConfig, args[1:])
	}

	return d.CLI.ExecuteCommand(appConfig.Name, appConfig, args[1:])
}

type mcpServerOptions struct {
	profileName string
	transport   string
	port        string
}

func (d *Deps) parseMCPServerArgs(args []string, defaultProfile string) mcpServerOptions {
	opts := mcpServerOptions{
		transport: "stdio",
		port:      "8080",
	}

	for i, arg := range args {
		opts.profileName = d.parseArgValue(arg, args, i, "--profile", "-p", opts.profileName)
		opts.transport = d.parseArgValue(arg, args, i, "--transport", "", opts.transport)
		opts.port = d.parseArgValue(arg, args, i, "--port", "", opts.port)
	}

	if opts.profileName == "" {
		opts.profileName = defaultProfile
	}
	return opts
}

func (d *Deps) parseArgValue(arg string, args []string, i int, longFlag, shortFlag, current string) string {
	if arg == longFlag || (shortFlag != "" && arg == shortFlag) {
		if i+1 < len(args) {
			return args[i+1]
		}
	}
	if after, ok := strings.CutPrefix(arg, longFlag+"="); ok {
		return after
	}
	return current
}

func (d *Deps) startMCPServer(appConfig *config.AppConfig, args []string) error {
	specDoc, err := d.Parser.LoadSpec(appConfig.SpecSource)
	if err != nil {
		return &cli.SpecLoadError{Source: appConfig.SpecSource, Err: err}
	}

	opts := d.parseMCPServerArgs(args, appConfig.DefaultProfile)

	profile, ok := appConfig.GetProfile(opts.profileName)
	if !ok {
		return fmt.Errorf("profile '%s' not found", opts.profileName)
	}

	// Fail fast on invalid safety configuration (e.g. an uncompilable
	// dangerous_operation_patterns regex) instead of failing on first tool call.
	if err := mcp.ValidateSafetyConfig(&profile.SafetyConfig); err != nil {
		return fmt.Errorf("invalid MCP safety configuration for profile %q: %w", opts.profileName, err)
	}

	factory := mcp.NewServerFactory(appConfig.Name, buildInfo.Version)
	server := factory.CreateServer()

	// Check if progressive disclosure mode is enabled
	if profile.SafetyConfig.ProgressiveDisclosure {
		// Use ProgressiveHandler for progressive disclosure mode
		engineType, err := d.parseSearchEngineType(profile.SafetyConfig.SearchEngine)
		if err != nil {
			return fmt.Errorf("failed to parse search engine type: %w", err)
		}

		progressiveHandler, err := mcp.NewProgressiveHandler(d.Builder, nil, engineType)
		if err != nil {
			return fmt.Errorf("failed to create progressive handler: %w", err)
		}
		defer func() {
			_ = progressiveHandler.Close()
		}()

		if err := progressiveHandler.SetSpec(specDoc, &profile.SafetyConfig); err != nil {
			return fmt.Errorf("failed to set spec: %w", err)
		}
		progressiveHandler.SetAppConfig(appConfig, opts.profileName)
		progressiveHandler.Register(server)

		fmt.Fprintf(os.Stderr, "Starting MCP server (progressive mode) for app '%s' (profile: %s) via %s...\n", appConfig.Name, opts.profileName, opts.transport)
	} else {
		// Use standard Handler
		d.MCP.SetSpec(specDoc)
		d.MCP.SetAppConfig(appConfig, opts.profileName)
		d.MCP.Register(server, &profile.SafetyConfig)

		fmt.Fprintf(os.Stderr, "Starting MCP server for app '%s' (profile: %s) via %s...\n", appConfig.Name, opts.profileName, opts.transport)
	}

	return factory.RunServer(context.Background(), server, opts.transport, opts.port)
}

func (d *Deps) parseSearchEngineType(s string) (mcp.SearchEngineType, error) {
	if s == "" {
		return mcp.SearchEnginePredicate, nil // Default to predicate
	}
	return mcp.ParseSearchEngineType(s)
}
