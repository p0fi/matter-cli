// Copyright 2026 matter-cli contributors
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/p0fi/matter-cli/cli/output"
	"github.com/p0fi/matter-cli/internal/store"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// attrListKey identifies one cluster instance in a fake reader's script.
type attrListKey struct {
	endpoint  uint16
	clusterID uint32
}

// scriptedAttrLists builds an attrListReader that replays a fixed script and
// records the order in which clusters were visited. Clusters absent from the
// script fail with errBusy, standing in for a device that is momentarily
// unreachable.
func scriptedAttrLists(script map[attrListKey][]uint32, visited *[]attrListKey) attrListReader {
	return func(_ context.Context, endpoint uint16, clusterID uint32) ([]uint32, error) {
		key := attrListKey{endpoint, clusterID}
		if visited != nil {
			*visited = append(*visited, key)
		}
		ids, ok := script[key]
		if !ok {
			return nil, errBusy
		}
		return ids, nil
	}
}

var errBusy = errors.New("device busy")

// cacheTestNode is a node whose OnOff cluster already has a cached attribute
// list from an earlier run, and whose LevelControl cluster has never been read.
func cacheTestNode() *store.Node {
	return &store.Node{
		ID: 1,
		Endpoints: []store.Endpoint{
			{ID: 0, Clusters: []store.ClusterRef{{ID: 0x001D, Name: "Descriptor", Side: "server"}}},
			{ID: 1, Clusters: []store.ClusterRef{
				{ID: 0x0006, Name: "OnOff", Side: "server", Attributes: []uint32{0x0000, 0xFFFD}},
				{ID: 0x0008, Name: "LevelControl", Side: "server"},
			}},
		},
	}
}

// clusterRef returns the stored ClusterRef for an endpoint/cluster pair.
func clusterRef(t *testing.T, node *store.Node, endpoint uint16, clusterID uint32) store.ClusterRef {
	t.Helper()
	for _, ep := range node.Endpoints {
		if ep.ID != endpoint {
			continue
		}
		for _, cl := range ep.Clusters {
			if cl.ID == clusterID {
				return cl
			}
		}
	}
	t.Fatalf("cluster 0x%04X not found on endpoint %d", clusterID, endpoint)
	return store.ClusterRef{}
}

func TestRecordAttrListResult(t *testing.T) {
	t.Run("successful read refreshes the cache", func(t *testing.T) {
		node := cacheTestNode()
		assert.True(t, recordAttrListResult(node, 1, 0x0006, []uint32{0x0000, 0x4001}, nil))
		assert.Equal(t, []uint32{0x0000, 0x4001}, clusterRef(t, node, 1, 0x0006).Attributes)
	})

	t.Run("failed read keeps the previously cached list", func(t *testing.T) {
		node := cacheTestNode()
		assert.False(t, recordAttrListResult(node, 1, 0x0006, nil, errBusy))
		assert.Equal(t, []uint32{0x0000, 0xFFFD}, clusterRef(t, node, 1, 0x0006).Attributes)
	})

	t.Run("failed read on a cold cluster leaves it cold", func(t *testing.T) {
		node := cacheTestNode()
		assert.False(t, recordAttrListResult(node, 1, 0x0008, nil, errBusy))
		assert.Nil(t, clusterRef(t, node, 1, 0x0008).Attributes,
			"a cold cluster must stay cold so completion falls back to the spec list")
	})
}

// TestDiscoverAttributeLists is the `cluster discover` cache-refresh test: a
// node-wide sweep where one cluster's read fails must refresh the others and
// leave the failing cluster's earlier list intact.
func TestDiscoverAttributeLists(t *testing.T) {
	t.Run("refreshes every cluster it reads", func(t *testing.T) {
		node := cacheTestNode()
		targets := discoverTargets(node, &Target{NodeID: 1}, nil)
		require.Len(t, targets, 3)

		var visited []attrListKey
		read := scriptedAttrLists(map[attrListKey][]uint32{
			{0, 0x001D}: {0x0000, 0xFFFB},
			{1, 0x0006}: {0x0000, 0x4001, 0xFFFB},
			{1, 0x0008}: {0x0000, 0x0011},
		}, &visited)

		results, updated, failed := discoverAttributeLists(context.Background(), node, targets, read)

		assert.Equal(t, 3, updated)
		assert.Equal(t, 0, failed)
		require.Len(t, results, 3)
		assert.Equal(t, []attrListKey{{0, 0x001D}, {1, 0x0006}, {1, 0x0008}}, visited)

		assert.Equal(t, []uint32{0x0000, 0x4001, 0xFFFB}, clusterRef(t, node, 1, 0x0006).Attributes)
		assert.Equal(t, []uint32{0x0000, 0x0011}, clusterRef(t, node, 1, 0x0008).Attributes)
		assert.Equal(t, []uint32{0x0000, 0xFFFB}, clusterRef(t, node, 0, 0x001D).Attributes)
	})

	t.Run("a failed read leaves the stale cache untouched", func(t *testing.T) {
		node := cacheTestNode()
		targets := discoverTargets(node, &Target{NodeID: 1}, nil)

		// OnOff's read fails; the other two succeed.
		read := scriptedAttrLists(map[attrListKey][]uint32{
			{0, 0x001D}: {0x0000},
			{1, 0x0008}: {0x0000, 0x0011},
		}, nil)

		results, updated, failed := discoverAttributeLists(context.Background(), node, targets, read)

		assert.Equal(t, 2, updated)
		assert.Equal(t, 1, failed)
		assert.Equal(t, []uint32{0x0000, 0xFFFD}, clusterRef(t, node, 1, 0x0006).Attributes,
			"the failing cluster must keep its previously cached list")
		assert.Equal(t, []uint32{0x0000, 0x0011}, clusterRef(t, node, 1, 0x0008).Attributes,
			"a sibling failure must not block other refreshes")

		var onOffResult DiscoveredCluster
		for _, r := range results {
			if r.ClusterID == 0x0006 {
				onOffResult = r
			}
		}
		assert.NotEmpty(t, onOffResult.Error, "the failure should be reported to the user")
		assert.Empty(t, onOffResult.Attributes)
	})

	t.Run("endpoint-scoped sweep only touches that endpoint", func(t *testing.T) {
		node := cacheTestNode()
		targets := discoverTargets(node,
			&Target{NodeID: 1, Endpoint: 1, EndpointSet: true, ExplicitEndpoint: true}, nil)

		var visited []attrListKey
		read := scriptedAttrLists(map[attrListKey][]uint32{
			{1, 0x0006}: {0x0000},
			{1, 0x0008}: {0x0000},
		}, &visited)

		_, updated, failed := discoverAttributeLists(context.Background(), node, targets, read)

		assert.Equal(t, 2, updated)
		assert.Equal(t, 0, failed)
		assert.Equal(t, []attrListKey{{1, 0x0006}, {1, 0x0008}}, visited)
		assert.Nil(t, clusterRef(t, node, 0, 0x001D).Attributes,
			"endpoint 0 was out of scope and must not be touched")
	})

	t.Run("cluster-narrowed sweep only touches that cluster", func(t *testing.T) {
		node := cacheTestNode()
		onOff := onOffCluster(t)
		targets := discoverTargets(node, &Target{NodeID: 1}, onOff)

		var visited []attrListKey
		read := scriptedAttrLists(map[attrListKey][]uint32{{1, 0x0006}: {0x0000, 0x4001}}, &visited)

		_, updated, _ := discoverAttributeLists(context.Background(), node, targets, read)

		assert.Equal(t, 1, updated)
		assert.Equal(t, []attrListKey{{1, 0x0006}}, visited)
		assert.Equal(t, []uint32{0x0000, 0x4001}, clusterRef(t, node, 1, 0x0006).Attributes)
		assert.Nil(t, clusterRef(t, node, 1, 0x0008).Attributes)
	})
}

// treeDataFor builds the level-1/2 skeleton buildTreeData produces from a node,
// which is what treePopulateAttributes then augments.
func treeDataFor(node *store.Node, level int) *output.TreeData {
	data := &output.TreeData{NodeID: node.ID, Level: level}
	for _, ep := range node.Endpoints {
		te := output.TreeEndpoint{ID: ep.ID}
		for _, cl := range ep.Clusters {
			te.Clusters = append(te.Clusters, output.TreeCluster{ID: cl.ID, Name: cl.Name, Side: cl.Side})
		}
		data.Endpoints = append(data.Endpoints, te)
	}
	return data
}

// TestTreePopulateAttributes is the `tree -L 3`/`-L 4` cache-refresh test: the
// AttributeList read the tree already performs must write through to the same
// cache `cluster discover` populates, with the same partial-failure semantics.
func TestTreePopulateAttributes(t *testing.T) {
	noValues := func(context.Context, uint16, uint32, uint32) (string, error) {
		return "", errors.New("level 4 not requested")
	}

	t.Run("level 3 write-throughs every successful read", func(t *testing.T) {
		node := cacheTestNode()
		data := treeDataFor(node, 3)
		read := scriptedAttrLists(map[attrListKey][]uint32{
			{0, 0x001D}: {0x0000},
			{1, 0x0006}: {0x0000, 0x4001},
			{1, 0x0008}: {0x0000, 0x0011},
		}, nil)

		updated := treePopulateAttributes(context.Background(), data, node, 3, read, noValues)

		assert.True(t, updated)
		assert.Equal(t, []uint32{0x0000, 0x4001}, clusterRef(t, node, 1, 0x0006).Attributes)
		assert.Equal(t, []uint32{0x0000, 0x0011}, clusterRef(t, node, 1, 0x0008).Attributes)
		// The rendered tree still gets its attribute rows.
		assert.Len(t, data.Endpoints[1].Clusters[0].Attrs, 2)
	})

	t.Run("a failed read keeps the stale cache and reports the error in the tree", func(t *testing.T) {
		node := cacheTestNode()
		data := treeDataFor(node, 3)
		// OnOff fails; LevelControl succeeds.
		read := scriptedAttrLists(map[attrListKey][]uint32{
			{0, 0x001D}: {0x0000},
			{1, 0x0008}: {0x0000, 0x0011},
		}, nil)

		updated := treePopulateAttributes(context.Background(), data, node, 3, read, noValues)

		assert.True(t, updated, "other clusters were refreshed, so the node is worth persisting")
		assert.Equal(t, []uint32{0x0000, 0xFFFD}, clusterRef(t, node, 1, 0x0006).Attributes,
			"the failing cluster must keep its previously cached list")
		assert.Equal(t, []uint32{0x0000, 0x0011}, clusterRef(t, node, 1, 0x0008).Attributes)
		assert.NotEmpty(t, data.Endpoints[1].Clusters[0].ListErr)
		assert.Empty(t, data.Endpoints[1].Clusters[0].Attrs)
	})

	t.Run("no successful read means nothing to persist", func(t *testing.T) {
		node := cacheTestNode()
		data := treeDataFor(node, 3)
		read := scriptedAttrLists(nil, nil)

		updated := treePopulateAttributes(context.Background(), data, node, 3, read, noValues)

		assert.False(t, updated)
		assert.Equal(t, []uint32{0x0000, 0xFFFD}, clusterRef(t, node, 1, 0x0006).Attributes)
		assert.Nil(t, clusterRef(t, node, 1, 0x0008).Attributes)
	})

	t.Run("level 4 also reads values without changing cache semantics", func(t *testing.T) {
		node := cacheTestNode()
		data := treeDataFor(node, 4)
		read := scriptedAttrLists(map[attrListKey][]uint32{
			{0, 0x001D}: {0x0000},
			{1, 0x0006}: {0x0000},
			{1, 0x0008}: {0x0000},
		}, nil)
		readValue := func(_ context.Context, ep uint16, clID, attrID uint32) (string, error) {
			if clID == 0x0008 {
				return "", errBusy
			}
			return "42", nil
		}

		updated := treePopulateAttributes(context.Background(), data, node, 4, read, readValue)

		assert.True(t, updated)
		assert.Equal(t, "42", data.Endpoints[1].Clusters[0].Attrs[0].Value)
		assert.NotEmpty(t, data.Endpoints[1].Clusters[1].Attrs[0].Err,
			"a failed value read is surfaced per attribute")
		assert.Equal(t, []uint32{0x0000}, clusterRef(t, node, 1, 0x0008).Attributes,
			"a failed value read does not undo a successful AttributeList read")
	})

	t.Run("level 3 does not read values", func(t *testing.T) {
		node := cacheTestNode()
		data := treeDataFor(node, 3)
		read := scriptedAttrLists(map[attrListKey][]uint32{{1, 0x0006}: {0x0000}}, nil)

		valueReads := 0
		readValue := func(context.Context, uint16, uint32, uint32) (string, error) {
			valueReads++
			return "", nil
		}

		treePopulateAttributes(context.Background(), data, node, 3, read, readValue)
		assert.Zero(t, valueReads)
	})
}

// setupCLITestStore creates a temporary BoltDB store holding nodes and points
// the CLI's store helpers at it, so tests can exercise the real
// loadNodeForCompletion/persistNode paths rather than package variables.
func setupCLITestStore(t *testing.T, fabricID uint64, nodes []*store.Node) {
	t.Helper()

	dir := t.TempDir()
	dbDir := filepath.Join(dir, "matter-cli")
	if err := os.MkdirAll(dbDir, 0o755); err != nil {
		t.Fatalf("creating test db dir: %v", err)
	}

	s, err := store.NewBoltStore(filepath.Join(dbDir, "matter.db"))
	if err != nil {
		t.Fatalf("creating test BoltStore: %v", err)
	}
	if err := s.SaveFabric(&store.Fabric{ID: fabricID, Label: "test"}); err != nil {
		s.Close()
		t.Fatalf("saving test fabric: %v", err)
	}
	for _, n := range nodes {
		n.FabricID = fabricID
		if err := s.SaveNode(fabricID, n); err != nil {
			s.Close()
			t.Fatalf("saving test node %d: %v", n.ID, err)
		}
	}
	s.Close()

	t.Setenv("XDG_CONFIG_HOME", dir)
	viper.Set("default-fabric-id", fabricID)
	t.Cleanup(func() { viper.Set("default-fabric-id", nil) })
}

// TestPersistAttributeCacheKeepsSessionWrites is the regression test for the
// write-through clobbering unrelated fields. connectToNode refreshes LastSeen
// on every connect and LastAddress after mDNS rediscovery, both after the
// caller's snapshot was taken; persisting that snapshot would roll them back
// and leave the store pointing at an address already known to be dead.
func TestPersistAttributeCacheKeepsSessionWrites(t *testing.T) {
	const fabricID = 1
	stored := &store.Node{
		ID:          1,
		Name:        "Kitchen Light",
		LastAddress: "192.168.1.42:5540",
		Endpoints: []store.Endpoint{{
			ID: 1,
			Clusters: []store.ClusterRef{
				{ID: 0x0006, Name: "OnOff", Side: "server"},
				{ID: 0x0008, Name: "LevelControl", Side: "server"},
			},
		}},
	}
	setupCLITestStore(t, fabricID, []*store.Node{stored})

	// The snapshot the command loaded before opening its CASE session.
	snapshot, err := loadNodeForCompletion(fabricID, 1)
	require.NoError(t, err)

	// Meanwhile the session rediscovers the device at a new address and
	// stamps LastSeen, exactly as connectToNode does.
	live, err := loadNodeForCompletion(fabricID, 1)
	require.NoError(t, err)
	live.LastAddress = "192.168.1.99:5540"
	live.LastSeen = time.Date(2026, 8, 28, 12, 0, 0, 0, time.UTC)
	require.NoError(t, persistNode(fabricID, live))

	// The sweep caches what it read onto its stale snapshot, then persists.
	require.True(t, applyAttributeList(snapshot, 1, 0x0006, []uint32{0x0000, 0x4001}))
	require.NoError(t, persistAttributeCache(fabricID, snapshot))

	got, err := loadNodeForCompletion(fabricID, 1)
	require.NoError(t, err)

	assert.Equal(t, "192.168.1.99:5540", got.LastAddress,
		"the rediscovered address must survive the attribute write-through")
	assert.False(t, got.LastSeen.IsZero(), "LastSeen must not be rolled back")
	assert.Equal(t, []uint32{0x0000, 0x4001}, clusterRef(t, got, 1, 0x0006).Attributes,
		"the discovered attribute list must still be saved")
	assert.Nil(t, clusterRef(t, got, 1, 0x0008).Attributes,
		"clusters that were never read stay cold")
}

// TestFilterShorthandCommandsBuildsAttributeFilter covers the seam between the
// stored ClusterRef.Attributes and the completion filter — the conversion in
// filterShorthandCommands that the package-variable tests bypass.
func TestFilterShorthandCommandsBuildsAttributeFilter(t *testing.T) {
	const fabricID = 1
	setupCLITestStore(t, fabricID, []*store.Node{{
		ID: 1,
		Endpoints: []store.Endpoint{{
			ID: 1,
			Clusters: []store.ClusterRef{
				// Discovered: advertises OnOff and ClusterRevision only.
				{ID: 0x0006, Name: "OnOff", Side: "server", Attributes: []uint32{0x0000, 0xFFFD}},
				// Never discovered.
				{ID: 0x0008, Name: "LevelControl", Side: "server"},
				// Client side: must not contribute a filter at all.
				{ID: 0x001E, Name: "Binding", Side: "client", Attributes: []uint32{0x0000}},
			},
		}},
	}})
	restoreHiddenState(t)
	setAttributeCache(t, nil)

	filterShorthandCommands(&Target{NodeID: 1, Endpoint: 1, EndpointSet: true, ExplicitEndpoint: true})

	assert.Equal(t, map[uint32]bool{0x0000: true, 0xFFFD: true}, completionAttributeFilter(0x0006),
		"a discovered cluster's stored list should become its completion filter")
	assert.Nil(t, completionAttributeFilter(0x0008),
		"a cluster with no stored list must stay unfiltered")
	assert.Nil(t, completionAttributeFilter(0x001E),
		"client-side clusters are not completion targets")
}
