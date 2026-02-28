// Copyright 2026 matter-cli contributors
// SPDX-License-Identifier: Apache-2.0

package commissioning

import (
	"fmt"
	"strings"
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

func TestValidateThreadDataset(t *testing.T) {
	// Build a minimal valid dataset with all required TLV fields.
	validDataset := func() []byte {
		var ds []byte
		// Channel (type 0x00): 3 bytes value
		ds = append(ds, 0x00, 0x03, 0x00, 0x00, 0x0F)
		// PAN ID (type 0x01): 2 bytes
		ds = append(ds, 0x01, 0x02, 0xAB, 0xCD)
		// Extended PAN ID (type 0x02): 8 bytes
		ds = append(ds, 0x02, 0x08, 0xDE, 0xAD, 0x00, 0xBE, 0xEF, 0x00, 0xCA, 0xFE)
		// Network Name (type 0x03): 7 bytes "TestNet"
		ds = append(ds, 0x03, 0x07, 'T', 'e', 's', 't', 'N', 'e', 't')
		// PSKc (type 0x04): 16 bytes
		ds = append(ds, 0x04, 0x10)
		ds = append(ds, make([]byte, 16)...)
		// Network Key (type 0x05): 16 bytes
		ds = append(ds, 0x05, 0x10)
		ds = append(ds, make([]byte, 16)...)
		// Mesh-Local Prefix (type 0x07): 8 bytes
		ds = append(ds, 0x07, 0x08, 0xFD, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00)
		// Security Policy (type 0x0C): 3 bytes
		ds = append(ds, 0x0C, 0x03, 0x00, 0xF8, 0x00)
		// Active Timestamp (type 0x0E): 8 bytes
		ds = append(ds, 0x0E, 0x08, 0x00, 0x00, 0x00, 0x00, 0x00, 0x01, 0x00, 0x00)
		return ds
	}

	t.Run("valid dataset", func(t *testing.T) {
		if err := ValidateThreadDataset(validDataset()); err != nil {
			t.Errorf("valid dataset should pass: %v", err)
		}
	})

	t.Run("too short", func(t *testing.T) {
		err := ValidateThreadDataset(make([]byte, 16))
		if err == nil {
			t.Fatal("16-byte dataset should be rejected")
		}
		if !strings.Contains(err.Error(), "too short") {
			t.Errorf("error should mention 'too short', got: %v", err)
		}
	})

	t.Run("empty", func(t *testing.T) {
		err := ValidateThreadDataset(nil)
		if err == nil {
			t.Fatal("nil dataset should be rejected")
		}
	})

	t.Run("missing network key", func(t *testing.T) {
		// Build a dataset that is long enough but missing the Network Key TLV.
		var ds []byte
		ds = append(ds, 0x00, 0x03, 0x00, 0x00, 0x0F) // Channel
		ds = append(ds, 0x01, 0x02, 0xAB, 0xCD)         // PAN ID
		ds = append(ds, 0x02, 0x08, 0xDE, 0xAD, 0x00, 0xBE, 0xEF, 0x00, 0xCA, 0xFE) // ExtPAN
		ds = append(ds, 0x03, 0x07, 'T', 'e', 's', 't', 'N', 'e', 't')               // Name
		ds = append(ds, 0x04, 0x10)          // PSKc
		ds = append(ds, make([]byte, 16)...) // PSKc value
		// Skip Network Key (type 0x05)
		ds = append(ds, 0x07, 0x08, 0xFD, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00) // Mesh-Local
		ds = append(ds, 0x0C, 0x03, 0x00, 0xF8, 0x00)                                 // Security Policy
		ds = append(ds, 0x0E, 0x08, 0x00, 0x00, 0x00, 0x00, 0x00, 0x01, 0x00, 0x00)  // Timestamp

		err := ValidateThreadDataset(ds)
		if err == nil {
			t.Fatal("dataset without Network Key should be rejected")
		}
		if !strings.Contains(err.Error(), "Network Key") {
			t.Errorf("error should mention 'Network Key', got: %v", err)
		}
	})

	t.Run("missing multiple fields", func(t *testing.T) {
		// Long enough but only contains Channel and padding.
		ds := make([]byte, 60)
		ds[0] = 0x00 // Channel type
		ds[1] = 0x03 // Channel length
		// Rest is zero bytes which parse as Channel TLVs with length 0.

		err := ValidateThreadDataset(ds)
		if err == nil {
			t.Fatal("dataset missing most fields should be rejected")
		}
		if !strings.Contains(err.Error(), "missing required fields") {
			t.Errorf("error should mention 'missing required fields', got: %v", err)
		}
	})
}

func TestExtractExtendedPANID(t *testing.T) {
	t.Run("valid dataset", func(t *testing.T) {
		var ds []byte
		// Channel (type 0x00): 3 bytes
		ds = append(ds, 0x00, 0x03, 0x00, 0x00, 0x0F)
		// PAN ID (type 0x01): 2 bytes
		ds = append(ds, 0x01, 0x02, 0xAB, 0xCD)
		// Extended PAN ID (type 0x02): 8 bytes
		ds = append(ds, 0x02, 0x08, 0xDE, 0xAD, 0x00, 0xBE, 0xEF, 0x00, 0xCA, 0xFE)
		// Network Name (type 0x03): 7 bytes
		ds = append(ds, 0x03, 0x07, 'T', 'e', 's', 't', 'N', 'e', 't')

		got, err := ExtractExtendedPANID(ds)
		if err != nil {
			t.Fatalf("ExtractExtendedPANID: %v", err)
		}
		want := []byte{0xDE, 0xAD, 0x00, 0xBE, 0xEF, 0x00, 0xCA, 0xFE}
		if len(got) != len(want) {
			t.Fatalf("length: got %d, want %d", len(got), len(want))
		}
		for i := range want {
			if got[i] != want[i] {
				t.Errorf("byte %d: got 0x%02X, want 0x%02X", i, got[i], want[i])
			}
		}
	})

	t.Run("real dataset from ot-ctl", func(t *testing.T) {
		// This is the dataset from the failing commissioning log:
		// 000300001901028cef0208ed47ad8b290344c603084d79486f6d653634...
		ds, _ := hexDecode("000300001901028cef0208ed47ad8b290344c603084d79486f6d653634041028faa6e0518b624dd5af1f1ad4b45fc605108d0bc474b224d85c31021d53e404fac40708fd4881159eb900000c0402a0f8f80e080000000000010000")

		got, err := ExtractExtendedPANID(ds)
		if err != nil {
			t.Fatalf("ExtractExtendedPANID: %v", err)
		}
		want := []byte{0xed, 0x47, 0xad, 0x8b, 0x29, 0x03, 0x44, 0xc6}
		if len(got) != len(want) {
			t.Fatalf("length: got %d, want %d", len(got), len(want))
		}
		for i := range want {
			if got[i] != want[i] {
				t.Errorf("byte %d: got 0x%02X, want 0x%02X", i, got[i], want[i])
			}
		}
	})

	t.Run("missing extended PAN ID", func(t *testing.T) {
		var ds []byte
		// Channel only, no Extended PAN ID.
		ds = append(ds, 0x00, 0x03, 0x00, 0x00, 0x0F)
		ds = append(ds, 0x01, 0x02, 0xAB, 0xCD)

		_, err := ExtractExtendedPANID(ds)
		if err == nil {
			t.Fatal("expected error for dataset without Extended PAN ID")
		}
		if !strings.Contains(err.Error(), "does not contain") {
			t.Errorf("error should mention missing field, got: %v", err)
		}
	})

	t.Run("truncated TLV value", func(t *testing.T) {
		// Extended PAN ID header claims 8 bytes but only 4 remain.
		ds := []byte{0x02, 0x08, 0xDE, 0xAD, 0x00, 0xBE}

		_, err := ExtractExtendedPANID(ds)
		if err == nil {
			t.Fatal("expected error for truncated dataset")
		}
		if !strings.Contains(err.Error(), "truncated") {
			t.Errorf("error should mention truncation, got: %v", err)
		}
	})

	t.Run("empty dataset", func(t *testing.T) {
		_, err := ExtractExtendedPANID(nil)
		if err == nil {
			t.Fatal("expected error for empty dataset")
		}
	})

	t.Run("returns a copy", func(t *testing.T) {
		var ds []byte
		ds = append(ds, 0x02, 0x08, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08)

		got, err := ExtractExtendedPANID(ds)
		if err != nil {
			t.Fatalf("ExtractExtendedPANID: %v", err)
		}
		// Mutate the returned slice and verify the original is unchanged.
		got[0] = 0xFF
		if ds[2] == 0xFF {
			t.Error("ExtractExtendedPANID should return a copy, not a slice of the original")
		}
	})
}

// hexDecode is a test helper that decodes a hex string into bytes.
func hexDecode(s string) ([]byte, error) {
	if len(s)%2 != 0 {
		return nil, fmt.Errorf("odd-length hex string")
	}
	b := make([]byte, len(s)/2)
	for i := 0; i < len(s); i += 2 {
		hi := unhex(s[i])
		lo := unhex(s[i+1])
		if hi < 0 || lo < 0 {
			return nil, fmt.Errorf("invalid hex char at position %d", i)
		}
		b[i/2] = byte(hi<<4 | lo)
	}
	return b, nil
}

func unhex(c byte) int {
	switch {
	case c >= '0' && c <= '9':
		return int(c - '0')
	case c >= 'a' && c <= 'f':
		return int(c - 'a' + 10)
	case c >= 'A' && c <= 'F':
		return int(c - 'A' + 10)
	default:
		return -1
	}
}
