// Copyright 2026 matter-cli contributors
// SPDX-License-Identifier: Apache-2.0

package store

import (
	"encoding/json"
	"testing"
)

// TestClusterRefAttributesRoundTrip covers the ClusterRef.Attributes cache used
// by attribute-name completion. The store is JSON-encoded, so this is where the
// backward-compatibility contract lives: records written before the field
// existed must still decode, and must decode to nil rather than an empty slice
// so completion can tell "never read" apart from "device advertises nothing".
func TestClusterRefAttributesRoundTrip(t *testing.T) {
	tests := []struct {
		name string
		in   ClusterRef
		want []uint32
	}{
		{
			name: "populated list survives a round trip",
			in:   ClusterRef{ID: 6, Name: "OnOff", Side: "server", Attributes: []uint32{0, 0x4001, 0xFFFB}},
			want: []uint32{0, 0x4001, 0xFFFB},
		},
		{
			name: "nil list stays nil",
			in:   ClusterRef{ID: 6, Name: "OnOff", Side: "server"},
			want: nil,
		},
		{
			name: "empty list decodes as nil because it is omitted when empty",
			in:   ClusterRef{ID: 6, Name: "OnOff", Side: "server", Attributes: []uint32{}},
			want: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			raw, err := json.Marshal(tt.in)
			if err != nil {
				t.Fatalf("Marshal: %v", err)
			}
			var got ClusterRef
			if err := json.Unmarshal(raw, &got); err != nil {
				t.Fatalf("Unmarshal: %v", err)
			}
			if got.ID != tt.in.ID || got.Name != tt.in.Name || got.Side != tt.in.Side {
				t.Fatalf("round trip lost identity fields: %+v", got)
			}
			if len(got.Attributes) != len(tt.want) {
				t.Fatalf("Attributes = %v, want %v", got.Attributes, tt.want)
			}
			for i, id := range tt.want {
				if got.Attributes[i] != id {
					t.Fatalf("Attributes[%d] = 0x%04X, want 0x%04X", i, got.Attributes[i], id)
				}
			}
			if tt.want == nil && got.Attributes != nil {
				t.Fatalf("expected nil Attributes, got %v", got.Attributes)
			}
		})
	}
}

// TestClusterRefDecodesLegacyRecord pins that a store record written before the
// Attributes field existed still decodes cleanly, with a nil cache.
func TestClusterRefDecodesLegacyRecord(t *testing.T) {
	const legacy = `{"id":6,"name":"OnOff","side":"server"}`

	var got ClusterRef
	if err := json.Unmarshal([]byte(legacy), &got); err != nil {
		t.Fatalf("Unmarshal legacy record: %v", err)
	}
	if got.ID != 6 || got.Name != "OnOff" || got.Side != "server" {
		t.Fatalf("legacy record decoded as %+v", got)
	}
	if got.Attributes != nil {
		t.Fatalf("Attributes = %v, want nil for a legacy record", got.Attributes)
	}
}

// TestClusterRefOmitsEmptyAttributes keeps existing store files from growing an
// "attributes":null key for every cluster on every save.
func TestClusterRefOmitsEmptyAttributes(t *testing.T) {
	raw, err := json.Marshal(ClusterRef{ID: 6, Name: "OnOff", Side: "server"})
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if want := `{"id":6,"name":"OnOff","side":"server"}`; string(raw) != want {
		t.Fatalf("Marshal = %s, want %s", raw, want)
	}
}
