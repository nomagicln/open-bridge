// Code moved from commands.go during the batch-5 app split.
// Behavior is intentionally unchanged; only the file layout differs.
package app

import (
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/nomagicln/open-bridge/internal/install"
	"github.com/nomagicln/open-bridge/pkg/cli"
	"github.com/nomagicln/open-bridge/pkg/config"
	"github.com/nomagicln/open-bridge/pkg/credential"
	installTui "github.com/nomagicln/open-bridge/pkg/tui/install"
	"github.com/spf13/cobra"
)

func (d *Deps) prepareInteractiveInstall(appName string, opts install.InstallOptions) (install.InstallOptions, error) {
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

func (d *Deps) mergeExistingConfig(opts install.InstallOptions, existing *config.AppConfig) install.InstallOptions {
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

func (d *Deps) storeInstallCredentials(appName string, opts install.InstallOptions) {
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

func (d *Deps) printInstallResult(appName string, result *install.InstallResult) {
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

type installCmdFlags struct {
	specSource  string
	baseURL     string
	description string
	authType    string
	createShim  bool
	force       bool
	interactive bool
}

func (d *Deps) runInstallCmd(appName string, flags *installCmdFlags) error {
	opts := install.InstallOptions{
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

	result, err := d.Installer.InstallApp(appName, opts)
	if err != nil {
		return fmt.Errorf("installation failed: %w", err)
	}

	d.storeInstallCredentials(appName, opts)
	d.printInstallResult(appName, result)

	return nil
}

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

func (d *Deps) uninstallApp(appName string, removeShim bool) error {
	if !d.Config.AppExists(appName) {
		d.showAppNotFoundError(appName)
		return &cli.PrintedError{Err: fmt.Errorf("app not found: %s", appName)}
	}

	err := d.Installer.UninstallApp(appName, removeShim)
	if err != nil {
		return fmt.Errorf("uninstallation failed: %w", err)
	}

	fmt.Printf("✓ Successfully uninstalled app '%s'\n", appName)
	return nil
}

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
