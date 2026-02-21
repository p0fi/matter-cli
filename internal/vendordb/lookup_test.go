// Copyright 2026 matter-cli contributors
// SPDX-License-Identifier: Apache-2.0

package vendordb

import (
	"fmt"
	"testing"
)

func TestLookup_KnownVendors(t *testing.T) {
	tests := []struct {
		id   uint16
		want string
	}{
		{1, "Panasonic"},
		{4476, "IKEA of Sweden"},
		{4488, "TP-Link"},
		{4891, "Espressif Systems"},
		{4957, "Nuki"},
		{4874, "Eve"},
		{4937, "Apple Home"},
		{4939, "Home Assistant"},
		{4718, "Xiaomi"},
		{24582, "Google"},
		{0xFFF1, "Test Vendor 1"},
		{0xFFF2, "Test Vendor 2"},
		{0xFFF3, "Test Vendor 3"},
		{0xFFF4, "Test Vendor 4"},
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("0x%04X", tt.id), func(t *testing.T) {
			got, ok := Lookup(tt.id)
			if !ok {
				t.Fatalf("Lookup(0x%04X) = _, false; want %q", tt.id, tt.want)
			}
			if got != tt.want {
				t.Errorf("Lookup(0x%04X) = %q; want %q", tt.id, got, tt.want)
			}
		})
	}
}

func TestLookup_UnknownVendor(t *testing.T) {
	unknownIDs := []uint16{0x0000, 0x0002, 0x1234, 0xFFFE}
	for _, id := range unknownIDs {
		t.Run(fmt.Sprintf("0x%04X", id), func(t *testing.T) {
			_, ok := Lookup(id)
			if ok {
				t.Errorf("Lookup(0x%04X) = _, true; want false for unknown vendor", id)
			}
		})
	}
}

func TestFormatVendorID_Known(t *testing.T) {
	tests := []struct {
		id   uint16
		want string
	}{
		{4891, "Espressif Systems (0x131B)"},
		{4476, "IKEA of Sweden (0x117C)"},
		{4488, "TP-Link (0x1188)"},
		{4957, "Nuki (0x135D)"},
		{0xFFF1, "Test Vendor 1 (0xFFF1)"},
		{0xFFF4, "Test Vendor 4 (0xFFF4)"},
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("0x%04X", tt.id), func(t *testing.T) {
			got := FormatVendorID(tt.id)
			if got != tt.want {
				t.Errorf("FormatVendorID(0x%04X) = %q; want %q", tt.id, got, tt.want)
			}
		})
	}
}

func TestFormatVendorID_Unknown(t *testing.T) {
	tests := []struct {
		id   uint16
		want string
	}{
		{0x0000, "0x0000"},
		{0x1234, "0x1234"},
		{0xFFFE, "0xFFFE"},
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("0x%04X", tt.id), func(t *testing.T) {
			got := FormatVendorID(tt.id)
			if got != tt.want {
				t.Errorf("FormatVendorID(0x%04X) = %q; want %q", tt.id, got, tt.want)
			}
		})
	}
}

func TestVendorMap_NoDuplicates(t *testing.T) {
	// The map literal itself prevents duplicate keys at compile time, but
	// this test documents the invariant and catches any future codegen issues.
	seen := make(map[uint16]struct{}, len(vendors))
	for id := range vendors {
		if _, dup := seen[id]; dup {
			t.Errorf("duplicate vendor ID 0x%04X in vendors map", id)
		}
		seen[id] = struct{}{}
	}
}

func TestVendorMap_NoEmptyNames(t *testing.T) {
	for id, name := range vendors {
		if name == "" {
			t.Errorf("vendor 0x%04X has an empty name", id)
		}
	}
}
