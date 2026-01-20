// Package main is the entry point for the OpenBridge CLI.
// OpenBridge is a universal API runtime engine that bridges OpenAPI specifications
// to semantic CLI commands and MCP server for AI agents.
package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/nomagicln/open-bridge/pkg/cli"
	"github.com/nomagicln/open-bridge/pkg/completion"
	"github.com/nomagicln/open-bridge/pkg/config"
	"github.com/nomagicln/open-bridge/pkg/credential"
	"github.com/nomagicln/open-bridge/pkg/mcp"
	"github.com/nomagicln/open-bridge/pkg/request"
	"github.com/nomagicln/open-bridge/pkg/router"
	"github.com/nomagicln/open-bridge/pkg/semantic"
	"github.com/nomagicln/open-bridge/pkg/spec"
	installTui "github.com/nomagicln/open-bridge/pkg/tui/install"
	"github.com/spf13/cobra"
)

// Build information, set via ldflags
var (
	version = "dev"
	commit  = "unknown"
	date    = "unknown"
)

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
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// Execute runs the root command
func Execute() error {
	// Check if we're running as a shim (binary name is not 'ob')
	binaryName := filepath.Base(os.Args[0])
	binaryName = strings.TrimSuffix(binaryName, filepath.Ext(binaryName))

	if binaryName != "ob" {
		// Shim mode: route directly to app handler
		return appRouter.Execute(os.Args)
	}

	// Normal 'ob' mode: use cobra CLI
	rootCmd := &cobra.Command{
		Use:   "ob",
		Short: "OpenBridge - Universal API Runtime Engine",
		Long: `OpenBridge is a universal API runtime engine that bridges OpenAPI specifications
to two distinct interfaces:
  - A semantic CLI for human users
  - An MCP server for AI agents

One Spec, Dual Interface.`,
		Version:      fmt.Sprintf("%s (commit: %s, built: %s)", version, commit, date),
		SilenceUsage: true,
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

			// If interactive mode, run TUI wizard
			if interactive {
				// Initialize model with any CLI args provided
				// Pass AppExists check
				appExists := configMgr.AppExists(appName)

				// Load existing config to use as defaults if app exists
				if appExists {
					existingConfig, err := configMgr.GetAppConfig(appName)
					if err == nil {
						// Merge existing config into options if options are empty
						if opts.SpecSource == "" && len(opts.SpecSources) == 0 {
							opts.SpecSource = existingConfig.SpecSource
							opts.SpecSources = existingConfig.SpecSources
						}
						if opts.Description == "" {
							opts.Description = existingConfig.Description
						}
						if profile, ok := existingConfig.Profiles[existingConfig.DefaultProfile]; ok {
							if opts.BaseURL == "" {
								opts.BaseURL = profile.BaseURL
							}
							if opts.AuthType == "" {
								opts.AuthType = profile.Auth.Type
							}
						}
					}
				}

				m := installTui.NewModel(appName, opts, appExists)
				p := tea.NewProgram(m)

				// Run TUI
				finalModel, err := p.Run()
				if err != nil {
					return fmt.Errorf("wizard failed: %w", err)
				}

				// Check result
				model := finalModel.(installTui.Model)
				if model.Result() == nil {
					// User aborted
					return fmt.Errorf("installation aborted")
				}

				// Use configured options
				opts = *model.Result()

				// Disable interactive flag for core logic since we gathered everything
				opts.Interactive = false
			} else {
				// Non-interactive check: spec is required
				if specSource == "" {
					return fmt.Errorf("--spec flag is required (or use -i for interactive mode)")
				}
			}

			result, err := configMgr.InstallApp(appName, opts)
			if err != nil {
				return fmt.Errorf("installation failed: %w", err)
			}

			// Store credentials if provided
			if len(opts.AuthParams) > 0 {
				var cred *credential.Credential
				switch opts.AuthType {
				case "bearer":
					if token, ok := opts.AuthParams["token"]; ok && token != "" {
						cred = credential.NewBearerCredential(token)
					}
				case "api_key":
					if token, ok := opts.AuthParams["token"]; ok && token != "" {
						cred = credential.NewAPIKeyCredential(token)
					}
					// Update KeyName in config if provided
					if keyName, ok := opts.AuthParams["key_name"]; ok && keyName != "" {
						appConfig, err := configMgr.GetAppConfig(appName)
						if err == nil {
							// Update default profile
							if profile, ok := appConfig.Profiles["default"]; ok {
								profile.Auth.KeyName = keyName
								appConfig.Profiles["default"] = profile
								if err := configMgr.SaveAppConfig(appConfig); err != nil {
									fmt.Printf("Warning: failed to save API key name: %v\n", err)
								}
							}
						}
					}
				case "basic":
					user := opts.AuthParams["username"]
					pass := opts.AuthParams["password"]
					if user != "" || pass != "" {
						cred = credential.NewBasicCredential(user, pass)
					}
				}

				if cred != nil && credMgr != nil {
					// Default profile is always "default" for new install
					if err := credMgr.StoreCredential(result.AppName, "default", cred); err != nil {
						fmt.Printf("Warning: failed to store credentials: %v\n", err)
					} else {
						fmt.Printf("  Credentials stored securely in %s\n", credMgr.Backend())
					}
				}
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

	// Parse flags
	profileName := ""
	transport := "stdio"
	port := "8080"

	for i, arg := range args {
		// Profile
		if arg == "--profile" || arg == "-p" {
			if i+1 < len(args) {
				profileName = args[i+1]
			}
		}
		if strings.HasPrefix(arg, "--profile=") {
			profileName = strings.TrimPrefix(arg, "--profile=")
		}

		// Transport
		if arg == "--transport" {
			if i+1 < len(args) {
				transport = args[i+1]
			}
		}
		if strings.HasPrefix(arg, "--transport=") {
			transport = strings.TrimPrefix(arg, "--transport=")
		}

		// Port
		if arg == "--port" {
			if i+1 < len(args) {
				port = args[i+1]
			}
		}
		if strings.HasPrefix(arg, "--port=") {
			port = strings.TrimPrefix(arg, "--port=")
		}
	}

	if profileName == "" {
		profileName = appConfig.DefaultProfile
	}

	// Configure handler
	mcpHandler.SetSpec(specDoc)
	mcpHandler.SetAppConfig(appConfig, profileName)

	// Retrieve profile for safety config
	profile, ok := appConfig.GetProfile(profileName)
	if !ok {
		return fmt.Errorf("profile '%s' not found", profileName)
	}

	// Create server using factory
	factory := mcp.NewServerFactory(appConfig.Name, version)
	server := factory.CreateServer()

	// Register tools
	mcpHandler.Register(server, &profile.SafetyConfig)

	// Run server
	fmt.Fprintf(os.Stderr, "Starting MCP server for app '%s' (profile: %s) via %s...\n", appConfig.Name, profileName, transport)

	return factory.RunServer(context.Background(), server, transport, port)
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
		// Complete resource
		resources := completionHelper.CompleteResources(appName, toComplete)
		return resources, cobra.ShellCompDirectiveNoFileComp
	}

	if len(args) == 2 {
		// Complete verb (filter by resource if possible)
		resource := args[1]
		verbs := completionHelper.CompleteVerbsForResource(appName, resource, toComplete)
		return verbs, cobra.ShellCompDirectiveNoFileComp
	}

	// For args after resource and verb, complete flags
	if len(args) >= 3 && strings.HasPrefix(toComplete, "-") {
		resource := args[1]
		verb := args[2]
		flags := completionHelper.CompleteFlags(appName, resource, verb, toComplete)
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
