// Copyright 2026 matter-cli contributors
// SPDX-License-Identifier: Apache-2.0

package clusters

import (
	"testing"
)

func newTestCluster() ClusterInfo {
	return ClusterInfo{
		ID:          0x0006,
		Name:        "OnOff",
		DisplayName: "On/Off",
		Attributes: []AttributeInfo{
			{ID: 0, Name: "OnOff", DisplayName: "OnOff", Type: "bool", Readable: true},
		},
		Commands: []CommandInfo{
			{ID: 0, Name: "Off", DisplayName: "Off"},
			{ID: 1, Name: "On", DisplayName: "On"},
			{ID: 2, Name: "Toggle", DisplayName: "Toggle"},
		},
	}
}

func TestRegister(t *testing.T) {
	r := NewRegistry()
	ci := newTestCluster()
	r.Register(ci)

	all := r.AllClusters()
	if len(all) != 1 {
		t.Fatalf("expected 1 cluster, got %d", len(all))
	}
	if all[0].ID != 0x0006 {
		t.Fatalf("expected ID 0x0006, got 0x%04X", all[0].ID)
	}
}

func TestRegisterReplace(t *testing.T) {
	r := NewRegistry()
	r.Register(newTestCluster())

	updated := newTestCluster()
	updated.DisplayName = "On/Off Updated"
	r.Register(updated)

	all := r.AllClusters()
	if len(all) != 1 {
		t.Fatalf("expected 1 cluster after replace, got %d", len(all))
	}
	if all[0].DisplayName != "On/Off Updated" {
		t.Fatalf("expected updated display name, got %q", all[0].DisplayName)
	}
}

func TestClusterByName(t *testing.T) {
	r := NewRegistry()
	r.Register(newTestCluster())

	tests := []struct {
		name  string
		query string
		found bool
	}{
		{"exact", "OnOff", true},
		{"upper", "ONOFF", true},
		{"mixed", "onoff", true},
		{"missing", "LevelControl", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ci, ok := r.ClusterByName(tt.query)
			if ok != tt.found {
				t.Fatalf("ClusterByName(%q) found=%v, want %v", tt.query, ok, tt.found)
			}
			if ok && ci.ID != 0x0006 {
				t.Fatalf("expected ID 0x0006, got 0x%04X", ci.ID)
			}
		})
	}
}

func TestClusterByID(t *testing.T) {
	r := NewRegistry()
	r.Register(newTestCluster())

	tests := []struct {
		name  string
		id    uint32
		found bool
	}{
		{"existing", 0x0006, true},
		{"missing", 0x9999, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, ok := r.ClusterByID(tt.id)
			if ok != tt.found {
				t.Fatalf("ClusterByID(0x%04X) found=%v, want %v", tt.id, ok, tt.found)
			}
		})
	}
}

func TestAttributeByName(t *testing.T) {
	r := NewRegistry()
	r.Register(newTestCluster())

	tests := []struct {
		name      string
		clusterID uint32
		attrName  string
		found     bool
	}{
		{"exact", 0x0006, "OnOff", true},
		{"case-insensitive", 0x0006, "onoff", true},
		{"missing-attr", 0x0006, "brightness", false},
		{"missing-cluster", 0x9999, "OnOff", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, ok := r.AttributeByName(tt.clusterID, tt.attrName)
			if ok != tt.found {
				t.Fatalf("AttributeByName(%d, %q) found=%v, want %v", tt.clusterID, tt.attrName, ok, tt.found)
			}
		})
	}
}

func TestCommandByName(t *testing.T) {
	r := NewRegistry()
	r.Register(newTestCluster())

	tests := []struct {
		name      string
		clusterID uint32
		cmdName   string
		found     bool
	}{
		{"exact", 0x0006, "On", true},
		{"case-insensitive", 0x0006, "on", true},
		{"toggle", 0x0006, "Toggle", true},
		{"missing-cmd", 0x0006, "dimmer", false},
		{"missing-cluster", 0x9999, "On", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, ok := r.CommandByName(tt.clusterID, tt.cmdName)
			if ok != tt.found {
				t.Fatalf("CommandByName(%d, %q) found=%v, want %v", tt.clusterID, tt.cmdName, ok, tt.found)
			}
		})
	}
}

func TestSearchClusters(t *testing.T) {
	r := NewRegistry()
	r.Register(newTestCluster())
	r.Register(ClusterInfo{
		ID:          0x0008,
		Name:        "LevelControl",
		DisplayName: "Level Control",
	})

	tests := []struct {
		name  string
		query string
		count int
	}{
		{"matches-both", "Control", 1}, // "LevelControl" Name and "Level Control" DisplayName both match, but it's the same cluster
		{"exact-match", "level", 1},
		{"display-name", "On/Off", 1},
		{"no-match", "thermostat", 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			results := r.SearchClusters(tt.query)
			if len(results) != tt.count {
				t.Fatalf("SearchClusters(%q) returned %d, want %d", tt.query, len(results), tt.count)
			}
		})
	}
}

func TestSearchCommands(t *testing.T) {
	r := NewRegistry()
	r.Register(newTestCluster())

	tests := []struct {
		name      string
		clusterID uint32
		query     string
		count     int
	}{
		{"exact", 0x0006, "Off", 1},
		{"prefix-lower", 0x0006, "of", 1},
		{"prefix-upper", 0x0006, "OF", 1},
		{"empty-returns-all", 0x0006, "", 3},
		{"no-match", 0x0006, "zzz", 0},
		{"missing-cluster", 0x9999, "Off", 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			results := r.SearchCommands(tt.clusterID, tt.query)
			if len(results) != tt.count {
				t.Fatalf("SearchCommands(%d, %q) returned %d, want %d", tt.clusterID, tt.query, len(results), tt.count)
			}
		})
	}
}

func TestSearchAttributes(t *testing.T) {
	r := NewRegistry()
	r.Register(newTestCluster())

	tests := []struct {
		name      string
		clusterID uint32
		query     string
		count     int
	}{
		{"found", 0x0006, "OnOff", 1},
		{"global-attr", 0x0006, "FeatureMap", 1},
		{"no-match", 0x0006, "brightness", 0},
		{"missing-cluster", 0x9999, "OnOff", 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			results := r.SearchAttributes(tt.clusterID, tt.query)
			if len(results) != tt.count {
				t.Fatalf("SearchAttributes(%d, %q) returned %d, want %d", tt.clusterID, tt.query, len(results), tt.count)
			}
		})
	}
}

func TestAttributeByID(t *testing.T) {
	r := NewRegistry()
	r.Register(newTestCluster())

	tests := []struct {
		name      string
		clusterID uint32
		attrID    uint32
		wantName  string
		wantFound bool
	}{
		{"cluster-attribute", 0x0006, 0x0000, "OnOff", true},
		{"global-attribute", 0x0006, 0xFFFB, "AttributeList", true},
		{"unknown-attribute", 0x0006, 0x1234, "", false},
		{"missing-cluster", 0x9999, 0x0000, "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			attr, ok := r.AttributeByID(tt.clusterID, tt.attrID)
			if ok != tt.wantFound {
				t.Fatalf("AttributeByID(0x%04X, 0x%04X) found = %v, want %v",
					tt.clusterID, tt.attrID, ok, tt.wantFound)
			}
			if !tt.wantFound {
				return
			}
			if attr.Name != tt.wantName {
				t.Fatalf("AttributeByID(0x%04X, 0x%04X) name = %q, want %q",
					tt.clusterID, tt.attrID, attr.Name, tt.wantName)
			}
			if attr.ID != tt.attrID {
				t.Fatalf("AttributeByID returned ID 0x%04X, want 0x%04X", attr.ID, tt.attrID)
			}
		})
	}
}

// TestAttributeByIDMatchesAttributeByName pins the two lookups to the same
// underlying entry: a numeric ID must resolve to a fully-typed definition, not
// a partial one, since write encoding depends on the Type field.
func TestAttributeByIDMatchesAttributeByName(t *testing.T) {
	r := NewRegistry()
	r.Register(newTestCluster())

	byName, ok := r.AttributeByName(0x0006, "OnOff")
	if !ok {
		t.Fatal("AttributeByName(0x0006, \"OnOff\") not found")
	}
	byID, ok := r.AttributeByID(0x0006, byName.ID)
	if !ok {
		t.Fatalf("AttributeByID(0x0006, 0x%04X) not found", byName.ID)
	}
	if *byID != *byName {
		t.Fatalf("AttributeByID = %+v, AttributeByName = %+v", *byID, *byName)
	}
}
