// Copyright 2026 matter-cli contributors
// SPDX-License-Identifier: Apache-2.0

package commissioning

import (
	"testing"
)

func TestNetworkTypeString(t *testing.T) {
	tests := []struct {
		nt   NetworkType
		want string
	}{
		{NetworkEthernet, "Ethernet"},
		{NetworkWiFi, "WiFi"},
		{NetworkThread, "Thread"},
		{NetworkType(99), "Unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			got := tt.nt.String()
			if got != tt.want {
				t.Errorf("NetworkType(%d).String() = %q, want %q", tt.nt, got, tt.want)
			}
		})
	}
}

func TestNewWiFiCredentials(t *testing.T) {
	creds := NewWiFiCredentials("TestSSID", "TestPassword")

	if creds.Type != NetworkWiFi {
		t.Errorf("Type: got %v, want WiFi", creds.Type)
	}
	if creds.WiFi == nil {
		t.Fatal("WiFi credentials should not be nil")
	}
	if creds.WiFi.SSID != "TestSSID" {
		t.Errorf("SSID: got %q, want %q", creds.WiFi.SSID, "TestSSID")
	}
	if creds.WiFi.Password != "TestPassword" {
		t.Errorf("Password: got %q, want %q", creds.WiFi.Password, "TestPassword")
	}
	if creds.Thread != nil {
		t.Error("Thread should be nil for WiFi credentials")
	}
}

func TestNewThreadCredentials(t *testing.T) {
	dataset := []byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08}
	creds := NewThreadCredentials(dataset)

	if creds.Type != NetworkThread {
		t.Errorf("Type: got %v, want Thread", creds.Type)
	}
	if creds.Thread == nil {
		t.Fatal("Thread credentials should not be nil")
	}
	if len(creds.Thread.OperationalDataset) != len(dataset) {
		t.Errorf("dataset length: got %d, want %d", len(creds.Thread.OperationalDataset), len(dataset))
	}
	for i, b := range creds.Thread.OperationalDataset {
		if b != dataset[i] {
			t.Errorf("dataset[%d]: got 0x%02X, want 0x%02X", i, b, dataset[i])
		}
	}
	if creds.WiFi != nil {
		t.Error("WiFi should be nil for Thread credentials")
	}
}

func TestNewEthernetCredentials(t *testing.T) {
	creds := NewEthernetCredentials()

	if creds.Type != NetworkEthernet {
		t.Errorf("Type: got %v, want Ethernet", creds.Type)
	}
	if creds.WiFi != nil {
		t.Error("WiFi should be nil for Ethernet credentials")
	}
	if creds.Thread != nil {
		t.Error("Thread should be nil for Ethernet credentials")
	}
}
