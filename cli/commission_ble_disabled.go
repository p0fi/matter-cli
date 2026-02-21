// Copyright 2026 matter-cli contributors
// SPDX-License-Identifier: Apache-2.0

//go:build noble

// This file provides a no-op stub for addBLECommissionCommands when the binary
// is compiled with -tags noble (BLE support disabled).  Without this stub the
// cli package fails to build because commission.go unconditionally calls
// addBLECommissionCommands.
package cli

import "github.com/spf13/cobra"

// addBLECommissionCommands is a no-op when BLE support is compiled out.
// Recompile without -tags noble to enable the `commission ble` and
// `commission ble-address` subcommands.
func addBLECommissionCommands(_ *cobra.Command) {}
