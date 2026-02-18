// Copyright 2026 matter-cli contributors
// SPDX-License-Identifier: Apache-2.0

package interaction

import (
	"testing"

	"github.com/p0fi/matter-cli/internal/tlv"
)

func TestInvokeRequest_TLVRoundTrip(t *testing.T) {
	t.Run("single command no fields", func(t *testing.T) {
		orig := InvokeRequest{
			SuppressResponse: false,
			TimedRequest:     false,
			InvokeRequests: []CommandDataIB{
				{
					Path:   NewCommandPath(1, 0x0006, 0x0001),
					Fields: []byte{},
				},
			},
		}
		data, err := tlv.Marshal(orig)
		if err != nil {
			t.Fatalf("Marshal: %v", err)
		}
		var decoded InvokeRequest
		if err := tlv.Unmarshal(data, &decoded); err != nil {
			t.Fatalf("Unmarshal: %v", err)
		}
		if len(decoded.InvokeRequests) != 1 {
			t.Fatalf("InvokeRequests len = %d, want 1", len(decoded.InvokeRequests))
		}
		cmd := decoded.InvokeRequests[0]
		if cmd.Path.EndpointID != 1 {
			t.Errorf("EndpointID = %d, want 1", cmd.Path.EndpointID)
		}
		if cmd.Path.ClusterID != 0x0006 {
			t.Errorf("ClusterID = 0x%04X, want 0x0006", cmd.Path.ClusterID)
		}
		if cmd.Path.CommandID != 0x0001 {
			t.Errorf("CommandID = 0x%04X, want 0x0001", cmd.Path.CommandID)
		}
	})

	t.Run("command with fields", func(t *testing.T) {
		// Simulate MoveToLevel command fields (Level=128, TransitionTime=10)
		fields := []byte{0x04, 0x80, 0x04, 0x0A}
		orig := InvokeRequest{
			SuppressResponse: false,
			TimedRequest:     false,
			InvokeRequests: []CommandDataIB{
				{
					Path:   NewCommandPath(1, 0x0008, 0x0000),
					Fields: fields,
				},
			},
		}
		data, err := tlv.Marshal(orig)
		if err != nil {
			t.Fatalf("Marshal: %v", err)
		}
		var decoded InvokeRequest
		if err := tlv.Unmarshal(data, &decoded); err != nil {
			t.Fatalf("Unmarshal: %v", err)
		}
		cmd := decoded.InvokeRequests[0]
		if len(cmd.Fields) != len(fields) {
			t.Errorf("Fields len = %d, want %d", len(cmd.Fields), len(fields))
		}
	})

	t.Run("timed request", func(t *testing.T) {
		orig := InvokeRequest{
			SuppressResponse: false,
			TimedRequest:     true,
			InvokeRequests: []CommandDataIB{
				{
					Path:   NewCommandPath(0, 0x0030, 0x0000),
					Fields: []byte{0x09},
				},
			},
		}
		data, err := tlv.Marshal(orig)
		if err != nil {
			t.Fatalf("Marshal: %v", err)
		}
		var decoded InvokeRequest
		if err := tlv.Unmarshal(data, &decoded); err != nil {
			t.Fatalf("Unmarshal: %v", err)
		}
		if !decoded.TimedRequest {
			t.Error("TimedRequest should be true")
		}
	})
}

func TestInvokeResponse_TLVRoundTrip(t *testing.T) {
	t.Run("success status only", func(t *testing.T) {
		orig := InvokeResponse{
			InvokeResponses: []InvokeResponseIB{
				{
					Status: &CommandStatusIB{
						Path:   NewCommandPath(1, 0x0006, 0x0001),
						Status: StatusIB{Status: uint8(StatusSuccess)},
					},
				},
			},
		}
		data, err := tlv.Marshal(orig)
		if err != nil {
			t.Fatalf("Marshal: %v", err)
		}
		var decoded InvokeResponse
		if err := tlv.Unmarshal(data, &decoded); err != nil {
			t.Fatalf("Unmarshal: %v", err)
		}
		if len(decoded.InvokeResponses) != 1 {
			t.Fatalf("InvokeResponses len = %d, want 1", len(decoded.InvokeResponses))
		}
		resp := decoded.InvokeResponses[0]
		if resp.Status == nil {
			t.Fatal("Status should not be nil")
		}
		if resp.Status.Status.Status != uint8(StatusSuccess) {
			t.Errorf("Status = 0x%02X, want SUCCESS", resp.Status.Status.Status)
		}
	})

	t.Run("with response data", func(t *testing.T) {
		responseFields := []byte{0x04, 0x00, 0x04, 0x01}
		orig := InvokeResponse{
			InvokeResponses: []InvokeResponseIB{
				{
					Command: &CommandDataIB{
						Path:   NewCommandPath(0, 0x0030, 0x0001),
						Fields: responseFields,
					},
				},
			},
		}
		data, err := tlv.Marshal(orig)
		if err != nil {
			t.Fatalf("Marshal: %v", err)
		}
		var decoded InvokeResponse
		if err := tlv.Unmarshal(data, &decoded); err != nil {
			t.Fatalf("Unmarshal: %v", err)
		}
		resp := decoded.InvokeResponses[0]
		if resp.Command == nil {
			t.Fatal("Command should not be nil")
		}
		if resp.Command.Path.ClusterID != 0x0030 {
			t.Errorf("ClusterID = 0x%04X, want 0x0030", resp.Command.Path.ClusterID)
		}
		if len(resp.Command.Fields) != len(responseFields) {
			t.Errorf("Fields len = %d, want %d", len(resp.Command.Fields), len(responseFields))
		}
	})

	t.Run("error status", func(t *testing.T) {
		orig := InvokeResponse{
			InvokeResponses: []InvokeResponseIB{
				{
					Status: &CommandStatusIB{
						Path:   NewCommandPath(1, 0x0006, 0x0099),
						Status: StatusIB{Status: uint8(StatusUnsupportedCommand)},
					},
				},
			},
		}
		data, err := tlv.Marshal(orig)
		if err != nil {
			t.Fatalf("Marshal: %v", err)
		}
		var decoded InvokeResponse
		if err := tlv.Unmarshal(data, &decoded); err != nil {
			t.Fatalf("Unmarshal: %v", err)
		}
		resp := decoded.InvokeResponses[0]
		if resp.Status == nil {
			t.Fatal("Status should not be nil")
		}
		if resp.Status.Status.Status != uint8(StatusUnsupportedCommand) {
			t.Errorf("Status = 0x%02X, want UNSUPPORTED_COMMAND", resp.Status.Status.Status)
		}
	})
}

func TestCommandDataIB_TLVRoundTrip(t *testing.T) {
	orig := CommandDataIB{
		Path:   NewCommandPath(2, 0x003E, 0x0006),
		Fields: []byte{0x10, 0x03, 0xAA, 0xBB, 0xCC},
	}
	data, err := tlv.Marshal(orig)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var decoded CommandDataIB
	if err := tlv.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if decoded.Path.EndpointID != 2 {
		t.Errorf("EndpointID = %d, want 2", decoded.Path.EndpointID)
	}
	if decoded.Path.ClusterID != 0x003E {
		t.Errorf("ClusterID = 0x%04X, want 0x003E", decoded.Path.ClusterID)
	}
	if decoded.Path.CommandID != 0x0006 {
		t.Errorf("CommandID = 0x%04X, want 0x0006", decoded.Path.CommandID)
	}
	if len(decoded.Fields) != 5 {
		t.Errorf("Fields len = %d, want 5", len(decoded.Fields))
	}
}

func TestCommandStatusIB_TLVRoundTrip(t *testing.T) {
	cc := uint8(0x03)
	orig := CommandStatusIB{
		Path: NewCommandPath(1, 0x0006, 0x0002),
		Status: StatusIB{
			Status:        uint8(StatusFailure),
			ClusterStatus: &cc,
		},
	}
	data, err := tlv.Marshal(orig)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var decoded CommandStatusIB
	if err := tlv.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if decoded.Path.CommandID != 0x0002 {
		t.Errorf("CommandID = 0x%04X, want 0x0002", decoded.Path.CommandID)
	}
	if decoded.Status.Status != uint8(StatusFailure) {
		t.Errorf("Status = 0x%02X, want FAILURE", decoded.Status.Status)
	}
	if decoded.Status.ClusterStatus == nil || *decoded.Status.ClusterStatus != 0x03 {
		t.Errorf("ClusterStatus = %v, want 0x03", decoded.Status.ClusterStatus)
	}
}
