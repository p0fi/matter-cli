// Copyright 2026 matter-cli contributors
// SPDX-License-Identifier: Apache-2.0

package interaction

import (
	"testing"

	"github.com/p0fi/matter-cli/internal/tlv"
)

func TestNewAttributePath(t *testing.T) {
	p := NewAttributePath(1, 0x0006, 0x0000)
	if p.EndpointID == nil || *p.EndpointID != 1 {
		t.Errorf("EndpointID = %v, want 1", p.EndpointID)
	}
	if p.ClusterID == nil || *p.ClusterID != 0x0006 {
		t.Errorf("ClusterID = %v, want 0x0006", p.ClusterID)
	}
	if p.AttributeID == nil || *p.AttributeID != 0x0000 {
		t.Errorf("AttributeID = %v, want 0x0000", p.AttributeID)
	}
	if p.NodeID != nil {
		t.Error("NodeID should be nil")
	}
	if p.ListIndex != nil {
		t.Error("ListIndex should be nil")
	}
}

func TestAttributePath_String(t *testing.T) {
	tests := []struct {
		name string
		path AttributePath
		want string
	}{
		{
			name: "fully specified",
			path: NewAttributePath(1, 0x0006, 0x0000),
			want: "EP:1/CL:0x0006/AT:0x0000",
		},
		{
			name: "wildcard endpoint",
			path: func() AttributePath {
				cl := uint32(0x0006)
				at := uint32(0x0001)
				return AttributePath{ClusterID: &cl, AttributeID: &at}
			}(),
			want: "EP:*/CL:0x0006/AT:0x0001",
		},
		{
			name: "all wildcards",
			path: AttributePath{},
			want: "EP:*/CL:*/AT:*",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.path.String()
			if got != tt.want {
				t.Errorf("String() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestAttributePath_TLVRoundTrip(t *testing.T) {
	t.Run("fully specified", func(t *testing.T) {
		orig := NewAttributePath(2, 0x0008, 0x0003)
		data, err := tlv.Marshal(orig)
		if err != nil {
			t.Fatalf("Marshal: %v", err)
		}
		var decoded AttributePath
		if err := tlv.Unmarshal(data, &decoded); err != nil {
			t.Fatalf("Unmarshal: %v", err)
		}
		if decoded.EndpointID == nil || *decoded.EndpointID != 2 {
			t.Errorf("EndpointID = %v, want 2", decoded.EndpointID)
		}
		if decoded.ClusterID == nil || *decoded.ClusterID != 0x0008 {
			t.Errorf("ClusterID = %v, want 0x0008", decoded.ClusterID)
		}
		if decoded.AttributeID == nil || *decoded.AttributeID != 0x0003 {
			t.Errorf("AttributeID = %v, want 0x0003", decoded.AttributeID)
		}
	})

	t.Run("with all optional fields", func(t *testing.T) {
		tagComp := true
		nodeID := uint64(0x1234)
		ep := uint16(0)
		cl := uint32(0x0006)
		at := uint32(0x0000)
		li := uint16(5)
		orig := AttributePath{
			EnableTagCompression: &tagComp,
			NodeID:               &nodeID,
			EndpointID:           &ep,
			ClusterID:            &cl,
			AttributeID:          &at,
			ListIndex:            &li,
		}
		data, err := tlv.Marshal(orig)
		if err != nil {
			t.Fatalf("Marshal: %v", err)
		}
		var decoded AttributePath
		if err := tlv.Unmarshal(data, &decoded); err != nil {
			t.Fatalf("Unmarshal: %v", err)
		}
		if decoded.EnableTagCompression == nil || !*decoded.EnableTagCompression {
			t.Error("EnableTagCompression should be true")
		}
		if decoded.NodeID == nil || *decoded.NodeID != 0x1234 {
			t.Errorf("NodeID = %v, want 0x1234", decoded.NodeID)
		}
		if decoded.ListIndex == nil || *decoded.ListIndex != 5 {
			t.Errorf("ListIndex = %v, want 5", decoded.ListIndex)
		}
	})
}

func TestNewCommandPath(t *testing.T) {
	p := NewCommandPath(1, 0x0006, 0x0002)
	if p.EndpointID != 1 {
		t.Errorf("EndpointID = %d, want 1", p.EndpointID)
	}
	if p.ClusterID != 0x0006 {
		t.Errorf("ClusterID = 0x%04X, want 0x0006", p.ClusterID)
	}
	if p.CommandID != 0x0002 {
		t.Errorf("CommandID = 0x%04X, want 0x0002", p.CommandID)
	}
}

func TestCommandPath_String(t *testing.T) {
	p := NewCommandPath(1, 0x0006, 0x0002)
	got := p.String()
	want := "EP:1/CL:0x0006/CMD:0x0002"
	if got != want {
		t.Errorf("String() = %q, want %q", got, want)
	}
}

func TestCommandPath_TLVRoundTrip(t *testing.T) {
	orig := NewCommandPath(3, 0x003E, 0x0001)
	data, err := tlv.Marshal(orig)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var decoded CommandPath
	if err := tlv.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if decoded.EndpointID != orig.EndpointID {
		t.Errorf("EndpointID = %d, want %d", decoded.EndpointID, orig.EndpointID)
	}
	if decoded.ClusterID != orig.ClusterID {
		t.Errorf("ClusterID = 0x%04X, want 0x%04X", decoded.ClusterID, orig.ClusterID)
	}
	if decoded.CommandID != orig.CommandID {
		t.Errorf("CommandID = 0x%04X, want 0x%04X", decoded.CommandID, orig.CommandID)
	}
}

func TestEventPath_String(t *testing.T) {
	ep := uint16(0)
	cl := uint32(0x0028)
	ev := uint32(0x0002)
	p := EventPath{EndpointID: &ep, ClusterID: &cl, EventID: &ev}
	got := p.String()
	want := "EP:0/CL:0x0028/EV:0x0002"
	if got != want {
		t.Errorf("String() = %q, want %q", got, want)
	}
}

func TestEventPath_TLVRoundTrip(t *testing.T) {
	ep := uint16(1)
	cl := uint32(0x0033)
	ev := uint32(0x0001)
	urgent := true
	orig := EventPath{
		EndpointID: &ep,
		ClusterID:  &cl,
		EventID:    &ev,
		IsUrgent:   &urgent,
	}
	data, err := tlv.Marshal(orig)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var decoded EventPath
	if err := tlv.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if decoded.EndpointID == nil || *decoded.EndpointID != 1 {
		t.Errorf("EndpointID = %v, want 1", decoded.EndpointID)
	}
	if decoded.ClusterID == nil || *decoded.ClusterID != 0x0033 {
		t.Errorf("ClusterID = %v, want 0x0033", decoded.ClusterID)
	}
	if decoded.EventID == nil || *decoded.EventID != 0x0001 {
		t.Errorf("EventID = %v, want 0x0001", decoded.EventID)
	}
	if decoded.IsUrgent == nil || !*decoded.IsUrgent {
		t.Error("IsUrgent should be true")
	}
}
