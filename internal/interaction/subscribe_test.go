// Copyright 2026 matter-cli contributors
// SPDX-License-Identifier: Apache-2.0

package interaction

import (
	"testing"

	"github.com/p0fi/matter-cli/internal/tlv"
)

func TestSubscribeRequest_TLVRoundTrip(t *testing.T) {
	t.Run("attributes only", func(t *testing.T) {
		orig := SubscribeRequest{
			KeepSubscriptions:  true,
			MinIntervalFloor:   1,
			MaxIntervalCeiling: 60,
			AttributeRequests: []AttributePath{
				NewAttributePath(1, 0x0006, 0x0000),
			},
			FabricFiltered: true,
		}
		data, err := tlv.Marshal(orig)
		if err != nil {
			t.Fatalf("Marshal: %v", err)
		}
		var decoded SubscribeRequest
		if err := tlv.Unmarshal(data, &decoded); err != nil {
			t.Fatalf("Unmarshal: %v", err)
		}
		if !decoded.KeepSubscriptions {
			t.Error("KeepSubscriptions should be true")
		}
		if decoded.MinIntervalFloor != 1 {
			t.Errorf("MinIntervalFloor = %d, want 1", decoded.MinIntervalFloor)
		}
		if decoded.MaxIntervalCeiling != 60 {
			t.Errorf("MaxIntervalCeiling = %d, want 60", decoded.MaxIntervalCeiling)
		}
		if len(decoded.AttributeRequests) != 1 {
			t.Fatalf("AttributeRequests len = %d, want 1", len(decoded.AttributeRequests))
		}
		if !decoded.FabricFiltered {
			t.Error("FabricFiltered should be true")
		}
	})

	t.Run("multiple attributes and events", func(t *testing.T) {
		ep := uint16(0)
		cl := uint32(0x0028)
		ev := uint32(0x0000)
		orig := SubscribeRequest{
			KeepSubscriptions:  false,
			MinIntervalFloor:   5,
			MaxIntervalCeiling: 300,
			AttributeRequests: []AttributePath{
				NewAttributePath(1, 0x0006, 0x0000),
				NewAttributePath(1, 0x0008, 0x0000),
			},
			EventRequests: []EventPath{
				{EndpointID: &ep, ClusterID: &cl, EventID: &ev},
			},
			FabricFiltered: false,
		}
		data, err := tlv.Marshal(orig)
		if err != nil {
			t.Fatalf("Marshal: %v", err)
		}
		var decoded SubscribeRequest
		if err := tlv.Unmarshal(data, &decoded); err != nil {
			t.Fatalf("Unmarshal: %v", err)
		}
		if len(decoded.AttributeRequests) != 2 {
			t.Errorf("AttributeRequests len = %d, want 2", len(decoded.AttributeRequests))
		}
		if len(decoded.EventRequests) != 1 {
			t.Errorf("EventRequests len = %d, want 1", len(decoded.EventRequests))
		}
	})
}

func TestSubscribeResponse_TLVRoundTrip(t *testing.T) {
	orig := SubscribeResponse{
		SubscriptionID: 54321,
		MaxInterval:    120,
	}
	data, err := tlv.Marshal(orig)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var decoded SubscribeResponse
	if err := tlv.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if decoded.SubscriptionID != 54321 {
		t.Errorf("SubscriptionID = %d, want 54321", decoded.SubscriptionID)
	}
	if decoded.MaxInterval != 120 {
		t.Errorf("MaxInterval = %d, want 120", decoded.MaxInterval)
	}
}

func TestSubscription_Cancel(t *testing.T) {
	cancelled := false
	sub := &Subscription{
		ID:     1,
		cancel: func() { cancelled = true },
	}
	sub.Cancel()
	if !cancelled {
		t.Error("Cancel should have called the cancel function")
	}
}

func TestSubscription_CancelNil(t *testing.T) {
	// A subscription with no cancel function should not panic.
	sub := &Subscription{ID: 1}
	sub.Cancel() // should not panic
}
