// Copyright 2026 matter-cli contributors
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os/exec"
	"runtime"
	"sync"
	"time"

	"github.com/p0fi/matter-cli/cli/output"
)

var (
	bleProfileOnce      sync.Once
	bleProfileInstalled bool
	bleProfileCheckDone bool // true if the check completed without error
)

// warnIfMissingBLEProfile checks whether the macOS Bluetooth Central Matter
// Client Developer Mode profile is installed. If the profile is missing, a
// warning is printed to w explaining how to install it. On non-macOS systems
// this is a no-op. The result is cached so the system_profiler command runs
// at most once per process.
func warnIfMissingBLEProfile(w io.Writer) {
	if runtime.GOOS != "darwin" {
		return
	}

	bleProfileOnce.Do(func() {
		installed, err := isMatterBLEProfileInstalled()
		if err != nil {
			// Could not determine — don't warn.
			return
		}
		bleProfileInstalled = installed
		bleProfileCheckDone = true
	})

	if !bleProfileCheckDone || bleProfileInstalled {
		return
	}

	fmt.Fprintf(w, "\n%s The macOS Bluetooth Central Matter Client Developer Mode profile does not appear to be installed.\n", output.WarningIcon())
	fmt.Fprintln(w, output.Muted("  BLE operations with Matter devices require this profile."))
	fmt.Fprintln(w, output.Muted("  Install via: System Settings → Privacy & Security → Profiles"))
	fmt.Fprintln(w, output.Muted("  Download:    https://developer.apple.com/services-account/download?path=/iOS/iOS_Logs/EnableBluetoothCentralMatterClientDeveloperMode.mobileconfig"))
	fmt.Fprintln(w)
}

// isMatterBLEProfileInstalled uses system_profiler to check whether any
// installed configuration profile looks like the Matter BLE developer profile.
// Returns (true, nil) if found, (false, nil) if definitely not found, or
// (false, err) if the check itself failed.
func isMatterBLEProfileInstalled() (bool, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "system_profiler", "SPConfigurationProfileDataType")
	out, err := cmd.Output()
	if err != nil {
		return false, fmt.Errorf("running system_profiler: %w", err)
	}

	// Match against the canonical profile identifier. Using a broad
	// multi-keyword check risks false positives when unrelated profiles
	// mention "matter" or "bluetooth" independently.
	lower := bytes.ToLower(out)
	return bytes.Contains(lower, []byte("enablebluetoothcentralmatterclientdevelopermode")), nil
}
