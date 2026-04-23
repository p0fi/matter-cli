// Copyright 2026 matter-cli contributors
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"fmt"
	"time"

	"github.com/p0fi/matter-cli/internal/daemon"
	"github.com/p0fi/matter-cli/internal/store"
	"github.com/spf13/viper"
)

// completionStoreTimeout is the maximum time to wait for a BoltDB lock when
// opening the store directly from completion paths (only reached when no
// daemon is running, so no lock contention is expected).
const completionStoreTimeout = 100 * time.Millisecond

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

// loadNodes returns all commissioned nodes. When a session daemon is running it
// queries the daemon via its Unix socket; otherwise it opens the DB with the
// default (blocking) timeout. Use this for normal command paths.
func loadNodes(fabricID uint64) ([]*store.Node, error) {
	dc := daemon.NewClient("")
	if dc.IsRunning() {
		return dc.ListNodes(fabricID)
	}
	s, err := openStore()
	if err != nil {
		return nil, err
	}
	defer s.Close()
	return s.ListNodes(fabricID)
}

// loadNodesForCompletion returns all commissioned nodes for use in shell
// completion and target-resolution code paths. When a session daemon is
// running it queries the daemon via its Unix socket (avoiding the BoltDB
// exclusive lock that the daemon holds). Otherwise it opens the DB directly.
func loadNodesForCompletion(fabricID uint64) ([]*store.Node, error) {
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

// loadNodeForCompletion is like loadNodesForCompletion but returns a single
// node by ID, searching the full node list returned by the daemon or store.
func loadNodeForCompletion(fabricID, nodeID uint64) (*store.Node, error) {
	nodes, err := loadNodesForCompletion(fabricID)
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

// loadFabric returns the fabric record, querying the daemon if it is running
// (to avoid contending on the BoltDB exclusive lock), otherwise opening the
// store directly.
func loadFabric(fabricID uint64) (*store.Fabric, error) {
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

// removeNode removes a node record from the store, routing through the daemon
// when it is running so that the BoltDB exclusive lock is respected. The daemon
// also evicts any cached CASE session for the node.
func removeNode(fabricID, nodeID uint64) error {
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

// persistNode persists an updated node record, routing through the daemon when
// it is running so the BoltDB exclusive lock is respected.
func persistNode(fabricID uint64, node *store.Node) error {
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

// resolveNodeLabel returns the node's name if it has one, otherwise "node X".
// Used to build consistent Bold-ready labels throughout CLI output.
func resolveNodeLabel(nodeID uint64) string {
	fid := viper.GetUint64("default-fabric-id")
	if fid == 0 {
		fid = 1
	}
	if node, err := loadNodeForCompletion(fid, nodeID); err == nil && node.Name != "" {
		return node.Name
	}
	return fmt.Sprintf("node %d", nodeID)
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
