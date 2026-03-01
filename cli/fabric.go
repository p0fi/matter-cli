// Copyright 2026 matter-cli contributors
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"fmt"

	"github.com/p0fi/matter-cli/cli/completion"
	"github.com/p0fi/matter-cli/cli/output"
	"github.com/p0fi/matter-cli/internal/vendordb"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

func fabricID() uint64 {
	id := viper.GetUint64("default-fabric-id")
	if id == 0 {
		return 1
	}
	return id
}

func init() {
	rootCmd.AddCommand(withGroup(newFabricCmd(), groupDevices))
}

// newFabricCmd creates the `matter fabric` subcommand group for fabric-level
// operations such as listing all commissioned devices on the fabric.
func newFabricCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "fabric",
		Short: "Manage the Matter fabric and its devices",
	}
	cmd.AddCommand(newFabricLsCmd())
	cmd.AddCommand(newFabricInfoCmd())
	cmd.AddCommand(newFabricRemoveCmd())
	return cmd
}

// newFabricLsCmd creates `matter fabric ls` which lists all commissioned
// devices on the current fabric.
func newFabricLsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "ls",
		Short: "List commissioned devices on the fabric",
		Example: `  matter fabric ls
  matter fabric ls --format json
  matter fabric ls -f yaml`,
		RunE: func(cmd *cobra.Command, args []string) error {
			nodes, err := listNodesForCompletion(fabricID())
			if err != nil {
				return fmt.Errorf("listing nodes: %w", err)
			}
			if len(nodes) == 0 {
				fmt.Fprintln(cmd.ErrOrStderr(), output.Muted("No commissioned devices found."))
				return nil
			}

			format, _ := cmd.Flags().GetString("format")
			f := output.New(format)

			if _, ok := f.(*output.TableFormatter); ok {
				td := &output.TableData{
					Headers: []string{"NODE", "NAME", "VENDOR", "PRODUCT", "ENDPOINTS", "LAST SEEN"},
				}
				for _, n := range nodes {
					td.Rows = append(td.Rows, []string{
						fmt.Sprintf("%d", n.ID),
						n.Name,
						vendordb.FormatVendorID(n.VendorID),
						fmt.Sprintf("0x%04X", n.ProductID),
						fmt.Sprintf("%d", len(n.Endpoints)),
						formatLastSeen(n.LastSeen),
					})
				}
				return f.Format(cmd.OutOrStdout(), td)
			}
			return f.Format(cmd.OutOrStdout(), nodes)
		},
	}
}

// newFabricInfoCmd creates `matter fabric info` which displays information
// about the current fabric identity (label, root certificate, vendor ID).
func newFabricInfoCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "info",
		Short: "Show information about the current fabric",
		Example: `  matter fabric info
  matter fabric info --format json`,
		RunE: func(cmd *cobra.Command, args []string) error {
			fid := fabricID()
			fabric, err := getFabric(fid)
			if err != nil {
				return fmt.Errorf("getting fabric %d: %w", fid, err)
			}

			format, _ := cmd.Flags().GetString("format")
			f := output.New(format)

			if _, ok := f.(*output.TableFormatter); ok {
				w := cmd.OutOrStdout()
				fmt.Fprintf(w, "%s\n\n", output.Header("Fabric"))
				fmt.Fprintf(w, "  %s         %s\n", output.Label("ID:"), output.Value(fmt.Sprintf("%d", fabric.ID)))
				fmt.Fprintf(w, "  %s      %s\n", output.Label("Label:"), output.Value(fabric.Label))
				fmt.Fprintf(w, "  %s  %s\n", output.Label("Vendor ID:"), output.Accent(vendordb.FormatVendorID(fabric.VendorID)))
				fmt.Fprintf(w, "  %s      %s\n", output.Label("Index:"), output.Value(fmt.Sprintf("%d", fabric.FabricIndex)))
				fmt.Fprintf(w, "  %s    %s\n", output.Label("Created:"), output.Muted(fabric.CreatedAt.Format("2006-01-02 15:04:05")))

				if nodes, err := listNodesForCompletion(fid); err == nil {
					fmt.Fprintf(w, "  %s    %s\n", output.Label("Devices:"), output.Value(fmt.Sprintf("%d", len(nodes))))
				}
				return nil
			}
			return f.Format(cmd.OutOrStdout(), fabric)
		},
	}
}

// newFabricRemoveCmd creates `matter fabric remove` which removes a
// commissioned device from the local fabric database. The target device is
// specified using the standard @target syntax, either as a positional argument
// or as an inline token before the subcommand:
//
//	matter fabric remove @1
//	matter fabric remove @kitchen
//	matter @1 fabric remove
func newFabricRemoveCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "remove @target",
		Short: "Remove a commissioned device from the fabric",
		Example: `  matter fabric remove @1
  matter fabric remove @kitchen
  matter @1 fabric remove`,
		Args: cobra.MaximumNArgs(1),
		ValidArgsFunction: completion.TargetCompletionFunc(),
		RunE: func(cmd *cobra.Command, args []string) error {
			// Support both `matter fabric remove @1` (positional arg) and
			// `matter @1 fabric remove` (inline @target resolved via PersistentPreRunE).
			if len(args) == 1 {
				raw := args[0]
				if !IsTargetArg(raw) {
					raw = "@" + raw
				}
				t, err := ParseTarget(raw)
				if err != nil {
					return fmt.Errorf("invalid target %q: %w", args[0], err)
				}
				resolvedTarget = t
			}

			nodeID, _, err := requireTarget(cmd)
			if err != nil {
				return err
			}

			fid := fabricID()

			// Look up the node name before deleting for a friendlier confirmation.
			node, lookupErr := getNodeForCompletion(fid, nodeID)

			if err := deleteNode(fid, nodeID); err != nil {
				return fmt.Errorf("removing node %d: %w", nodeID, err)
			}

			var label string
			if lookupErr == nil && node.Name != "" {
				label = output.Bold(node.Name) + " " + output.Muted(fmt.Sprintf("(node %d)", nodeID))
			} else {
				label = output.Bold(fmt.Sprintf("%d", nodeID))
			}
			fmt.Fprintf(cmd.OutOrStdout(), "%s Removed %s from fabric.\n",
				output.SuccessIcon(), label)
			return nil
		},
	}
}
