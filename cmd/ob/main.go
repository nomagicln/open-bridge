// Package main is the entry point for the OpenBridge CLI.
// OpenBridge is a universal API runtime engine that bridges OpenAPI specifications
// to semantic CLI commands and MCP server for AI agents.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
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
	"gopkg.in/yaml.v3"
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
		// Don't print errors that have already been printed (e.g., formatted CLI errors)
		if !cli.IsPrintedError(err) && err.Error() != "" {
			fmt.Fprintln(os.Stderr, err)
		}
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
		Version:       fmt.Sprintf("%s (commit: %s, built: %s)", version, commit, date),
		SilenceUsage:  true,
		SilenceErrors: true,
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
	rootCmd.PersistentFlags().StringP("output", "o", "yaml", "Output format: json, yaml")

	// Add subcommands
	rootCmd.AddCommand(
		newInstallCmd(),
		newUninstallCmd(),
		newListCmd(),
		newInfoCmd(),
		newRunCmd(),
		newCompletionCmd(),
	)

	return rootCmd.Execute()
}

// prepareInteractiveInstall handles the interactive TUI installation flow.
func prepareInteractiveInstall(appName string, opts config.InstallOptions) (config.InstallOptions, error) {
	appExists := configMgr.AppExists(appName)

	// Load existing config to use as defaults if app exists
	if appExists {
		existingConfig, err := configMgr.GetAppConfig(appName)
		if err == nil {
			opts = mergeExistingConfig(opts, existingConfig)
		}
	}

	m := installTui.NewModel(appName, opts, appExists)
	p := tea.NewProgram(m)

	finalModel, err := p.Run()
	if err != nil {
		return opts, fmt.Errorf("wizard failed: %w", err)
	}

	model, ok := finalModel.(installTui.Model)
	if !ok {
		return opts, fmt.Errorf("unexpected TUI model type: %T", finalModel)
	}
	if model.Result() == nil {
		return opts, fmt.Errorf("installation aborted")
	}

	result := *model.Result()
	result.Interactive = false
	return result, nil
}

// mergeExistingConfig merges existing app config into install options if options are empty.
func mergeExistingConfig(opts config.InstallOptions, existing *config.AppConfig) config.InstallOptions {
	if opts.SpecSource == "" && len(opts.SpecSources) == 0 {
		opts.SpecSource = existing.SpecSource
		opts.SpecSources = existing.SpecSources
	}
	if opts.Description == "" {
		opts.Description = existing.Description
	}
	if profile, ok := existing.Profiles[existing.DefaultProfile]; ok {
		if opts.BaseURL == "" {
			opts.BaseURL = profile.BaseURL
		}
		if opts.AuthType == "" {
			opts.AuthType = profile.Auth.Type
		}
	}
	return opts
}

// storeInstallCredentials stores credentials after installation.
func storeInstallCredentials(appName string, opts config.InstallOptions) {
	if len(opts.AuthParams) == 0 {
		return
	}

	cred := createCredentialFromParams(opts.AuthType, opts.AuthParams)
	if cred == nil {
		return
	}

	// Handle API key name update
	if opts.AuthType == "api_key" {
		updateAPIKeyName(appName, opts.AuthParams)
	}

	if credMgr != nil {
		if err := credMgr.StoreCredential(appName, "default", cred); err != nil {
			fmt.Printf("Warning: failed to store credentials: %v\n", err)
		} else {
			fmt.Printf("  Credentials stored securely in %s\n", credMgr.Backend())
		}
	}
}

// createCredentialFromParams creates a credential from auth parameters.
func createCredentialFromParams(authType string, params map[string]string) *credential.Credential {
	switch authType {
	case "bearer":
		if token := params["token"]; token != "" {
			return credential.NewBearerCredential(token)
		}
	case "api_key":
		if token := params["token"]; token != "" {
			return credential.NewAPIKeyCredential(token)
		}
	case "basic":
		user, pass := params["username"], params["password"]
		if user != "" || pass != "" {
			return credential.NewBasicCredential(user, pass)
		}
	default:
		// "none" or unknown auth type - no credential needed
	}
	return nil
}

// updateAPIKeyName updates the API key name in config if provided.
func updateAPIKeyName(appName string, params map[string]string) {
	keyName, ok := params["key_name"]
	if !ok || keyName == "" {
		return
	}

	appConfig, err := configMgr.GetAppConfig(appName)
	if err != nil {
		return
	}

	profile, ok := appConfig.Profiles["default"]
	if !ok {
		return
	}

	profile.Auth.KeyName = keyName
	appConfig.Profiles["default"] = profile
	if err := configMgr.SaveAppConfig(appConfig); err != nil {
		fmt.Printf("Warning: failed to save API key name: %v\n", err)
	}
}

// printInstallResult prints the installation result.
func printInstallResult(appName string, result *config.InstallResult) {
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
}

// installCmdFlags holds the flag values for the install command.
type installCmdFlags struct {
	specSource  string
	baseURL     string
	description string
	authType    string
	createShim  bool
	force       bool
	interactive bool
}

// runInstallCmd executes the install command logic.
func runInstallCmd(appName string, flags *installCmdFlags) error {
	opts := config.InstallOptions{
		SpecSource:  flags.specSource,
		BaseURL:     flags.baseURL,
		Description: flags.description,
		AuthType:    flags.authType,
		CreateShim:  flags.createShim,
		Force:       flags.force,
		Interactive: flags.interactive,
		Reader:      os.Stdin,
		Writer:      os.Stdout,
	}

	if flags.interactive {
		var err error
		opts, err = prepareInteractiveInstall(appName, opts)
		if err != nil {
			return err
		}
	} else if flags.specSource == "" {
		return fmt.Errorf("--spec flag is required (or use -i for interactive mode)")
	}

	result, err := configMgr.InstallApp(appName, opts)
	if err != nil {
		return fmt.Errorf("installation failed: %w", err)
	}

	storeInstallCredentials(appName, opts)
	printInstallResult(appName, result)

	return nil
}

// newInstallCmd creates the install subcommand
func newInstallCmd() *cobra.Command {
	flags := &installCmdFlags{createShim: true}

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
		RunE: func(_ *cobra.Command, args []string) error {
			return runInstallCmd(args[0], flags)
		},
	}

	cmd.Flags().StringVarP(&flags.specSource, "spec", "s", "", "Path or URL to the OpenAPI specification")
	cmd.Flags().StringVar(&flags.baseURL, "base-url", "", "Base URL for API requests (overrides spec)")
	cmd.Flags().StringVar(&flags.description, "description", "", "Description of the application")
	cmd.Flags().StringVar(&flags.authType, "auth", "", "Authentication type: none, bearer, api_key, basic")
	cmd.Flags().BoolVar(&flags.createShim, "shim", true, "Create command shortcut (shim)")
	cmd.Flags().BoolVarP(&flags.force, "force", "f", false, "Overwrite existing app configuration")
	cmd.Flags().BoolVarP(&flags.interactive, "interactive", "i", false, "Interactive installation mode")

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
				// Wrap with PrintedError to prevent double printing
				return &cli.PrintedError{Err: fmt.Errorf("app not found: %s", appName)}
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

// printAppDetails prints detailed information for a single app.
func printAppDetails(info *config.InstalledAppInfo) {
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

// printAppsDetailed prints detailed view of all apps.
func printAppsDetailed(apps []string) {
	for i, appName := range apps {
		info, err := configMgr.GetInstalledAppInfo(appName)
		if err != nil {
			fmt.Printf("Error loading app '%s': %v\n", appName, err)
			continue
		}
		if i > 0 {
			fmt.Println()
		}
		printAppDetails(info)
	}
}

// printAppsTable prints table view of all apps.
func printAppsTable(apps []string) {
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
				printAppsDetailed(apps)
			} else {
				printAppsTable(apps)
			}

			return nil
		},
	}

	cmd.Flags().BoolVarP(&showDetails, "details", "d", false, "Show detailed information")

	return cmd
}

// newInfoCmd creates the info subcommand to show app configuration
func newInfoCmd() *cobra.Command {
	var outputFormat string

	cmd := &cobra.Command{
		Use:   "info <app-name>",
		Short: "Show configuration for an installed application",
		Long: `Show detailed configuration for an installed API application.

This command displays all configuration settings including:
  - Spec source and API information
  - Profile configurations (base URL, auth, TLS, headers)
  - MCP/AI safety settings
  - Metadata and timestamps

Example:
  ob info petstore
  ob info petstore -o yaml
  ob info petstore -o json`,
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: completeAppNames,
		RunE: func(cmd *cobra.Command, args []string) error {
			appName := args[0]

			if !configMgr.AppExists(appName) {
				return fmt.Errorf("app '%s' not found", appName)
			}

			appConfig, err := configMgr.GetAppConfig(appName)
			if err != nil {
				return fmt.Errorf("failed to get app config: %w", err)
			}

			return printAppConfig(appConfig, outputFormat)
		},
	}

	cmd.Flags().StringVarP(&outputFormat, "output", "o", "text", "Output format: text, json, yaml")

	return cmd
}

// printAppConfig prints the app configuration in the specified format.
func printAppConfig(cfg *config.AppConfig, format string) error {
	switch format {
	case "json":
		return printAppConfigJSON(cfg)
	case "yaml":
		return printAppConfigYAML(cfg)
	default:
		printAppConfigText(cfg)
		return nil
	}
}

// printAppConfigJSON prints the app configuration as JSON.
func printAppConfigJSON(cfg *config.AppConfig) error {
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal config to JSON: %w", err)
	}
	fmt.Println(string(data))
	return nil
}

// printAppConfigYAML prints the app configuration as YAML.
func printAppConfigYAML(cfg *config.AppConfig) error {
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("failed to marshal config to YAML: %w", err)
	}
	fmt.Print(string(data))
	return nil
}

// printAppConfigText prints the app configuration as human-readable text.
func printAppConfigText(cfg *config.AppConfig) {
	fmt.Printf("Application: %s\n", cfg.Name)
	fmt.Println(strings.Repeat("=", 50))

	// Basic info
	fmt.Println("\n📋 Basic Information")
	fmt.Printf("  Description:     %s\n", valueOrNone(cfg.Description))
	fmt.Printf("  Spec Source:     %s\n", cfg.SpecSource)
	if len(cfg.SpecSources) > 0 {
		fmt.Printf("  Additional Specs: %s\n", strings.Join(cfg.SpecSources, ", "))
	}
	fmt.Printf("  Version:         %s\n", valueOrNone(cfg.Version))
	fmt.Printf("  Operations:      %d\n", cfg.OperationCount)
	if !cfg.CreatedAt.IsZero() {
		fmt.Printf("  Created:         %s\n", cfg.CreatedAt.Format("2006-01-02 15:04:05"))
	}
	if !cfg.UpdatedAt.IsZero() {
		fmt.Printf("  Updated:         %s\n", cfg.UpdatedAt.Format("2006-01-02 15:04:05"))
	}

	// Profiles
	fmt.Printf("\n👤 Profiles (%d)\n", len(cfg.Profiles))
	fmt.Printf("  Default Profile: %s\n", cfg.DefaultProfile)

	for name, profile := range cfg.Profiles {
		fmt.Printf("\n  [%s]\n", name)
		printProfileDetails(&profile, "    ")
	}

	// Metadata
	if len(cfg.Metadata) > 0 {
		fmt.Println("\n📝 Metadata")
		for k, v := range cfg.Metadata {
			fmt.Printf("  %s: %s\n", k, v)
		}
	}
}

// printProfileDetails prints the details of a profile.
func printProfileDetails(p *config.Profile, indent string) {
	fmt.Printf("%sBase URL:    %s\n", indent, valueOrNone(p.BaseURL))
	fmt.Printf("%sDescription: %s\n", indent, valueOrNone(p.Description))

	printProfileAuth(p, indent)
	printProfileTLS(p, indent)
	printProfileHeaders(p, indent)
	printProfileSafety(p, indent)

	if p.Timeout.Duration > 0 {
		fmt.Printf("%sTimeout:     %s\n", indent, p.Timeout.String())
	}
}

// printProfileAuth prints authentication configuration.
func printProfileAuth(p *config.Profile, indent string) {
	if p.Auth.Type != "" && p.Auth.Type != "none" {
		fmt.Printf("%sAuth Type:   %s\n", indent, p.Auth.Type)
		if p.Auth.KeyName != "" {
			fmt.Printf("%sAuth Key:    %s\n", indent, p.Auth.KeyName)
		}
	}
}

// printProfileTLS prints TLS configuration.
func printProfileTLS(p *config.Profile, indent string) {
	if !p.TLSConfig.InsecureSkipVerify && p.TLSConfig.CAFile == "" && p.TLSConfig.CertFile == "" {
		return
	}
	fmt.Printf("%sTLS:\n", indent)
	if p.TLSConfig.InsecureSkipVerify {
		fmt.Printf("%s  Skip Verify: true (insecure)\n", indent)
	}
	if p.TLSConfig.CAFile != "" {
		fmt.Printf("%s  CA File:     %s\n", indent, p.TLSConfig.CAFile)
	}
	if p.TLSConfig.CertFile != "" {
		fmt.Printf("%s  Cert File:   %s\n", indent, p.TLSConfig.CertFile)
	}
	if p.TLSConfig.KeyFile != "" {
		fmt.Printf("%s  Key File:    %s\n", indent, p.TLSConfig.KeyFile)
	}
}

// printProfileHeaders prints custom headers configuration.
func printProfileHeaders(p *config.Profile, indent string) {
	if len(p.Headers) == 0 {
		return
	}
	fmt.Printf("%sHeaders:\n", indent)
	for k, v := range p.Headers {
		displayValue := v
		if isSensitiveHeader(k) {
			displayValue = maskValue(v)
		}
		fmt.Printf("%s  %s: %s\n", indent, k, displayValue)
	}
}

// printProfileSafety prints MCP safety configuration.
func printProfileSafety(p *config.Profile, indent string) {
	sc := &p.SafetyConfig
	if !sc.ReadOnlyMode && !sc.ProgressiveDisclosure &&
		len(sc.AllowedOperations) == 0 && len(sc.DeniedOperations) == 0 {
		return
	}
	fmt.Printf("%sSafety (MCP):\n", indent)
	if sc.ReadOnlyMode {
		fmt.Printf("%s  Read-Only Mode:          true\n", indent)
	}
	if sc.ProgressiveDisclosure {
		fmt.Printf("%s  Progressive Disclosure:  true\n", indent)
	}
	if sc.SearchEngine != "" {
		fmt.Printf("%s  Search Engine:           %s\n", indent, sc.SearchEngine)
	}
	if len(sc.AllowedOperations) > 0 {
		fmt.Printf("%s  Allowed Operations:      %s\n", indent, strings.Join(sc.AllowedOperations, ", "))
	}
	if len(sc.DeniedOperations) > 0 {
		fmt.Printf("%s  Denied Operations:       %s\n", indent, strings.Join(sc.DeniedOperations, ", "))
	}
}

// valueOrNone returns the value if non-empty, otherwise returns "(none)".
func valueOrNone(value string) string {
	if value == "" {
		return "(none)"
	}
	return value
}

// isSensitiveHeader checks if a header name is sensitive.
func isSensitiveHeader(name string) bool {
	sensitive := []string{"authorization", "x-api-key", "api-key", "token", "secret", "password"}
	nameLower := strings.ToLower(name)
	for _, s := range sensitive {
		if strings.Contains(nameLower, s) {
			return true
		}
	}
	return false
}

// maskValue masks a sensitive value, showing only first and last 2 chars.
func maskValue(value string) string {
	if len(value) <= 4 {
		return "****"
	}
	return value[:2] + strings.Repeat("*", len(value)-4) + value[len(value)-2:]
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
			isMCPMode := slices.Contains(args[1:], "--mcp")

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

// mcpServerOptions holds parsed MCP server options.
type mcpServerOptions struct {
	profileName string
	transport   string
	port        string
}

// parseMCPServerArgs parses MCP server arguments.
func parseMCPServerArgs(args []string, defaultProfile string) mcpServerOptions {
	opts := mcpServerOptions{
		transport: "stdio",
		port:      "8080",
	}

	for i, arg := range args {
		opts.profileName = parseArgValue(arg, args, i, "--profile", "-p", opts.profileName)
		opts.transport = parseArgValue(arg, args, i, "--transport", "", opts.transport)
		opts.port = parseArgValue(arg, args, i, "--port", "", opts.port)
	}

	if opts.profileName == "" {
		opts.profileName = defaultProfile
	}
	return opts
}

// parseArgValue parses a flag value from args.
func parseArgValue(arg string, args []string, i int, longFlag, shortFlag, current string) string {
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

// startMCPServer starts the MCP server for an app
func startMCPServer(appConfig *config.AppConfig, args []string) error {
	specDoc, err := specParser.LoadSpec(appConfig.SpecSource)
	if err != nil {
		return fmt.Errorf("failed to load spec: %w", err)
	}

	opts := parseMCPServerArgs(args, appConfig.DefaultProfile)

	profile, ok := appConfig.GetProfile(opts.profileName)
	if !ok {
		return fmt.Errorf("profile '%s' not found", opts.profileName)
	}

	factory := mcp.NewServerFactory(appConfig.Name, version)
	server := factory.CreateServer()

	// Check if progressive disclosure mode is enabled
	if profile.SafetyConfig.ProgressiveDisclosure {
		// Use ProgressiveHandler for progressive disclosure mode
		engineType, err := parseSearchEngineType(profile.SafetyConfig.SearchEngine)
		if err != nil {
			return fmt.Errorf("failed to parse search engine type: %w", err)
		}

		progressiveHandler, err := mcp.NewProgressiveHandler(reqBuilder, nil, engineType)
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
		mcpHandler.SetSpec(specDoc)
		mcpHandler.SetAppConfig(appConfig, opts.profileName)
		mcpHandler.Register(server, &profile.SafetyConfig)

		fmt.Fprintf(os.Stderr, "Starting MCP server for app '%s' (profile: %s) via %s...\n", appConfig.Name, opts.profileName, opts.transport)
	}

	return factory.RunServer(context.Background(), server, opts.transport, opts.port)
}

// parseSearchEngineType parses the search engine type from config string.
func parseSearchEngineType(s string) (mcp.SearchEngineType, error) {
	if s == "" {
		return mcp.SearchEnginePredicate, nil // Default to predicate
	}
	return mcp.ParseSearchEngineType(s)
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
