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
  use @node/endpoint   Set the default target (e.g. use @1/1)
  on-off toggle        Invoke a cluster command
  cluster read ...     Read an attribute
  exit / quit          Exit the REPL`,
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
				replArgs = injectTarget(replArgs, state)
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
	fmt.Fprintf(w, "  %s              %s\n", output.Bold("use @node/endpoint"), output.Muted("Set default target (e.g. use @1/1)"))
	fmt.Fprintf(w, "  %s  %s\n", output.Bold("cluster read/write/invoke ..."), output.Muted("Interact with clusters"))
	fmt.Fprintf(w, "  %s            %s\n", output.Bold("<cluster> <command>"), output.Muted("Shorthand (e.g. 'on-off toggle')"))
	fmt.Fprintf(w, "  %s       %s\n", output.Bold("fabric ls / device inspect"), output.Muted("Fabric & device management"))
	fmt.Fprintf(w, "  %s                           %s\n", output.Bold("help"), output.Muted("Show this help"))
	fmt.Fprintf(w, "  %s                           %s\n", output.Bold("exit / quit"), output.Muted("Exit the REPL"))
}

func handleUse(cmd *cobra.Command, state *replState, line string) {
	parts := strings.Fields(line)
	if len(parts) < 2 {
		if state.nodeID != 0 {
			fmt.Fprintf(cmd.OutOrStdout(), "%s Current target: %s\n",
				output.SuccessIcon(), output.Bold(targetHint(state.nodeID, state.endpoint)))
		} else {
			fmt.Fprintf(cmd.OutOrStdout(), "%s No target set. Usage: %s\n",
				output.Muted("●"), output.Bold("use @node/endpoint"))
		}
		return
	}

	// Try @target syntax first (e.g. "use @1/1").
	arg := parts[1]
	if IsTargetArg(arg) || (len(parts) == 2 && !strings.ContainsAny(arg, " ")) {
		// Allow "use 1/1" without @ prefix for convenience in the REPL.
		targetStr := arg
		if !strings.HasPrefix(targetStr, "@") {
			targetStr = "@" + targetStr
		}
		t, err := ParseTarget(targetStr)
		if err == nil {
			state.nodeID = t.NodeID
			if t.EndpointSet {
				state.endpoint = t.Endpoint
			}
			fmt.Fprintf(cmd.OutOrStdout(), "%s Target set to %s\n",
				output.SuccessIcon(), output.Bold(targetHint(state.nodeID, state.endpoint)))
			return
		}
		// Fall through to legacy parsing if @target parsing fails.
	}

	fmt.Fprintf(cmd.ErrOrStderr(), "%s Invalid target %q — use @node/endpoint syntax (e.g. use @1/1)\n",
		output.WarningIcon(), arg)
}

// injectTarget prepends an @node/endpoint token to the argument list so that
// REPL commands automatically target the currently selected device. If the
// args already contain an @target token the list is returned unchanged.
func injectTarget(args []string, state *replState) []string {
	for _, a := range args {
		if strings.HasPrefix(a, "@") && len(a) > 1 {
			return args // already has a target
		}
	}
	target := fmt.Sprintf("@%d/%d", state.nodeID, state.endpoint)
	result := make([]string, 0, len(args)+1)
	result = append(result, target)
	result = append(result, args...)
	return result
}
