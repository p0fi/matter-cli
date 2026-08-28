// Copyright 2026 matter-cli contributors
// SPDX-License-Identifier: Apache-2.0

package clusters

import (
	"strings"
	"sync"
)

// Global is the default cluster registry. Cluster packages register
// themselves here via init() functions.
var Global = NewRegistry()

// GlobalAttributes are the standard Matter global attributes present on every
// cluster (spec section 7.13 "Global Attributes"). They are automatically
// appended to each cluster's Attributes list during Register().
var GlobalAttributes = []AttributeInfo{
	{ID: 0xFFF8, Name: "GeneratedCommandList", DisplayName: "GeneratedCommandList", Type: "list", Readable: true},
	{ID: 0xFFF9, Name: "AcceptedCommandList", DisplayName: "AcceptedCommandList", Type: "list", Readable: true},
	{ID: 0xFFFA, Name: "EventList", DisplayName: "EventList", Type: "list", Readable: true},
	{ID: 0xFFFB, Name: "AttributeList", DisplayName: "AttributeList", Type: "list", Readable: true},
	{ID: 0xFFFC, Name: "FeatureMap", DisplayName: "FeatureMap", Type: "bitmap32", Readable: true},
	{ID: 0xFFFD, Name: "ClusterRevision", DisplayName: "ClusterRevision", Type: "uint16", Readable: true},
}

// Registry holds known Matter cluster definitions and provides lookup and
// search operations for CLI auto-complete and validation.
type Registry struct {
	mu       sync.RWMutex
	byID     map[uint32]ClusterInfo
	byName   map[string]ClusterInfo // lower-case name -> info
	clusters []ClusterInfo
}

// NewRegistry creates an empty Registry.
func NewRegistry() *Registry {
	return &Registry{
		byID:   make(map[uint32]ClusterInfo),
		byName: make(map[string]ClusterInfo),
	}
}

// Register adds a cluster definition to the registry. If a cluster with the
// same ID is already registered, the new definition replaces it.
func (r *Registry) Register(info ClusterInfo) {
	r.mu.Lock()
	defer r.mu.Unlock()

	// Remove old entry if re-registering the same ID.
	if old, exists := r.byID[info.ID]; exists {
		delete(r.byName, strings.ToLower(old.Name))
		for i, c := range r.clusters {
			if c.ID == info.ID {
				r.clusters = append(r.clusters[:i], r.clusters[i+1:]...)
				break
			}
		}
	}

	// Append standard global attributes so they are available for read/subscribe.
	info.Attributes = append(info.Attributes, GlobalAttributes...)

	r.byID[info.ID] = info
	r.byName[strings.ToLower(info.Name)] = info
	r.clusters = append(r.clusters, info)
}

// ClusterByName returns the cluster with the given name (case-insensitive).
func (r *Registry) ClusterByName(name string) (*ClusterInfo, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	info, ok := r.byName[strings.ToLower(name)]
	if !ok {
		return nil, false
	}
	return &info, true
}

// ClusterByID returns the cluster with the given numeric ID.
func (r *Registry) ClusterByID(id uint32) (*ClusterInfo, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	info, ok := r.byID[id]
	if !ok {
		return nil, false
	}
	return &info, true
}

// AttributeByName returns the attribute with the given name (case-insensitive)
// within the specified cluster.
func (r *Registry) AttributeByName(clusterID uint32, name string) (*AttributeInfo, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	ci, ok := r.byID[clusterID]
	if !ok {
		return nil, false
	}
	lower := strings.ToLower(name)
	for i := range ci.Attributes {
		if strings.ToLower(ci.Attributes[i].Name) == lower {
			return &ci.Attributes[i], true
		}
	}
	return nil, false
}

// AttributeByID returns the attribute with the given numeric ID within the
// specified cluster. It complements AttributeByName so that callers can accept
// a raw attribute ID (e.g. "0x0006") wherever a name is accepted, and still
// recover the spec-defined name and type needed to format a read or encode a
// write.
func (r *Registry) AttributeByID(clusterID, id uint32) (*AttributeInfo, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	ci, ok := r.byID[clusterID]
	if !ok {
		return nil, false
	}
	for i := range ci.Attributes {
		if ci.Attributes[i].ID == id {
			return &ci.Attributes[i], true
		}
	}
	return nil, false
}

// CommandByName returns the command with the given name (case-insensitive)
// within the specified cluster.
func (r *Registry) CommandByName(clusterID uint32, name string) (*CommandInfo, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	ci, ok := r.byID[clusterID]
	if !ok {
		return nil, false
	}
	lower := strings.ToLower(name)
	for i := range ci.Commands {
		if strings.ToLower(ci.Commands[i].Name) == lower {
			return &ci.Commands[i], true
		}
	}
	return nil, false
}

// AllClusters returns a copy of all registered cluster definitions.
func (r *Registry) AllClusters() []ClusterInfo {
	r.mu.RLock()
	defer r.mu.RUnlock()

	out := make([]ClusterInfo, len(r.clusters))
	copy(out, r.clusters)
	return out
}

// SearchClusters returns clusters whose name or display name contains the
// query string (case-insensitive). Useful for CLI auto-complete.
func (r *Registry) SearchClusters(query string) []ClusterInfo {
	r.mu.RLock()
	defer r.mu.RUnlock()

	lower := strings.ToLower(query)
	var results []ClusterInfo
	for _, c := range r.clusters {
		if strings.Contains(strings.ToLower(c.Name), lower) ||
			strings.Contains(strings.ToLower(c.DisplayName), lower) {
			results = append(results, c)
		}
	}
	return results
}

// SearchCommands returns commands of the given cluster whose name or display
// name contains the query string (case-insensitive). Useful for CLI auto-complete.
func (r *Registry) SearchCommands(clusterID uint32, query string) []CommandInfo {
	r.mu.RLock()
	defer r.mu.RUnlock()

	ci, ok := r.byID[clusterID]
	if !ok {
		return nil
	}
	lower := strings.ToLower(query)
	var results []CommandInfo
	for _, c := range ci.Commands {
		if strings.Contains(strings.ToLower(c.Name), lower) ||
			strings.Contains(strings.ToLower(c.DisplayName), lower) {
			results = append(results, c)
		}
	}
	return results
}

// SearchAttributes returns attributes of the given cluster whose name or
// display name contains the query string (case-insensitive).
func (r *Registry) SearchAttributes(clusterID uint32, query string) []AttributeInfo {
	r.mu.RLock()
	defer r.mu.RUnlock()

	ci, ok := r.byID[clusterID]
	if !ok {
		return nil
	}
	lower := strings.ToLower(query)
	var results []AttributeInfo
	for _, a := range ci.Attributes {
		if strings.Contains(strings.ToLower(a.Name), lower) ||
			strings.Contains(strings.ToLower(a.DisplayName), lower) {
			results = append(results, a)
		}
	}
	return results
}
