// Copyright 2026 matter-cli contributors
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"testing"

	"github.com/p0fi/matter-cli/internal/commissioning"
)

func TestFormatCommissioningFlow(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		input commissioning.CommissioningFlow
		want  string
	}{
		{"standard", commissioning.FlowStandard, "Standard (0)"},
		{"user-intent", commissioning.FlowUserIntent, "User-Intent (1)"},
		{"custom", commissioning.FlowCustom, "Custom (2)"},
		{"unknown", commissioning.CommissioningFlow(9), "Unknown (9)"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := formatCommissioningFlow(tc.input)
			if got != tc.want {
				t.Errorf("formatCommissioningFlow(%d) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}

func TestFormatDiscoveryCapabilities(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		input commissioning.DiscoveryCapabilities
		want  string
	}{
		{"none", 0, "None (0x00)"},
		{"softap only", commissioning.DiscoverySoftAP, "SoftAP (0x01)"},
		{"ble only", commissioning.DiscoveryBLE, "BLE (0x02)"},
		{"on-network only", commissioning.DiscoveryOnNetwork, "OnNetwork (0x04)"},
		{"ble and on-network", commissioning.DiscoveryBLE | commissioning.DiscoveryOnNetwork, "BLE, OnNetwork (0x06)"},
		{"all flags", commissioning.DiscoverySoftAP | commissioning.DiscoveryBLE | commissioning.DiscoveryOnNetwork, "SoftAP, BLE, OnNetwork (0x07)"},
		{"unknown bits", commissioning.DiscoveryCapabilities(0x80), "Unknown (0x80)"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := formatDiscoveryCapabilities(tc.input)
			if got != tc.want {
				t.Errorf("formatDiscoveryCapabilities(0x%02X) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}

func TestLooksLikeQRPayload(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		input string
		want  bool
	}{
		{"valid base38 string", "Y3.13OTB00KA0648G00", true},
		{"all digits numeric", "34970112332", true},
		{"too short", "ABC", false},
		{"lowercase letters", "abcdefgh", false},
		{"has MT prefix already", "MT:Y3.13OTB00KA0648G00", false}, // contains ':'
		{"spaces", "ABCD EFG", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := looksLikeQRPayload(tc.input)
			if got != tc.want {
				t.Errorf("looksLikeQRPayload(%q) = %v, want %v", tc.input, got, tc.want)
			}
		})
	}
}
