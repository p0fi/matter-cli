// Copyright 2026 matter-cli contributors
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"bytes"
	"runtime"
	"testing"
)

func TestWarnIfMissingBLEProfile_NonDarwin(t *testing.T) {
	if runtime.GOOS == "darwin" {
		t.Skip("this test validates the no-op path on non-darwin")
	}

	var buf bytes.Buffer
	warnIfMissingBLEProfile(&buf)

	if buf.Len() != 0 {
		t.Errorf("expected no output on non-darwin, got: %s", buf.String())
	}
}

func TestWarnIfMissingBLEProfile_Darwin(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("darwin-only test")
	}

	// This test verifies the function runs without panicking on macOS.
	// The actual output depends on whether the profile is installed,
	// which varies by machine — so we just confirm no crash.
	var buf bytes.Buffer
	warnIfMissingBLEProfile(&buf)

	// The function should either produce no output (profile installed or
	// check failed) or produce a warning containing "profile".
	if buf.Len() > 0 && !bytes.Contains(bytes.ToLower(buf.Bytes()), []byte("profile")) {
		t.Errorf("unexpected output: %s", buf.String())
	}
}
