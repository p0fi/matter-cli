// Copyright 2026 matter-cli contributors
// SPDX-License-Identifier: Apache-2.0

//go:build noble

// Package transport stub for builds compiled with -tags noble.
//
// When the "noble" build tag is set all BLE-related source files (ble.go,
// ble_scanner.go, ble_adapter_tinygo.go, internal/discovery/ble.go, …) are
// excluded from the build. This file provides the minimum set of exported
// symbols that the rest of the codebase references unconditionally so that
// "go build -tags noble ./..." compiles cleanly without any Bluetooth support.
//
// At runtime, any attempt to use BLE will return ErrBLENotSupported.
package transport

import "errors"

// ErrBLENotSupported is returned by all BLE entry-points when the binary has
// been compiled without BLE support (i.e. with -tags noble).
var ErrBLENotSupported = errors.New("BLE support not compiled in (rebuild without -tags noble to enable)")
