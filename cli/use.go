// Copyright 2026 matter-cli contributors
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"fmt"
	"strconv"

	"github.com/p0fi/matter-cli/cli/output"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

func init() {
	rootCmd.AddCommand(withGroup(newUseCmd(), groupDevices))
}

// newUseCmd creates the `matter use` command for setting a sticky default
// target (node and endpoint). Once set, all subsequent commands that require
// a device target will use this default unless overridden by an inline
// @target or the MATTER_TARGET env var.
//
// Examples:
//
//	matter use @1
//	matter use @1/1
//	matter use @1/2
//	matter use --clear
//	matter use --show
func newUseCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "use [@target]",
		Short: "Set or show the default device target",
		Long: `Set a sticky default device target so you don't have to specify @target
on every command.

The target is persisted in your config file and used by all subsequent
commands unless overridden by:
  1. An inline @target argument
  2. The MATTER_TARGET environment variable

Use --clear to remove the sticky default, or --show to display it.`,
		Example: `  matter use @1                Set node 1 as default (endpoint unset)
  matter use @1/1              Set node 1, endpoint 1 as default
  matter use @42/2             Set node 42, endpoint 2 as default
  matter use --show            Show the current default target
  matter use --clear           Clear the sticky default`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			showFlag, _ := cmd.Flags().GetBool("show")
			clearFlag, _ := cmd.Flags().GetBool("clear")

			w := cmd.OutOrStdout()

			// --show: display current default.
			if showFlag {
				return showCurrentTarget(cmd)
			}

			// --clear: remove the sticky default.
			if clearFlag {
				return clearTarget(cmd)
			}

			// No argument and no flags — show current target.
			if len(args) == 0 {
				return showCurrentTarget(cmd)
			}

			// Parse the provided target.
			targetStr := args[0]
			// Allow with or without @ prefix for convenience.
			if !IsTargetArg(targetStr) {
				targetStr = "@" + targetStr
			}

			t, err := ParseTarget(targetStr)
			if err != nil {
				return fmt.Errorf("invalid target %q: %w", args[0], err)
			}

			// Persist to config.
			viper.Set("default-node", t.NodeID)
			if t.EndpointSet {
				viper.Set("default-endpoint", int(t.Endpoint))
			} else {
				// If no endpoint specified, try to infer one but don't fail.
				viper.Set("default-endpoint", 0)
			}

			if err := writeConfig(); err != nil {
				return fmt.Errorf("saving config: %w", err)
			}

			// Build display string.
			targetDisplay := fmt.Sprintf("@%d", t.NodeID)
			if t.EndpointSet {
				targetDisplay = fmt.Sprintf("@%d/%d", t.NodeID, t.Endpoint)
			}

			fmt.Fprintf(w, "%s Default target set to %s\n",
				output.SuccessIcon(), output.Bold(targetDisplay))

			// Show a hint about how to override.
			fmt.Fprintf(w, "\n  %s\n",
				output.Muted("Override for a single command with: matter @other/1 <command>"))

			return nil
		},
	}

	cmd.Flags().Bool("show", false, "show the current default target")
	cmd.Flags().Bool("clear", false, "clear the sticky default target")
	cmd.MarkFlagsMutuallyExclusive("show", "clear")

	return cmd
}

// showCurrentTarget displays the currently configured default target.
func showCurrentTarget(cmd *cobra.Command) error {
	w := cmd.OutOrStdout()
	nodeID := viper.GetUint64("default-node")

	if nodeID == 0 {
		fmt.Fprintf(w, "%s No default target set\n\n", output.Muted("●"))
		fmt.Fprintf(w, "  Set one with: %s\n", output.Bold("matter use @node/endpoint"))
		return nil
	}

	endpoint := viper.GetUint64("default-endpoint")
	targetDisplay := "@" + strconv.FormatUint(nodeID, 10)
	if endpoint > 0 {
		targetDisplay += "/" + strconv.FormatUint(endpoint, 10)
	}

	fmt.Fprintf(w, "%s Default target: %s\n",
		output.SuccessIcon(), output.Bold(targetDisplay))

	// Try to look up the device name for a friendlier display.
	fid := viper.GetUint64("default-fabric-id")
	if fid == 0 {
		fid = 1
	}
	if node, err := getNodeForCompletion(fid, nodeID); err == nil && node.Name != "" {
		fmt.Fprintf(w, "  %s %s %s\n",
			output.Label("Device:"), output.Bold(node.Name),
			output.Muted(fmt.Sprintf("(node %d)", nodeID)))
	}

	return nil
}

// clearTarget removes the sticky default target from config.
func clearTarget(cmd *cobra.Command) error {
	w := cmd.OutOrStdout()

	viper.Set("default-node", 0)
	viper.Set("default-endpoint", 0)

	if err := writeConfig(); err != nil {
		return fmt.Errorf("saving config: %w", err)
	}

	fmt.Fprintf(w, "%s Default target cleared\n", output.SuccessIcon())
	return nil
}
