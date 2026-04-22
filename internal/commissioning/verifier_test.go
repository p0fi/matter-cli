// Copyright 2026 matter-cli contributors
// SPDX-License-Identifier: Apache-2.0

package commissioning

import (
	"bytes"
	"testing"

	"github.com/p0fi/matter-cli/internal/crypto"
)

func TestComputePAKEVerifier(t *testing.T) {
	const (
		passcode   = uint32(20202021)
		iterations = 1000
	)
	salt := []byte("SPAKE2P Key Salt")

	verifier, err := ComputePAKEVerifier(passcode, salt, iterations)
	if err != nil {
		t.Fatalf("ComputePAKEVerifier: %v", err)
	}

	if len(verifier) != PAKEVerifierSize {
		t.Fatalf("verifier length %d, want %d", len(verifier), PAKEVerifierSize)
	}

	// Cross-check against the primitive derivation to guard against regressions
	// in ComputePAKEVerifier's composition.
	w0, w1, err := crypto.DeriveSPAKE2PValues(passcode, salt, iterations)
	if err != nil {
		t.Fatalf("DeriveSPAKE2PValues: %v", err)
	}
	L := crypto.ComputeL(w1)

	if !bytes.Equal(verifier[:PAKEVerifierW0Size], w0) {
		t.Errorf("verifier[0:32] does not match w0")
	}
	if !bytes.Equal(verifier[PAKEVerifierW0Size:], L) {
		t.Errorf("verifier[32:97] does not match L")
	}

	// L must be an uncompressed SEC1 point: leading 0x04 and 64 bytes of X||Y.
	if verifier[PAKEVerifierW0Size] != 0x04 {
		t.Errorf("L should start with 0x04 (uncompressed point tag), got 0x%02X",
			verifier[PAKEVerifierW0Size])
	}
}

func TestComputePAKEVerifierDeterministic(t *testing.T) {
	salt := []byte("deterministic-salt-16b")
	a, err := ComputePAKEVerifier(11223344, salt, MinIterations)
	if err != nil {
		t.Fatalf("first call: %v", err)
	}
	b, err := ComputePAKEVerifier(11223344, salt, MinIterations)
	if err != nil {
		t.Fatalf("second call: %v", err)
	}
	if !bytes.Equal(a, b) {
		t.Errorf("verifier should be deterministic for the same inputs")
	}
}

func TestComputePAKEVerifierRejectsInvalidInputs(t *testing.T) {
	validSalt := make([]byte, MinSaltLength)

	cases := []struct {
		name       string
		passcode   uint32
		salt       []byte
		iterations int
	}{
		{"passcode zero", 0, validSalt, MinIterations},
		{"passcode disallowed", 11111111, validSalt, MinIterations},
		{"passcode exceeds 27 bits", MaxPasscode + 1, validSalt, MinIterations},
		{"salt too short", 12345, make([]byte, MinSaltLength-1), MinIterations},
		{"salt too long", 12345, make([]byte, MaxSaltLength+1), MinIterations},
		{"iterations too low", 12345, validSalt, MinIterations - 1},
		{"iterations too high", 12345, validSalt, MaxIterations + 1},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := ComputePAKEVerifier(tc.passcode, tc.salt, tc.iterations); err == nil {
				t.Errorf("expected error for %s, got nil", tc.name)
			}
		})
	}
}

func TestValidatePasscode(t *testing.T) {
	invalid := []uint32{
		0, 11111111, 22222222, 33333333, 44444444, 55555555,
		66666666, 77777777, 88888888, 99999999, 12345678, 87654321,
	}
	for _, p := range invalid {
		if err := ValidatePasscode(p); err == nil {
			t.Errorf("passcode %d should be invalid", p)
		}
	}
	valid := []uint32{1, 20202021, 33221100, 12345679}
	for _, p := range valid {
		if err := ValidatePasscode(p); err != nil {
			t.Errorf("passcode %d should be valid: %v", p, err)
		}
	}
	if err := ValidatePasscode(MaxPasscode + 1); err == nil {
		t.Errorf("passcode exceeding 27 bits should be rejected")
	}
}

func TestGenerateRandomPasscode(t *testing.T) {
	seen := make(map[uint32]struct{})
	for i := 0; i < 32; i++ {
		p, err := GenerateRandomPasscode()
		if err != nil {
			t.Fatalf("GenerateRandomPasscode: %v", err)
		}
		if err := ValidatePasscode(p); err != nil {
			t.Fatalf("generated passcode %d failed validation: %v", p, err)
		}
		if p > MaxPasscode {
			t.Fatalf("generated passcode %d exceeds 27 bits", p)
		}
		seen[p] = struct{}{}
	}
	// Extremely unlikely to collide on every call — guard against a stuck RNG.
	if len(seen) < 2 {
		t.Errorf("expected diverse random passcodes, got only %d unique", len(seen))
	}
}

func TestGenerateRandomSalt(t *testing.T) {
	for _, size := range []int{MinSaltLength, 20, MaxSaltLength} {
		salt, err := GenerateRandomSalt(size)
		if err != nil {
			t.Fatalf("GenerateRandomSalt(%d): %v", size, err)
		}
		if len(salt) != size {
			t.Errorf("salt length %d, want %d", len(salt), size)
		}
	}
	if _, err := GenerateRandomSalt(MinSaltLength - 1); err == nil {
		t.Errorf("expected error for salt size below minimum")
	}
	if _, err := GenerateRandomSalt(MaxSaltLength + 1); err == nil {
		t.Errorf("expected error for salt size above maximum")
	}
}

func TestGenerateRandomDiscriminator(t *testing.T) {
	for i := 0; i < 16; i++ {
		d, err := GenerateRandomDiscriminator()
		if err != nil {
			t.Fatalf("GenerateRandomDiscriminator: %v", err)
		}
		if d > MaxDiscriminator {
			t.Fatalf("discriminator %d exceeds 12 bits", d)
		}
	}
}
