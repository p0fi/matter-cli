// Copyright 2026 matter-cli contributors
// SPDX-License-Identifier: Apache-2.0

// Package commissioning implements Matter device commissioning including
// setup payload parsing (QR code and Manual Pairing Code), device attestation,
// network provisioning types, and the commissioning flow orchestrator.
package commissioning

import (
	"fmt"
	"strings"
)

// Base38 character set used by Matter QR codes.
const base38Charset = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZ-."

// CommissioningFlow indicates the pairing mode required.
type CommissioningFlow uint8

const (
	// FlowStandard indicates the device can be commissioned with standard flow.
	FlowStandard CommissioningFlow = 0
	// FlowUserIntent indicates user-intent commissioning is required.
	FlowUserIntent CommissioningFlow = 1
	// FlowCustom indicates custom commissioning flow.
	FlowCustom CommissioningFlow = 2
)

// DiscoveryCapabilities is a bitmask of discovery methods supported by the device.
type DiscoveryCapabilities uint8

const (
	// DiscoverySoftAP indicates the device supports Wi-Fi SoftAP discovery.
	DiscoverySoftAP DiscoveryCapabilities = 1 << 0
	// DiscoveryBLE indicates the device supports BLE discovery.
	DiscoveryBLE DiscoveryCapabilities = 1 << 1
	// DiscoveryOnNetwork indicates the device supports on-network (IP) discovery.
	DiscoveryOnNetwork DiscoveryCapabilities = 1 << 2
)

// SetupPayload contains the information encoded in a Matter QR code or Manual Pairing Code.
type SetupPayload struct {
	// Version is the payload version (currently 0).
	Version uint8
	// VendorID is the vendor identifier.
	VendorID uint16
	// ProductID is the product identifier.
	ProductID uint16
	// CommissioningFlow indicates the pairing mode.
	CommissioningFlow CommissioningFlow
	// DiscoveryCapabilities is a bitmask of supported discovery methods.
	DiscoveryCapabilities DiscoveryCapabilities
	// Discriminator is the 12-bit device discriminator.
	Discriminator uint16
	// Passcode is the 27-bit setup passcode.
	Passcode uint32
}

// ShortDiscriminator returns the upper 4 bits of the 12-bit discriminator,
// as used in Manual Pairing Codes.
func (p *SetupPayload) ShortDiscriminator() uint8 {
	return uint8(p.Discriminator >> 8)
}

// QRCode returns the "MT:..." QR code string representation of the payload.
func (p *SetupPayload) QRCode() (string, error) {
	if p.Passcode > 0x7FFFFFF {
		return "", fmt.Errorf("commissioning: passcode %d exceeds 27 bits", p.Passcode)
	}
	if p.Discriminator > 0xFFF {
		return "", fmt.Errorf("commissioning: discriminator %d exceeds 12 bits", p.Discriminator)
	}

	// Pack 88 bits into 11 bytes (little-endian bit order):
	// Bit 0-2:   version (3 bits)
	// Bit 3-18:  VID (16 bits)
	// Bit 19-34: PID (16 bits)
	// Bit 35-36: commissioning flow (2 bits)
	// Bit 37-44: discovery capabilities (8 bits)
	// Bit 45-56: discriminator (12 bits)
	// Bit 57-83: passcode (27 bits)
	// Bit 84-87: padding (4 bits)
	bits := make([]byte, 11)

	writeBits(bits, 0, 3, uint64(p.Version))
	writeBits(bits, 3, 16, uint64(p.VendorID))
	writeBits(bits, 19, 16, uint64(p.ProductID))
	writeBits(bits, 35, 2, uint64(p.CommissioningFlow))
	writeBits(bits, 37, 8, uint64(p.DiscoveryCapabilities))
	writeBits(bits, 45, 12, uint64(p.Discriminator))
	writeBits(bits, 57, 27, uint64(p.Passcode))

	encoded := base38Encode(bits)
	return "MT:" + encoded, nil
}

// ParseQRCode parses a Matter QR code string ("MT:...") into a SetupPayload.
func ParseQRCode(code string) (*SetupPayload, error) {
	if !strings.HasPrefix(code, "MT:") {
		return nil, fmt.Errorf("commissioning: QR code must start with \"MT:\"")
	}

	encoded := code[3:]
	bits, err := base38Decode(encoded)
	if err != nil {
		return nil, fmt.Errorf("commissioning: decoding QR code base38: %w", err)
	}

	if len(bits) < 11 {
		padded := make([]byte, 11)
		copy(padded, bits)
		bits = padded
	}

	p := &SetupPayload{}
	p.Version = uint8(readBits(bits, 0, 3))
	p.VendorID = uint16(readBits(bits, 3, 16))
	p.ProductID = uint16(readBits(bits, 19, 16))
	p.CommissioningFlow = CommissioningFlow(readBits(bits, 35, 2))
	p.DiscoveryCapabilities = DiscoveryCapabilities(readBits(bits, 37, 8))
	p.Discriminator = uint16(readBits(bits, 45, 12))
	p.Passcode = uint32(readBits(bits, 57, 27))

	if err := validatePasscode(p.Passcode); err != nil {
		return nil, err
	}

	return p, nil
}

// ManualPairingCode returns the 11-digit (or 21-digit for custom flow) Manual Pairing Code.
func (p *SetupPayload) ManualPairingCode() (string, error) {
	if p.Passcode > 0x7FFFFFF {
		return "", fmt.Errorf("commissioning: passcode %d exceeds 27 bits", p.Passcode)
	}

	shortDisc := p.ShortDiscriminator()

	vidPIDPresent := uint8(0)
	if p.CommissioningFlow == FlowCustom {
		vidPIDPresent = 1
	}

	// Digit 1: (vidPIDPresent << 2) | (shortDiscriminator >> 2)
	digit1 := (vidPIDPresent << 2) | (shortDisc >> 2)

	// Digits 2-6: (shortDisc[1:0] << 14) | passcode[13:0]
	chunk2 := (uint32(shortDisc&0x03) << 14) | (p.Passcode & 0x3FFF)

	// Digits 7-10: passcode[26:14]
	chunk3 := (p.Passcode >> 14) & 0x1FFF

	code := fmt.Sprintf("%d%05d%04d", digit1, chunk2, chunk3)

	if vidPIDPresent == 1 {
		code += fmt.Sprintf("%05d%05d", p.VendorID, p.ProductID)
	}

	check := verhoeffGenerate(code)
	code += fmt.Sprintf("%d", check)

	return code, nil
}

// ParseManualPairingCode parses an 11 or 21-digit Manual Pairing Code into a SetupPayload.
func ParseManualPairingCode(code string) (*SetupPayload, error) {
	code = strings.ReplaceAll(code, "-", "")
	code = strings.ReplaceAll(code, " ", "")

	if len(code) != 11 && len(code) != 21 {
		return nil, fmt.Errorf("commissioning: manual pairing code must be 11 or 21 digits, got %d", len(code))
	}

	for i, c := range code {
		if c < '0' || c > '9' {
			return nil, fmt.Errorf("commissioning: invalid character %q at position %d", c, i)
		}
	}

	if !verhoeffValidate(code) {
		return nil, fmt.Errorf("commissioning: invalid check digit")
	}

	p := &SetupPayload{}

	digit1 := code[0] - '0'
	vidPIDPresent := (digit1 >> 2) & 0x01
	shortDiscHigh := digit1 & 0x03

	chunk2 := parseDecimal(code[1:6])
	chunk3 := parseDecimal(code[6:10])

	shortDiscLow := uint8((chunk2 >> 14) & 0x03)
	shortDisc := (shortDiscHigh << 2) | shortDiscLow

	passcode := (chunk3 << 14) | (chunk2 & 0x3FFF)

	p.Discriminator = uint16(shortDisc) << 8
	p.Passcode = passcode

	if vidPIDPresent == 1 {
		p.CommissioningFlow = FlowCustom
		p.VendorID = uint16(parseDecimal(code[10:15]))
		p.ProductID = uint16(parseDecimal(code[15:20]))
	}

	if err := validatePasscode(p.Passcode); err != nil {
		return nil, err
	}

	return p, nil
}

// validatePasscode checks whether a setup passcode is valid per the Matter spec.
func validatePasscode(passcode uint32) error {
	if passcode == 0 {
		return fmt.Errorf("commissioning: passcode must not be 0")
	}
	invalid := []uint32{
		11111111, 22222222, 33333333, 44444444,
		55555555, 66666666, 77777777, 88888888,
		99999999, 12345678, 87654321,
	}
	for _, inv := range invalid {
		if passcode == inv {
			return fmt.Errorf("commissioning: passcode %d is not allowed", passcode)
		}
	}
	return nil
}

// parseDecimal converts a decimal string to uint32.
func parseDecimal(s string) uint32 {
	var val uint32
	for _, c := range s {
		val = val*10 + uint32(c-'0')
	}
	return val
}

// writeBits writes a value into a bit buffer at the given bit offset.
// The buffer is treated as a little-endian bit stream.
func writeBits(buf []byte, offset, length int, value uint64) {
	for i := 0; i < length; i++ {
		bitIndex := offset + i
		byteIndex := bitIndex / 8
		bitPos := bitIndex % 8
		if value&(1<<uint(i)) != 0 {
			buf[byteIndex] |= 1 << uint(bitPos)
		}
	}
}

// readBits reads a value from a bit buffer at the given bit offset.
func readBits(buf []byte, offset, length int) uint64 {
	var value uint64
	for i := 0; i < length; i++ {
		bitIndex := offset + i
		byteIndex := bitIndex / 8
		bitPos := bitIndex % 8
		if buf[byteIndex]&(1<<uint(bitPos)) != 0 {
			value |= 1 << uint(i)
		}
	}
	return value
}

// base38Encode encodes bytes as a base38 string. The input is processed
// in 3-byte chunks, each producing 5 base38 characters.
func base38Encode(data []byte) string {
	var result []byte
	i := 0
	for i < len(data) {
		remaining := len(data) - i
		if remaining >= 3 {
			val := uint32(data[i]) | uint32(data[i+1])<<8 | uint32(data[i+2])<<16
			for j := 0; j < 5; j++ {
				result = append(result, base38Charset[val%38])
				val /= 38
			}
			i += 3
		} else if remaining == 2 {
			val := uint32(data[i]) | uint32(data[i+1])<<8
			for j := 0; j < 4; j++ {
				result = append(result, base38Charset[val%38])
				val /= 38
			}
			i += 2
		} else {
			val := uint32(data[i])
			for j := 0; j < 2; j++ {
				result = append(result, base38Charset[val%38])
				val /= 38
			}
			i++
		}
	}
	return string(result)
}

// base38Decode decodes a base38 string back to bytes.
func base38Decode(s string) ([]byte, error) {
	charMap := make(map[byte]uint32)
	for i, c := range []byte(base38Charset) {
		charMap[c] = uint32(i)
	}

	var result []byte
	i := 0
	for i < len(s) {
		remaining := len(s) - i
		if remaining >= 5 {
			var val uint32
			for j := 4; j >= 0; j-- {
				v, ok := charMap[s[i+j]]
				if !ok {
					return nil, fmt.Errorf("invalid base38 character %q", s[i+j])
				}
				val = val*38 + v
			}
			result = append(result, byte(val), byte(val>>8), byte(val>>16))
			i += 5
		} else if remaining >= 4 {
			var val uint32
			for j := 3; j >= 0; j-- {
				v, ok := charMap[s[i+j]]
				if !ok {
					return nil, fmt.Errorf("invalid base38 character %q", s[i+j])
				}
				val = val*38 + v
			}
			result = append(result, byte(val), byte(val>>8))
			i += 4
		} else if remaining >= 2 {
			var val uint32
			for j := 1; j >= 0; j-- {
				v, ok := charMap[s[i+j]]
				if !ok {
					return nil, fmt.Errorf("invalid base38 character %q", s[i+j])
				}
				val = val*38 + v
			}
			result = append(result, byte(val))
			i += 2
		} else {
			return nil, fmt.Errorf("invalid base38 string length")
		}
	}

	return result, nil
}

// Verhoeff algorithm tables.
var (
	verhoeffD = [10][10]int{
		{0, 1, 2, 3, 4, 5, 6, 7, 8, 9},
		{1, 2, 3, 4, 0, 6, 7, 8, 9, 5},
		{2, 3, 4, 0, 1, 7, 8, 9, 5, 6},
		{3, 4, 0, 1, 2, 8, 9, 5, 6, 7},
		{4, 0, 1, 2, 3, 9, 5, 6, 7, 8},
		{5, 9, 8, 7, 6, 0, 4, 3, 2, 1},
		{6, 5, 9, 8, 7, 1, 0, 4, 3, 2},
		{7, 6, 5, 9, 8, 2, 1, 0, 4, 3},
		{8, 7, 6, 5, 9, 3, 2, 1, 0, 4},
		{9, 8, 7, 6, 5, 4, 3, 2, 1, 0},
	}

	verhoeffInv = [10]int{0, 4, 3, 2, 1, 5, 6, 7, 8, 9}

	verhoeffP = [8][10]int{
		{0, 1, 2, 3, 4, 5, 6, 7, 8, 9},
		{1, 5, 7, 6, 2, 8, 3, 0, 9, 4},
		{5, 8, 0, 3, 7, 9, 6, 1, 4, 2},
		{8, 9, 1, 6, 0, 4, 3, 5, 2, 7},
		{9, 4, 5, 3, 1, 2, 6, 8, 7, 0},
		{4, 2, 8, 6, 5, 7, 3, 9, 0, 1},
		{2, 7, 9, 3, 8, 0, 6, 4, 1, 5},
		{7, 0, 4, 6, 9, 1, 3, 2, 5, 8},
	}
)

// verhoeffGenerate computes the Verhoeff check digit for a numeric string.
func verhoeffGenerate(s string) int {
	c := 0
	digits := reverseString(s)
	for i, ch := range digits {
		d := int(ch - '0')
		c = verhoeffD[c][verhoeffP[(i+1)%8][d]]
	}
	return verhoeffInv[c]
}

// verhoeffValidate checks whether a numeric string (including its check digit) is valid.
func verhoeffValidate(s string) bool {
	c := 0
	digits := reverseString(s)
	for i, ch := range digits {
		d := int(ch - '0')
		c = verhoeffD[c][verhoeffP[i%8][d]]
	}
	return c == 0
}

// reverseString reverses a string.
func reverseString(s string) string {
	runes := []rune(s)
	for i, j := 0, len(runes)-1; i < j; i, j = i+1, j-1 {
		runes[i], runes[j] = runes[j], runes[i]
	}
	return string(runes)
}
