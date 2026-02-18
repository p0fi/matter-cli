// Copyright 2026 matter-cli contributors
// SPDX-License-Identifier: Apache-2.0

//go:build !windows

package cli

import "syscall"

// daemonSysProcAttr returns the SysProcAttr used when spawning the background
// session daemon process. On Unix systems it sets Setsid so the daemon runs in
// its own session and is not killed when the parent terminal closes.
func daemonSysProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{
		Setsid: true,
	}
}
