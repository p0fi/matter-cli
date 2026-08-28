// Copyright 2026 matter-cli contributors
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"strings"
	"testing"

	"github.com/p0fi/matter-cli/internal/clusters"
	"github.com/p0fi/matter-cli/internal/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// onOffCluster returns the registered OnOff cluster, which every test in this
// file uses as a stand-in for "a real spec cluster".
func onOffCluster(t *testing.T) *clusters.ClusterInfo {
	t.Helper()
	cl, ok := clusters.Global.ClusterByName("OnOff")
	require.True(t, ok, "OnOff cluster must be registered")
	return cl
}

func TestParseAttributeID(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  uint32
		ok    bool
	}{
		{"hex-lower", "0x0006", 0x0006, true},
		{"hex-upper", "0X4321", 0x4321, true},
		{"hex-mixed-digits", "0xFffB", 0xFFFB, true},
		{"decimal", "6", 6, true},
		{"decimal-large", "4294967295", 4294967295, true},
		{"surrounding-space", "  0x0006  ", 0x0006, true},
		{"attribute-name", "OnOff", 0, false},
		{"empty", "", 0, false},
		{"bare-hex-digits", "FFFB", 0, false},
		{"overflow", "4294967296", 0, false},
		{"negative", "-1", 0, false},
		{"hex-prefix-only", "0x", 0, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := parseAttributeID(tt.input)
			assert.Equal(t, tt.ok, ok)
			if tt.ok {
				assert.Equal(t, tt.want, got)
			}
		})
	}
}

// TestResolveReadableAttribute covers the read/subscribe escape hatch: names
// resolve as before, registry-known IDs resolve to the full spec definition,
// and IDs the registry has never heard of still resolve — with an empty Type,
// so formatAttrValue falls through to the generic TLV decoder.
func TestResolveReadableAttribute(t *testing.T) {
	cl := onOffCluster(t)

	t.Run("by name", func(t *testing.T) {
		attr, err := resolveReadableAttribute(cl, "OnOff")
		require.NoError(t, err)
		assert.Equal(t, uint32(0x0000), attr.ID)
		assert.Equal(t, "bool", attr.Type)
	})

	t.Run("by name is case-insensitive", func(t *testing.T) {
		attr, err := resolveReadableAttribute(cl, "onoff")
		require.NoError(t, err)
		assert.Equal(t, uint32(0x0000), attr.ID)
	})

	t.Run("registry-known hex ID keeps the spec type", func(t *testing.T) {
		attr, err := resolveReadableAttribute(cl, "0x0000")
		require.NoError(t, err)
		assert.Equal(t, uint32(0x0000), attr.ID)
		assert.Equal(t, "OnOff", attr.Name)
		assert.Equal(t, "bool", attr.Type)
	})

	t.Run("registry-known decimal ID", func(t *testing.T) {
		attr, err := resolveReadableAttribute(cl, "16385")
		require.NoError(t, err)
		assert.Equal(t, uint32(0x4001), attr.ID)
	})

	t.Run("global attribute by ID", func(t *testing.T) {
		attr, err := resolveReadableAttribute(cl, "0xFFFB")
		require.NoError(t, err)
		assert.Equal(t, "AttributeList", attr.Name)
	})

	t.Run("unknown ID falls back to a raw TLV read", func(t *testing.T) {
		attr, err := resolveReadableAttribute(cl, "0x1234")
		require.NoError(t, err)
		assert.Equal(t, uint32(0x1234), attr.ID)
		assert.Equal(t, "0x1234", attr.Name)
		assert.Equal(t, "0x1234", attr.DisplayName)
		assert.Empty(t, attr.Type, "an empty Type routes formatAttrValue to the generic TLV decoder")
		assert.True(t, attr.Readable)
	})

	t.Run("unknown name is rejected with a discover hint", func(t *testing.T) {
		_, err := resolveReadableAttribute(cl, "NotAnAttribute")
		require.Error(t, err)
		assert.Contains(t, err.Error(), `unknown attribute "NotAnAttribute"`)
		assert.Contains(t, err.Error(), "matter cluster discover")
	})
}

// TestResolveWritableAttribute covers the deliberately narrower write escape
// hatch: encoding a value into TLV needs the attribute's type up front, so an
// ID outside the spec registry has to be refused rather than guessed at.
func TestResolveWritableAttribute(t *testing.T) {
	cl := onOffCluster(t)

	t.Run("by name", func(t *testing.T) {
		attr, err := resolveWritableAttribute(cl, "OnTime")
		require.NoError(t, err)
		assert.True(t, attr.Writable)
	})

	t.Run("registry-known hex ID resolves with its type", func(t *testing.T) {
		byName, ok := clusters.Global.AttributeByName(cl.ID, "OnTime")
		require.True(t, ok)

		attr, err := resolveWritableAttribute(cl, "0x4001")
		require.NoError(t, err)
		assert.Equal(t, byName.ID, attr.ID)
		assert.Equal(t, byName.Type, attr.Type)
		assert.NotEmpty(t, attr.Type, "a write needs a type to encode against")
	})

	t.Run("registry-known decimal ID", func(t *testing.T) {
		attr, err := resolveWritableAttribute(cl, "16385")
		require.NoError(t, err)
		assert.Equal(t, uint32(0x4001), attr.ID)
	})

	t.Run("unknown ID is refused, not attempted", func(t *testing.T) {
		_, err := resolveWritableAttribute(cl, "0x1234")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "0x1234")
		assert.Contains(t, err.Error(), "type is unknown")
		assert.Contains(t, err.Error(), "matter cluster read")
	})

	t.Run("unknown name is rejected", func(t *testing.T) {
		_, err := resolveWritableAttribute(cl, "NotAnAttribute")
		require.Error(t, err)
		assert.Contains(t, err.Error(), `unknown attribute "NotAnAttribute"`)
	})
}

// TestResolveReadableAttributeIsNotAffectedByFilter documents the contract that
// makes the escape hatch worth having: resolution never consults the cached
// AttributeList, so an attribute the device implements without advertising it
// stays reachable.
func TestResolveReadableAttributeIsNotAffectedByFilter(t *testing.T) {
	prev := targetEndpointAttributeIDs
	t.Cleanup(func() { targetEndpointAttributeIDs = prev })

	cl := onOffCluster(t)
	// A cache claiming the device implements nothing at all.
	targetEndpointAttributeIDs = map[uint32]map[uint32]bool{cl.ID: {}}

	attr, err := resolveReadableAttribute(cl, "0x0000")
	require.NoError(t, err)
	assert.Equal(t, uint32(0x0000), attr.ID)

	named, err := resolveReadableAttribute(cl, "OnOff")
	require.NoError(t, err)
	assert.Equal(t, uint32(0x0000), named.ID)
}

func TestApplyAttributeList(t *testing.T) {
	newNode := func() *store.Node {
		return &store.Node{
			ID: 1,
			Endpoints: []store.Endpoint{
				{ID: 0, Clusters: []store.ClusterRef{{ID: 0x001D, Side: "server"}}},
				{ID: 1, Clusters: []store.ClusterRef{
					{ID: 0x0006, Side: "server"},
					{ID: 0x0008, Side: "server", Attributes: []uint32{0x0000}},
				}},
			},
		}
	}

	t.Run("records the list on the matching cluster", func(t *testing.T) {
		node := newNode()
		assert.True(t, applyAttributeList(node, 1, 0x0006, []uint32{0, 0x4001, 0xFFFB}))
		assert.Equal(t, []uint32{0, 0x4001, 0xFFFB}, node.Endpoints[1].Clusters[0].Attributes)
		// Sibling clusters and endpoints are untouched.
		assert.Equal(t, []uint32{0x0000}, node.Endpoints[1].Clusters[1].Attributes)
		assert.Nil(t, node.Endpoints[0].Clusters[0].Attributes)
	})

	t.Run("replaces a previously cached list", func(t *testing.T) {
		node := newNode()
		require.True(t, applyAttributeList(node, 1, 0x0008, []uint32{0x0000, 0x0001}))
		assert.Equal(t, []uint32{0x0000, 0x0001}, node.Endpoints[1].Clusters[1].Attributes)
	})

	t.Run("reports unknown endpoint and cluster", func(t *testing.T) {
		node := newNode()
		assert.False(t, applyAttributeList(node, 9, 0x0006, []uint32{0}))
		assert.False(t, applyAttributeList(node, 1, 0x9999, []uint32{0}))
	})

	t.Run("tolerates a nil node", func(t *testing.T) {
		assert.False(t, applyAttributeList(nil, 1, 0x0006, []uint32{0}))
	})
}

// TestApplyAttributeListPartialFailureKeepsStaleCache mirrors the write-through
// loop in tree and cluster discover: only clusters that were read successfully
// get applied, so a cluster whose read failed keeps its earlier list rather
// than reverting to "no filtering".
func TestApplyAttributeListPartialFailureKeepsStaleCache(t *testing.T) {
	node := &store.Node{
		ID: 1,
		Endpoints: []store.Endpoint{{
			ID: 1,
			Clusters: []store.ClusterRef{
				{ID: 0x0006, Side: "server", Attributes: []uint32{0x0000}},
				{ID: 0x0008, Side: "server", Attributes: []uint32{0x0000, 0x0011}},
			},
		}},
	}

	// OnOff read succeeds; LevelControl's read failed, so it is never applied.
	require.True(t, applyAttributeList(node, 1, 0x0006, []uint32{0x0000, 0x4001}))

	assert.Equal(t, []uint32{0x0000, 0x4001}, node.Endpoints[0].Clusters[0].Attributes,
		"successful read should refresh the cache")
	assert.Equal(t, []uint32{0x0000, 0x0011}, node.Endpoints[0].Clusters[1].Attributes,
		"failed read must leave the previously cached list untouched")
}

// TestAttributeHelpTextDocumentsEscapeHatch guards the acceptance criterion that
// the help text stops lying about numeric IDs.
func TestAttributeHelpTextDocumentsEscapeHatch(t *testing.T) {
	assert.Contains(t, attributeFlagUsage, "0x0006")
	assert.Contains(t, writeAttributeFlagUsage, "0x0006")
	assert.Contains(t, attributeEscapeHatchHelp, "matter cluster discover")
	assert.Contains(t, writeAttributeEscapeHatchHelp, "matter cluster discover")
	assert.True(t, strings.Contains(writeAttributeEscapeHatchHelp, "spec registry knows"),
		"write help must explain why unknown IDs are refused")
}
