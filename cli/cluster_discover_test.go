// Copyright 2026 matter-cli contributors
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"testing"

	"github.com/p0fi/matter-cli/internal/clusters"
	"github.com/p0fi/matter-cli/internal/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// discoverTestNode is a two-endpoint node: a root endpoint plus a light
// endpoint carrying OnOff, LevelControl, and one client-side binding.
func discoverTestNode() *store.Node {
	return &store.Node{
		ID: 1,
		Endpoints: []store.Endpoint{
			{ID: 0, Clusters: []store.ClusterRef{
				{ID: 0x001D, Name: "Descriptor", Side: "server"},
				{ID: 0x0028, Name: "BasicInformation", Side: "server"},
			}},
			{ID: 1, Clusters: []store.ClusterRef{
				{ID: 0x0006, Name: "OnOff", Side: "server"},
				{ID: 0x0008, Name: "LevelControl", Side: "server"},
				{ID: 0x001E, Name: "Binding", Side: "client"},
			}},
		},
	}
}

// pairs flattens discoverTargets output for terse assertions.
func pairs(targets []discoverTarget) [][2]uint32 {
	out := make([][2]uint32, 0, len(targets))
	for _, t := range targets {
		out = append(out, [2]uint32{uint32(t.endpoint), t.clusterID})
	}
	return out
}

// TestDiscoverTargets covers the three scopes the command supports. The node-only
// case is the reason it cannot use requireTarget: that helper collapses "no
// endpoint given" into endpoint 0, which would silently turn a node-wide sweep
// into an endpoint-0-only one.
func TestDiscoverTargets(t *testing.T) {
	onOff, ok := clusters.Global.ClusterByName("OnOff")
	require.True(t, ok)

	tests := []struct {
		name   string
		target *Target
		only   *clusters.ClusterInfo
		want   [][2]uint32
	}{
		{
			name:   "node-only walks every endpoint",
			target: &Target{NodeID: 1},
			want: [][2]uint32{
				{0, 0x001D}, {0, 0x0028}, {1, 0x0006}, {1, 0x0008},
			},
		},
		{
			name:   "endpoint-explicit is scoped to that endpoint",
			target: &Target{NodeID: 1, Endpoint: 1, EndpointSet: true, ExplicitEndpoint: true},
			want:   [][2]uint32{{1, 0x0006}, {1, 0x0008}},
		},
		{
			name:   "endpoint 0 explicit is distinct from node-only",
			target: &Target{NodeID: 1, Endpoint: 0, EndpointSet: true, ExplicitEndpoint: true},
			want:   [][2]uint32{{0, 0x001D}, {0, 0x0028}},
		},
		{
			name:   "cluster filter narrows a node-wide sweep",
			target: &Target{NodeID: 1},
			only:   onOff,
			want:   [][2]uint32{{1, 0x0006}},
		},
		{
			name:   "cluster filter narrows an endpoint-scoped sweep",
			target: &Target{NodeID: 1, Endpoint: 1, EndpointSet: true, ExplicitEndpoint: true},
			only:   onOff,
			want:   [][2]uint32{{1, 0x0006}},
		},
		{
			name:   "cluster absent from the scoped endpoint yields nothing",
			target: &Target{NodeID: 1, Endpoint: 0, EndpointSet: true, ExplicitEndpoint: true},
			only:   onOff,
			want:   nil,
		},
		{
			name:   "unknown endpoint yields nothing",
			target: &Target{NodeID: 1, Endpoint: 9, EndpointSet: true, ExplicitEndpoint: true},
			want:   nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := discoverTargets(discoverTestNode(), tt.target, tt.only)
			if tt.want == nil {
				assert.Empty(t, got)
				return
			}
			assert.Equal(t, tt.want, pairs(got))
		})
	}
}

// TestDiscoverTargetsSkipsClientClusters pins the decision to skip client-side
// cluster references: an AttributeList read only makes sense against a server.
func TestDiscoverTargetsSkipsClientClusters(t *testing.T) {
	got := discoverTargets(discoverTestNode(), &Target{NodeID: 1}, nil)
	for _, tgt := range got {
		assert.NotEqual(t, uint32(0x001E), tgt.clusterID,
			"client-side Binding cluster should not be swept")
	}
}

// TestDiscoverTargetsTreatsBlankSideAsServer keeps older store records — written
// before the Side field was populated — discoverable.
func TestDiscoverTargetsTreatsBlankSideAsServer(t *testing.T) {
	node := &store.Node{
		ID:        1,
		Endpoints: []store.Endpoint{{ID: 1, Clusters: []store.ClusterRef{{ID: 0x0006}}}},
	}
	got := discoverTargets(node, &Target{NodeID: 1}, nil)
	assert.Equal(t, [][2]uint32{{1, 0x0006}}, pairs(got))
}

func TestDiscoverNoTargetsError(t *testing.T) {
	onOff, ok := clusters.Global.ClusterByName("OnOff")
	require.True(t, ok)
	node := discoverTestNode()

	tests := []struct {
		name   string
		target *Target
		only   *clusters.ClusterInfo
		want   string
	}{
		{
			name:   "cluster on a specific endpoint",
			target: &Target{NodeID: 1, Endpoint: 0, EndpointSet: true, ExplicitEndpoint: true},
			only:   onOff,
			want:   "no OnOff cluster on endpoint 0",
		},
		{
			name:   "cluster anywhere on the node",
			target: &Target{NodeID: 1},
			only:   onOff,
			want:   "no OnOff cluster on any endpoint",
		},
		{
			name:   "endpoint with no server clusters",
			target: &Target{NodeID: 1, Endpoint: 3, EndpointSet: true, ExplicitEndpoint: true},
			want:   "no server clusters on endpoint 3",
		},
		{
			name:   "node with no server clusters",
			target: &Target{NodeID: 1},
			want:   "no server clusters",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := discoverNoTargetsError(node, tt.target, tt.only)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.want)
		})
	}
}

func TestDiscoverClusterLabel(t *testing.T) {
	assert.Equal(t, "On/Off", discoverClusterLabel(0x0006, "OnOff"),
		"the registry display name wins over the stored name")
	assert.Equal(t, "MyCluster", discoverClusterLabel(0xFC00, "MyCluster"),
		"unregistered clusters fall back to the stored name")
	assert.Equal(t, "0xFC00", discoverClusterLabel(0xFC00, ""),
		"with neither, fall back to hex")
}

func TestPluralClusters(t *testing.T) {
	assert.Equal(t, "0 clusters", pluralClusters(0))
	assert.Equal(t, "1 cluster", pluralClusters(1))
	assert.Equal(t, "2 clusters", pluralClusters(2))
}

// TestClusterDiscoverRequiresTarget checks the command refuses to run without a
// node, and points the user at the ways to supply one.
func TestClusterDiscoverRequiresTarget(t *testing.T) {
	prev := resolvedTarget
	resolvedTarget = nil
	t.Cleanup(func() { resolvedTarget = prev })

	cmd := newClusterDiscoverCmd()
	err := runClusterDiscover(cmd)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no target specified")
}

// TestClusterDiscoverRegistered ensures `cluster discover` is wired into the
// generic cluster command group and, per the issue, has no shorthand form.
func TestClusterDiscoverRegistered(t *testing.T) {
	var discover bool
	for _, sub := range newClusterCmd().Commands() {
		if sub.Name() == "discover" {
			discover = true
		}
	}
	assert.True(t, discover, "cluster discover should be a subcommand of cluster")

	for _, sh := range shorthandCmds {
		for _, sub := range sh.Commands() {
			assert.NotEqual(t, "discover", sub.Name(),
				"shorthand cluster %q should not gain a discover verb", sh.Name())
		}
	}
}
