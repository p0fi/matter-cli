// Copyright 2026 matter-cli contributors
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"bytes"
	"encoding/hex"
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

// TestAttrReports_DaemonAndDirectParity drives the transport-boundary
// normalisers used by runClusterRead (daemonAttrReports/directAttrReports)
// with equivalent daemon-wire and direct-CASE response shapes, asserting both
// produce identical transport-neutral reports — so the records built from them
// cannot depend on whether a session daemon happens to be running.
func TestAttrReports_DaemonAndDirectParity(t *testing.T) {
	cc := uint8(0x10)
	payload := []byte{0x08} // an arbitrary encoded TLV boolean(false)

	daemonReports := []daemon.AttrReportResp{
		{Endpoint: 1, ClusterID: 0x0006, AttributeID: 0x0000, Data: daemon.EncodeFields(payload)},
		{Endpoint: 1, ClusterID: 0x0006, AttributeID: 0x4003, StatusCode: uint8(interaction.StatusConstraintError), ClusterStatus: &cc},
	}
	directReports := []interaction.AttributeReport{
		{Data: &interaction.AttributeData{
			Path: interaction.NewAttributePath(1, 0x0006, 0x0000),
			Data: payload,
		}},
		{Status: &interaction.AttributeStatus{
			Path:   interaction.NewAttributePath(1, 0x0006, 0x4003),
			Status: interaction.StatusIB{Status: uint8(interaction.StatusConstraintError), ClusterStatus: &cc},
		}},
	}

	got := daemonAttrReports(daemonReports)
	want := directAttrReports(directReports)

	if len(got) != 2 || len(want) != 2 {
		t.Fatalf("report counts = %d/%d, want 2/2", len(got), len(want))
	}
	for i := range got {
		if got[i].attributeID != want[i].attributeID {
			t.Errorf("report %d attribute ID = 0x%04X (daemon) vs 0x%04X (direct)", i, got[i].attributeID, want[i].attributeID)
		}
		if string(got[i].data) != string(want[i].data) {
			t.Errorf("report %d data = %x (daemon) vs %x (direct)", i, got[i].data, want[i].data)
		}
		switch {
		case got[i].err == nil && want[i].err == nil:
		case got[i].err == nil || want[i].err == nil:
			t.Errorf("report %d error presence differs: daemon=%v direct=%v", i, got[i].err, want[i].err)
		case got[i].err.Error() != want[i].err.Error():
			t.Errorf("report %d error = %q (daemon) vs %q (direct)", i, got[i].err, want[i].err)
		}
	}

	// The first report carries data; the second carries the same typed status
	// error a single-attribute read has always produced.
	if string(got[0].data) != string(payload) {
		t.Errorf("data = %x, want %x", got[0].data, payload)
	}
	if got[1].err == nil {
		t.Fatal("expected the status report to carry an error")
	}
	if want := "CONSTRAINT_ERROR (0x87), cluster status 0x10"; got[1].err.Error() != want {
		t.Errorf("status text = %q, want %q", got[1].err.Error(), want)
	}
	var se *interaction.StatusError
	if !errors.As(got[1].err, &se) {
		t.Fatal("report error does not unwrap to *interaction.StatusError")
	}
	if !interaction.IsStatus(got[1].err, interaction.StatusConstraintError) {
		t.Error("IsStatus should recognize CONSTRAINT_ERROR from a normalised report")
	}
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
// truncation behavior under both tlvFidelity modes. It exists because that
// path had no prior test coverage and was refactored (extracted onto the
// shared tlvChildren walker also used by decodeTLVNative in subscribe.go) as
// part of hardening attribute subscriptions — this pins its externally
// observable behavior. It was later extended (issue #95) to prove that
// fidelityCompact reproduces the original elided/truncated behavior
// byte-for-byte while fidelityFull never elides a struct field or array
// element and never truncates a scalar leaf.
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
		want := "[1, 2]"
		for _, fidelity := range []tlvFidelity{fidelityCompact, fidelityFull} {
			if got := decodeTLVValue(w.Bytes(), fidelity); got != want {
				t.Errorf("decodeTLVValue(%s) = %q, want %q", fidelity, got, want)
			}
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
		want := "{0: true}"
		for _, fidelity := range []tlvFidelity{fidelityCompact, fidelityFull} {
			if got := decodeTLVValue(w.Bytes(), fidelity); got != want {
				t.Errorf("decodeTLVValue(%s) = %q, want %q", fidelity, got, want)
			}
		}
	})

	t.Run("long array: compact truncates, full shows everything", func(t *testing.T) {
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

		compact := decodeTLVValue(w.Bytes(), fidelityCompact)
		if len(compact) > maxValueLen {
			t.Errorf("compact = %q (len %d), want it truncated to at most %d chars", compact, len(compact), maxValueLen)
		}
		if compact[0] != '[' || compact[len(compact)-1] != ']' {
			t.Errorf("compact = %q, want it to remain array-bracketed", compact)
		}
		if !strings.Contains(compact, "0, ") || !strings.Contains(compact, "..., 19]") {
			t.Errorf("compact = %q, want it to show the first and last elements", compact)
		}

		full := decodeTLVValue(w.Bytes(), fidelityFull)
		if strings.Contains(full, "...") {
			t.Errorf("full = %q, want no elision marker", full)
		}
		for i := 0; i < 20; i++ {
			if !strings.Contains(full, fmt.Sprintf("%d", i)) {
				t.Errorf("full = %q, want it to contain element %d", full, i)
			}
		}
	})

	t.Run("array of structs: compact elides struct fields, full shows every field", func(t *testing.T) {
		w := tlv.NewWriter()
		if err := w.StartArray(tlv.AnonymousTag()); err != nil {
			t.Fatal(err)
		}
		for i := 0; i < 4; i++ {
			if err := w.StartStructure(tlv.AnonymousTag()); err != nil {
				t.Fatal(err)
			}
			if err := w.PutUTF8String(tlv.ContextTag(0), fmt.Sprintf("iface%d", i)); err != nil {
				t.Fatal(err)
			}
			if err := w.PutBool(tlv.ContextTag(1), true); err != nil {
				t.Fatal(err)
			}
			if err := w.PutOctetString(tlv.ContextTag(2), []byte{0xAA, 0xBB, 0xCC, 0xDD, 0xEE, 0xFF}); err != nil {
				t.Fatal(err)
			}
			if err := w.PutUnsignedInt(tlv.ContextTag(3), uint64(i)); err != nil {
				t.Fatal(err)
			}
			if err := w.EndContainer(); err != nil {
				t.Fatal(err)
			}
		}
		if err := w.EndContainer(); err != nil {
			t.Fatal(err)
		}

		compact := decodeTLVValue(w.Bytes(), fidelityCompact)
		if !strings.Contains(compact, "...") {
			t.Errorf("compact = %q, want elision for a 4-element array of structs", compact)
		}

		full := decodeTLVValue(w.Bytes(), fidelityFull)
		if strings.Contains(full, "...") {
			t.Errorf("full = %q, want no elision marker at all", full)
		}
		for i := 0; i < 4; i++ {
			for _, want := range []string{
				fmt.Sprintf("0: \"iface%d\"", i),
				"1: true",
				"2: 0xaabbccddeeff",
				fmt.Sprintf("3: %d", i),
			} {
				if !strings.Contains(full, want) {
					t.Errorf("full = %q, missing field %q for interface %d", full, want, i)
				}
			}
		}
	})

	t.Run("wide struct: compact elides fields, full shows every field", func(t *testing.T) {
		w := tlv.NewWriter()
		if err := w.StartStructure(tlv.AnonymousTag()); err != nil {
			t.Fatal(err)
		}
		for i := 0; i < 10; i++ {
			if err := w.PutUnsignedInt(tlv.ContextTag(uint8(i)), uint64(i*1111)); err != nil {
				t.Fatal(err)
			}
		}
		if err := w.EndContainer(); err != nil {
			t.Fatal(err)
		}

		compact := decodeTLVValue(w.Bytes(), fidelityCompact)
		if !strings.Contains(compact, "...") {
			t.Errorf("compact = %q, want elision for a 10-field struct", compact)
		}

		full := decodeTLVValue(w.Bytes(), fidelityFull)
		if strings.Contains(full, "...") {
			t.Errorf("full = %q, want no elision marker at all", full)
		}
		for i := 0; i < 10; i++ {
			want := fmt.Sprintf("%d: %d", i, i*1111)
			if !strings.Contains(full, want) {
				t.Errorf("full = %q, missing field %q", full, want)
			}
		}
	})

	t.Run("long scalar leaf: compact middle-truncates, full prints in full", func(t *testing.T) {
		long := strings.Repeat("x", 80)

		t.Run("string", func(t *testing.T) {
			w := tlv.NewWriter()
			if err := w.PutUTF8String(tlv.AnonymousTag(), long); err != nil {
				t.Fatal(err)
			}
			compact := decodeTLVValue(w.Bytes(), fidelityCompact)
			if len(compact) > maxValueLen || !strings.Contains(compact, "...") {
				t.Errorf("compact = %q, want it middle-truncated to at most %d chars", compact, maxValueLen)
			}
			full := decodeTLVValue(w.Bytes(), fidelityFull)
			if want := fmt.Sprintf("%q", long); full != want {
				t.Errorf("full = %q, want the untruncated %q", full, want)
			}
		})

		t.Run("byte array", func(t *testing.T) {
			longBytes := bytes.Repeat([]byte{0xAB}, 40)
			w := tlv.NewWriter()
			if err := w.PutOctetString(tlv.AnonymousTag(), longBytes); err != nil {
				t.Fatal(err)
			}
			compact := decodeTLVValue(w.Bytes(), fidelityCompact)
			if len(compact) > maxValueLen || !strings.Contains(compact, "...") {
				t.Errorf("compact = %q, want it middle-truncated to at most %d chars", compact, maxValueLen)
			}
			full := decodeTLVValue(w.Bytes(), fidelityFull)
			want := "0x" + hex.EncodeToString(longBytes)
			if full != want {
				t.Errorf("full = %q, want the untruncated %q", full, want)
			}
		})
	})
}
