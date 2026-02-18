// Copyright 2026 matter-cli contributors
// SPDX-License-Identifier: Apache-2.0

package interaction

import (
	"testing"

	"github.com/p0fi/matter-cli/internal/tlv"
)

func TestWriteRequest_TLVRoundTrip(t *testing.T) {
	t.Run("single write", func(t *testing.T) {
		orig := WriteRequest{
			SuppressResponse: false,
			WriteRequests: []AttributeWrite{
				{
					Path: NewAttributePath(1, 0x0008, 0x0000),
					Data: []byte{0x04, 0x80}, // TLV uint8(128)
				},
			},
		}
		data, err := tlv.Marshal(orig)
		if err != nil {
			t.Fatalf("Marshal: %v", err)
		}
		var decoded WriteRequest
		if err := tlv.Unmarshal(data, &decoded); err != nil {
			t.Fatalf("Unmarshal: %v", err)
		}
		if len(decoded.WriteRequests) != 1 {
			t.Fatalf("WriteRequests len = %d, want 1", len(decoded.WriteRequests))
		}
		w := decoded.WriteRequests[0]
		if w.Path.EndpointID == nil || *w.Path.EndpointID != 1 {
			t.Errorf("EndpointID = %v, want 1", w.Path.EndpointID)
		}
		if w.Path.ClusterID == nil || *w.Path.ClusterID != 0x0008 {
			t.Errorf("ClusterID = %v, want 0x0008", w.Path.ClusterID)
		}
		if len(w.Data) != 2 {
			t.Errorf("Data len = %d, want 2", len(w.Data))
		}
	})

	t.Run("with data version", func(t *testing.T) {
		dv := uint32(42)
		orig := WriteRequest{
			WriteRequests: []AttributeWrite{
				{
					DataVersion: &dv,
					Path:        NewAttributePath(1, 0x0006, 0x0000),
					Data:        []byte{0x09}, // TLV true
				},
			},
		}
		data, err := tlv.Marshal(orig)
		if err != nil {
			t.Fatalf("Marshal: %v", err)
		}
		var decoded WriteRequest
		if err := tlv.Unmarshal(data, &decoded); err != nil {
			t.Fatalf("Unmarshal: %v", err)
		}
		w := decoded.WriteRequests[0]
		if w.DataVersion == nil || *w.DataVersion != 42 {
			t.Errorf("DataVersion = %v, want 42", w.DataVersion)
		}
	})

	t.Run("multiple writes with timed request", func(t *testing.T) {
		orig := WriteRequest{
			TimedRequest: true,
			WriteRequests: []AttributeWrite{
				{
					Path: NewAttributePath(1, 0x0006, 0x0000),
					Data: []byte{0x09},
				},
				{
					Path: NewAttributePath(1, 0x0008, 0x0000),
					Data: []byte{0x04, 0xFF},
				},
			},
		}
		data, err := tlv.Marshal(orig)
		if err != nil {
			t.Fatalf("Marshal: %v", err)
		}
		var decoded WriteRequest
		if err := tlv.Unmarshal(data, &decoded); err != nil {
			t.Fatalf("Unmarshal: %v", err)
		}
		if !decoded.TimedRequest {
			t.Error("TimedRequest should be true")
		}
		if len(decoded.WriteRequests) != 2 {
			t.Fatalf("WriteRequests len = %d, want 2", len(decoded.WriteRequests))
		}
	})
}

func TestWriteResponse_TLVRoundTrip(t *testing.T) {
	t.Run("all success", func(t *testing.T) {
		orig := WriteResponse{
			WriteResponses: []AttributeStatus{
				{
					Path:   NewAttributePath(1, 0x0006, 0x0000),
					Status: StatusIB{Status: uint8(StatusSuccess)},
				},
				{
					Path:   NewAttributePath(1, 0x0008, 0x0000),
					Status: StatusIB{Status: uint8(StatusSuccess)},
				},
			},
		}
		data, err := tlv.Marshal(orig)
		if err != nil {
			t.Fatalf("Marshal: %v", err)
		}
		var decoded WriteResponse
		if err := tlv.Unmarshal(data, &decoded); err != nil {
			t.Fatalf("Unmarshal: %v", err)
		}
		if len(decoded.WriteResponses) != 2 {
			t.Fatalf("WriteResponses len = %d, want 2", len(decoded.WriteResponses))
		}
		for i, wr := range decoded.WriteResponses {
			if wr.Status.Status != uint8(StatusSuccess) {
				t.Errorf("WriteResponses[%d].Status = 0x%02X, want SUCCESS", i, wr.Status.Status)
			}
		}
	})

	t.Run("mixed success and error", func(t *testing.T) {
		orig := WriteResponse{
			WriteResponses: []AttributeStatus{
				{
					Path:   NewAttributePath(1, 0x0006, 0x0000),
					Status: StatusIB{Status: uint8(StatusSuccess)},
				},
				{
					Path:   NewAttributePath(1, 0x0008, 0x0000),
					Status: StatusIB{Status: uint8(StatusUnsupportedWrite)},
				},
			},
		}
		data, err := tlv.Marshal(orig)
		if err != nil {
			t.Fatalf("Marshal: %v", err)
		}
		var decoded WriteResponse
		if err := tlv.Unmarshal(data, &decoded); err != nil {
			t.Fatalf("Unmarshal: %v", err)
		}
		if len(decoded.WriteResponses) != 2 {
			t.Fatalf("WriteResponses len = %d, want 2", len(decoded.WriteResponses))
		}
		if decoded.WriteResponses[0].Status.Status != uint8(StatusSuccess) {
			t.Errorf("first write status = 0x%02X, want SUCCESS", decoded.WriteResponses[0].Status.Status)
		}
		if decoded.WriteResponses[1].Status.Status != uint8(StatusUnsupportedWrite) {
			t.Errorf("second write status = 0x%02X, want UNSUPPORTED_WRITE", decoded.WriteResponses[1].Status.Status)
		}
	})
}

func TestAttributeWrite_TLVRoundTrip(t *testing.T) {
	dv := uint32(7)
	orig := AttributeWrite{
		DataVersion: &dv,
		Path:        NewAttributePath(0, 0x001D, 0x0003),
		Data:        []byte{0x0C, 0x05, 0x68, 0x65, 0x6C, 0x6C, 0x6F}, // TLV utf8 "hello"
	}
	data, err := tlv.Marshal(orig)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var decoded AttributeWrite
	if err := tlv.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if decoded.DataVersion == nil || *decoded.DataVersion != 7 {
		t.Errorf("DataVersion = %v, want 7", decoded.DataVersion)
	}
	if decoded.Path.ClusterID == nil || *decoded.Path.ClusterID != 0x001D {
		t.Errorf("ClusterID = %v, want 0x001D", decoded.Path.ClusterID)
	}
	if len(decoded.Data) != len(orig.Data) {
		t.Errorf("Data len = %d, want %d", len(decoded.Data), len(orig.Data))
	}
}
