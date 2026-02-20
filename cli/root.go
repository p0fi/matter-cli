// Copyright 2026 matter-cli contributors
// SPDX-License-Identifier: Apache-2.0

// Package cli implements the matter command tree using cobra.
package cli

import (
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
		"styleHeader":  output.Header,
		"styleBold":    output.Bold,
		"styleDim":     output.Dim,
		"styleCmd":     output.Command,
		"styleCmdPad":  styleCmdPad,
		"styleFlags":   styleFlagUsages,
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
	// Tab at any position offers device targets.
	rootCmd.ValidArgsFunction = completion.TargetCompletionFunc()

	rootCmd.AddCommand(withGroup(newVersionCmd(), groupTools))
	rootCmd.AddCommand(withGroup(newCompletionCmd(), groupTools))
	rootCmd.AddCommand(withGroup(newConfigCmd(), groupTools))

	// Apply styled help template.
	rootCmd.SetUsageTemplate(styledUsageTemplate())
}

// Execute runs the root command. It is the main entry point called from main.go.
//
// Before handing off to cobra, it scans os.Args for an @target token (e.g.
// "@1/2", "@kitchen") and extracts it so that cobra never sees it. The parsed
// target is stored in extractedTarget and applied during PersistentPreRunE via
// resolveTarget().
func Execute() error {
	if len(os.Args) > 1 {
		cleaned, target := ExtractTargetFromArgs(os.Args[1:])
		if target != nil {
			extractedTarget = target
			rootCmd.SetArgs(cleaned)
		}
	}
	return rootCmd.Execute()
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

func initConfig() {
	dir := configDir()
	viper.SetConfigName("config")
	viper.SetConfigType("yaml")
	viper.AddConfigPath(dir)
	viper.SetEnvPrefix("MATTER")
	viper.AutomaticEnv()

	// Silently ignore missing config file — it is optional.
	_ = viper.ReadInConfig()
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
	return `{{ "USAGE" | styleHeader }}
  {{ .UseLine | styleBold }}{{if .HasAvailableSubCommands}} [command]{{end}}
{{- if gt (len .Aliases) 0}}

{{ "Aliases:" | styleHeader }}
  {{.NameAndAliases}}
{{- end}}
{{- if .HasExample}}

{{ "EXAMPLES" | styleHeader }}
{{ .Example | styleDim }}
{{- end}}
{{- if .HasAvailableSubCommands}}{{$cmds := .Commands}}{{range $group := .Groups}}

{{$group.Title | trimTrailingWhitespaces | styleHeader}}
{{range $cmds}}{{if (and (eq .GroupID $group.ID) (or .IsAvailableCommand (eq .Name "help")))}}  {{.Name | styleCmdPad .NamePadding}} {{.Short}}
{{end}}{{end}}{{end}}
{{- if not .AllChildCommandsHaveGroup}}

{{ "ADDITIONAL COMMANDS" | styleHeader }}
{{range $cmds}}{{if (and (eq .GroupID "") (or .IsAvailableCommand (eq .Name "help")))}}  {{.Name | styleCmdPad .NamePadding}} {{.Short}}
{{end}}{{end}}{{end}}{{end}}
{{- if .HasAvailableLocalFlags}}

{{ "FLAGS" | styleHeader }}
{{.LocalFlags.FlagUsages | trimTrailingWhitespaces | styleFlags}}
{{- end}}
{{- if .HasAvailableInheritedFlags}}

{{ "GLOBAL FLAGS" | styleHeader }}
{{.InheritedFlags.FlagUsages | trimTrailingWhitespaces | styleFlags}}
{{- end}}

  {{ "Use \"" | styleDim }}{{ "matter [command] --help" | styleBold }}{{ "\" for more information about a command." | styleDim }}
`
}
