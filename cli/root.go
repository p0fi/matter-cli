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
		return maybeStartDaemon(cmd)
	},
	SilenceUsage:  true,
	SilenceErrors: true,
}

func init() {
	cobra.OnInitialize(initConfig)

	// Register command groups.
	rootCmd.AddGroup(
		&cobra.Group{ID: groupDevices, Title: "Device Commands:"},
		&cobra.Group{ID: groupClusters, Title: "Cluster Commands:"},
		&cobra.Group{ID: groupTools, Title: "Tools:"},
	)

	pf := rootCmd.PersistentFlags()
	pf.StringP("format", "f", "", "output format: json, table, yaml (default: table for TTY, json for pipes)")
	pf.BoolP("verbose", "v", false, "enable verbose/debug logging")
	pf.Uint64P("node", "n", 0, "target node ID")
	pf.Uint16P("endpoint", "e", 0, "target endpoint ID")
	pf.StringP("keep-alive", "K", "", "start/reuse a background session daemon with the given idle timeout (e.g. 5m, 30m, 1h)")

	_ = viper.BindPFlag("format", pf.Lookup("format"))

	_ = rootCmd.RegisterFlagCompletionFunc("node", completion.NodeIDCompletionFunc())
	_ = rootCmd.RegisterFlagCompletionFunc("endpoint", completion.EndpointIDCompletionFunc())

	rootCmd.AddCommand(withGroup(newVersionCmd(), groupTools))
	rootCmd.AddCommand(withGroup(newConfigCmd(), groupTools))

	// Apply styled help template.
	rootCmd.SetUsageTemplate(styledUsageTemplate())
}

// Execute runs the root command. It is the main entry point called from main.go.
func Execute() error {
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

// styledUsageTemplate returns a cobra usage template with color styling.
func styledUsageTemplate() string {
	// We inject ANSI styling via lipgloss into the template literals.
	// Cobra templates use {{.X}} for dynamic content, so we only style
	// the static labels.
	h := func(s string) string { return output.Header(s) }
	d := func(s string) string { return output.Dim(s) }
	b := func(s string) string { return output.Bold(s) }

	return fmt.Sprintf(`%s
  {{.UseLine}}{{if .HasAvailableSubCommands}} [command]{{end}}
{{if gt (len .Aliases) 0}}
%s
  {{.NameAndAliases}}
{{end}}{{if .HasAvailableSubCommands}}{{$cmds := .Commands}}{{range $group := .Groups}}

%s
{{range $cmds}}{{if (and (eq .GroupID $group.ID) (or .IsAvailableCommand (eq .Name "help")))}}  %s {{.Short}}
{{end}}{{end}}{{end}}{{if not .AllChildCommandsHaveGroup}}

%s
{{range $cmds}}{{if (and (eq .GroupID "") (or .IsAvailableCommand (eq .Name "help")))}}  %s {{.Short}}
{{end}}{{end}}{{end}}{{end}}{{if .HasAvailableLocalFlags}}
%s
{{.LocalFlags.FlagUsages | trimTrailingWhitespaces}}
{{end}}{{if .HasAvailableInheritedFlags}}
%s
{{.InheritedFlags.FlagUsages | trimTrailingWhitespaces}}
{{end}}
  %s
`,
		h("Usage:"),
		h("Aliases:"),
		`{{$group.Title | trimTrailingWhitespaces}}`, // group titles are already strings
		`{{rpad .Name .NamePadding}}`,                // command name
		h("Additional Commands:"),
		`{{rpad .Name .NamePadding}}`, // command name
		h("Flags:"),
		h("Global Flags:"),
		d(fmt.Sprintf("Use \"%s\" for more information about a command.", b("matter [command] --help"))),
	)
}
