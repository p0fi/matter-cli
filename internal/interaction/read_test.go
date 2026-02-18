// Copyright 2026 matter-cli contributors
// SPDX-License-Identifier: Apache-2.0

package interaction

import (
	"testing"

	"github.com/p0fi/matter-cli/internal/tlv"
)

func TestReadRequest_TLVRoundTrip(t *testing.T) {
	t.Run("single attribute", func(t *testing.T) {
		orig := ReadRequest{
			AttributeRequests: []AttributePath{
				NewAttributePath(1, 0x0006, 0x0000),
			},
			FabricFiltered: true,
		}
		data, err := tlv.Marshal(orig)
		if err != nil {
			t.Fatalf("Marshal: %v", err)
		}
		var decoded ReadRequest
		if err := tlv.Unmarshal(data, &decoded); err != nil {
			t.Fatalf("Unmarshal: %v", err)
		}
		if len(decoded.AttributeRequests) != 1 {
			t.Fatalf("AttributeRequests len = %d, want 1", len(decoded.AttributeRequests))
		}
		p := decoded.AttributeRequests[0]
		if p.EndpointID == nil || *p.EndpointID != 1 {
			t.Errorf("EndpointID = %v, want 1", p.EndpointID)
		}
		if p.ClusterID == nil || *p.ClusterID != 0x0006 {
			t.Errorf("ClusterID = %v, want 0x0006", p.ClusterID)
		}
		if !decoded.FabricFiltered {
			t.Error("FabricFiltered should be true")
		}
	})

	t.Run("multiple attributes", func(t *testing.T) {
		orig := ReadRequest{
			AttributeRequests: []AttributePath{
				NewAttributePath(1, 0x0006, 0x0000),
				NewAttributePath(1, 0x0008, 0x0000),
				NewAttributePath(2, 0x0006, 0x0000),
			},
			FabricFiltered: false,
		}
		data, err := tlv.Marshal(orig)
		if err != nil {
			t.Fatalf("Marshal: %v", err)
		}
		var decoded ReadRequest
		if err := tlv.Unmarshal(data, &decoded); err != nil {
			t.Fatalf("Unmarshal: %v", err)
		}
		if len(decoded.AttributeRequests) != 3 {
			t.Fatalf("AttributeRequests len = %d, want 3", len(decoded.AttributeRequests))
		}
	})

	t.Run("with event requests", func(t *testing.T) {
		ep := uint16(0)
		cl := uint32(0x0028)
		ev := uint32(0x0000)
		orig := ReadRequest{
			AttributeRequests: []AttributePath{
				NewAttributePath(1, 0x0006, 0x0000),
			},
			EventRequests: []EventPath{
				{EndpointID: &ep, ClusterID: &cl, EventID: &ev},
			},
			FabricFiltered: true,
		}
		data, err := tlv.Marshal(orig)
		if err != nil {
			t.Fatalf("Marshal: %v", err)
		}
		var decoded ReadRequest
		if err := tlv.Unmarshal(data, &decoded); err != nil {
			t.Fatalf("Unmarshal: %v", err)
		}
		if len(decoded.EventRequests) != 1 {
			t.Fatalf("EventRequests len = %d, want 1", len(decoded.EventRequests))
		}
	})
}

func TestReportData_TLVRoundTrip(t *testing.T) {
	t.Run("with attribute data", func(t *testing.T) {
		orig := ReportData{
			AttributeReports: []AttributeReport{
				{
					Data: &AttributeData{
						DataVersion: 42,
						Path:        NewAttributePath(1, 0x0006, 0x0000),
						Data:        []byte{0x09}, // TLV true
					},
				},
			},
		}
		data, err := tlv.Marshal(orig)
		if err != nil {
			t.Fatalf("Marshal: %v", err)
		}
		var decoded ReportData
		if err := tlv.Unmarshal(data, &decoded); err != nil {
			t.Fatalf("Unmarshal: %v", err)
		}
		if len(decoded.AttributeReports) != 1 {
			t.Fatalf("AttributeReports len = %d, want 1", len(decoded.AttributeReports))
		}
		report := decoded.AttributeReports[0]
		if report.Data == nil {
			t.Fatal("Data should not be nil")
		}
		if report.Data.DataVersion != 42 {
			t.Errorf("DataVersion = %d, want 42", report.Data.DataVersion)
		}
		if report.Data.Path.EndpointID == nil || *report.Data.Path.EndpointID != 1 {
			t.Errorf("EndpointID = %v, want 1", report.Data.Path.EndpointID)
		}
	})

	t.Run("with attribute status error", func(t *testing.T) {
		orig := ReportData{
			AttributeReports: []AttributeReport{
				{
					Status: &AttributeStatus{
						Path:   NewAttributePath(1, 0x0006, 0x0000),
						Status: StatusIB{Status: uint8(StatusUnsupportedAttribute)},
					},
				},
			},
		}
		data, err := tlv.Marshal(orig)
		if err != nil {
			t.Fatalf("Marshal: %v", err)
		}
		var decoded ReportData
		if err := tlv.Unmarshal(data, &decoded); err != nil {
			t.Fatalf("Unmarshal: %v", err)
		}
		if len(decoded.AttributeReports) != 1 {
			t.Fatalf("AttributeReports len = %d, want 1", len(decoded.AttributeReports))
		}
		report := decoded.AttributeReports[0]
		if report.Status == nil {
			t.Fatal("Status should not be nil")
		}
		if report.Status.Status.Status != uint8(StatusUnsupportedAttribute) {
			t.Errorf("Status = 0x%02X, want 0x%02X", report.Status.Status.Status, StatusUnsupportedAttribute)
		}
	})

	t.Run("with subscription ID and chunking", func(t *testing.T) {
		subID := uint32(12345)
		more := true
		suppress := false
		orig := ReportData{
			SubscriptionID:      &subID,
			MoreChunkedMessages: &more,
			SuppressResponse:    &suppress,
			AttributeReports: []AttributeReport{
				{
					Data: &AttributeData{
						DataVersion: 1,
						Path:        NewAttributePath(0, 0x001D, 0x0000),
						Data:        []byte{0x04, 0x01}, // TLV uint8(1)
					},
				},
			},
		}
		data, err := tlv.Marshal(orig)
		if err != nil {
			t.Fatalf("Marshal: %v", err)
		}
		var decoded ReportData
		if err := tlv.Unmarshal(data, &decoded); err != nil {
			t.Fatalf("Unmarshal: %v", err)
		}
		if decoded.SubscriptionID == nil || *decoded.SubscriptionID != 12345 {
			t.Errorf("SubscriptionID = %v, want 12345", decoded.SubscriptionID)
		}
		if decoded.MoreChunkedMessages == nil || !*decoded.MoreChunkedMessages {
			t.Error("MoreChunkedMessages should be true")
		}
		if decoded.SuppressResponse == nil || *decoded.SuppressResponse {
			t.Error("SuppressResponse should be false")
		}
	})
}

func TestAttributeReport_TLVRoundTrip(t *testing.T) {
	// Test a report with both data and status fields — only one should be set
	// in practice, but we verify both round-trip individually.
	t.Run("data only", func(t *testing.T) {
		orig := AttributeReport{
			Data: &AttributeData{
				DataVersion: 100,
				Path:        NewAttributePath(1, 0x0006, 0x0000),
				Data:        []byte{0x08}, // TLV false
			},
		}
		data, err := tlv.Marshal(orig)
		if err != nil {
			t.Fatalf("Marshal: %v", err)
		}
		var decoded AttributeReport
		if err := tlv.Unmarshal(data, &decoded); err != nil {
			t.Fatalf("Unmarshal: %v", err)
		}
		if decoded.Data == nil {
			t.Fatal("Data should not be nil")
		}
		if decoded.Data.DataVersion != 100 {
			t.Errorf("DataVersion = %d, want 100", decoded.Data.DataVersion)
		}
	})

	t.Run("status only", func(t *testing.T) {
		orig := AttributeReport{
			Status: &AttributeStatus{
				Path:   NewAttributePath(1, 0x0006, 0x0000),
				Status: StatusIB{Status: uint8(StatusNotFound)},
			},
		}
		data, err := tlv.Marshal(orig)
		if err != nil {
			t.Fatalf("Marshal: %v", err)
		}
		var decoded AttributeReport
		if err := tlv.Unmarshal(data, &decoded); err != nil {
			t.Fatalf("Unmarshal: %v", err)
		}
		if decoded.Status == nil {
			t.Fatal("Status should not be nil")
		}
		if decoded.Status.Status.Status != uint8(StatusNotFound) {
			t.Errorf("Status = 0x%02X, want 0x%02X", decoded.Status.Status.Status, StatusNotFound)
		}
	})
}
