// Copyright 2026 matter-cli contributors
// SPDX-License-Identifier: Apache-2.0

package commissioning

import (
	"testing"
)

func TestBase38RoundTrip(t *testing.T) {
	tests := []struct {
		name string
		data []byte
	}{
		{"empty", []byte{}},
		{"single_byte", []byte{0x42}},
		{"two_bytes", []byte{0x12, 0x34}},
		{"three_bytes", []byte{0x12, 0x34, 0x56}},
		{"eleven_bytes", make([]byte, 11)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			encoded := base38Encode(tt.data)
			decoded, err := base38Decode(encoded)
			if err != nil {
				t.Fatalf("base38Decode(%q): %v", encoded, err)
			}
			if len(decoded) != len(tt.data) {
				t.Fatalf("length mismatch: got %d, want %d", len(decoded), len(tt.data))
			}
			for i := range decoded {
				if decoded[i] != tt.data[i] {
					t.Errorf("byte %d: got 0x%02X, want 0x%02X", i, decoded[i], tt.data[i])
				}
			}
		})
	}
}

func TestVerhoeff(t *testing.T) {
	tests := []struct {
		input string
		check int
	}{
		{"236", 3},
		{"12345", 1},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := verhoeffGenerate(tt.input)
			if got != tt.check {
				t.Errorf("verhoeffGenerate(%q) = %d, want %d", tt.input, got, tt.check)
			}

			full := tt.input + string(rune('0'+tt.check))
			if !verhoeffValidate(full) {
				t.Errorf("verhoeffValidate(%q) = false, want true", full)
			}
		})
	}
}

func TestVerhoeffValidateInvalid(t *testing.T) {
	if verhoeffValidate("2360") {
		t.Error("verhoeffValidate(\"2360\") = true, want false")
	}
}

func TestWriteReadBits(t *testing.T) {
	tests := []struct {
		name   string
		offset int
		length int
		value  uint64
	}{
		{"3_bits_at_0", 0, 3, 5},
		{"16_bits_at_3", 3, 16, 0xABCD},
		{"27_bits_at_57", 57, 27, 20202021},
		{"12_bits_at_45", 45, 12, 0xF00},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			buf := make([]byte, 11)
			writeBits(buf, tt.offset, tt.length, tt.value)
			got := readBits(buf, tt.offset, tt.length)
			if got != tt.value {
				t.Errorf("readBits after writeBits: got %d, want %d", got, tt.value)
			}
		})
	}
}

func TestQRCodeRoundTrip(t *testing.T) {
	tests := []struct {
		name    string
		payload SetupPayload
	}{
		{
			name: "standard_flow",
			payload: SetupPayload{
				Version:               0,
				VendorID:              0xFFF1,
				ProductID:             0x8001,
				CommissioningFlow:     FlowStandard,
				DiscoveryCapabilities: DiscoveryBLE | DiscoveryOnNetwork,
				Discriminator:         3840,
				Passcode:              20202021,
			},
		},
		{
			name: "custom_flow",
			payload: SetupPayload{
				Version:               0,
				VendorID:              0x1234,
				ProductID:             0x5678,
				CommissioningFlow:     FlowCustom,
				DiscoveryCapabilities: DiscoverySoftAP,
				Discriminator:         2048,
				Passcode:              12345679,
			},
		},
		{
			name: "zeros_except_passcode",
			payload: SetupPayload{
				Version:               0,
				VendorID:              0,
				ProductID:             0,
				CommissioningFlow:     FlowStandard,
				DiscoveryCapabilities: 0,
				Discriminator:         0,
				Passcode:              1,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			qr, err := tt.payload.QRCode()
			if err != nil {
				t.Fatalf("QRCode(): %v", err)
			}

			if qr[:3] != "MT:" {
				t.Errorf("QR code should start with MT:, got %q", qr[:3])
			}

			parsed, err := ParseQRCode(qr)
			if err != nil {
				t.Fatalf("ParseQRCode(%q): %v", qr, err)
			}

			if parsed.Version != tt.payload.Version {
				t.Errorf("Version: got %d, want %d", parsed.Version, tt.payload.Version)
			}
			if parsed.VendorID != tt.payload.VendorID {
				t.Errorf("VendorID: got 0x%04X, want 0x%04X", parsed.VendorID, tt.payload.VendorID)
			}
			if parsed.ProductID != tt.payload.ProductID {
				t.Errorf("ProductID: got 0x%04X, want 0x%04X", parsed.ProductID, tt.payload.ProductID)
			}
			if parsed.CommissioningFlow != tt.payload.CommissioningFlow {
				t.Errorf("Flow: got %d, want %d", parsed.CommissioningFlow, tt.payload.CommissioningFlow)
			}
			if parsed.DiscoveryCapabilities != tt.payload.DiscoveryCapabilities {
				t.Errorf("Capabilities: got %d, want %d", parsed.DiscoveryCapabilities, tt.payload.DiscoveryCapabilities)
			}
			if parsed.Discriminator != tt.payload.Discriminator {
				t.Errorf("Discriminator: got %d, want %d", parsed.Discriminator, tt.payload.Discriminator)
			}
			if parsed.Passcode != tt.payload.Passcode {
				t.Errorf("Passcode: got %d, want %d", parsed.Passcode, tt.payload.Passcode)
			}
		})
	}
}

func TestParseQRCodeInvalidPrefix(t *testing.T) {
	_, err := ParseQRCode("XX:ABC")
	if err == nil {
		t.Error("expected error for invalid prefix")
	}
}

func TestManualPairingCodeRoundTrip(t *testing.T) {
	tests := []struct {
		name    string
		payload SetupPayload
	}{
		{
			name: "standard_discriminator_3840",
			payload: SetupPayload{
				CommissioningFlow: FlowStandard,
				Discriminator:     3840, // short disc = 15 (0xF)
				Passcode:          20202021,
			},
		},
		{
			name: "small_passcode",
			payload: SetupPayload{
				CommissioningFlow: FlowStandard,
				Discriminator:     256, // short disc = 1
				Passcode:          1,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			code, err := tt.payload.ManualPairingCode()
			if err != nil {
				t.Fatalf("ManualPairingCode(): %v", err)
			}

			if len(code) != 11 {
				t.Errorf("code length: got %d, want 11", len(code))
			}

			parsed, err := ParseManualPairingCode(code)
			if err != nil {
				t.Fatalf("ParseManualPairingCode(%q): %v", code, err)
			}

			// Manual code only preserves short discriminator (upper 4 bits).
			expectedDisc := uint16(tt.payload.ShortDiscriminator()) << 8
			if parsed.Discriminator != expectedDisc {
				t.Errorf("Discriminator: got %d, want %d", parsed.Discriminator, expectedDisc)
			}
			if parsed.Passcode != tt.payload.Passcode {
				t.Errorf("Passcode: got %d, want %d", parsed.Passcode, tt.payload.Passcode)
			}
		})
	}
}

func TestManualPairingCodeWithDashes(t *testing.T) {
	payload := SetupPayload{
		CommissioningFlow: FlowStandard,
		Discriminator:     3840,
		Passcode:          20202021,
	}

	code, err := payload.ManualPairingCode()
	if err != nil {
		t.Fatalf("ManualPairingCode(): %v", err)
	}

	// Insert dashes and spaces.
	dashed := code[:4] + "-" + code[4:8] + " " + code[8:]
	parsed, err := ParseManualPairingCode(dashed)
	if err != nil {
		t.Fatalf("ParseManualPairingCode(%q): %v", dashed, err)
	}

	if parsed.Passcode != payload.Passcode {
		t.Errorf("Passcode: got %d, want %d", parsed.Passcode, payload.Passcode)
	}
}

func TestParseManualPairingCodeInvalidLength(t *testing.T) {
	_, err := ParseManualPairingCode("12345")
	if err == nil {
		t.Error("expected error for invalid length")
	}
}

func TestParseManualPairingCodeBadCheckDigit(t *testing.T) {
	// Generate a valid code and corrupt the last digit.
	payload := SetupPayload{
		CommissioningFlow: FlowStandard,
		Discriminator:     3840,
		Passcode:          20202021,
	}
	code, _ := payload.ManualPairingCode()

	lastDigit := code[len(code)-1]
	corrupted := code[:len(code)-1]
	if lastDigit == '0' {
		corrupted += "1"
	} else {
		corrupted += "0"
	}

	_, err := ParseManualPairingCode(corrupted)
	if err == nil {
		t.Error("expected error for invalid check digit")
	}
}

func TestValidatePasscodeInvalid(t *testing.T) {
	invalids := []uint32{0, 11111111, 22222222, 12345678, 87654321}
	for _, passcode := range invalids {
		err := validatePasscode(passcode)
		if err == nil {
			t.Errorf("validatePasscode(%d): expected error", passcode)
		}
	}
}

func TestValidatePasscodeValid(t *testing.T) {
	valids := []uint32{1, 20202021, 123456, 99999998}
	for _, passcode := range valids {
		err := validatePasscode(passcode)
		if err != nil {
			t.Errorf("validatePasscode(%d): unexpected error: %v", passcode, err)
		}
	}
}

func TestShortDiscriminator(t *testing.T) {
	tests := []struct {
		disc  uint16
		short uint8
	}{
		{0x0F00, 0x0F},
		{0x0000, 0x00},
		{0x0FFF, 0x0F},
		{0x0100, 0x01},
	}

	for _, tt := range tests {
		p := &SetupPayload{Discriminator: tt.disc}
		got := p.ShortDiscriminator()
		if got != tt.short {
			t.Errorf("ShortDiscriminator(%d) = %d, want %d", tt.disc, got, tt.short)
		}
	}
}

func TestManualPairingCodeCustomFlow(t *testing.T) {
	payload := SetupPayload{
		CommissioningFlow: FlowCustom,
		VendorID:          0xFFF1,
		ProductID:         0x8001,
		Discriminator:     3840,
		Passcode:          20202021,
	}

	code, err := payload.ManualPairingCode()
	if err != nil {
		t.Fatalf("ManualPairingCode(): %v", err)
	}

	if len(code) != 21 {
		t.Errorf("custom flow code length: got %d, want 21", len(code))
	}

	parsed, err := ParseManualPairingCode(code)
	if err != nil {
		t.Fatalf("ParseManualPairingCode(%q): %v", code, err)
	}

	if parsed.CommissioningFlow != FlowCustom {
		t.Errorf("Flow: got %d, want FlowCustom", parsed.CommissioningFlow)
	}
	if parsed.VendorID != payload.VendorID {
		t.Errorf("VendorID: got 0x%04X, want 0x%04X", parsed.VendorID, payload.VendorID)
	}
	if parsed.ProductID != payload.ProductID {
		t.Errorf("ProductID: got 0x%04X, want 0x%04X", parsed.ProductID, payload.ProductID)
	}
	if parsed.Passcode != payload.Passcode {
		t.Errorf("Passcode: got %d, want %d", parsed.Passcode, payload.Passcode)
	}
}
