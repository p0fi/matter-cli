// Copyright 2026 matter-cli contributors
// SPDX-License-Identifier: Apache-2.0

package basicinformation

import "fmt"

// FormatSpecVersion converts a packed SpecificationVersion uint32 (e.g.
// 0x01030000 for Matter 1.3) into a human-readable string ("1.3"). Trailing
// zero minor/patch components are omitted. Returns an empty string for zero.
func FormatSpecVersion(v uint32) string {
	if v == 0 {
		return ""
	}
	major := (v >> 24) & 0xFF
	minor := (v >> 16) & 0xFF
	patch := (v >> 8) & 0xFF
	if patch != 0 {
		return fmt.Sprintf("%d.%d.%d", major, minor, patch)
	}
	return fmt.Sprintf("%d.%d", major, minor)
}
