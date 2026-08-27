// Copyright 2026 matter-cli contributors
// SPDX-License-Identifier: Apache-2.0

package daemon

import (
	"encoding/json"
	"errors"
	"fmt"
	"testing"

	"github.com/p0fi/matter-cli/internal/interaction"
)

// TestStatusResponse_PreservesTypedStatus verifies that statusResponse
// carries the general and cluster status codes from a typed
// *interaction.StatusError onto the wire-level Response, and leaves them
// zero for a plain (non-status) error.
func TestStatusResponse_PreservesTypedStatus(t *testing.T) {
	t.Run("plain error carries no status", func(t *testing.T) {
		resp := statusResponse("read failed", errors.New("connection reset"))
		if resp.OK {
			t.Fatal("expected OK=false")
		}
		if resp.StatusCode != 0 || resp.ClusterStatus != nil {
			t.Errorf("plain error should not populate status fields, got StatusCode=0x%02X ClusterStatus=%v",
				resp.StatusCode, resp.ClusterStatus)
		}
		wantErr := "read failed: connection reset"
		if resp.Error != wantErr {
			t.Errorf("Error = %q, want %q", resp.Error, wantErr)
		}
	})

	t.Run("typed status error without cluster code", func(t *testing.T) {
		se := &interaction.StatusError{GeneralCode: interaction.StatusBusy}
		resp := statusResponse("read failed", fmt.Errorf("client.Read: %w", se))
		if resp.StatusCode != uint8(interaction.StatusBusy) {
			t.Errorf("StatusCode = 0x%02X, want 0x%02X", resp.StatusCode, uint8(interaction.StatusBusy))
		}
		if resp.ClusterStatus != nil {
			t.Errorf("ClusterStatus = %v, want nil", resp.ClusterStatus)
		}
	})

	t.Run("typed status error with cluster code", func(t *testing.T) {
		cc := uint8(0x03)
		se := &interaction.StatusError{GeneralCode: interaction.StatusFailure, ClusterCode: &cc}
		resp := statusResponse("write failed", se)
		if resp.StatusCode != uint8(interaction.StatusFailure) {
			t.Errorf("StatusCode = 0x%02X, want 0x%02X", resp.StatusCode, uint8(interaction.StatusFailure))
		}
		if resp.ClusterStatus == nil || *resp.ClusterStatus != cc {
			t.Errorf("ClusterStatus = %v, want %#v", resp.ClusterStatus, &cc)
		}
	})
}

// TestRespError_ReconstructsTypedStatus verifies that respError rebuilds an
// *interaction.StatusError from a Response's structured status fields, so
// daemon-backed callers can errors.As/IsStatus it exactly like a direct CASE
// caller would.
func TestRespError_ReconstructsTypedStatus(t *testing.T) {
	t.Run("plain failure has no typed status", func(t *testing.T) {
		resp := &Response{OK: false, Error: "connecting to node: timeout"}
		err := respError("daemon: read", resp)
		var se *interaction.StatusError
		if errors.As(err, &se) {
			t.Fatal("expected no *interaction.StatusError for a plain failure")
		}
		want := "daemon: read: connecting to node: timeout"
		if err.Error() != want {
			t.Errorf("Error() = %q, want %q", err.Error(), want)
		}
	})

	t.Run("status failure reconstructs typed error", func(t *testing.T) {
		cc := uint8(0x03)
		resp := &Response{OK: false, StatusCode: uint8(interaction.StatusConstraintError), ClusterStatus: &cc}
		err := respError("daemon: write", resp)

		var se *interaction.StatusError
		if !errors.As(err, &se) {
			t.Fatal("expected errors.As to recover *interaction.StatusError")
		}
		if se.GeneralCode != interaction.StatusConstraintError {
			t.Errorf("GeneralCode = %v, want %v", se.GeneralCode, interaction.StatusConstraintError)
		}
		if se.ClusterCode == nil || *se.ClusterCode != cc {
			t.Errorf("ClusterCode = %v, want %#v", se.ClusterCode, &cc)
		}
		if !interaction.IsStatus(err, interaction.StatusConstraintError) {
			t.Error("IsStatus should recognize CONSTRAINT_ERROR through the wrapped error")
		}
	})
}

// TestStatusRoundTrip_ServerToClient simulates the full server -> JSON wire
// -> client path for a status failure, verifying it reconstructs into an
// equivalent typed StatusError on the client side — the same typed error a
// direct CASE interaction would have produced for the same status.
func TestStatusRoundTrip_ServerToClient(t *testing.T) {
	cc := uint8(0x87)
	serverErr := &interaction.StatusError{GeneralCode: interaction.StatusFailure, ClusterCode: &cc}

	// Server side: build the Response as handleRead/handleWrite/handleInvoke would.
	serverResp := statusResponse("write failed", serverErr)

	// Wire: marshal/unmarshal exactly as the Unix socket transport does.
	data, err := json.Marshal(serverResp)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var clientResp Response
	if err := json.Unmarshal(data, &clientResp); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	// Client side: reconstruct the typed error as Client.Write would.
	clientErr := respError("daemon: write", &clientResp)

	var se *interaction.StatusError
	if !errors.As(clientErr, &se) {
		t.Fatal("client-side error does not unwrap to *interaction.StatusError")
	}
	if se.GeneralCode != serverErr.GeneralCode {
		t.Errorf("GeneralCode = %v, want %v", se.GeneralCode, serverErr.GeneralCode)
	}
	if se.ClusterCode == nil || *se.ClusterCode != *serverErr.ClusterCode {
		t.Errorf("ClusterCode = %v, want %v", se.ClusterCode, serverErr.ClusterCode)
	}

	// The rendered status text must match the direct-CASE StatusError.Error()
	// exactly — daemon transport must not change user-visible status text.
	if se.Error() != serverErr.Error() {
		t.Errorf("round-tripped status text = %q, want %q", se.Error(), serverErr.Error())
	}
}
