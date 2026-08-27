// Copyright 2026 matter-cli contributors
// SPDX-License-Identifier: Apache-2.0

package interaction

import (
	"errors"
	"fmt"
	"testing"

	"github.com/p0fi/matter-cli/internal/tlv"
)

func TestStatusCode_String(t *testing.T) {
	tests := []struct {
		code StatusCode
		want string
	}{
		{StatusSuccess, "SUCCESS"},
		{StatusFailure, "FAILURE"},
		{StatusInvalidSubscription, "INVALID_SUBSCRIPTION"},
		{StatusUnsupportedAccess, "UNSUPPORTED_ACCESS"},
		{StatusUnsupportedEndpoint, "UNSUPPORTED_ENDPOINT"},
		{StatusInvalidAction, "INVALID_ACTION"},
		{StatusUnsupportedCommand, "UNSUPPORTED_COMMAND"},
		{StatusInvalidCommand, "INVALID_COMMAND"},
		{StatusUnsupportedAttribute, "UNSUPPORTED_ATTRIBUTE"},
		{StatusConstraintError, "CONSTRAINT_ERROR"},
		{StatusUnsupportedWrite, "UNSUPPORTED_WRITE"},
		{StatusResourceExhausted, "RESOURCE_EXHAUSTED"},
		{StatusNotFound, "NOT_FOUND"},
		{StatusUnreportableAttribute, "UNREPORTABLE_ATTRIBUTE"},
		{StatusInvalidDataType, "INVALID_DATA_TYPE"},
		{StatusUnsupportedRead, "UNSUPPORTED_READ"},
		{StatusDataVersionMismatch, "DATA_VERSION_MISMATCH"},
		{StatusTimeout, "TIMEOUT"},
		{StatusBusy, "BUSY"},
		{StatusAccessRestricted, "ACCESS_RESTRICTED"},
		{StatusUnsupportedCluster, "UNSUPPORTED_CLUSTER"},
		{StatusNoUpstreamSubscription, "NO_UPSTREAM_SUBSCRIPTION"},
		{StatusNeedsTimedInteraction, "NEEDS_TIMED_INTERACTION"},
		{StatusUnsupportedEvent, "UNSUPPORTED_EVENT"},
		{StatusPathsExhausted, "PATHS_EXHAUSTED"},
		{StatusTimedRequestMismatch, "TIMED_REQUEST_MISMATCH"},
		{StatusFailsafeRequired, "FAILSAFE_REQUIRED"},
		{StatusInvalidInState, "INVALID_IN_STATE"},
		{StatusNoCommandResponse, "NO_COMMAND_RESPONSE"},
		{StatusDynamicConstraintError, "DYNAMIC_CONSTRAINT_ERROR"},
		{StatusAlreadyExists, "ALREADY_EXISTS"},
		{StatusInvalidTransportType, "INVALID_TRANSPORT_TYPE"},
		// Reserved, deprecated, and unrecognized values fall back to UNKNOWN.
		{StatusCode(0x82), "UNKNOWN"},
		{StatusCode(0x8A), "UNKNOWN"},
		{StatusCode(0xC4), "UNKNOWN"},
		{StatusCode(0xF0), "UNKNOWN"}, // WRITE_IGNORED: SDK-only, not part of the spec catalog.
		{StatusCode(0xFF), "UNKNOWN"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			got := tt.code.String()
			if got != tt.want {
				t.Errorf("StatusCode(0x%02X).String() = %q, want %q", uint8(tt.code), got, tt.want)
			}
		})
	}
}

func TestStatusError_Error(t *testing.T) {
	t.Run("without cluster code", func(t *testing.T) {
		err := &StatusError{GeneralCode: StatusNotFound}
		got := err.Error()
		want := "NOT_FOUND (0x8B)"
		if got != want {
			t.Errorf("Error() = %q, want %q", got, want)
		}
	})

	t.Run("with cluster code", func(t *testing.T) {
		cc := uint8(0x42)
		err := &StatusError{GeneralCode: StatusFailure, ClusterCode: &cc}
		got := err.Error()
		want := "FAILURE (0x01), cluster status 0x42"
		if got != want {
			t.Errorf("Error() = %q, want %q", got, want)
		}
	})

	t.Run("regression: 0x87 renders as CONSTRAINT_ERROR", func(t *testing.T) {
		err := &StatusError{GeneralCode: StatusConstraintError}
		got := err.Error()
		want := "CONSTRAINT_ERROR (0x87)"
		if got != want {
			t.Errorf("Error() = %q, want %q", got, want)
		}
	})

	t.Run("unknown code", func(t *testing.T) {
		err := &StatusError{GeneralCode: StatusCode(0xFF)}
		got := err.Error()
		want := "UNKNOWN (0xFF)"
		if got != want {
			t.Errorf("Error() = %q, want %q", got, want)
		}
	})
}

func TestFormatStatus(t *testing.T) {
	t.Run("known status, no cluster code", func(t *testing.T) {
		got := FormatStatus(StatusConstraintError, nil)
		want := "CONSTRAINT_ERROR (0x87)"
		if got != want {
			t.Errorf("FormatStatus() = %q, want %q", got, want)
		}
	})

	t.Run("known status, with cluster code", func(t *testing.T) {
		cc := uint8(0x03)
		got := FormatStatus(StatusFailure, &cc)
		want := "FAILURE (0x01), cluster status 0x03"
		if got != want {
			t.Errorf("FormatStatus() = %q, want %q", got, want)
		}
	})

	t.Run("unknown, reserved, or deprecated status falls back to UNKNOWN", func(t *testing.T) {
		for _, code := range []StatusCode{0x82, 0x8A, 0xC4, 0xF0, 0xFF} {
			got := FormatStatus(code, nil)
			want := fmt.Sprintf("UNKNOWN (0x%02X)", uint8(code))
			if got != want {
				t.Errorf("FormatStatus(0x%02X) = %q, want %q", uint8(code), got, want)
			}
		}
	})
}

func TestIsStatus(t *testing.T) {
	t.Run("matching status", func(t *testing.T) {
		err := &StatusError{GeneralCode: StatusBusy}
		if !IsStatus(err, StatusBusy) {
			t.Error("IsStatus should return true for matching code")
		}
	})

	t.Run("non-matching status", func(t *testing.T) {
		err := &StatusError{GeneralCode: StatusBusy}
		if IsStatus(err, StatusTimeout) {
			t.Error("IsStatus should return false for non-matching code")
		}
	})

	t.Run("non-status error", func(t *testing.T) {
		err := errors.New("some other error")
		if IsStatus(err, StatusBusy) {
			t.Error("IsStatus should return false for non-StatusError")
		}
	})
}

func TestStatusFromIB(t *testing.T) {
	t.Run("success returns nil", func(t *testing.T) {
		ib := StatusIB{Status: uint8(StatusSuccess)}
		err := statusFromIB(ib)
		if err != nil {
			t.Errorf("statusFromIB(success) = %v, want nil", err)
		}
	})

	t.Run("failure returns error", func(t *testing.T) {
		ib := StatusIB{Status: uint8(StatusNotFound)}
		err := statusFromIB(ib)
		if err == nil {
			t.Fatal("statusFromIB(NotFound) = nil, want error")
		}
		se, ok := err.(*StatusError)
		if !ok {
			t.Fatalf("expected *StatusError, got %T", err)
		}
		if se.GeneralCode != StatusNotFound {
			t.Errorf("GeneralCode = 0x%02X, want 0x%02X", se.GeneralCode, StatusNotFound)
		}
	})

	t.Run("with cluster status", func(t *testing.T) {
		cc := uint8(0x10)
		ib := StatusIB{Status: uint8(StatusFailure), ClusterStatus: &cc}
		err := statusFromIB(ib)
		if err == nil {
			t.Fatal("statusFromIB(Failure) = nil, want error")
		}
		se := err.(*StatusError)
		if se.ClusterCode == nil || *se.ClusterCode != 0x10 {
			t.Errorf("ClusterCode = %v, want 0x10", se.ClusterCode)
		}
	})
}

func TestStatusIB_TLVRoundTrip(t *testing.T) {
	t.Run("without cluster status", func(t *testing.T) {
		orig := StatusIB{Status: uint8(StatusNotFound)}
		data, err := tlv.Marshal(orig)
		if err != nil {
			t.Fatalf("Marshal: %v", err)
		}
		var decoded StatusIB
		if err := tlv.Unmarshal(data, &decoded); err != nil {
			t.Fatalf("Unmarshal: %v", err)
		}
		if decoded.Status != orig.Status {
			t.Errorf("Status = %d, want %d", decoded.Status, orig.Status)
		}
	})

	t.Run("with cluster status", func(t *testing.T) {
		cc := uint8(0x42)
		orig := StatusIB{Status: uint8(StatusFailure), ClusterStatus: &cc}
		data, err := tlv.Marshal(orig)
		if err != nil {
			t.Fatalf("Marshal: %v", err)
		}
		var decoded StatusIB
		if err := tlv.Unmarshal(data, &decoded); err != nil {
			t.Fatalf("Unmarshal: %v", err)
		}
		if decoded.Status != orig.Status {
			t.Errorf("Status = %d, want %d", decoded.Status, orig.Status)
		}
		if decoded.ClusterStatus == nil || *decoded.ClusterStatus != cc {
			t.Errorf("ClusterStatus = %v, want %d", decoded.ClusterStatus, cc)
		}
	})
}

func TestStatusResponseMessage_TLVRoundTrip(t *testing.T) {
	orig := StatusResponseMessage{
		Status: uint8(StatusBusy),
	}
	data, err := tlv.Marshal(orig)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var decoded StatusResponseMessage
	if err := tlv.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if decoded.Status != orig.Status {
		t.Errorf("Status = %d, want %d", decoded.Status, orig.Status)
	}
}

func TestOpcodeConstants(t *testing.T) {
	// Verify opcode values match the Matter specification.
	tests := []struct {
		name   string
		opcode byte
		want   byte
	}{
		{"StatusResponse", OpcodeStatusResponse, 0x01},
		{"ReadRequest", OpcodeReadRequest, 0x02},
		{"SubscribeRequest", OpcodeSubscribeRequest, 0x03},
		{"SubscribeResponse", OpcodeSubscribeResponse, 0x04},
		{"ReportData", OpcodeReportData, 0x05},
		{"WriteRequest", OpcodeWriteRequest, 0x06},
		{"WriteResponse", OpcodeWriteResponse, 0x07},
		{"InvokeRequest", OpcodeInvokeRequest, 0x08},
		{"InvokeResponse", OpcodeInvokeResponse, 0x09},
		{"TimedRequest", OpcodeTimedRequest, 0x0A},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.opcode != tt.want {
				t.Errorf("%s = 0x%02X, want 0x%02X", tt.name, tt.opcode, tt.want)
			}
		})
	}
}
