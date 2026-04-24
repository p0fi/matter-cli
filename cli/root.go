// Copyright 2026 matter-cli contributors
// SPDX-License-Identifier: Apache-2.0

// Package cli implements the matter command tree using cobra.
package cli

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"text/template"
	"time"

	"github.com/p0fi/matter-cli/cli/completion"
	"github.com/p0fi/matter-cli/cli/output"
	"github.com/p0fi/matter-cli/internal/clusters"
	"github.com/p0fi/matter-cli/internal/daemon"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

// Command group IDs for organizing help output.
const (
	groupDevices  = "devices"
	groupClusters = "clusters"
	groupTools    = "tools"
)

// Version information set at build time via ldflags.
var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

// rootCmd is the top-level cobra command for matter.
var rootCmd = &cobra.Command{
	Use:   "matter",
	Short: "A pure Go Matter controller CLI",
	Long:  "matter is a command-line tool for interacting with Matter smart home devices.",
	Example: "  $ matter commission code MT:Y3.13OTB00KA0648G00\n" +
		"  $ matter @1/1 OnOff Toggle\n" +
		"  $ matter @1/1 OnOff read OnOff\n" +
		"  $ matter @1/1 LevelControl write CurrentLevel 128",
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		if err := setupLogging(cmd); err != nil {
			return err
		}
		// Apply the @target resolution chain: inline @target →
		// MATTER_TARGET env → sticky default from config.
		resolveTarget()
		// Hide shorthand cluster commands not present on the target endpoint
		// so that tab-completion only offers relevant commands.
		filterShorthandCommands(resolvedTarget)
		return maybeStartDaemon(cmd)
	},
	SilenceUsage:  true,
	SilenceErrors: true,
}

func init() {
	cobra.OnInitialize(initConfig)

	// Register template functions for styled help output.
	cobra.AddTemplateFuncs(template.FuncMap{
		"styleHeader":        output.Header,
		"styleBold":          output.Bold,
		"styleDim":           output.Dim,
		"styleCmd":           output.Command,
		"styleCmdPad":        styleCmdPad,
		"styleFlags":         styleFlagUsages,
		"isShorthandCluster": isShorthandCluster,
		"visibleCmdPadding":  visibleCmdPadding,
		"splitLines":         func(s string) []string { return strings.Split(s, "\n") },
	})

	// Register command groups.
	rootCmd.AddGroup(
		&cobra.Group{ID: groupDevices, Title: "DEVICE COMMANDS"},
		&cobra.Group{ID: groupClusters, Title: "CLUSTER COMMANDS"},
		&cobra.Group{ID: groupTools, Title: "TOOLS"},
	)

	pf := rootCmd.PersistentFlags()
	pf.StringP("format", "f", "", "output format: json, table, yaml (default: table for TTY, json for pipes)")
	pf.BoolP("verbose", "v", false, "enable verbose/debug logging")
	pf.StringP("keep-alive", "K", "", "start/reuse a background session daemon with the given idle timeout (e.g. 5m, 30m, 1h)")

	_ = viper.BindPFlag("format", pf.Lookup("format"))

	// Enable @target completion on the root command so that typing "@" then
	// Tab at any position offers device targets. Cluster completions are
	// filtered to those present on the current target endpoint via
	// completionClusterFilter (populated in PersistentPreRunE). The
	// top-level command snapshot feeds the "@N+<cmd>" expansion when the
	// user Tab-completes an exact numeric node match.
	rootCmd.ValidArgsFunction = completion.RootCompletionFunc(
		clusters.Global, completionClusterFilter, topLevelCommandsForCompletion,
	)

	rootCmd.AddCommand(withGroup(newVersionCmd(), groupTools))
	rootCmd.AddCommand(withGroup(newCompletionCmd(), groupTools))
	rootCmd.AddCommand(withGroup(newConfigCmd(), groupTools))

	// Apply styled help template.
	rootCmd.SetUsageTemplate(styledUsageTemplate())
}

// Execute runs the root command. It is the main entry point called from main.go.
//
// Before handing off to cobra, it scans os.Args for an @target token (e.g.
// "@1" or "@1/2") and extracts it so that cobra never sees it. The parsed
// target is stored in extractedTarget and applied during PersistentPreRunE via
// resolveTarget().
//
// Cobra's built-in completion subcommands (__complete, __completeNoDesc) are
// exempt: their toComplete argument may contain a bare @N token that our
// completion handler needs to see in order to offer context-aware candidates
// (endpoints, device commands, etc.). Extracting it here would leave cobra
// with fewer args than __complete requires and completion would silently
// return nothing.
func Execute() error {
	if len(os.Args) > 1 {
		args := os.Args[1:]
		if !isCompletionInvocation(args) {
			cleaned, target := ExtractTargetFromArgs(args)
			if target != nil {
				extractedTarget = target
				args = cleaned
			}
		}
		normalized := normalizeShorthandArgs(args, clusters.Global)
		rootCmd.SetArgs(normalized)
	}
	return rootCmd.Execute()
}

// isCompletionInvocation reports whether args start with one of cobra's
// completion-script-facing subcommands. When true, @target extraction must be
// skipped so that the completion handler sees the token as toComplete.
func isCompletionInvocation(args []string) bool {
	if len(args) == 0 {
		return false
	}
	switch args[0] {
	case "__complete", "__completeNoDesc":
		return true
	}
	return false
}

// normalizeShorthandArgs rewrites cluster shorthand command and sub-command
// tokens in args to their canonical PascalCase forms so that cobra's
// case-sensitive dispatch works regardless of how the user typed them.
// For example ["onoff", "on"] becomes ["OnOff", "On"].
//
// Only the first positional token (cluster name) and the immediately following
// positional token (command name) are examined; flags and all other tokens are
// left unchanged.
func normalizeShorthandArgs(args []string, registry *clusters.Registry) []string {
	result := make([]string, len(args))
	copy(result, args)

	// Find and normalize the first non-flag positional arg as a cluster name.
	clusterIdx := -1
	var cl *clusters.ClusterInfo
	for i, arg := range result {
		if strings.HasPrefix(arg, "-") {
			continue
		}
		c, ok := registry.ClusterByName(arg)
		if !ok {
			// First positional doesn't match any cluster — nothing to normalize.
			break
		}
		result[i] = c.Name
		clusterIdx = i
		cl = c
		break
	}

	if cl == nil {
		return result
	}

	// Find and normalize the next non-flag positional arg as a command name.
	for i := clusterIdx + 1; i < len(result); i++ {
		if strings.HasPrefix(result[i], "-") {
			continue
		}
		if cmd, ok := registry.CommandByName(cl.ID, result[i]); ok {
			result[i] = cmd.Name
		}
		break
	}

	return result
}

// withGroup assigns a command to a group and returns it for chaining.
func withGroup(cmd *cobra.Command, groupID string) *cobra.Command {
	cmd.GroupID = groupID
	return cmd
}

// configDir returns the matter configuration directory, respecting
// XDG_CONFIG_HOME.
func configDir() string {
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		return filepath.Join(xdg, "matter-cli")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(".", ".matter-cli")
	}
	return filepath.Join(home, ".config", "matter-cli")
}

// configTemplateFor returns the config file template with the resolved path
// embedded as a comment. All keys are commented out so the file is
// self-documenting without changing any defaults.
func configTemplateFor(cfgFile string) string {
	return fmt.Sprintf(`# matter-cli configuration
# %s

# Default fabric ID used when --fabric-id is not specified.
# default-fabric-id: 1

# Output format: table | json | yaml (default: table for TTY, json for pipes).
# format: table

# Sticky default target set by "matter use @<node>/<endpoint>".
# default-node: 0
# default-endpoint: 0

# WiFi credentials used during BLE commissioning.
# Avoids passing --wifi-ssid and --wifi-password on every commission invocation.
# wifi:
#   ssid: MyNetwork
#   password: s3cr3t

# Thread operational dataset (hex-encoded) used during BLE commissioning.
# thread:
#   dataset: 0e080000000000010000000300001235...
`, cfgFile)
}

func initConfig() {
	dir := configDir()
	viper.SetConfigName("config")
	viper.SetConfigType("yaml")
	viper.AddConfigPath(dir)
	viper.SetEnvPrefix("MATTER")
	viper.SetEnvKeyReplacer(strings.NewReplacer(".", "_", "-", "_"))
	viper.AutomaticEnv()

	// Bootstrap config file on first run so users can discover available keys.
	cfgFile := filepath.Join(dir, "config.yaml")
	if mkErr := os.MkdirAll(dir, 0o700); mkErr == nil {
		if f, err := os.OpenFile(cfgFile, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600); err == nil {
			_, _ = f.WriteString(configTemplateFor(cfgFile))
			_ = f.Close()
		}
		// os.O_EXCL ensures we never overwrite an existing config.
	}

	if err := viper.ReadInConfig(); err != nil {
		var notFound viper.ConfigFileNotFoundError
		if !errors.As(err, &notFound) {
			fmt.Fprintf(os.Stderr, "warning: config file error: %v\n", err)
		}
	}
}

// maybeStartDaemon checks the --keep-alive / -K flag and, if set, ensures a
// background session daemon is running with the requested idle timeout. If one
// is already running, this is a no-op. The daemon is spawned as a detached
// background process so it survives the current CLI invocation.
func maybeStartDaemon(cmd *cobra.Command) error {
	ka, _ := cmd.Flags().GetString("keep-alive")
	if ka == "" {
		return nil
	}

	dur, err := time.ParseDuration(ka)
	if err != nil {
		return fmt.Errorf("invalid --keep-alive value %q: %w", ka, err)
	}

	client := daemon.NewClient("")
	if client.IsRunning() {
		return nil // daemon already running
	}

	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("finding executable path: %w", err)
	}

	daemonCmd := exec.Command(exe, "__daemon", "--timeout", dur.String())
	daemonCmd.Stdin = nil
	daemonCmd.Stdout = nil
	daemonCmd.Stderr = nil
	daemonCmd.SysProcAttr = daemonSysProcAttr()

	if err := daemonCmd.Start(); err != nil {
		fmt.Fprintf(cmd.ErrOrStderr(), "%s Failed to start session daemon: %v\n",
			output.WarningIcon(), err)
		return nil // non-fatal — fall back to direct connection
	}

	// Wait briefly for the daemon to be ready.
	if waitForDaemon(client, 3*time.Second) {
		fmt.Fprintf(cmd.ErrOrStderr(), "%s Session daemon started %s\n",
			output.SuccessIcon(),
			output.Muted(fmt.Sprintf("(pid %d, idle timeout: %s)", daemonCmd.Process.Pid, dur)))
	}

	return nil
}

func setupLogging(cmd *cobra.Command) error {
	verbose, err := cmd.Flags().GetBool("verbose")
	if err != nil {
		return fmt.Errorf("reading verbose flag: %w", err)
	}

	level := slog.LevelWarn
	if verbose {
		level = slog.LevelDebug
	}
	handler := output.NewLogHandler(os.Stderr, level)
	slog.SetDefault(slog.New(handler))
	return nil
}

// flagPattern matches short and long flag names like -v, --verbose, --keep-alive.
var flagPattern = regexp.MustCompile(`(--?[\w][\w-]*)`)

// styleFlagUsages colorizes flag names (e.g. --verbose, -f) in the given
// flag usage string. When NO_COLOR is set, the input is returned unchanged.
func styleFlagUsages(s string) string {
	if output.NoColor() {
		return s
	}
	return flagPattern.ReplaceAllStringFunc(s, func(match string) string {
		return output.Flag(match)
	})
}

// isShorthandCluster reports whether cmd is a generated shorthand cluster
// command. These are hidden from the root help output but remain active for
// direct invocation and shell completion.
func isShorthandCluster(cmd *cobra.Command) bool {
	return cmd.Annotations["shorthand-cluster"] == "true"
}

// visibleCmdPadding returns the column width needed to align descriptions for
// the visible (non-shorthand-cluster) commands in cmds. It replaces cobra's
// built-in .NamePadding, which counts all registered commands including hidden
// shorthand cluster names that dominate the width.
func visibleCmdPadding(cmds []*cobra.Command) int {
	max := 0
	for _, cmd := range cmds {
		if !isShorthandCluster(cmd) && len(cmd.Name()) > max {
			max = len(cmd.Name())
		}
	}
	return max + 2
}

// styleCmdPad styles a command name in cyan and right-pads it to the given
// width based on the plain-text length (ignoring ANSI escape sequences).
func styleCmdPad(padding int, name string) string {
	styled := output.Command(name)
	if pad := padding - len(name); pad > 0 {
		styled += strings.Repeat(" ", pad)
	}
	return styled
}

// styledUsageTemplate returns a cobra usage template with color styling.
// Template funcs styleHeader, styleBold, styleDim, styleCmd, and styleFlags
// are registered in init() via cobra.AddTemplateFuncs.
func styledUsageTemplate() string {
	return fmt.Sprintf(`{{ "USAGE" | styleHeader }}
  {{ .UseLine | styleBold }}{{if .HasAvailableSubCommands}} [command]{{end}}
{{- if gt (len .Aliases) 0}}

{{ "Aliases:" | styleHeader }}
  {{.NameAndAliases}}
{{- end}}
{{- if .HasExample}}

{{ "EXAMPLES" | styleHeader }}
{{- range (splitLines .Example)}}
{{ . | styleDim }}
{{- end}}
{{- end}}
{{- if .HasAvailableSubCommands}}{{$cmds := .Commands}}{{$pad := visibleCmdPadding $cmds}}{{range $group := .Groups}}

{{$group.Title | trimTrailingWhitespaces | styleHeader}}
{{range $cmds}}{{if (and (eq .GroupID $group.ID) (or .IsAvailableCommand (eq .Name "help")) (not (isShorthandCluster .)))}}  {{.Name | styleCmdPad $pad}} {{.Short}}
{{end}}{{end}}{{if eq $group.ID "%s"}}{{range $cmds}}{{if (and (eq .GroupID $group.ID) (eq .Name "cluster") .IsAvailableCommand (not (isShorthandCluster .)))}}
  {{ "Run " | styleDim }}{{ "matter cluster list" | styleBold }}{{ " for all available clusters." | styleDim }}
  {{ "Use " | styleDim }}{{ "matter <ClusterName> --help" | styleBold }}{{ " for shorthand commands (e.g. " | styleDim }}{{ "matter OnOff --help" | styleBold }}{{ ")." | styleDim }}
{{end}}{{end}}{{end}}{{end}}
{{- if not .AllChildCommandsHaveGroup}}

{{ "ADDITIONAL COMMANDS" | styleHeader }}
{{range $cmds}}{{if (and (eq .GroupID "") (or .IsAvailableCommand (eq .Name "help")))}}  {{.Name | styleCmdPad $pad}} {{.Short}}
{{end}}{{end}}{{end}}{{end}}
{{- if .HasAvailableLocalFlags}}

{{ "FLAGS" | styleHeader }}
{{.LocalFlags.FlagUsages | trimTrailingWhitespaces | styleFlags}}
{{- end}}
{{- if .HasAvailableInheritedFlags}}

{{ "GLOBAL FLAGS" | styleHeader }}
{{.InheritedFlags.FlagUsages | trimTrailingWhitespaces | styleFlags}}
{{- end}}

  {{ "Use " | styleDim }}{{ "matter [command] --help" | styleBold }}{{ " for more information about a command." | styleDim }}
`, groupClusters)
}
