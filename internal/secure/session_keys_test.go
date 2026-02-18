// Copyright 2026 matter-cli contributors
// SPDX-License-Identifier: Apache-2.0

package secure

import (
	"bytes"
	"testing"
)

func TestDeriveSessionKeys(t *testing.T) {
	tests := []struct {
		name         string
		sharedSecret []byte
		wantErr      bool
	}{
		{
			name:         "valid 16-byte secret",
			sharedSecret: bytes.Repeat([]byte{0x42}, 16),
			wantErr:      false,
		},
		{
			name:         "valid 32-byte secret",
			sharedSecret: bytes.Repeat([]byte{0xAB}, 32),
			wantErr:      false,
		},
		{
			name:         "single byte secret",
			sharedSecret: []byte{0x01},
			wantErr:      false,
		},
		{
			name:         "empty secret",
			sharedSecret: []byte{},
			wantErr:      true,
		},
		{
			name:         "nil secret",
			sharedSecret: nil,
			wantErr:      true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			keys, err := DeriveSessionKeys(tt.sharedSecret)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if len(keys.I2RKey) != SessionKeyLength {
				t.Errorf("I2RKey length = %d, want %d", len(keys.I2RKey), SessionKeyLength)
			}
			if len(keys.R2IKey) != SessionKeyLength {
				t.Errorf("R2IKey length = %d, want %d", len(keys.R2IKey), SessionKeyLength)
			}
			if len(keys.AttestationChallenge) != AttestationChallengeLength {
				t.Errorf("AttestationChallenge length = %d, want %d", len(keys.AttestationChallenge), AttestationChallengeLength)
			}

			allZero := true
			for _, b := range keys.I2RKey {
				if b != 0 {
					allZero = false
					break
				}
			}
			if allZero {
				t.Error("I2RKey is all zeros")
			}

			if bytes.Equal(keys.I2RKey, keys.R2IKey) {
				t.Error("I2RKey and R2IKey should be different")
			}
		})
	}
}

func TestDeriveSessionKeysDeterministic(t *testing.T) {
	secret := bytes.Repeat([]byte{0x55}, 16)

	keys1, err := DeriveSessionKeys(secret)
	if err != nil {
		t.Fatalf("first derivation failed: %v", err)
	}

	keys2, err := DeriveSessionKeys(secret)
	if err != nil {
		t.Fatalf("second derivation failed: %v", err)
	}

	if !bytes.Equal(keys1.I2RKey, keys2.I2RKey) {
		t.Error("I2RKey not deterministic")
	}
	if !bytes.Equal(keys1.R2IKey, keys2.R2IKey) {
		t.Error("R2IKey not deterministic")
	}
	if !bytes.Equal(keys1.AttestationChallenge, keys2.AttestationChallenge) {
		t.Error("AttestationChallenge not deterministic")
	}
}

func TestDeriveSessionKeysDifferentSecrets(t *testing.T) {
	keys1, err := DeriveSessionKeys([]byte("secret1-padding-to-16"))
	if err != nil {
		t.Fatalf("first derivation failed: %v", err)
	}

	keys2, err := DeriveSessionKeys([]byte("secret2-padding-to-16"))
	if err != nil {
		t.Fatalf("second derivation failed: %v", err)
	}

	if bytes.Equal(keys1.I2RKey, keys2.I2RKey) {
		t.Error("different secrets should produce different I2RKeys")
	}
	if bytes.Equal(keys1.R2IKey, keys2.R2IKey) {
		t.Error("different secrets should produce different R2IKeys")
	}
	if bytes.Equal(keys1.AttestationChallenge, keys2.AttestationChallenge) {
		t.Error("different secrets should produce different AttestationChallenges")
	}
}
