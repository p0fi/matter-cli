// Copyright 2026 matter-cli contributors
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/p0fi/matter-cli/cli/output"
	"github.com/spf13/cobra"
)

func init() {
	rootCmd.AddCommand(withGroup(newInteractiveCmd(), groupTools))
}

// newInteractiveCmd creates the `matter-cli interactive` command.
// This is a basic line-based REPL. A full bubbletea TUI will be added later.
func newInteractiveCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "interactive",
		Aliases: []string{"i", "repl"},
		Short:   "Start an interactive REPL session",
		Long: `Start an interactive REPL session for controlling Matter devices.

Commands in interactive mode mirror the CLI commands:
  use node <id> endpoint <id>   Set the default node and endpoint
  on-off toggle                 Invoke a cluster command
  cluster read ...              Read an attribute
  exit / quit                   Exit the REPL`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runREPL(cmd)
		},
	}
}

// replState holds the state of the interactive REPL session.
type replState struct {
	nodeID   uint64
	endpoint uint16
}

func (s *replState) prompt() string {
	if s.nodeID != 0 {
		return fmt.Sprintf("matter/node-%d/ep-%d> ", s.nodeID, s.endpoint)
	}
	return "matter> "
}

func runREPL(cmd *cobra.Command) error {
	state := &replState{}
	scanner := bufio.NewScanner(os.Stdin)

	fmt.Fprintln(cmd.OutOrStdout(), output.Header("matter interactive mode"))
	fmt.Fprintln(cmd.OutOrStdout(), output.Muted("Type 'help' for commands, 'exit' to quit."))
	fmt.Fprintln(cmd.OutOrStdout())

	for {
		fmt.Fprint(cmd.OutOrStdout(), state.prompt())
		if !scanner.Scan() {
			break
		}
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		switch {
		case line == "exit" || line == "quit":
			fmt.Fprintln(cmd.OutOrStdout(), "Goodbye.")
			return nil
		case line == "help":
			printREPLHelp(cmd)
		case strings.HasPrefix(line, "use "):
			handleUse(cmd, state, line)
		default:
			// Dispatch to the cobra command tree by injecting current context.
			replArgs := strings.Fields(line)
			if state.nodeID != 0 {
				replArgs = injectNodeFlags(replArgs, state)
			}
			replCmd := &cobra.Command{Use: "matter"}
			replCmd.SetOut(cmd.OutOrStdout())
			replCmd.SetErr(cmd.ErrOrStderr())
			// Re-use the root command's subcommands for dispatch.
			rootCmd.SetArgs(replArgs)
			if err := rootCmd.Execute(); err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "%s %v\n", output.Error("Error:"), err)
			}
		}
	}
	return scanner.Err()
}

func printREPLHelp(cmd *cobra.Command) {
	w := cmd.OutOrStdout()
	fmt.Fprintln(w, output.Header("Available commands:"))
	fmt.Fprintf(w, "  %s  %s\n", output.Bold("use node <id> [endpoint <id>]"), output.Muted("Set default node and endpoint"))
	fmt.Fprintf(w, "  %s  %s\n", output.Bold("cluster read/write/invoke ..."), output.Muted("Interact with clusters"))
	fmt.Fprintf(w, "  %s            %s\n", output.Bold("<cluster> <command>"), output.Muted("Shorthand (e.g. 'on-off toggle')"))
	fmt.Fprintf(w, "  %s            %s\n", output.Bold("device ls / inspect"), output.Muted("Device management"))
	fmt.Fprintf(w, "  %s                           %s\n", output.Bold("help"), output.Muted("Show this help"))
	fmt.Fprintf(w, "  %s                           %s\n", output.Bold("exit / quit"), output.Muted("Exit the REPL"))
}

func handleUse(cmd *cobra.Command, state *replState, line string) {
	parts := strings.Fields(line)
	for i := 1; i < len(parts); i++ {
		switch parts[i] {
		case "node":
			if i+1 < len(parts) {
				i++
				var n uint64
				if _, err := fmt.Sscanf(parts[i], "%d", &n); err == nil {
					state.nodeID = n
				} else {
					fmt.Fprintf(cmd.ErrOrStderr(), "Invalid node ID: %s\n", parts[i])
				}
			}
		case "endpoint":
			if i+1 < len(parts) {
				i++
				var e uint16
				if _, err := fmt.Sscanf(parts[i], "%d", &e); err == nil {
					state.endpoint = e
				} else {
					fmt.Fprintf(cmd.ErrOrStderr(), "Invalid endpoint ID: %s\n", parts[i])
				}
			}
		}
	}
	fmt.Fprintf(cmd.OutOrStdout(), "%s Context set: node %s, endpoint %s\n",
		output.SuccessIcon(), output.Bold(fmt.Sprintf("%d", state.nodeID)), output.Bold(fmt.Sprintf("%d", state.endpoint)))
}

func injectNodeFlags(args []string, state *replState) []string {
	hasNode := false
	hasEndpoint := false
	for _, a := range args {
		if a == "--node" || a == "-n" {
			hasNode = true
		}
		if a == "--endpoint" || a == "-e" {
			hasEndpoint = true
		}
	}
	if !hasNode {
		args = append(args, "--node", fmt.Sprintf("%d", state.nodeID))
	}
	if !hasEndpoint {
		args = append(args, "--endpoint", fmt.Sprintf("%d", state.endpoint))
	}
	return args
}
