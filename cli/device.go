// Copyright 2026 matter-cli contributors
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"fmt"
	"time"

	"github.com/p0fi/matter-cli/cli/output"
	"github.com/p0fi/matter-cli/internal/store"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

func init() {
	rootCmd.AddCommand(withGroup(newDeviceCmd(), groupDevices))
}

// newDeviceCmd creates the `matter-cli device` subcommand group.
func newDeviceCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "device",
		Short: "Manage commissioned devices",
	}
	cmd.AddCommand(newDeviceLsCmd())
	cmd.AddCommand(newDeviceInspectCmd())
	cmd.AddCommand(newDeviceAliasCmd())
	return cmd
}

func newDeviceLsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "ls",
		Short: "List commissioned devices",
		Example: `  matter device ls
  matter device ls --format json`,
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := openStore()
			if err != nil {
				return err
			}
			defer s.Close()

			fabricID := viper.GetUint64("default-fabric-id")
			if fabricID == 0 {
				fabricID = 1
			}
			nodes, err := s.ListNodes(fabricID)
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
						fmt.Sprintf("0x%04X", n.VendorID),
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

func newDeviceInspectCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "inspect",
		Short: "Inspect a commissioned device",
		Example: `  matter device inspect --node 1
  matter device inspect -n 1 --format json`,
		RunE: func(cmd *cobra.Command, args []string) error {
			nodeID, _ := cmd.Flags().GetUint64("node")
			if nodeID == 0 {
				return fmt.Errorf("--node/-n is required")
			}

			s, err := openStore()
			if err != nil {
				return err
			}
			defer s.Close()

			fabricID := viper.GetUint64("default-fabric-id")
			if fabricID == 0 {
				fabricID = 1
			}
			node, err := s.GetNode(fabricID, nodeID)
			if err != nil {
				return fmt.Errorf("getting node %d: %w", nodeID, err)
			}

			format, _ := cmd.Flags().GetString("format")
			f := output.New(format)
			if _, ok := f.(*output.TableFormatter); ok {
				return output.FormatTree(cmd.OutOrStdout(), node)
			}
			return f.Format(cmd.OutOrStdout(), node)
		},
	}
}

func newDeviceAliasCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "alias",
		Short: "Set a friendly name for a device",
		Example: `  matter device alias --node 1 --name "Kitchen Light"`,
		RunE: func(cmd *cobra.Command, args []string) error {
			nodeID, _ := cmd.Flags().GetUint64("node")
			if nodeID == 0 {
				return fmt.Errorf("--node/-n is required")
			}
			name, _ := cmd.Flags().GetString("name")
			if name == "" {
				return fmt.Errorf("--name is required")
			}

			s, err := openStore()
			if err != nil {
				return err
			}
			defer s.Close()

			fabricID := viper.GetUint64("default-fabric-id")
			if fabricID == 0 {
				fabricID = 1
			}
			node, err := s.GetNode(fabricID, nodeID)
			if err != nil {
				return fmt.Errorf("getting node %d: %w", nodeID, err)
			}
			node.Name = name
			if err := s.SaveNode(fabricID, node); err != nil {
				return fmt.Errorf("saving node %d: %w", nodeID, err)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "%s Node %s aliased to %s\n",
				output.SuccessIcon(), output.Bold(fmt.Sprintf("%d", nodeID)), output.Success(fmt.Sprintf("%q", name)))
			return nil
		},
	}
	cmd.Flags().String("name", "", "friendly name for the device")
	return cmd
}

// openStore returns a BoltDB-backed store opened at the default config location.
func openStore() (store.Store, error) {
	dbPath, err := store.DefaultDBPath()
	if err != nil {
		return nil, fmt.Errorf("determining store path: %w", err)
	}
	s, err := store.NewBoltStore(dbPath)
	if err != nil {
		return nil, fmt.Errorf("opening store: %w", err)
	}
	return s, nil
}

func formatLastSeen(t time.Time) string {
	if t.IsZero() {
		return "never"
	}
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%d min ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%d hours ago", int(d.Hours()))
	default:
		return fmt.Sprintf("%d days ago", int(d.Hours()/24))
	}
}
