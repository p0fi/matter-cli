// Copyright 2026 matter-cli contributors
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"

	"github.com/p0fi/matter-cli/cli/output"
	"github.com/p0fi/matter-cli/internal/clusters/administratorcommissioning"
	"github.com/p0fi/matter-cli/internal/interaction"
)

// recordingHandler is a minimal slog.Handler that captures emitted records so
// tests can assert on structured log attributes without a real logger sink.
type recordingHandler struct {
	records *[]slog.Record
}

func (h *recordingHandler) Enabled(context.Context, slog.Level) bool { return true }
func (h *recordingHandler) Handle(_ context.Context, r slog.Record) error {
	*h.records = append(*h.records, r)
	return nil
}
func (h *recordingHandler) WithAttrs(_ []slog.Attr) slog.Handler { return h }
func (h *recordingHandler) WithGroup(_ string) slog.Handler      { return h }

// recordAttr looks up an attribute value by key on a captured record.
func recordAttr(r slog.Record, key string) (slog.Value, bool) {
	var v slog.Value
	var found bool
	r.Attrs(func(a slog.Attr) bool {
		if a.Key == key {
			v = a.Value
			found = true
			return false
		}
		return true
	})
	return v, found
}

func TestHandleAdminStatus(t *testing.T) {
	stepper := output.NewStepper(io.Discard, false)

	t.Run("success (0, nil) returns nil", func(t *testing.T) {
		if err := handleAdminStatus(stepper, administratorcommissioning.CmdOpenCommissioningWindow, 0, nil); err != nil {
			t.Errorf("handleAdminStatus() = %v, want nil", err)
		}
	})

	t.Run("known cluster status stays concise with no appended code", func(t *testing.T) {
		cc := adminBusy
		err := handleAdminStatus(stepper, administratorcommissioning.CmdOpenCommissioningWindow, uint8(interaction.StatusFailure), &cc)
		if err == nil {
			t.Fatal("expected an error")
		}
		want := "device is busy (Busy) — a commissioning window is already open"
		if err.Error() != want {
			t.Errorf("Error() = %q, want %q", err.Error(), want)
		}
		if strings.Contains(err.Error(), "0x") {
			t.Errorf("concise decoder message should not append a generic status code: %q", err.Error())
		}

		var se *interaction.StatusError
		if !errors.As(err, &se) {
			t.Fatal("expected errors.As to recover the underlying *interaction.StatusError")
		}
		if se.GeneralCode != interaction.StatusFailure || se.ClusterCode == nil || *se.ClusterCode != adminBusy {
			t.Errorf("unexpected underlying status: %+v", se)
		}
		if !interaction.IsStatus(err, interaction.StatusFailure) {
			t.Error("IsStatus should recognize FAILURE through the wrapped decoder error")
		}
	})

	t.Run("PAKEParameterError stays concise", func(t *testing.T) {
		cc := adminPAKEParamError
		err := handleAdminStatus(stepper, administratorcommissioning.CmdOpenCommissioningWindow, uint8(interaction.StatusFailure), &cc)
		want := "device rejected the PAKE parameters (PAKEParameterError)"
		if err == nil || err.Error() != want {
			t.Errorf("Error() = %v, want %q", err, want)
		}
	})

	t.Run("WindowNotOpen wording depends on command", func(t *testing.T) {
		cc := adminWindowNotOpen

		revokeErr := handleAdminStatus(stepper, administratorcommissioning.CmdRevokeCommissioning, uint8(interaction.StatusFailure), &cc)
		wantRevoke := "no commissioning window is currently open (WindowNotOpen)"
		if revokeErr == nil || revokeErr.Error() != wantRevoke {
			t.Errorf("revoke Error() = %v, want %q", revokeErr, wantRevoke)
		}

		openErr := handleAdminStatus(stepper, administratorcommissioning.CmdOpenCommissioningWindow, uint8(interaction.StatusFailure), &cc)
		wantOpen := "WindowNotOpen"
		if openErr == nil || openErr.Error() != wantOpen {
			t.Errorf("open Error() = %v, want %q", openErr, wantOpen)
		}
	})

	t.Run("unmapped cluster status stays numeric", func(t *testing.T) {
		cc := uint8(0x99)
		err := handleAdminStatus(stepper, administratorcommissioning.CmdOpenCommissioningWindow, uint8(interaction.StatusFailure), &cc)
		want := "cluster status 0x99"
		if err == nil || err.Error() != want {
			t.Errorf("Error() = %v, want %q", err, want)
		}
		if !interaction.IsStatus(err, interaction.StatusFailure) {
			t.Error("IsStatus should still recognize FAILURE for an unmapped cluster status")
		}
	})

	t.Run("general status with no cluster status falls back to NAME (0xNN)", func(t *testing.T) {
		err := handleAdminStatus(stepper, administratorcommissioning.CmdOpenCommissioningWindow, uint8(interaction.StatusBusy), nil)
		want := "BUSY (0x9C)"
		if err == nil || err.Error() != want {
			t.Errorf("Error() = %v, want %q", err, want)
		}
		if !interaction.IsStatus(err, interaction.StatusBusy) {
			t.Error("IsStatus should recognize BUSY through the general-status fallback")
		}
	})
}

// TestHandleAdminStatus_VerboseLogging verifies that a rejected admin
// commissioning command logs the general and cluster status as structured
// slog fields, so verbose troubleshooting has access to the raw codes even
// though the returned error's message stays concise.
func TestHandleAdminStatus_VerboseLogging(t *testing.T) {
	var records []slog.Record
	prev := slog.Default()
	slog.SetDefault(slog.New(&recordingHandler{records: &records}))
	defer slog.SetDefault(prev)

	stepper := output.NewStepper(io.Discard, false)
	cc := adminBusy
	if err := handleAdminStatus(stepper, administratorcommissioning.CmdOpenCommissioningWindow, uint8(interaction.StatusFailure), &cc); err == nil {
		t.Fatal("expected an error")
	}

	if len(records) == 0 {
		t.Fatal("expected handleAdminStatus to emit a log record")
	}
	r := records[0]

	statusAttr, ok := recordAttr(r, "status")
	if !ok || statusAttr.String() != "FAILURE" {
		t.Errorf("status attr = %v (ok=%v), want %q", statusAttr, ok, "FAILURE")
	}
	codeAttr, ok := recordAttr(r, "statusCode")
	if !ok || codeAttr.String() != "0x01" {
		t.Errorf("statusCode attr = %v (ok=%v), want %q", codeAttr, ok, "0x01")
	}
	clusterAttr, ok := recordAttr(r, "clusterStatus")
	if !ok {
		t.Fatal("expected a clusterStatus attr")
	}
	got, ok := clusterAttr.Any().(*uint8)
	if !ok || got == nil || *got != adminBusy {
		t.Errorf("clusterStatus attr = %v, want pointer to 0x%02X", clusterAttr.Any(), adminBusy)
	}
}

// TestLoadNodeVIDPID_BestEffortIsNonFatal verifies loadNodeVIDPID's
// best-effort contract: a node that cannot be reached (no store entry, no
// daemon, no CASE session) must not fail the caller. It returns zero values
// and must not surface a failure in normal (non-verbose) stepper output —
// only a best-effort slog.Debug entry, per the issue's testing decision that
// "best-effort interaction tests must verify that optional status failures
// remain non-fatal and absent from normal output."
func TestLoadNodeVIDPID_BestEffortIsNonFatal(t *testing.T) {
	// Sandbox the store and daemon-socket paths to an empty temp dir so this
	// test never touches the real ~/.config/matter-cli, and node lookup
	// fails fast (no store entry) instead of attempting real network I/O.
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	var records []slog.Record
	prev := slog.Default()
	slog.SetDefault(slog.New(&recordingHandler{records: &records}))
	defer slog.SetDefault(prev)

	var buf bytes.Buffer
	stepper := output.NewStepper(&buf, false)

	const unreachableNodeID = 0xDEADBEEF

	vid, pid := loadNodeVIDPID(context.Background(), unreachableNodeID, stepper)

	if vid != 0 || pid != 0 {
		t.Errorf("loadNodeVIDPID() = (%d, %d), want (0, 0) for an unreachable node", vid, pid)
	}

	if out := buf.String(); out != "" {
		t.Errorf("normal output should stay silent about the best-effort failure, got %q", out)
	}

	found := false
	for _, r := range records {
		if r.Level == slog.LevelDebug {
			found = true
		}
		if r.Level > slog.LevelDebug {
			t.Errorf("best-effort failure must not log above Debug level, got %v: %s", r.Level, r.Message)
		}
	}
	if !found {
		t.Error("expected a Debug-level log entry for the best-effort failure")
	}
}
