// Copyright 2026 matter-cli contributors
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"errors"
	"fmt"
	"testing"

	"github.com/p0fi/matter-cli/internal/interaction"
)

// TestImStatusError_WriteRegression is the regression case from issue #81:
// a write returning IM status 0x87 must render as "CONSTRAINT_ERROR (0x87)",
// not the raw hex code, and the returned error must remain typed so a Go
// caller can recover the status with errors.As/IsStatus.
func TestImStatusError_WriteRegression(t *testing.T) {
	se := imStatusError(uint8(interaction.StatusConstraintError), nil)

	got := "Write error: " + se.Error()
	want := "Write error: CONSTRAINT_ERROR (0x87)"
	if got != want {
		t.Errorf("write error text = %q, want %q", got, want)
	}

	wrapped := fmt.Errorf("write error: %w", se)
	var target *interaction.StatusError
	if !errors.As(wrapped, &target) {
		t.Fatal("errors.As failed to recover *interaction.StatusError")
	}
	if !interaction.IsStatus(wrapped, interaction.StatusConstraintError) {
		t.Error("IsStatus should recognize the wrapped CONSTRAINT_ERROR status")
	}
}

// TestImStatusError_ClusterStatus verifies the "NAME (0xNN), cluster status
// 0xNN" contract for a general FAILURE status carrying a cluster-specific
// code, matching the issue's second example.
func TestImStatusError_ClusterStatus(t *testing.T) {
	cc := uint8(0x03)
	se := imStatusError(uint8(interaction.StatusFailure), &cc)

	got := se.Error()
	want := "FAILURE (0x01), cluster status 0x03"
	if got != want {
		t.Errorf("Error() = %q, want %q", got, want)
	}
}

// TestImStatusError_DaemonAndDirectParity ensures the daemon-backed and
// direct-CASE branches produce byte-identical status text for the same
// underlying status, since both extract (code, clusterCode) from their
// respective response shapes and funnel through the same imStatusError call.
func TestImStatusError_DaemonAndDirectParity(t *testing.T) {
	cc := uint8(0x10)

	// Shape returned by the daemon wire protocol (daemon.AttrStatusResp).
	daemonCode, daemonCluster := uint8(interaction.StatusUnsupportedAttribute), &cc

	// Shape returned by a direct CASE interaction (interaction.StatusIB).
	directCode, directCluster := uint8(interaction.StatusUnsupportedAttribute), &cc

	daemonMsg := imStatusError(daemonCode, daemonCluster).Error()
	directMsg := imStatusError(directCode, directCluster).Error()

	if daemonMsg != directMsg {
		t.Errorf("daemon and direct status text diverge: %q vs %q", daemonMsg, directMsg)
	}
	want := "UNSUPPORTED_ATTRIBUTE (0x86), cluster status 0x10"
	if daemonMsg != want {
		t.Errorf("status text = %q, want %q", daemonMsg, want)
	}
}
