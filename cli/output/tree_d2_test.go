// Copyright 2026 matter-cli contributors
// SPDX-License-Identifier: Apache-2.0

package output

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/p0fi/matter-cli/internal/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildD2Script_Level1(t *testing.T) {
	data := &TreeData{
		NodeID:    1,
		NodeName:  "Kitchen Light",
		VendorID:  0x1234,
		ProductID: 0x5678,
		Level:     1,
		Endpoints: []TreeEndpoint{
			{ID: 0, DeviceTypes: []store.DeviceType{{ID: 0x0016}}},
			{ID: 1, DeviceTypes: []store.DeviceType{{ID: 0x0100}}},
		},
	}

	script := buildD2Script(data)

	assert.Contains(t, script, "Kitchen Light")
	assert.Contains(t, script, "0x1234")
	assert.Contains(t, script, "ProductID: 0x5678")
	// Nested: endpoints inside node container
	assert.Contains(t, script, "ep0:")
	assert.Contains(t, script, "ep1:")
	assert.Contains(t, script, "Root Node")
	assert.Contains(t, script, "On/Off Light")
	// Level 1: no clusters
	assert.NotContains(t, script, "cl0")
}

func TestBuildD2Script_Level2(t *testing.T) {
	data := &TreeData{
		NodeID:   1,
		NodeName: "Test",
		Level:    2,
		Endpoints: []TreeEndpoint{
			{
				ID: 0,
				Clusters: []TreeCluster{
					{ID: 0x001D, Name: "Descriptor"},
					{ID: 0x0006, Name: "OnOff"},
				},
			},
		},
	}

	script := buildD2Script(data)

	// Clusters nested inside endpoint container
	assert.Contains(t, script, "cl001d:")
	assert.Contains(t, script, "cl0006:")
	assert.Contains(t, script, "Descriptor (0x001D)")
	assert.Contains(t, script, "OnOff (0x0006)")
}

func TestBuildD2Script_Level3_ClassShape(t *testing.T) {
	data := &TreeData{
		NodeID:   1,
		NodeName: "Test",
		Level:    3,
		Endpoints: []TreeEndpoint{
			{
				ID: 1,
				Clusters: []TreeCluster{
					{
						ID:   0x0006,
						Name: "OnOff",
						Attrs: []TreeAttribute{
							{ID: 0x0000, Name: "OnOff"},
							{ID: 0x4000, Name: "GlobalSceneControl"},
							{ID: 0xFFFD, Name: "ClusterRevision"},
						},
					},
				},
			},
		},
	}

	script := buildD2Script(data)

	assert.Contains(t, script, "shape: class")
	assert.Contains(t, script, "OnOff (0x0006)")
	assert.Contains(t, script, "GlobalSceneControl")
	assert.Contains(t, script, "ClusterRevision")
}

func TestBuildD2Script_Level4_Values(t *testing.T) {
	data := &TreeData{
		NodeID:   1,
		NodeName: "Test",
		Level:    4,
		Endpoints: []TreeEndpoint{
			{
				ID: 1,
				Clusters: []TreeCluster{
					{
						ID:   0x0006,
						Name: "OnOff",
						Attrs: []TreeAttribute{
							{ID: 0x0000, Name: "OnOff", Value: "true"},
							{ID: 0x4000, Name: "GlobalSceneControl", Err: "<timeout>"},
						},
					},
				},
			},
		},
	}

	script := buildD2Script(data)

	assert.Contains(t, script, `OnOff: "true"`)
	assert.Contains(t, script, `GlobalSceneControl: "<timeout>"`)
}

func TestBuildD2Script_UnknownCluster(t *testing.T) {
	data := &TreeData{
		NodeID:   1,
		NodeName: "Test",
		Level:    2,
		Endpoints: []TreeEndpoint{
			{
				ID: 0,
				Clusters: []TreeCluster{
					{ID: 0x9999, Name: ""},
				},
			},
		},
	}

	script := buildD2Script(data)

	assert.Contains(t, script, `"0x9999"`)
}

func TestBuildD2Script_NodeStyling(t *testing.T) {
	data := &TreeData{
		NodeID:   1,
		NodeName: "Test",
		Level:    1,
		Endpoints: []TreeEndpoint{
			{ID: 0, DeviceTypes: []store.DeviceType{{ID: 0x0016}}},
		},
	}

	script := buildD2Script(data)

	assert.Contains(t, script, "style.stroke-width: 3")
	assert.Contains(t, script, `style.fill: "#FAFAFA"`)
}

func TestBuildD2Script_EndpointColors(t *testing.T) {
	data := &TreeData{
		NodeID:   1,
		NodeName: "Test",
		Level:    1,
		Endpoints: []TreeEndpoint{
			{ID: 0, DeviceTypes: []store.DeviceType{{ID: 0x0016}}}, // Root Node → indigo
			{ID: 1, DeviceTypes: []store.DeviceType{{ID: 0x0100}}}, // On/Off Light → amber
		},
	}

	script := buildD2Script(data)

	assert.Contains(t, script, `"#E8EAF6"`) // Root Node color
	assert.Contains(t, script, `"#FFF8E1"`) // Lighting color
}

func TestBuildD2Script_ClusterSideTag(t *testing.T) {
	data := &TreeData{
		NodeID:   1,
		NodeName: "Test",
		Level:    2,
		Endpoints: []TreeEndpoint{
			{
				ID: 0,
				Clusters: []TreeCluster{
					{ID: 0x0006, Name: "OnOff", Side: "server"},
					{ID: 0x0008, Name: "LevelControl", Side: "client"},
				},
			},
		},
	}

	script := buildD2Script(data)

	assert.Contains(t, script, "OnOff (0x0006) [S]")
	assert.Contains(t, script, "LevelControl (0x0008) [C]")
}

func TestBuildD2Script_UtilityClusterOpacity(t *testing.T) {
	data := &TreeData{
		NodeID:   1,
		NodeName: "Test",
		Level:    2,
		Endpoints: []TreeEndpoint{
			{
				ID: 0,
				Clusters: []TreeCluster{
					{ID: 0x001D, Name: "Descriptor"},  // utility
					{ID: 0x0006, Name: "OnOff"},        // application
				},
			},
		},
	}

	script := buildD2Script(data)

	// Descriptor should have opacity styling
	assert.Contains(t, script, "style.opacity: 0.7")
	// Count occurrences - only utility cluster should have it
	assert.Equal(t, 1, strings.Count(script, "style.opacity"))
}

func TestD2SafeKey(t *testing.T) {
	assert.Equal(t, "OnOff", d2SafeKey("OnOff"))
	assert.Equal(t, "ClusterRevision", d2SafeKey("ClusterRevision"))
	assert.Equal(t, "0x0006", d2SafeKey("0x0006"))
	assert.Equal(t, `"has space"`, d2SafeKey("has space"))
}

func TestRenderTreeSVG(t *testing.T) {
	data := &TreeData{
		NodeID:      1,
		NodeName:    "Test Device",
		VendorID:    0x1234,
		ProductID:   0x5678,
		LastAddress: "192.168.1.42:5540",
		Level:       2,
		Endpoints: []TreeEndpoint{
			{
				ID:          0,
				DeviceTypes: []store.DeviceType{{ID: 0x0016}},
				Clusters: []TreeCluster{
					{ID: 0x001D, Name: "Descriptor"},
					{ID: 0x0028, Name: "BasicInformation"},
				},
			},
			{
				ID:          1,
				DeviceTypes: []store.DeviceType{{ID: 0x0100}},
				Clusters: []TreeCluster{
					{ID: 0x0006, Name: "OnOff"},
					{ID: 0x0008, Name: "LevelControl"},
				},
			},
		},
	}

	dir := t.TempDir()
	outFile := filepath.Join(dir, "test.svg")

	err := RenderTreeSVG(data, outFile)
	require.NoError(t, err)

	content, err := os.ReadFile(outFile)
	require.NoError(t, err)

	svg := string(content)
	assert.True(t, strings.Contains(svg, "<svg") || strings.Contains(svg, "<?xml"), "output should be SVG")
	assert.Contains(t, svg, "0x1234")
	assert.Contains(t, svg, "Endpoint 0")
	assert.Contains(t, svg, "Endpoint 1")
}

func TestRenderTreeSVG_Level3(t *testing.T) {
	data := &TreeData{
		NodeID:   1,
		NodeName: "Light",
		Level:    3,
		Endpoints: []TreeEndpoint{
			{
				ID: 1,
				Clusters: []TreeCluster{
					{
						ID:   0x0006,
						Name: "OnOff",
						Attrs: []TreeAttribute{
							{ID: 0x0000, Name: "OnOff"},
							{ID: 0xFFFD, Name: "ClusterRevision"},
						},
					},
				},
			},
		},
	}

	dir := t.TempDir()
	outFile := filepath.Join(dir, "test-l3.svg")

	err := RenderTreeSVG(data, outFile)
	require.NoError(t, err)

	content, err := os.ReadFile(outFile)
	require.NoError(t, err)

	svg := string(content)
	assert.Contains(t, svg, "OnOff")
	assert.Contains(t, svg, "ClusterRevision")
}
