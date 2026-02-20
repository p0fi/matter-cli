// Copyright 2026 matter-cli contributors
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"fmt"
	"time"

	"github.com/p0fi/matter-cli/cli/output"
	"github.com/p0fi/matter-cli/internal/daemon"
	"github.com/p0fi/matter-cli/internal/store"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

// completionStoreTimeout is the maximum time to wait for a BoltDB lock when
// opening the store directly from completion paths (only reached when no
// daemon is running, so no lock contention is expected).
const completionStoreTimeout = 100 * time.Millisecond

func init() {
	rootCmd.AddCommand(withGroup(newDeviceCmd(), groupDevices))
}

// newDeviceCmd creates the `matter-cli device` subcommand group.
func newDeviceCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "device",
		Short: "Inspect and manage individual devices",
	}
	cmd.AddCommand(newDeviceInspectCmd())
	cmd.AddCommand(newDeviceAliasCmd())
	return cmd
}

func newDeviceInspectCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "inspect",
		Short: "Inspect a commissioned device",
		Example: `  matter @1 device inspect
  matter @kitchen device inspect`,
		RunE: func(cmd *cobra.Command, args []string) error {
			nodeID, _, err := requireTarget(cmd)
			if err != nil {
				return err
			}

			fabricID := viper.GetUint64("default-fabric-id")
			if fabricID == 0 {
				fabricID = 1
			}
			node, err := getNodeForCompletion(fabricID, nodeID)
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
		Example: `  matter @1 device alias --name "Kitchen Light"`,
		RunE: func(cmd *cobra.Command, args []string) error {
			nodeID, _, err := requireTarget(cmd)
			if err != nil {
				return err
			}
			name, _ := cmd.Flags().GetString("name")
			if name == "" {
				return fmt.Errorf("--name is required")
			}

			fabricID := viper.GetUint64("default-fabric-id")
			if fabricID == 0 {
				fabricID = 1
			}
			node, err := getNodeForCompletion(fabricID, nodeID)
			if err != nil {
				return fmt.Errorf("getting node %d: %w", nodeID, err)
			}
			node.Name = name
			if err := saveNode(fabricID, node); err != nil {
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

// openStoreForCompletion is like openStore but uses a short timeout. Only used
// as a fallback when no daemon is running (so no lock contention expected).
func openStoreForCompletion() (store.Store, error) {
	dbPath, err := store.DefaultDBPath()
	if err != nil {
		return nil, fmt.Errorf("determining store path: %w", err)
	}
	s, err := store.NewBoltStoreTimeout(dbPath, completionStoreTimeout)
	if err != nil {
		return nil, fmt.Errorf("opening store: %w", err)
	}
	return s, nil
}

// listNodesForCompletion returns all commissioned nodes for use in shell
// completion and target-resolution code paths. When a session daemon is
// running it queries the daemon via its Unix socket (avoiding the BoltDB
// exclusive lock that the daemon holds). Otherwise it opens the DB directly.
func listNodesForCompletion(fabricID uint64) ([]*store.Node, error) {
	dc := daemon.NewClient("")
	if dc.IsRunning() {
		return dc.ListNodes(fabricID)
	}
	s, err := openStoreForCompletion()
	if err != nil {
		return nil, err
	}
	defer s.Close()
	return s.ListNodes(fabricID)
}

// getNodeForCompletion is like listNodesForCompletion but returns a single
// node by ID, searching the full node list returned by the daemon or store.
func getNodeForCompletion(fabricID, nodeID uint64) (*store.Node, error) {
	nodes, err := listNodesForCompletion(fabricID)
	if err != nil {
		return nil, err
	}
	for _, n := range nodes {
		if n.ID == nodeID {
			return n, nil
		}
	}
	return nil, fmt.Errorf("node %d not found in fabric %d", nodeID, fabricID)
}

// getFabric returns the fabric record, querying the daemon if it is running
// (to avoid contending on the BoltDB exclusive lock), otherwise opening the
// store directly.
func getFabric(fabricID uint64) (*store.Fabric, error) {
	dc := daemon.NewClient("")
	if dc.IsRunning() {
		return dc.GetFabric(fabricID)
	}
	s, err := openStore()
	if err != nil {
		return nil, err
	}
	defer s.Close()
	return s.GetFabric(fabricID)
}

// saveNode persists a node record, routing through the daemon when it is
// running so that the BoltDB exclusive lock held by the daemon is respected.
func saveNode(fabricID uint64, node *store.Node) error {
	dc := daemon.NewClient("")
	if dc.IsRunning() {
		return dc.SaveNode(fabricID, node)
	}
	s, err := openStore()
	if err != nil {
		return err
	}
	defer s.Close()
	return s.SaveNode(fabricID, node)
}

// deleteNode removes a node record from the store, routing through the daemon
// when it is running so that the BoltDB exclusive lock is respected. The daemon
// also evicts any cached CASE session for the node.
func deleteNode(fabricID, nodeID uint64) error {
	dc := daemon.NewClient("")
	if dc.IsRunning() {
		return dc.DeleteNode(fabricID, nodeID)
	}
	s, err := openStore()
	if err != nil {
		return err
	}
	defer s.Close()
	return s.DeleteNode(fabricID, nodeID)
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
