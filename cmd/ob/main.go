// Package main is the entry point for the OpenBridge CLI.
// OpenBridge is a universal API runtime engine that bridges OpenAPI specifications
// to semantic CLI commands and MCP server for AI agents.
package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/nomagicln/open-bridge/pkg/cli"
	"github.com/nomagicln/open-bridge/pkg/completion"
	"github.com/nomagicln/open-bridge/pkg/config"
	"github.com/nomagicln/open-bridge/pkg/credential"
	"github.com/nomagicln/open-bridge/pkg/mcp"
	"github.com/nomagicln/open-bridge/pkg/request"
	"github.com/nomagicln/open-bridge/pkg/router"
	"github.com/nomagicln/open-bridge/pkg/semantic"
	"github.com/nomagicln/open-bridge/pkg/spec"
	"github.com/spf13/cobra"
)

// Build information, set via ldflags
var (
	version = "dev"
	commit  = "unknown"
	date    = "unknown"
)

var obCommands = map[string]struct{}{
	"install":    {},
	"uninstall":  {},
	"list":       {},
	"run":        {},
	"completion": {},
	"version":    {},
	"help":       {},
}

// Global dependencies
var (
	configMgr        *config.Manager
	specParser       *spec.Parser
	credMgr          *credential.Manager
	mapper           *semantic.Mapper
	reqBuilder       *request.Builder
	cliHandler       *cli.Handler
	mcpHandler       *mcp.Handler
	appRouter        *router.Router
	completionHelper *completion.Provider
)

func init() {
	// Initialize dependencies
	var err error

	// Config manager
	configMgr, err = config.NewManager()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to initialize config manager: %v\n", err)
		os.Exit(1)
	}

	// Spec parser
	specParser = spec.NewParser()

	// Credential manager
	credMgr, err = credential.NewManager()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: credential manager unavailable: %v\n", err)
		// Continue without credential manager - auth will be limited
	}

	// Semantic mapper
	mapper = semantic.NewMapper()

	// Request builder
	reqBuilder = request.NewBuilder(credMgr)

	// CLI handler
	cliHandler = cli.NewHandler(specParser, mapper, reqBuilder, configMgr)

	// MCP handler
	mcpHandler = mcp.NewHandler(mapper, reqBuilder, nil)

	// Router
	appRouter = router.NewRouter(cliHandler, mcpHandler, configMgr, specParser)

	// Completion provider
	completionHelper = completion.NewProvider(configMgr, specParser, mapper)
}

func main() {
	if err := Execute(); err != nil {
		// Don't print error here - it's already formatted and printed by handlers
		os.Exit(1)
	}
}

// Execute runs the root command
func Execute() error {
	// Check if we're running as a shim (binary name is not 'ob')
	binaryName := router.BinaryName(os.Args)

	if binaryName != "ob" {
		// Shim mode: route directly to app handler
		return executeApp(os.Args)
	}

	if len(os.Args) > 1 {
		appName := os.Args[1]
		if _, isCommand := obCommands[appName]; !isCommand && configMgr.AppExists(appName) {
			return executeApp(os.Args)
		}
	}

	// Normal 'ob' mode: use cobra CLI
	rootCmd := &cobra.Command{
		Use:   "ob",
		Short: "OpenBridge - Universal API Runtime Engine",
		Long: `OpenBridge is a universal API runtime engine that bridges OpenAPI specifications
to two distinct interfaces:
  - A semantic CLI for human users (kubectl-style)
  - An MCP server for AI agents

One Spec, Dual Interface.`,
		Version: fmt.Sprintf("%s (commit: %s, built: %s)", version, commit, date),
		// Handle unknown subcommands as app names
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return cmd.Help()
			}
			// First arg might be an app name
			appName := args[0]
			if configMgr.AppExists(appName) {
				// Route to app handler
				return appRouter.Execute(append([]string{os.Args[0]}, args...))
			}
			return fmt.Errorf("unknown command or app: %s\nRun 'ob --help' for usage", appName)
		},
	}

	// Disable the default completion command (we'll use our custom one)
	rootCmd.CompletionOptions.DisableDefaultCmd = true

	// Add global flags
	rootCmd.PersistentFlags().StringP("profile", "p", "", "Profile to use for the operation")
	rootCmd.PersistentFlags().Bool("mcp", false, "Start in MCP server mode")
	rootCmd.PersistentFlags().StringP("output", "o", "table", "Output format: table, json, yaml")

	// Add subcommands
	rootCmd.AddCommand(
		newInstallCmd(),
		newUninstallCmd(),
		newListCmd(),
		newRunCmd(),
		newCompletionCmd(),
	)

	return rootCmd.Execute()
}

func executeApp(args []string) error {
	if err := appRouter.Execute(args); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return err
	}
	return nil
}

// newInstallCmd creates the install subcommand
func newInstallCmd() *cobra.Command {
	var (
		specSource  string
		baseURL     string
		description string
		authType    string
		createShim  bool
		force       bool
		interactive bool
	)

	cmd := &cobra.Command{
		Use:   "install <app-name>",
		Short: "Install an API as a CLI application",
		Long: `Install an OpenAPI specification as a CLI application.
The specification can be provided as a local file path or a remote URL.

Example:
  ob install myapi --spec ./openapi.yaml
  ob install petstore --spec https://petstore.swagger.io/v2/swagger.json
  ob install myapi -i  # Interactive mode`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			appName := args[0]

			// If no spec and not interactive, require spec
			if specSource == "" && !interactive {
				return fmt.Errorf("--spec flag is required (or use -i for interactive mode)")
			}

			opts := config.InstallOptions{
				SpecSource:  specSource,
				BaseURL:     baseURL,
				Description: description,
				AuthType:    authType,
				CreateShim:  createShim,
				Force:       force,
				Interactive: interactive,
				Reader:      os.Stdin,
				Writer:      os.Stdout,
			}

			result, err := configMgr.InstallApp(appName, opts)
			if err != nil {
				return fmt.Errorf("installation failed: %w", err)
			}

			// Print success message
			fmt.Printf("✓ Successfully installed app '%s'\n", result.AppName)
			fmt.Printf("  Config: %s\n", result.ConfigPath)
			if result.ShimPath != "" {
				fmt.Printf("  Shim: %s\n", result.ShimPath)
			}
			if result.SpecInfo != nil {
				fmt.Printf("  API: %s (v%s)\n", result.SpecInfo.Title, result.SpecInfo.Version)
				fmt.Printf("  Operations: %d\n", result.SpecInfo.Operations)
			}

			fmt.Printf("\nUsage:\n")
			if result.ShimPath != "" {
				fmt.Printf("  %s <verb> <resource> [flags]\n", appName)
			} else {
				fmt.Printf("  ob %s <verb> <resource> [flags]\n", appName)
			}
			fmt.Printf("  ob %s --help\n", appName)

			return nil
		},
	}

	cmd.Flags().StringVarP(&specSource, "spec", "s", "", "Path or URL to the OpenAPI specification")
	cmd.Flags().StringVar(&baseURL, "base-url", "", "Base URL for API requests (overrides spec)")
	cmd.Flags().StringVar(&description, "description", "", "Description of the application")
	cmd.Flags().StringVar(&authType, "auth", "", "Authentication type: none, bearer, api_key, basic")
	cmd.Flags().BoolVar(&createShim, "shim", true, "Create command shortcut (shim)")
	cmd.Flags().BoolVarP(&force, "force", "f", false, "Overwrite existing app configuration")
	cmd.Flags().BoolVarP(&interactive, "interactive", "i", false, "Interactive installation mode")

	return cmd
}

// newUninstallCmd creates the uninstall subcommand
func newUninstallCmd() *cobra.Command {
	var removeShim bool

	cmd := &cobra.Command{
		Use:   "uninstall <app-name>",
		Short: "Uninstall an API application",
		Long: `Uninstall a previously installed API application.
This removes the application configuration and any created shims.

Example:
  ob uninstall myapi`,
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: completeAppNames,
		RunE: func(cmd *cobra.Command, args []string) error {
			appName := args[0]

			// Check if app exists
			if !configMgr.AppExists(appName) {
				// Suggest similar apps
				apps, _ := configMgr.ListApps()
				if len(apps) > 0 {
					fmt.Fprintf(os.Stderr, "App '%s' not found.\n\nInstalled apps:\n", appName)
					for _, app := range apps {
						fmt.Fprintf(os.Stderr, "  %s\n", app)
					}
				} else {
					fmt.Fprintf(os.Stderr, "App '%s' not found. No apps are currently installed.\n", appName)
				}
				return fmt.Errorf("app not found: %s", appName)
			}

			// Uninstall
			err := configMgr.UninstallApp(appName, removeShim)
			if err != nil {
				return fmt.Errorf("uninstallation failed: %w", err)
			}

			fmt.Printf("✓ Successfully uninstalled app '%s'\n", appName)
			return nil
		},
	}

	cmd.Flags().BoolVar(&removeShim, "remove-shim", true, "Remove the command shortcut (shim)")

	return cmd
}

// newListCmd creates the list subcommand
func newListCmd() *cobra.Command {
	var showDetails bool

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List installed API applications",
		Long: `List all installed API applications along with their status.

Example:
  ob list
  ob list --details`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			apps, err := configMgr.ListApps()
			if err != nil {
				return fmt.Errorf("failed to list apps: %w", err)
			}

			if len(apps) == 0 {
				fmt.Println("No applications installed.")
				fmt.Println("\nTo install an app, run:")
				fmt.Println("  ob install <app-name> --spec <openapi-spec>")
				return nil
			}

			if showDetails {
				// Detailed view
				for i, appName := range apps {
					info, err := configMgr.GetInstalledAppInfo(appName)
					if err != nil {
						fmt.Printf("Error loading app '%s': %v\n", appName, err)
						continue
					}

					if i > 0 {
						fmt.Println()
					}
					fmt.Printf("App: %s\n", info.Name)
					if info.Description != "" {
						fmt.Printf("  Description: %s\n", info.Description)
					}
					fmt.Printf("  Spec: %s\n", info.SpecSource)
					fmt.Printf("  Profiles: %d (%s)\n", info.ProfileCount, strings.Join(info.Profiles, ", "))
					fmt.Printf("  Default Profile: %s\n", info.DefaultProfile)
					fmt.Printf("  Shim Installed: %v\n", info.ShimExists)
					if info.SpecInfo != nil {
						fmt.Printf("  API Version: %s\n", info.SpecInfo.Version)
						fmt.Printf("  Operations: %d\n", info.SpecInfo.Operations)
					}
				}
			} else {
				// Table view
				w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
				_, _ = fmt.Fprintln(w, "NAME\tDESCRIPTION\tPROFILES\tSHIM")
				_, _ = fmt.Fprintln(w, "----\t-----------\t--------\t----")

				for _, appName := range apps {
					info, err := configMgr.GetInstalledAppInfo(appName)
					if err != nil {
						_, _ = fmt.Fprintf(w, "%s\t<error>\t-\t-\n", appName)
						continue
					}

					desc := info.Description
					if len(desc) > 40 {
						desc = desc[:37] + "..."
					}

					shimStatus := "No"
					if info.ShimExists {
						shimStatus = "Yes"
					}

					_, _ = fmt.Fprintf(w, "%s\t%s\t%d\t%s\n", info.Name, desc, info.ProfileCount, shimStatus)
				}
				_ = w.Flush()
			}

			return nil
		},
	}

	cmd.Flags().BoolVarP(&showDetails, "details", "d", false, "Show detailed information")

	return cmd
}

// newRunCmd creates the run subcommand for running app commands
func newRunCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "run <app-name> [args...]",
		Short: "Run commands for an installed API application",
		Long: `Run commands for an installed API application.

Example:
  ob run petstore list pets
  ob run petstore create pet --name "Fluffy" --status available
  ob run petstore --mcp  # Start MCP server`,
		Args:               cobra.MinimumNArgs(1),
		ValidArgsFunction:  completeRunArgs,
		DisableFlagParsing: true, // Pass all flags to the app handler
		RunE: func(cmd *cobra.Command, args []string) error {
			appName := args[0]

			// Check if app exists
			if !configMgr.AppExists(appName) {
				apps, _ := configMgr.ListApps()
				if len(apps) > 0 {
					fmt.Fprintf(os.Stderr, "App '%s' not found.\n\nInstalled apps:\n", appName)
					for _, app := range apps {
						fmt.Fprintf(os.Stderr, "  %s\n", app)
					}
				} else {
					fmt.Fprintf(os.Stderr, "App '%s' not found. No apps are currently installed.\n", appName)
				}
				return fmt.Errorf("app not found: %s", appName)
			}

			// Load app config
			appConfig, err := configMgr.GetAppConfig(appName)
			if err != nil {
				return fmt.Errorf("failed to load app config: %w", err)
			}

			// Check for MCP mode
			isMCPMode := false
			for _, arg := range args[1:] {
				if arg == "--mcp" {
					isMCPMode = true
					break
				}
			}

			if isMCPMode {
				// Start MCP server
				return startMCPServer(appConfig, args[1:])
			}

			// CLI mode
			return cliHandler.ExecuteCommand(appName, appConfig, args[1:])
		},
	}

	return cmd
}

// startMCPServer starts the MCP server for an app
func startMCPServer(appConfig *config.AppConfig, args []string) error {
	// Load spec
	specDoc, err := specParser.LoadSpec(appConfig.SpecSource)
	if err != nil {
		return fmt.Errorf("failed to load spec: %w", err)
	}

	// Determine profile
	profileName := ""
	for i, arg := range args {
		if arg == "--profile" || arg == "-p" {
			if i+1 < len(args) {
				profileName = args[i+1]
			}
		}
		if strings.HasPrefix(arg, "--profile=") {
			profileName = strings.TrimPrefix(arg, "--profile=")
		}
	}
	if profileName == "" {
		profileName = appConfig.DefaultProfile
	}

	// Configure handler
	mcpHandler.SetSpec(specDoc)
	mcpHandler.SetAppConfig(appConfig, profileName)

	// Start server
	server := mcp.NewServer(mcpHandler, os.Stdin, os.Stdout)

	// Use stderr for logs since stdout is used for JSON-RPC
	fmt.Fprintf(os.Stderr, "Starting MCP server for app '%s' (profile: %s)...\n", appConfig.Name, profileName)

	return server.Serve(context.Background())
}

// completeAppNames provides completion for installed app names
func completeAppNames(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	apps := completionHelper.CompleteAppNames(toComplete)
	return apps, cobra.ShellCompDirectiveNoFileComp
}

// completeRunArgs provides completion for the run command
func completeRunArgs(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	if len(args) == 0 {
		// Complete app name
		return completeAppNames(cmd, args, toComplete)
	}

	appName := args[0]
	if !configMgr.AppExists(appName) {
		return nil, cobra.ShellCompDirectiveError
	}

	if len(args) == 1 {
		// Complete verb
		verbs := completionHelper.CompleteVerbs(appName, toComplete)
		return verbs, cobra.ShellCompDirectiveNoFileComp
	}

	if len(args) == 2 {
		// Complete resource (filter by verb if possible)
		verb := args[1]
		resources := completionHelper.CompleteResourcesForVerb(appName, verb, toComplete)
		return resources, cobra.ShellCompDirectiveNoFileComp
	}

	// For args after verb and resource, complete flags
	if len(args) >= 3 && strings.HasPrefix(toComplete, "-") {
		verb := args[1]
		resource := args[2]
		flags := completionHelper.CompleteFlags(appName, verb, resource, toComplete)
		return flags, cobra.ShellCompDirectiveNoFileComp
	}

	return nil, cobra.ShellCompDirectiveNoFileComp
}

// newCompletionCmd creates the completion subcommand
func newCompletionCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "completion [bash|zsh|fish]",
		Short: "Generate shell completion script",
		Long: `Generate shell completion script for ob.

The completion script can be sourced to enable auto-completion in your shell.

Bash:
  # Load in current session
  source <(ob completion bash)
  
  # Install permanently (Linux)
  ob completion bash > /etc/bash_completion.d/ob
  
  # Install permanently (macOS)
  ob completion bash > /usr/local/etc/bash_completion.d/ob

Zsh:
  # Load in current session
  source <(ob completion zsh)
  
  # Install permanently
  ob completion zsh > "${fpath[1]}/_ob"

Fish:
  # Load in current session
  ob completion fish | source
  
  # Install permanently
  ob completion fish > ~/.config/fish/completions/ob.fish`,
		ValidArgs:             []string{"bash", "zsh", "fish"},
		Args:                  cobra.MatchAll(cobra.ExactArgs(1), cobra.OnlyValidArgs),
		DisableFlagsInUseLine: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			shell := args[0]
			switch shell {
			case "bash":
				return cmd.Root().GenBashCompletionV2(os.Stdout, true)
			case "zsh":
				return cmd.Root().GenZshCompletion(os.Stdout)
			case "fish":
				return cmd.Root().GenFishCompletion(os.Stdout, true)
			}
			return nil
		},
	}

	return cmd
}
