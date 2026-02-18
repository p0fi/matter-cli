// Copyright 2026 matter-cli contributors
// SPDX-License-Identifier: Apache-2.0

package matter

import (
	"testing"
)

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.FabricID != 1 {
		t.Errorf("FabricID = %d, want 1", cfg.FabricID)
	}
	if cfg.BindAddr != ":0" {
		t.Errorf("BindAddr = %q, want %q", cfg.BindAddr, ":0")
	}
	if cfg.StorePath != "" {
		t.Errorf("StorePath = %q, want empty", cfg.StorePath)
	}
}

func TestNewClientInMemory(t *testing.T) {
	client, err := NewClient(DefaultConfig())
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	defer client.Close()
}

func TestLookupCluster(t *testing.T) {
	tests := []struct {
		name    string
		wantID  uint32
		wantOK  bool
	}{
		{"on-off", 0x0006, true},
		{"level-control", 0x0008, true},
		{"color-control", 0x0300, true},
		{"door-lock", 0x0101, true},
		{"thermostat", 0x0201, true},
		{"window-covering", 0x0102, true},
		{"descriptor", 0x001D, true},
		{"nonexistent", 0, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			info, ok := LookupCluster(tt.name)
			if ok != tt.wantOK {
				t.Fatalf("LookupCluster(%q) ok = %v, want %v", tt.name, ok, tt.wantOK)
			}
			if ok && info.ID != tt.wantID {
				t.Errorf("LookupCluster(%q).ID = 0x%04X, want 0x%04X", tt.name, info.ID, tt.wantID)
			}
		})
	}
}

func TestLookupClusterByID(t *testing.T) {
	info, ok := LookupClusterByID(0x0006)
	if !ok {
		t.Fatal("LookupClusterByID(0x0006) not found")
	}
	if info.Name != "on-off" {
		t.Errorf("Name = %q, want %q", info.Name, "on-off")
	}
}

func TestAllClusters(t *testing.T) {
	all := AllClusters()
	if len(all) < 13 {
		t.Errorf("AllClusters returned %d clusters, want at least 13", len(all))
	}
}

func TestParseSetupCodeQR(t *testing.T) {
	// Known test QR code from the Matter spec test vectors.
	// MT:Y.K9042C00KA0648G00 decodes to:
	//   VID=0xFFF1, PID=0x8000, Passcode=20202021, Discriminator=3840
	payload, err := ParseSetupCode("MT:Y.K9042C00KA0648G00")
	if err != nil {
		t.Fatalf("ParseSetupCode QR: %v", err)
	}
	if payload.Passcode != 20202021 {
		t.Errorf("Passcode = %d, want 20202021", payload.Passcode)
	}
}

func TestParseSetupCodeManual(t *testing.T) {
	payload, err := ParseSetupCode("34970112332")
	if err != nil {
		t.Fatalf("ParseSetupCode Manual: %v", err)
	}
	if payload.Passcode != 20202021 {
		t.Errorf("Passcode = %d, want 20202021", payload.Passcode)
	}
}
