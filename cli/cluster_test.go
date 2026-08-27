// Copyright 2026 matter-cli contributors
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/p0fi/matter-cli/internal/daemon"
	"github.com/p0fi/matter-cli/internal/interaction"
	"github.com/p0fi/matter-cli/internal/tlv"
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

// TestReadResult_DaemonAndDirectParity drives the actual command-boundary
// decision functions used by readAttribute (daemonReadResult/directReadResult)
// with representative daemon-wire and direct-CASE response shapes for the
// same 0x87 failure, and for a successful read, asserting both transports
// produce identical typed errors and identical returned data.
func TestReadResult_DaemonAndDirectParity(t *testing.T) {
	t.Run("0x87 regression on first path", func(t *testing.T) {
		cc := uint8(0x10)

		daemonReports := []daemon.AttrReportResp{
			{Endpoint: 1, ClusterID: 0x0006, AttributeID: 0x0000, StatusCode: uint8(interaction.StatusConstraintError), ClusterStatus: &cc},
		}
		directReports := []interaction.AttributeReport{
			{Status: &interaction.AttributeStatus{
				Path:   interaction.NewAttributePath(1, 0x0006, 0x0000),
				Status: interaction.StatusIB{Status: uint8(interaction.StatusConstraintError), ClusterStatus: &cc},
			}},
		}

		_, daemonFound, daemonErr := daemonReadResult(daemonReports)
		_, directFound, directErr := directReadResult(directReports)

		if daemonFound || directFound {
			t.Errorf("found = %v/%v, want false/false on failure", daemonFound, directFound)
		}
		if daemonErr == nil || directErr == nil {
			t.Fatal("expected non-nil errors from both transports")
		}
		if daemonErr.Error() != directErr.Error() {
			t.Errorf("daemon and direct status text diverge: %q vs %q", daemonErr.Error(), directErr.Error())
		}
		want := "CONSTRAINT_ERROR (0x87), cluster status 0x10"
		if daemonErr.Error() != want {
			t.Errorf("status text = %q, want %q", daemonErr.Error(), want)
		}

		var se *interaction.StatusError
		if !errors.As(daemonErr, &se) {
			t.Fatal("daemon error does not unwrap to *interaction.StatusError")
		}
		if !errors.As(directErr, &se) {
			t.Fatal("direct error does not unwrap to *interaction.StatusError")
		}
		if !interaction.IsStatus(daemonErr, interaction.StatusConstraintError) {
			t.Error("IsStatus should recognize CONSTRAINT_ERROR from the daemon path")
		}
		if !interaction.IsStatus(directErr, interaction.StatusConstraintError) {
			t.Error("IsStatus should recognize CONSTRAINT_ERROR from the direct-CASE path")
		}
	})

	t.Run("successful read returns data on both transports", func(t *testing.T) {
		payload := []byte{0x08} // an arbitrary encoded TLV boolean(false)

		daemonReports := []daemon.AttrReportResp{
			{Endpoint: 1, ClusterID: 0x0006, AttributeID: 0x0000, Data: daemon.EncodeFields(payload)},
		}
		directReports := []interaction.AttributeReport{
			{Data: &interaction.AttributeData{
				Path: interaction.NewAttributePath(1, 0x0006, 0x0000),
				Data: payload,
			}},
		}

		daemonData, daemonFound, daemonErr := daemonReadResult(daemonReports)
		directData, directFound, directErr := directReadResult(directReports)

		if daemonErr != nil || directErr != nil {
			t.Fatalf("unexpected errors: daemon=%v direct=%v", daemonErr, directErr)
		}
		if !daemonFound || !directFound {
			t.Fatalf("found = %v/%v, want true/true on success", daemonFound, directFound)
		}
		if string(daemonData) != string(payload) || string(directData) != string(payload) {
			t.Errorf("data = %x/%x, want %x/%x", daemonData, directData, payload, payload)
		}
	})
}

// TestWriteError_DaemonAndDirectParity drives daemonWriteError/directWriteError
// (the decision functions inside writeAttribute) with representative
// multi-path responses, verifying both transports stop on the first failed
// path and produce identical typed status text.
func TestWriteError_DaemonAndDirectParity(t *testing.T) {
	daemonStatuses := []daemon.AttrStatusResp{
		{Endpoint: 1, ClusterID: 0x0006, AttributeID: 0x4000, StatusCode: 0}, // first path succeeds
		{Endpoint: 1, ClusterID: 0x0006, AttributeID: 0x0000, StatusCode: uint8(interaction.StatusConstraintError)},
	}
	directStatuses := []interaction.AttributeStatus{
		{Path: interaction.NewAttributePath(1, 0x0006, 0x4000), Status: interaction.StatusIB{Status: 0}},
		{Path: interaction.NewAttributePath(1, 0x0006, 0x0000), Status: interaction.StatusIB{Status: uint8(interaction.StatusConstraintError)}},
	}

	daemonErr := daemonWriteError(daemonStatuses)
	directErr := directWriteError(directStatuses)

	if daemonErr == nil || directErr == nil {
		t.Fatal("expected non-nil errors from both transports")
	}
	if daemonErr.Error() != directErr.Error() {
		t.Errorf("daemon and direct status text diverge: %q vs %q", daemonErr.Error(), directErr.Error())
	}
	want := "CONSTRAINT_ERROR (0x87)"
	if daemonErr.Error() != want {
		t.Errorf("status text = %q, want %q", daemonErr.Error(), want)
	}
	if !interaction.IsStatus(daemonErr, interaction.StatusConstraintError) {
		t.Error("IsStatus should recognize CONSTRAINT_ERROR from the daemon path")
	}
	if !interaction.IsStatus(directErr, interaction.StatusConstraintError) {
		t.Error("IsStatus should recognize CONSTRAINT_ERROR from the direct-CASE path")
	}

	t.Run("all paths succeed", func(t *testing.T) {
		if err := daemonWriteError([]daemon.AttrStatusResp{{StatusCode: 0}}); err != nil {
			t.Errorf("daemonWriteError() = %v, want nil", err)
		}
		if err := directWriteError([]interaction.AttributeStatus{{Status: interaction.StatusIB{Status: 0}}}); err != nil {
			t.Errorf("directWriteError() = %v, want nil", err)
		}
	})
}

// TestInvokeError_DaemonAndDirectParity drives daemonInvokeError/directInvokeError
// (the decision functions inside invokeCommand) with representative responses.
func TestInvokeError_DaemonAndDirectParity(t *testing.T) {
	cc := uint8(0x03)

	daemonResp := &daemon.InvokeResp{StatusCode: uint8(interaction.StatusFailure), ClusterStatus: &cc}
	directResp := &interaction.InvokeResponseIB{
		Status: &interaction.CommandStatusIB{
			Status: interaction.StatusIB{Status: uint8(interaction.StatusFailure), ClusterStatus: &cc},
		},
	}

	daemonErr := daemonInvokeError(daemonResp)
	directErr := directInvokeError(directResp)

	if daemonErr == nil || directErr == nil {
		t.Fatal("expected non-nil errors from both transports")
	}
	if daemonErr.Error() != directErr.Error() {
		t.Errorf("daemon and direct status text diverge: %q vs %q", daemonErr.Error(), directErr.Error())
	}
	want := "FAILURE (0x01), cluster status 0x03"
	if daemonErr.Error() != want {
		t.Errorf("status text = %q, want %q", daemonErr.Error(), want)
	}

	t.Run("success on both transports", func(t *testing.T) {
		if err := daemonInvokeError(&daemon.InvokeResp{StatusCode: 0}); err != nil {
			t.Errorf("daemonInvokeError() = %v, want nil", err)
		}
		if err := directInvokeError(&interaction.InvokeResponseIB{}); err != nil {
			t.Errorf("directInvokeError() = %v, want nil", err)
		}
		if err := directInvokeError(&interaction.InvokeResponseIB{
			Status: &interaction.CommandStatusIB{Status: interaction.StatusIB{Status: 0}},
		}); err != nil {
			t.Errorf("directInvokeError() = %v, want nil", err)
		}
	})
}

// TestDecodeTLVValue_Containers covers formatTLVContainer's array/struct/
// truncation behavior. It exists because that path had no prior test
// coverage and was refactored (extracted onto the shared tlvChildren walker
// also used by decodeTLVNative in subscribe.go) as part of hardening
// attribute subscriptions — this pins its externally observable behavior.
func TestDecodeTLVValue_Containers(t *testing.T) {
	t.Run("short array", func(t *testing.T) {
		w := tlv.NewWriter()
		if err := w.StartArray(tlv.AnonymousTag()); err != nil {
			t.Fatal(err)
		}
		if err := w.PutUnsignedInt(tlv.AnonymousTag(), 1); err != nil {
			t.Fatal(err)
		}
		if err := w.PutUnsignedInt(tlv.AnonymousTag(), 2); err != nil {
			t.Fatal(err)
		}
		if err := w.EndContainer(); err != nil {
			t.Fatal(err)
		}
		got := decodeTLVValue(w.Bytes())
		want := "[1, 2]"
		if got != want {
			t.Errorf("decodeTLVValue() = %q, want %q", got, want)
		}
	})

	t.Run("struct keyed by tag number", func(t *testing.T) {
		w := tlv.NewWriter()
		if err := w.StartStructure(tlv.AnonymousTag()); err != nil {
			t.Fatal(err)
		}
		if err := w.PutBool(tlv.ContextTag(0), true); err != nil {
			t.Fatal(err)
		}
		if err := w.EndContainer(); err != nil {
			t.Fatal(err)
		}
		got := decodeTLVValue(w.Bytes())
		want := "{0: true}"
		if got != want {
			t.Errorf("decodeTLVValue() = %q, want %q", got, want)
		}
	})

	t.Run("long array truncates with first...last", func(t *testing.T) {
		w := tlv.NewWriter()
		if err := w.StartArray(tlv.AnonymousTag()); err != nil {
			t.Fatal(err)
		}
		for i := 0; i < 20; i++ {
			if err := w.PutUnsignedInt(tlv.AnonymousTag(), uint64(i)); err != nil {
				t.Fatal(err)
			}
		}
		if err := w.EndContainer(); err != nil {
			t.Fatal(err)
		}
		got := decodeTLVValue(w.Bytes())
		if len(got) > maxValueLen {
			t.Errorf("decodeTLVValue() = %q (len %d), want it truncated to at most %d chars", got, len(got), maxValueLen)
		}
		if got[0] != '[' || got[len(got)-1] != ']' {
			t.Errorf("decodeTLVValue() = %q, want it to remain array-bracketed", got)
		}
		if !strings.Contains(got, "0, ") || !strings.Contains(got, "..., 19]") {
			t.Errorf("decodeTLVValue() = %q, want it to show the first and last elements", got)
		}
	})
}
