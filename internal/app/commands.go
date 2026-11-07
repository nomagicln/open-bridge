package app

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"
	"text/tabwriter"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/nomagicln/open-bridge/pkg/cli"
	"github.com/nomagicln/open-bridge/pkg/config"
	"github.com/nomagicln/open-bridge/pkg/credential"
	"github.com/nomagicln/open-bridge/pkg/mcp"
	installTui "github.com/nomagicln/open-bridge/pkg/tui/install"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

// prepareInteractiveInstall handles the interactive TUI installation flow.
func (d *Deps) prepareInteractiveInstall(appName string, opts config.InstallOptions) (config.InstallOptions, error) {
	appExists := d.Config.AppExists(appName)

	// Load existing config to use as defaults if app exists
	if appExists {
		existingConfig, err := d.Config.GetAppConfig(appName)
		if err == nil {
			opts = d.mergeExistingConfig(opts, existingConfig)
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
func (d *Deps) mergeExistingConfig(opts config.InstallOptions, existing *config.AppConfig) config.InstallOptions {
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
func (d *Deps) storeInstallCredentials(appName string, opts config.InstallOptions) {
	if len(opts.AuthParams) == 0 {
		return
	}

	cred := d.createCredentialFromParams(opts.AuthType, opts.AuthParams)
	if cred == nil {
		return
	}

	// Handle API key name update
	if opts.AuthType == "api_key" {
		d.updateAPIKeyName(appName, opts.AuthParams)
	}

	if d.Credential != nil {
		if err := d.Credential.StoreCredential(appName, "default", cred); err != nil {
			fmt.Printf("Warning: failed to store credentials: %v\n", err)
		} else {
			fmt.Printf("  Credentials stored securely in %s\n", d.Credential.Backend())
		}
	}
}

// createCredentialFromParams creates a credential from auth parameters.
func (d *Deps) createCredentialFromParams(authType string, params map[string]string) *credential.Credential {
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
func (d *Deps) updateAPIKeyName(appName string, params map[string]string) {
	keyName, ok := params["key_name"]
	if !ok || keyName == "" {
		return
	}

	appConfig, err := d.Config.GetAppConfig(appName)
	if err != nil {
		return
	}

	profile, ok := appConfig.Profiles["default"]
	if !ok {
		return
	}

	profile.Auth.KeyName = keyName
	appConfig.Profiles["default"] = profile
	if err := d.Config.SaveAppConfig(appConfig); err != nil {
		fmt.Printf("Warning: failed to save API key name: %v\n", err)
	}
}

// printInstallResult prints the installation result.
func (d *Deps) printInstallResult(appName string, result *config.InstallResult) {
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
		fmt.Printf("  %s <resource> <verb> [flags]\n", appName)
	} else {
		fmt.Printf("  ob run %s <resource> <verb> [flags]\n", appName)
	}
	fmt.Printf("  ob run %s --help\n", appName)
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
func (d *Deps) runInstallCmd(appName string, flags *installCmdFlags) error {
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
		opts, err = d.prepareInteractiveInstall(appName, opts)
		if err != nil {
			return err
		}
	} else if flags.specSource == "" {
		return fmt.Errorf("--spec flag is required (or use -i for interactive mode)")
	}

	result, err := d.Config.InstallApp(appName, opts)
	if err != nil {
		return fmt.Errorf("installation failed: %w", err)
	}

	d.storeInstallCredentials(appName, opts)
	d.printInstallResult(appName, result)

	return nil
}

// newInstallCmd creates the install subcommand
func (d *Deps) newInstallCmd() *cobra.Command {
	flags := &installCmdFlags{createShim: true}

	cmd := &cobra.Command{
		Use:   "install <app-name>",
		Short: "Install an API as a CLI application",
		Long: `Install an OpenAPI specification as a CLI application.
The specification can be provided as a local file path or a remote URL.

After installation, you can call API operations using either:
  - Automatic parameter classification (recommended):
    ob run myapp users list
    myapp users create --name "John"
  - Explicit parameter classification with '--' separator (use prefixes):
    ob run myapp pets get -- --path:petId=123

Example:
  ob install myapi --spec ./openapi.yaml
  ob install petstore --spec https://petstore.swagger.io/v2/swagger.json
  ob install myapi -i  # Interactive mode`,
		Args: cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			return d.runInstallCmd(args[0], flags)
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
func (d *Deps) newUninstallCmd() *cobra.Command {
	var removeShim bool

	cmd := &cobra.Command{
		Use:   "uninstall <app-name>",
		Short: "Uninstall an API application",
		Long: `Uninstall a previously installed API application.
This removes the application configuration and any created shims.

Example:
  ob uninstall myapi`,
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: d.completeAppNames,
		RunE: func(cmd *cobra.Command, args []string) error {
			return d.uninstallApp(args[0], removeShim)
		},
	}

	cmd.Flags().BoolVar(&removeShim, "remove-shim", true, "Remove the command shortcut (shim)")

	return cmd
}

// uninstallApp uninstalls an app by name.
func (d *Deps) uninstallApp(appName string, removeShim bool) error {
	if !d.Config.AppExists(appName) {
		d.showAppNotFoundError(appName)
		return &cli.PrintedError{Err: fmt.Errorf("app not found: %s", appName)}
	}

	err := d.Config.UninstallApp(appName, removeShim)
	if err != nil {
		return fmt.Errorf("uninstallation failed: %w", err)
	}

	fmt.Printf("✓ Successfully uninstalled app '%s'\n", appName)
	return nil
}

// showAppNotFoundError displays helpful error when app is not found.
func (d *Deps) showAppNotFoundError(appName string) {
	apps, _ := d.Config.ListApps()
	if len(apps) > 0 {
		fmt.Fprintf(os.Stderr, "App '%s' not found.\n\nInstalled apps:\n", appName)
		for _, app := range apps {
			fmt.Fprintf(os.Stderr, "  %s\n", app)
		}
	} else {
		fmt.Fprintf(os.Stderr, "App '%s' not found. No apps are currently installed.\n", appName)
	}
}

// printAppDetails prints detailed information for a single app.
func (d *Deps) printAppDetails(info *config.InstalledAppInfo) {
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
func (d *Deps) printAppsDetailed(apps []string) {
	for i, appName := range apps {
		info, err := d.Config.GetInstalledAppInfo(appName)
		if err != nil {
			fmt.Printf("Error loading app '%s': %v\n", appName, err)
			continue
		}
		if i > 0 {
			fmt.Println()
		}
		d.printAppDetails(info)
	}
}

// printAppsTable prints table view of all apps.
func (d *Deps) printAppsTable(apps []string) {
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(w, "NAME\tDESCRIPTION\tPROFILES\tSHIM")
	_, _ = fmt.Fprintln(w, "----\t-----------\t--------\t----")

	for _, appName := range apps {
		info, err := d.Config.GetInstalledAppInfo(appName)
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
func (d *Deps) newListCmd() *cobra.Command {
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
			return d.listApps(showDetails)
		},
	}

	cmd.Flags().BoolVarP(&showDetails, "details", "d", false, "Show detailed information")

	return cmd
}

// listApps lists all installed apps.
func (d *Deps) listApps(showDetails bool) error {
	apps, err := d.Config.ListApps()
	if err != nil {
		return fmt.Errorf("failed to list apps: %w", err)
	}

	if len(apps) == 0 {
		d.showEmptyStateMessage()
		return nil
	}

	d.displayApps(apps, showDetails)
	return nil
}

// showEmptyStateMessage shows message when no apps are installed.
func (d *Deps) showEmptyStateMessage() {
	fmt.Println("No applications installed.")
	fmt.Println("\nTo install an app, run:")
	fmt.Println("  ob install <app-name> --spec <openapi-spec>")
}

// displayApps displays the list of apps.
func (d *Deps) displayApps(apps []string, showDetails bool) {
	if showDetails {
		d.printAppsDetailed(apps)
	} else {
		d.printAppsTable(apps)
	}
}

// newInfoCmd creates the info subcommand to show app configuration
func (d *Deps) newInfoCmd() *cobra.Command {
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
		ValidArgsFunction: d.completeAppNames,
		RunE: func(cmd *cobra.Command, args []string) error {
			return d.showAppInfo(args[0], outputFormat)
		},
	}

	cmd.Flags().StringVarP(&outputFormat, "output", "o", "text", "Output format: text, json, yaml")

	return cmd
}

// showAppInfo shows detailed information about an app.
func (d *Deps) showAppInfo(appName, outputFormat string) error {
	if !d.Config.AppExists(appName) {
		return fmt.Errorf("app '%s' not found", appName)
	}

	appConfig, err := d.Config.GetAppConfig(appName)
	if err != nil {
		return fmt.Errorf("failed to get app config: %w", err)
	}

	return d.printAppConfig(appConfig, outputFormat)
}

// printAppConfig prints the app configuration in the specified format.
func (d *Deps) printAppConfig(cfg *config.AppConfig, format string) error {
	switch format {
	case "json":
		return d.printAppConfigJSON(os.Stdout, cfg)
	case "yaml":
		return d.printAppConfigYAML(os.Stdout, cfg)
	default:
		d.printAppConfigText(cfg)
		return nil
	}
}

// redactAppConfig returns a deep copy of cfg with sensitive header values
// replaced by masked placeholders, suitable for machine-readable output.
// The original configuration is never modified.
func (d *Deps) redactAppConfig(cfg *config.AppConfig) *config.AppConfig {
	if cfg == nil {
		return nil
	}

	redacted := *cfg
	if cfg.Profiles == nil {
		return &redacted
	}

	redacted.Profiles = make(map[string]config.Profile, len(cfg.Profiles))
	for name, profile := range cfg.Profiles {
		cloned := profile
		cloned.Headers = d.redactHeaders(profile.Headers)
		cloned.SpecFetchHeaders = d.redactHeaders(profile.SpecFetchHeaders)
		redacted.Profiles[name] = cloned
	}

	return &redacted
}

// redactHeaders returns a copy of headers with sensitive values masked.
func (d *Deps) redactHeaders(headers map[string]string) map[string]string {
	if headers == nil {
		return nil
	}

	redacted := make(map[string]string, len(headers))
	for key, value := range headers {
		if d.isSensitiveHeader(key) {
			redacted[key] = d.maskValue(value)
			continue
		}
		redacted[key] = value
	}

	return redacted
}

// printAppConfigJSON prints the app configuration as JSON.
func (d *Deps) printAppConfigJSON(w io.Writer, cfg *config.AppConfig) error {
	data, err := json.MarshalIndent(d.redactAppConfig(cfg), "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal config to JSON: %w", err)
	}
	if _, err := fmt.Fprintln(w, string(data)); err != nil {
		return fmt.Errorf("failed to write config as JSON: %w", err)
	}
	return nil
}

// printAppConfigYAML prints the app configuration as YAML.
func (d *Deps) printAppConfigYAML(w io.Writer, cfg *config.AppConfig) error {
	data, err := yaml.Marshal(d.redactAppConfig(cfg))
	if err != nil {
		return fmt.Errorf("failed to marshal config to YAML: %w", err)
	}
	if _, err := fmt.Fprint(w, string(data)); err != nil {
		return fmt.Errorf("failed to write config as YAML: %w", err)
	}
	return nil
}

// printAppConfigText prints the app configuration as human-readable text.
func (d *Deps) printAppConfigText(cfg *config.AppConfig) {
	fmt.Printf("Application: %s\n", cfg.Name)
	fmt.Println(strings.Repeat("=", 50))

	// Basic info
	fmt.Println("\n📋 Basic Information")
	fmt.Printf("  Description:     %s\n", d.valueOrNone(cfg.Description))
	if cfg.SpecTitle != "" || cfg.SpecVersion != "" {
		api := cfg.SpecTitle
		if cfg.SpecVersion != "" {
			api = strings.TrimSpace(api + " (v" + cfg.SpecVersion + ")")
		}
		fmt.Printf("  API:             %s\n", d.valueOrNone(api))
	}
	fmt.Printf("  Spec Source:     %s\n", cfg.SpecSource)
	if len(cfg.SpecSources) > 0 {
		fmt.Printf("  Additional Specs: %s\n", strings.Join(cfg.SpecSources, ", "))
	}
	fmt.Printf("  Version:         %s\n", d.valueOrNone(cfg.Version))
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
		d.printProfileDetails(&profile, "    ")
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
func (d *Deps) printProfileDetails(p *config.Profile, indent string) {
	fmt.Printf("%sBase URL:    %s\n", indent, d.valueOrNone(p.BaseURL))
	fmt.Printf("%sDescription: %s\n", indent, d.valueOrNone(p.Description))

	d.printProfileAuth(p, indent)
	d.printProfileTLS(p, indent)
	d.printProfileHeaders(p, indent)
	d.printProfileSafety(p, indent)

	if p.Timeout.Duration > 0 {
		fmt.Printf("%sTimeout:     %s\n", indent, p.Timeout.String())
	}
}

// printProfileAuth prints authentication configuration.
func (d *Deps) printProfileAuth(p *config.Profile, indent string) {
	if p.Auth.Type != "" && p.Auth.Type != "none" {
		fmt.Printf("%sAuth Type:   %s\n", indent, p.Auth.Type)
		if p.Auth.KeyName != "" {
			fmt.Printf("%sAuth Key:    %s\n", indent, p.Auth.KeyName)
		}
	}
}

// printProfileTLS prints TLS configuration.
func (d *Deps) printProfileTLS(p *config.Profile, indent string) {
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
func (d *Deps) printProfileHeaders(p *config.Profile, indent string) {
	if len(p.Headers) == 0 {
		return
	}
	fmt.Printf("%sHeaders:\n", indent)
	for k, v := range p.Headers {
		displayValue := v
		if d.isSensitiveHeader(k) {
			displayValue = d.maskValue(v)
		}
		fmt.Printf("%s  %s: %s\n", indent, k, displayValue)
	}
}

// printProfileSafety prints MCP safety configuration.
func (d *Deps) printProfileSafety(p *config.Profile, indent string) {
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
func (d *Deps) valueOrNone(value string) string {
	if value == "" {
		return "(none)"
	}
	return value
}

// isSensitiveHeader checks if a header name is sensitive.
func (d *Deps) isSensitiveHeader(name string) bool {
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
func (d *Deps) maskValue(value string) string {
	if len(value) <= 4 {
		return "****"
	}
	return value[:2] + strings.Repeat("*", len(value)-4) + value[len(value)-2:]
}

// newRunCmd creates the run subcommand for running app commands
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

// runCommand executes the run command logic.
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

// validateAppExists checks if the app exists and shows helpful error if not.
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

// loadAppConfig loads the app configuration.
func (d *Deps) loadAppConfig(appName string) (*config.AppConfig, error) {
	appConfig, err := d.Config.GetAppConfig(appName)
	if err != nil {
		return nil, fmt.Errorf("failed to load app config: %w", err)
	}
	return appConfig, nil
}

// executeRunMode executes the appropriate run mode (CLI or MCP).
func (d *Deps) executeRunMode(args []string, appConfig *config.AppConfig) error {
	isMCPMode := slices.Contains(args[1:], "--mcp")

	if isMCPMode {
		return d.startMCPServer(appConfig, args[1:])
	}

	return d.CLI.ExecuteCommand(appConfig.Name, appConfig, args[1:])
}

// mcpServerOptions holds parsed MCP server options.
type mcpServerOptions struct {
	profileName string
	transport   string
	port        string
}

// parseMCPServerArgs parses MCP server arguments.
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

// parseArgValue parses a flag value from args.
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

// startMCPServer starts the MCP server for an app
func (d *Deps) startMCPServer(appConfig *config.AppConfig, args []string) error {
	specDoc, err := d.Parser.LoadSpec(appConfig.SpecSource)
	if err != nil {
		return fmt.Errorf("failed to load spec: %w", err)
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

// parseSearchEngineType parses the search engine type from config string.
func (d *Deps) parseSearchEngineType(s string) (mcp.SearchEngineType, error) {
	if s == "" {
		return mcp.SearchEnginePredicate, nil // Default to predicate
	}
	return mcp.ParseSearchEngineType(s)
}

// completeAppNames provides completion for installed app names
func (d *Deps) completeAppNames(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	apps := d.Completion.CompleteAppNames(toComplete)
	return apps, cobra.ShellCompDirectiveNoFileComp
}

// completeRunArgs provides completion for the run command
func (d *Deps) completeRunArgs(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	if len(args) == 0 {
		return d.completeAppNames(cmd, args, toComplete)
	}

	appName := args[0]
	if !d.Config.AppExists(appName) {
		return nil, cobra.ShellCompDirectiveError
	}

	if len(args) == 1 {
		return d.completeResources(appName, toComplete)
	}

	if len(args) == 2 {
		return d.completeVerbs(appName, args[1], toComplete)
	}

	if values, directive, ok := d.completeFlagValue(appName, args, toComplete); ok {
		return values, directive
	}

	return d.completeFlags(appName, args[1], args[2], toComplete)
}

// flagValueCompletionContext reports whether the shell is completing the value
// of a flag. It returns the flag name (with leading dashes), the value prefix to
// filter by, and whether the "flag=prefix" form is being completed.
func flagValueCompletionContext(args []string, toComplete string) (flagName, prefix string, equalsForm, ok bool) {
	// " --status=<TAB>" and "--status=av<TAB>" are completed in place.
	if strings.HasPrefix(toComplete, "--") {
		name, valuePrefix, found := strings.Cut(toComplete, "=")
		if found && len(name) > 2 {
			return name, valuePrefix, true, true
		}

		return "", "", false, false
	}

	// "--status <TAB>": the flag is the last completed word and the value is
	// still empty.
	if toComplete == "" && len(args) > 0 {
		last := args[len(args)-1]
		if strings.HasPrefix(last, "--") && !strings.Contains(last, "=") && len(last) > 2 {
			return last, "", false, true
		}
	}

	return "", "", false, false
}

// completeFlagValue completes the value of a flag, returning ok=false when the
// completion should fall back to flag-name completion.
func (d *Deps) completeFlagValue(appName string, args []string, toComplete string) ([]string, cobra.ShellCompDirective, bool) {
	flagName, prefix, equalsForm, ok := flagValueCompletionContext(args, toComplete)
	if !ok || d.Completion == nil {
		return nil, cobra.ShellCompDirectiveDefault, false
	}

	resource, verb := args[1], args[2]
	values := d.Completion.CompleteFlagValues(appName, resource, verb, flagName)
	if len(values) == 0 {
		if equalsForm {
			// No known values for "flag=": keep file-name completion disabled.
			return nil, cobra.ShellCompDirectiveNoFileComp, true
		}

		return nil, cobra.ShellCompDirectiveDefault, false
	}

	completions := make([]string, 0, len(values))

	for _, value := range values {
		if !strings.HasPrefix(value, prefix) {
			continue
		}

		if equalsForm {
			completions = append(completions, flagName+"="+value)
			continue
		}

		completions = append(completions, value)
	}

	if len(completions) == 0 && !equalsForm {
		return nil, cobra.ShellCompDirectiveDefault, false
	}

	return completions, cobra.ShellCompDirectiveNoFileComp, true
}

// completeResources completes resource names.
func (d *Deps) completeResources(appName, toComplete string) ([]string, cobra.ShellCompDirective) {
	resources := d.Completion.CompleteResources(appName, toComplete)
	return resources, cobra.ShellCompDirectiveNoFileComp
}

// completeVerbs completes verb names for a resource.
func (d *Deps) completeVerbs(appName, resource, toComplete string) ([]string, cobra.ShellCompDirective) {
	verbs := d.Completion.CompleteVerbsForResource(appName, resource, toComplete)
	return verbs, cobra.ShellCompDirectiveNoFileComp
}

// completeFlags completes flag names.
func (d *Deps) completeFlags(appName, resource, verb, toComplete string) ([]string, cobra.ShellCompDirective) {
	flags := d.Completion.CompleteFlags(appName, resource, verb, toComplete)
	return flags, cobra.ShellCompDirectiveNoFileComp
}

// newCompletionCmd creates the completion subcommand
func (d *Deps) newCompletionCmd() *cobra.Command {
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
