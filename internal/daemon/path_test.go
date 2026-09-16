// Copyright 2026 matter-cli contributors
// SPDX-License-Identifier: Apache-2.0

package daemon

import (
	"encoding/json"
	"testing"
)

// TestAttrPathFromReq pins the one place the wildcard flag acquires wire
// semantics. Daemon and direct-CASE reads diverging is a failure mode with no
// symptom other than wrong data, so the translation is asserted directly.
func TestAttrPathFromReq(t *testing.T) {
	t.Run("named attribute keeps its ID", func(t *testing.T) {
		path := attrPathFromReq(AttrPathReq{Endpoint: 1, ClusterID: 0x0006, AttributeID: 0x4001})

		if path.AttributeID == nil {
			t.Fatal("attribute ID = nil, want 0x4001")
		}
		if *path.AttributeID != 0x4001 {
			t.Errorf("attribute ID = 0x%04X, want 0x4001", *path.AttributeID)
		}
		if path.EndpointID == nil || *path.EndpointID != 1 {
			t.Error("endpoint should be pinned to 1")
		}
		if path.ClusterID == nil || *path.ClusterID != 0x0006 {
			t.Error("cluster should be pinned to 0x0006")
		}
	})

	t.Run("wildcard drops the attribute ID", func(t *testing.T) {
		// AttributeID is set as well, to prove the flag wins: the CLI leaves
		// it zero, but a wildcard request must never read attribute 0.
		path := attrPathFromReq(AttrPathReq{Endpoint: 0, ClusterID: 0x0028, AttributeID: 0x4001, WildcardAttribute: true})

		if path.AttributeID != nil {
			t.Errorf("attribute ID = 0x%04X, want nil (wildcard)", *path.AttributeID)
		}
		if path.EndpointID == nil || *path.EndpointID != 0 {
			t.Error("the endpoint must stay pinned, never wildcarded alongside the attribute")
		}
		if path.ClusterID == nil || *path.ClusterID != 0x0028 {
			t.Error("the cluster must stay pinned, never wildcarded alongside the attribute")
		}
	})
}

// TestAttrPathReqWildcardIsAdditive checks that the wildcard field is omitted
// from the wire when unset and absent from an older CLI's request, so that a
// daemon and a CLI on either side of this change still agree on what a
// single-attribute read means.
func TestAttrPathReqWildcardIsAdditive(t *testing.T) {
	encoded, err := json.Marshal(AttrPathReq{Endpoint: 1, ClusterID: 0x0006, AttributeID: 0x0000})
	if err != nil {
		t.Fatal(err)
	}
	if got := string(encoded); got != `{"endpoint":1,"cluster_id":6,"attribute_id":0}` {
		t.Errorf("encoded = %s, want the wildcard field omitted", got)
	}

	var decoded AttrPathReq
	if err := json.Unmarshal([]byte(`{"endpoint":1,"cluster_id":6,"attribute_id":0}`), &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.WildcardAttribute {
		t.Error("a request without the field must decode as a single-attribute read")
	}
}
