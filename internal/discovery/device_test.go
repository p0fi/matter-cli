// Copyright 2026 matter-cli contributors
// SPDX-License-Identifier: Apache-2.0

package discovery

import (
	"net"
	"testing"
)

func TestParseTXTRecords(t *testing.T) {
	tests := []struct {
		name      string
		records   []string
		wantDisc  uint16
		wantCM    CommissioningMode
		wantVID   uint16
		wantPID   uint16
		wantSII   uint32
		wantSAI   uint32
		wantCount int // expected number of parsed TXT entries
	}{
		{
			name:      "all fields present",
			records:   []string{"D=3840", "CM=1", "VP=65521+32769", "SII=300", "SAI=500"},
			wantDisc:  3840,
			wantCM:    CommissioningModeBasic,
			wantVID:   65521,
			wantPID:   32769,
			wantSII:   300,
			wantSAI:   500,
			wantCount: 5,
		},
		{
			name:      "discriminator only",
			records:   []string{"D=100"},
			wantDisc:  100,
			wantCM:    CommissioningModeNone,
			wantVID:   0,
			wantPID:   0,
			wantCount: 1,
		},
		{
			name:      "commissioning mode enhanced",
			records:   []string{"CM=2"},
			wantCM:    CommissioningModeEnhanced,
			wantCount: 1,
		},
		{
			name:      "VP without product ID",
			records:   []string{"VP=65521"},
			wantVID:   65521,
			wantPID:   0,
			wantCount: 1,
		},
		{
			name:      "empty records",
			records:   []string{},
			wantCount: 0,
		},
		{
			name:      "malformed records are skipped",
			records:   []string{"NOEQUALS", "D=abc", "CM=xyz", "VP=bad+data", "SII=overflow99999999999999", "SAI="},
			wantDisc:  0,
			wantCM:    CommissioningModeNone,
			wantVID:   0,
			wantPID:   0,
			wantSII:   0,
			wantSAI:   0,
			wantCount: 5, // malformed ones without = are skipped entirely
		},
		{
			name:      "unknown keys are stored in raw map",
			records:   []string{"D=42", "PH=33", "PI="},
			wantDisc:  42,
			wantCount: 3,
		},
		{
			name:      "zero discriminator",
			records:   []string{"D=0", "CM=0"},
			wantDisc:  0,
			wantCM:    CommissioningModeNone,
			wantCount: 2,
		},
		{
			name:      "max 12-bit discriminator",
			records:   []string{"D=4095"},
			wantDisc:  4095,
			wantCount: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := &Device{}
			d.parseTXTRecords(tt.records)

			if d.Discriminator != tt.wantDisc {
				t.Errorf("Discriminator = %d, want %d", d.Discriminator, tt.wantDisc)
			}
			if d.CommissioningMode != tt.wantCM {
				t.Errorf("CommissioningMode = %d, want %d", d.CommissioningMode, tt.wantCM)
			}
			if d.VendorID != tt.wantVID {
				t.Errorf("VendorID = %d, want %d", d.VendorID, tt.wantVID)
			}
			if d.ProductID != tt.wantPID {
				t.Errorf("ProductID = %d, want %d", d.ProductID, tt.wantPID)
			}
			if d.SessionIdleInterval != tt.wantSII {
				t.Errorf("SessionIdleInterval = %d, want %d", d.SessionIdleInterval, tt.wantSII)
			}
			if d.SessionActiveInterval != tt.wantSAI {
				t.Errorf("SessionActiveInterval = %d, want %d", d.SessionActiveInterval, tt.wantSAI)
			}
			if len(d.TXTRecords) != tt.wantCount {
				t.Errorf("TXTRecords count = %d, want %d", len(d.TXTRecords), tt.wantCount)
			}
		})
	}
}

func TestParseVP(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantVID uint16
		wantPID uint16
	}{
		{"full", "65521+32769", 65521, 32769},
		{"vendor only", "65521", 65521, 0},
		{"both zero", "0+0", 0, 0},
		{"empty", "", 0, 0},
		{"invalid vendor", "abc+123", 0, 123},
		{"invalid product", "123+abc", 123, 0},
		{"both invalid", "abc+def", 0, 0},
		{"overflow vendor", "99999+1", 0, 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			vid, pid := parseVP(tt.input)
			if vid != tt.wantVID {
				t.Errorf("VendorID = %d, want %d", vid, tt.wantVID)
			}
			if pid != tt.wantPID {
				t.Errorf("ProductID = %d, want %d", pid, tt.wantPID)
			}
		})
	}
}

func TestDeviceString(t *testing.T) {
	d := &Device{
		Name:              "test-device",
		IPs:               []net.IP{net.ParseIP("192.168.1.42"), net.ParseIP("fe80::1")},
		Port:              5540,
		Discriminator:     3840,
		CommissioningMode: CommissioningModeBasic,
		VendorID:          65521,
		ProductID:         32769,
	}

	got := d.String()
	want := "test-device @ 192.168.1.42,fe80::1:5540 (D=3840, CM=1, VP=65521+32769)"
	if got != want {
		t.Errorf("String() = %q, want %q", got, want)
	}
}

func TestDeviceStringNoIPs(t *testing.T) {
	d := &Device{
		Name: "empty-device",
		Port: 5540,
	}

	got := d.String()
	want := "empty-device @ :5540 (D=0, CM=0, VP=0+0)"
	if got != want {
		t.Errorf("String() = %q, want %q", got, want)
	}
}
