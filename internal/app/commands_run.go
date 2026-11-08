// Code moved from commands.go during the batch-5 app split.
// Behavior is intentionally unchanged; only the file layout differs.
package app

import (
	"fmt"
	"os"
	"strings"

	"github.com/nomagicln/open-bridge/pkg/router"
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
			return d.runCommand(cmd, args)
		},
	}

	return cmd
}

// runCommand runs an installed application. The router owns the execution
// path, so the CLI and the MCP server behave the same for every entry point.
func (d *Deps) runCommand(cmd *cobra.Command, args []string) error {
	options, appArgs := splitRootOptions(args)

	appName := appArgs[0]
	if !d.validateAppExists(appName) {
		return fmt.Errorf("app not found: %s", appName)
	}

	return d.Router.ExecuteAppContext(cmd.Context(), appName, appArgs[1:], options)
}

// splitRootOptions splits the leading runtime options from the application
// arguments.
//
// The run command disables flag parsing, so options placed before the
// application name arrive as plain arguments. They keep the same meaning as
// the root-level options: defaults that an application argument overrides.
func splitRootOptions(args []string) (router.AppOptions, []string) {
	var options router.AppOptions

	for len(args) > 0 {
		name, inlineValue, hasInlineValue := strings.Cut(args[0], "=")

		switch name {
		case "--profile", "-p":
			value, rest, ok := consumeOptionValue(args, inlineValue, hasInlineValue)
			if !ok {
				return options, args
			}
			options.ProfileName = value
			args = rest
		case "--output", "-o":
			value, rest, ok := consumeOptionValue(args, inlineValue, hasInlineValue)
			if !ok {
				return options, args
			}
			options.OutputFormat = value
			args = rest
		case "--mcp":
			options.MCPMode = true
			args = args[1:]
		default:
			return options, args
		}
	}

	return options, args
}

// consumeOptionValue consumes a flag and its value from the argument list. It
// returns the value, the remaining arguments and whether the flag was
// consumed. A flag that expects a value but has none is left in place.
func consumeOptionValue(args []string, inlineValue string, hasInlineValue bool) (string, []string, bool) {
	if hasInlineValue {
		return inlineValue, args[1:], true
	}
	if len(args) < 2 {
		return "", args, false
	}
	return args[1], args[2:], true
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
