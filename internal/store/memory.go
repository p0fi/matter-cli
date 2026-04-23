// Copyright 2026 matter-cli contributors
// SPDX-License-Identifier: Apache-2.0

package store

import (
	"fmt"
	"sync"
)

// MemoryStore is a concurrency-safe, in-memory implementation of Store. It
// never touches the filesystem and is intended for use in tests.
type MemoryStore struct {
	mu      sync.RWMutex
	fabrics map[uint64]*Fabric
	nodes   map[uint64]map[uint64]*Node // fabricID -> nodeID -> Node
	resume  map[uint64]*ResumptionInfo  // peerNodeID -> info
	kv      map[string][]byte
	closed  bool
}

// NewMemoryStore returns a new empty MemoryStore.
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		fabrics: make(map[uint64]*Fabric),
		nodes:   make(map[uint64]map[uint64]*Node),
		resume:  make(map[uint64]*ResumptionInfo),
		kv:      make(map[string][]byte),
	}
}

func (m *MemoryStore) SaveFabric(fabric *Fabric) error {
	if fabric == nil {
		return fmt.Errorf("store: fabric must not be nil")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	// Deep copy to avoid caller mutations.
	cp := *fabric
	m.fabrics[cp.ID] = &cp
	return nil
}

func (m *MemoryStore) GetFabric(fabricID uint64) (*Fabric, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	f, ok := m.fabrics[fabricID]
	if !ok {
		return nil, fmt.Errorf("store: fabric %d: %w", fabricID, ErrNotFound)
	}
	cp := *f
	return &cp, nil
}

func (m *MemoryStore) ListFabrics() ([]*Fabric, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]*Fabric, 0, len(m.fabrics))
	for _, f := range m.fabrics {
		cp := *f
		out = append(out, &cp)
	}
	return out, nil
}

func (m *MemoryStore) DeleteFabric(fabricID uint64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.fabrics[fabricID]; !ok {
		return fmt.Errorf("store: fabric %d: %w", fabricID, ErrNotFound)
	}
	delete(m.fabrics, fabricID)
	delete(m.nodes, fabricID)
	return nil
}

func (m *MemoryStore) SaveNode(fabricID uint64, node *Node) error {
	if node == nil {
		return fmt.Errorf("store: node must not be nil")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.fabrics[fabricID]; !ok {
		return fmt.Errorf("store: fabric %d: %w", fabricID, ErrNotFound)
	}
	if m.nodes[fabricID] == nil {
		m.nodes[fabricID] = make(map[uint64]*Node)
	}
	cp := *node
	cp.FabricID = fabricID
	m.nodes[fabricID][cp.ID] = &cp
	return nil
}

func (m *MemoryStore) GetNode(fabricID uint64, nodeID uint64) (*Node, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	nodes, ok := m.nodes[fabricID]
	if !ok {
		return nil, fmt.Errorf("store: node %d in fabric %d: %w", nodeID, fabricID, ErrNotFound)
	}
	n, ok := nodes[nodeID]
	if !ok {
		return nil, fmt.Errorf("store: node %d in fabric %d: %w", nodeID, fabricID, ErrNotFound)
	}
	cp := *n
	return &cp, nil
}

func (m *MemoryStore) ListNodes(fabricID uint64) ([]*Node, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if _, ok := m.fabrics[fabricID]; !ok {
		return nil, fmt.Errorf("store: fabric %d: %w", fabricID, ErrNotFound)
	}
	nodes := m.nodes[fabricID]
	out := make([]*Node, 0, len(nodes))
	for _, n := range nodes {
		cp := *n
		out = append(out, &cp)
	}
	return out, nil
}

func (m *MemoryStore) DeleteNode(fabricID uint64, nodeID uint64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	nodes, ok := m.nodes[fabricID]
	if !ok {
		return fmt.Errorf("store: node %d in fabric %d: %w", nodeID, fabricID, ErrNotFound)
	}
	if _, ok := nodes[nodeID]; !ok {
		return fmt.Errorf("store: node %d in fabric %d: %w", nodeID, fabricID, ErrNotFound)
	}
	delete(nodes, nodeID)
	return nil
}

func (m *MemoryStore) SaveResumptionInfo(info *ResumptionInfo) error {
	if info == nil {
		return fmt.Errorf("store: resumption info must not be nil")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := *info
	// Deep-copy slices.
	cp.ResumptionID = append([]byte(nil), info.ResumptionID...)
	cp.SharedSecret = append([]byte(nil), info.SharedSecret...)
	cp.CASESessionParams = append([]byte(nil), info.CASESessionParams...)
	m.resume[cp.PeerNodeID] = &cp
	return nil
}

func (m *MemoryStore) GetResumptionInfo(peerNodeID uint64) (*ResumptionInfo, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	info, ok := m.resume[peerNodeID]
	if !ok {
		return nil, fmt.Errorf("store: resumption info for peer %d: %w", peerNodeID, ErrNotFound)
	}
	cp := *info
	cp.ResumptionID = append([]byte(nil), info.ResumptionID...)
	cp.SharedSecret = append([]byte(nil), info.SharedSecret...)
	cp.CASESessionParams = append([]byte(nil), info.CASESessionParams...)
	return &cp, nil
}

func (m *MemoryStore) Set(key string, value []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.kv[key] = append([]byte(nil), value...)
	return nil
}

func (m *MemoryStore) Get(key string) ([]byte, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	v, ok := m.kv[key]
	if !ok {
		return nil, fmt.Errorf("store: key %q: %w", key, ErrNotFound)
	}
	return append([]byte(nil), v...), nil
}

// Close is a no-op for the in-memory store.
func (m *MemoryStore) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.closed = true
	return nil
}
