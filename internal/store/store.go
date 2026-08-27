// Copyright 2026 matter-cli contributors
// SPDX-License-Identifier: Apache-2.0

package store

import (
	"errors"
	"sort"
)

// ErrNotFound is returned when a requested entity does not exist in the store.
var ErrNotFound = errors.New("not found")

// sortNodesByID sorts nodes in place by ascending numeric Node.ID. Both Store
// implementations use this helper so their ordering guarantees cannot drift.
func sortNodesByID(nodes []*Node) {
	sort.Slice(nodes, func(i, j int) bool {
		return nodes[i].ID < nodes[j].ID
	})
}

// Store defines the interface for persistent storage of Matter fabrics,
// nodes, session resumption data, and arbitrary key-value pairs.
type Store interface {
	// SaveFabric persists a fabric, creating or updating it by ID.
	SaveFabric(fabric *Fabric) error
	// GetFabric retrieves a fabric by its ID. Returns ErrNotFound if it does
	// not exist.
	GetFabric(fabricID uint64) (*Fabric, error)
	// ListFabrics returns all stored fabrics.
	ListFabrics() ([]*Fabric, error)
	// DeleteFabric removes a fabric by ID. Returns ErrNotFound if it does not
	// exist.
	DeleteFabric(fabricID uint64) error

	// SaveNode persists a node under the given fabric, creating or updating it
	// by ID.
	SaveNode(fabricID uint64, node *Node) error
	// GetNode retrieves a node by fabric and node ID. Returns ErrNotFound if
	// either the fabric or node does not exist.
	GetNode(fabricID uint64, nodeID uint64) (*Node, error)
	// ListNodes returns all nodes belonging to the given fabric, ordered by
	// ascending numeric Node.ID. Returns ErrNotFound if the fabric does not
	// exist.
	ListNodes(fabricID uint64) ([]*Node, error)
	// DeleteNode removes a node by fabric and node ID. Returns ErrNotFound if
	// it does not exist.
	DeleteNode(fabricID uint64, nodeID uint64) error

	// SaveResumptionInfo persists CASE session resumption data keyed by peer
	// node ID.
	SaveResumptionInfo(info *ResumptionInfo) error
	// GetResumptionInfo retrieves resumption data for the given peer node.
	// Returns ErrNotFound if none exists.
	GetResumptionInfo(peerNodeID uint64) (*ResumptionInfo, error)

	// Set stores an arbitrary key-value pair.
	Set(key string, value []byte) error
	// Get retrieves the value for the given key. Returns ErrNotFound if the key
	// does not exist.
	Get(key string) ([]byte, error)

	// Close releases any resources held by the store.
	Close() error
}
