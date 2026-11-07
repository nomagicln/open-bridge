// Package app wires the OpenBridge CLI dependency graph and command surface.
//
// The package is the composition root for the CLI: dependencies are constructed
// explicitly in New instead of living in package-level variables, which keeps
// the command layer testable and makes initialization failures explicit.
package app

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/nomagicln/open-bridge/internal/install"
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

// BuildInfo describes the build metadata injected through ldflags.
type BuildInfo struct {
	Version string
	Commit  string
	Date    string
}

// buildInfo is populated by SetBuildInfo before New/Run are called.
var buildInfo = BuildInfo{Version: "dev", Commit: "unknown", Date: "unknown"}

// SetBuildInfo records the linker-provided build metadata used by the CLI.
func SetBuildInfo(version, commit, date string) {
	buildInfo = BuildInfo{Version: version, Commit: commit, Date: date}
}

// Deps holds the explicitly constructed CLI dependencies.
type Deps struct {
	Config     *config.Manager
	Installer  *install.Installer
	Parser     *spec.Parser
	Credential *credential.Manager
	Mapper     *semantic.Mapper
	Builder    *request.Builder
	CLI        *cli.Handler
	MCP        *mcp.Handler
	Router     *router.Router
	Completion *completion.Provider
}

// New constructs the CLI dependency graph.
//
// Failure policy matches the previous init(): a configuration manager failure is
// fatal (returned to the caller), while an unavailable credential manager only
// degrades authentication with a warning.
func New() (*Deps, error) {
	configMgr, err := config.NewManager()
	if err != nil {
		return nil, fmt.Errorf("failed to initialize config manager: %w", err)
	}

	// Spec parser. Remote specs are cached under the config directory so that
	// later commands can reload them without a network round-trip (and can fall
	// back to the cached copy when the remote source is unreachable).
	specParser := spec.NewParser(
		spec.WithRemoteCache(filepath.Join(configMgr.ConfigDir(), "spec-cache")),
		spec.WithIndexCache(filepath.Join(configMgr.ConfigDir(), "index-cache")),
	)

	// Credential manager; auth is limited when the system keyring is unavailable.
	credentialMgr, err := credential.NewManager()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: credential manager unavailable: %v\n", err)
	}

	mapper := semantic.NewMapper()
	reqBuilder := request.NewBuilder(credentialMgr)
	cliHandler := cli.NewHandler(specParser, mapper, reqBuilder, configMgr)
	mcpHandler := mcp.NewHandler(mapper, reqBuilder, nil)
	appRouter := router.NewRouter(cliHandler, mcpHandler, configMgr, specParser)
	completionProvider := completion.NewProvider(configMgr, specParser, mapper)
	installer := install.New(configMgr)

	return &Deps{
		Config:     configMgr,
		Installer:  installer,
		Parser:     specParser,
		Credential: credentialMgr,
		Mapper:     mapper,
		Builder:    reqBuilder,
		CLI:        cliHandler,
		MCP:        mcpHandler,
		Router:     appRouter,
		Completion: completionProvider,
	}, nil
}

// Run constructs the dependencies and executes the CLI with the provided argv.
// argv[0] is the binary path, matching os.Args.
func Run(argv []string) error {
	deps, err := New()
	if err != nil {
		return err
	}
	return deps.Run(argv)
}

// Run executes the CLI with the provided argv.
func (d *Deps) Run(argv []string) error {
	// Check if we're running as a shim (binary name is not 'ob').
	binaryName := filepath.Base(argv[0])
	binaryName = strings.TrimSuffix(binaryName, filepath.Ext(binaryName))

	if binaryName != "ob" {
		// Shim mode: route directly to app handler.
		return d.Router.Execute(argv)
	}

	// Normal 'ob' mode: use cobra CLI.
	return d.newRootCmd().Execute()
}

// rootCmdLong is the long description of the root command.
const rootCmdLong = `OpenBridge is a universal API runtime engine that bridges OpenAPI specifications
to two distinct interfaces:
  - A semantic CLI for human users
  - An MCP server for AI agents

One Spec, Dual Interface.`

// rootCmdExample shows the supported invocation forms.
const rootCmdExample = `  ob install petstore --spec https://petstore.swagger.io/v2/swagger.json
  ob run petstore pets list
  ob petstore pets list
  ob petstore --help
  ob petstore --mcp`

// newRootCmd builds the root cobra command for 'ob' mode.
func (d *Deps) newRootCmd() *cobra.Command {
	rootCmd := &cobra.Command{
		Use:           "ob",
		Short:         "OpenBridge - Universal API Runtime Engine",
		Long:          rootCmdLong,
		Example:       rootCmdExample,
		Version:       fmt.Sprintf("%s (commit: %s, built: %s)", buildInfo.Version, buildInfo.Commit, buildInfo.Date),
		Args:          cobra.ArbitraryArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE:          d.runRootCommand,
	}

	// Stop flag parsing at the first positional argument so that app-level
	// flags (--help, --mcp, ...) reach the app handler instead of cobra.
	rootCmd.Flags().SetInterspersed(false)

	// Disable the default completion command (we'll use our custom one).
	rootCmd.CompletionOptions.DisableDefaultCmd = true

	rootCmd.PersistentFlags().StringP("profile", "p", "", "Profile to use for the operation")
	rootCmd.PersistentFlags().Bool("mcp", false, "Start in MCP server mode")
	rootCmd.PersistentFlags().StringP("output", "o", "yaml", "Output format: json, yaml")

	rootCmd.AddCommand(
		d.newInstallCmd(),
		d.newUninstallCmd(),
		d.newListCmd(),
		d.newInfoCmd(),
		d.newRunCmd(),
		d.newCompletionCmd(),
	)

	return rootCmd
}

// runRootCommand treats an unknown subcommand as an app name.
func (d *Deps) runRootCommand(cmd *cobra.Command, args []string) error {
	if len(args) == 0 {
		return cmd.Help()
	}

	// First arg might be an app name.
	appName := args[0]
	if d.Config.AppExists(appName) {
		return d.Router.ExecuteApp(appName, args[1:])
	}

	return fmt.Errorf("unknown command or app: %s\nRun 'ob --help' for usage", appName)
}
