// Copyright 2026 matter-cli contributors
// SPDX-License-Identifier: Apache-2.0

// Package vendordb provides a compiled-in lookup table mapping Matter vendor
// IDs to human-readable vendor names sourced from the CSA Distributed
// Compliance Ledger (DCL) at https://on.dcl.csa-iot.org/dcl/vendorinfo/vendors.
//
// The data is regenerated via:
//
//	go generate ./internal/vendordb/...
package vendordb

import (
	"fmt"
	"strings"
)

// Lookup returns the vendor name for the given vendor ID.
// The second return value is false when the ID is not in the database.
func Lookup(id uint16) (string, bool) {
	name, ok := vendors[id]
	return name, ok
}

// FormatVendorID returns a human-readable string for a vendor ID.
// When the ID is known it returns "Name (0xXXXX)"; otherwise "0xXXXX".
func FormatVendorID(id uint16) string {
	if name, ok := vendors[id]; ok {
		return fmt.Sprintf("%s (0x%04X)", strings.TrimSpace(name), id)
	}
	return fmt.Sprintf("0x%04X", id)
}
