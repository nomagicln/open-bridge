// Code moved from commands.go during the batch-5 app split.
// Behavior is intentionally unchanged; only the file layout differs.
package app

import (
	"os"
	"strings"

	"github.com/spf13/cobra"
)

func (d *Deps) completeAppNames(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	apps := d.Completion.CompleteAppNames(toComplete)
	return apps, cobra.ShellCompDirectiveNoFileComp
}

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

	// "--status av<TAB>": the flag is the last completed word and the user has
	// already started typing a plain value prefix (space form). A prefix that
	// starts with "-" stays flag-name completion so short/long flags keep
	// working.
	if toComplete != "" && !strings.HasPrefix(toComplete, "-") && len(args) > 0 {
		last := args[len(args)-1]
		if strings.HasPrefix(last, "--") && !strings.Contains(last, "=") && len(last) > 2 {
			return last, toComplete, false, true
		}
	}

	return "", "", false, false
}

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

func (d *Deps) completeResources(appName, toComplete string) ([]string, cobra.ShellCompDirective) {
	resources := d.Completion.CompleteResources(appName, toComplete)
	return resources, cobra.ShellCompDirectiveNoFileComp
}

func (d *Deps) completeVerbs(appName, resource, toComplete string) ([]string, cobra.ShellCompDirective) {
	verbs := d.Completion.CompleteVerbsForResource(appName, resource, toComplete)
	return verbs, cobra.ShellCompDirectiveNoFileComp
}

func (d *Deps) completeFlags(appName, resource, verb, toComplete string) ([]string, cobra.ShellCompDirective) {
	flags := d.Completion.CompleteFlags(appName, resource, verb, toComplete)
	return flags, cobra.ShellCompDirectiveNoFileComp
}

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
