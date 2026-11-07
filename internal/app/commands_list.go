// Code moved from commands.go during the batch-5 app split.
// Behavior is intentionally unchanged; only the file layout differs.
package app

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/nomagicln/open-bridge/internal/install"
	"github.com/nomagicln/open-bridge/pkg/config"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

func (d *Deps) printAppDetails(info *install.InstalledAppInfo) {
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

func (d *Deps) printAppsDetailed(apps []string) {
	for i, appName := range apps {
		info, err := d.Installer.GetInstalledAppInfo(appName)
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

func (d *Deps) printAppsTable(apps []string) {
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(w, "NAME\tDESCRIPTION\tPROFILES\tSHIM")
	_, _ = fmt.Fprintln(w, "----\t-----------\t--------\t----")

	for _, appName := range apps {
		info, err := d.Installer.GetInstalledAppInfo(appName)
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

func (d *Deps) showEmptyStateMessage() {
	fmt.Println("No applications installed.")
	fmt.Println("\nTo install an app, run:")
	fmt.Println("  ob install <app-name> --spec <openapi-spec>")
}

func (d *Deps) displayApps(apps []string, showDetails bool) {
	if showDetails {
		d.printAppsDetailed(apps)
	} else {
		d.printAppsTable(apps)
	}
}

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

func (d *Deps) printProfileAuth(p *config.Profile, indent string) {
	if p.Auth.Type != "" && p.Auth.Type != "none" {
		fmt.Printf("%sAuth Type:   %s\n", indent, p.Auth.Type)
		if p.Auth.KeyName != "" {
			fmt.Printf("%sAuth Key:    %s\n", indent, p.Auth.KeyName)
		}
	}
}

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

func (d *Deps) valueOrNone(value string) string {
	if value == "" {
		return "(none)"
	}
	return value
}

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

func (d *Deps) maskValue(value string) string {
	if len(value) <= 4 {
		return "****"
	}
	return value[:2] + strings.Repeat("*", len(value)-4) + value[len(value)-2:]
}
