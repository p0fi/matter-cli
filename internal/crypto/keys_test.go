// Copyright 2026 matter-cli contributors
// SPDX-License-Identifier: Apache-2.0

package crypto

import (
	"bytes"
	"crypto/ecdsa"
	"testing"
)

func TestGenerateKeyPair(t *testing.T) {
	key, err := GenerateKeyPair()
	if err != nil {
		t.Fatalf("GenerateKeyPair() error: %v", err)
	}
	if key.Curve != P256() {
		t.Fatal("key is not P-256")
	}
	if key.D.Sign() == 0 {
		t.Fatal("private scalar is zero")
	}
}

func TestPublicKeyRoundTrip(t *testing.T) {
	key, err := GenerateKeyPair()
	if err != nil {
		t.Fatalf("GenerateKeyPair() error: %v", err)
	}

	t.Run("Uncompressed", func(t *testing.T) {
		raw := PublicKeyToUncompressed(&key.PublicKey)
		if len(raw) != 65 {
			t.Fatalf("uncompressed key length = %d, want 65", len(raw))
		}
		if raw[0] != 0x04 {
			t.Fatalf("uncompressed key prefix = 0x%02x, want 0x04", raw[0])
		}
		pub, err := PublicKeyFromUncompressed(raw)
		if err != nil {
			t.Fatalf("PublicKeyFromUncompressed() error: %v", err)
		}
		if pub.X.Cmp(key.PublicKey.X) != 0 || pub.Y.Cmp(key.PublicKey.Y) != 0 {
			t.Fatal("round-trip mismatch")
		}
	})

	t.Run("Compressed", func(t *testing.T) {
		comp := CompressPublicKey(&key.PublicKey)
		if len(comp) != 33 {
			t.Fatalf("compressed key length = %d, want 33", len(comp))
		}
		pub, err := DecompressPublicKey(comp)
		if err != nil {
			t.Fatalf("DecompressPublicKey() error: %v", err)
		}
		if pub.X.Cmp(key.PublicKey.X) != 0 || pub.Y.Cmp(key.PublicKey.Y) != 0 {
			t.Fatal("round-trip mismatch")
		}
	})
}

func TestECDH(t *testing.T) {
	keyA, err := GenerateKeyPair()
	if err != nil {
		t.Fatalf("GenerateKeyPair() A error: %v", err)
	}
	keyB, err := GenerateKeyPair()
	if err != nil {
		t.Fatalf("GenerateKeyPair() B error: %v", err)
	}

	secretAB, err := ECDH(keyA, &keyB.PublicKey)
	if err != nil {
		t.Fatalf("ECDH(A, B) error: %v", err)
	}
	secretBA, err := ECDH(keyB, &keyA.PublicKey)
	if err != nil {
		t.Fatalf("ECDH(B, A) error: %v", err)
	}

	if !bytes.Equal(secretAB, secretBA) {
		t.Fatal("ECDH shared secrets do not match")
	}
	if len(secretAB) != 32 {
		t.Fatalf("ECDH secret length = %d, want 32", len(secretAB))
	}
}

func TestECDHFromBytes(t *testing.T) {
	keyA, err := GenerateKeyPair()
	if err != nil {
		t.Fatalf("GenerateKeyPair() A error: %v", err)
	}
	keyB, err := GenerateKeyPair()
	if err != nil {
		t.Fatalf("GenerateKeyPair() B error: %v", err)
	}

	secretAB, err := ECDHFromBytes(PrivateKeyToBytes(keyA), PublicKeyToUncompressed(&keyB.PublicKey))
	if err != nil {
		t.Fatalf("ECDHFromBytes(A, B) error: %v", err)
	}
	secretBA, err := ECDHFromBytes(PrivateKeyToBytes(keyB), PublicKeyToUncompressed(&keyA.PublicKey))
	if err != nil {
		t.Fatalf("ECDHFromBytes(B, A) error: %v", err)
	}

	if !bytes.Equal(secretAB, secretBA) {
		t.Fatal("ECDHFromBytes shared secrets do not match")
	}
}

func TestSignVerifyECDSA(t *testing.T) {
	key, err := GenerateKeyPair()
	if err != nil {
		t.Fatalf("GenerateKeyPair() error: %v", err)
	}

	msg := []byte("Matter protocol test message")
	sig, err := SignECDSA(key, msg)
	if err != nil {
		t.Fatalf("SignECDSA() error: %v", err)
	}

	if !VerifyECDSA(&key.PublicKey, msg, sig) {
		t.Fatal("VerifyECDSA() returned false for valid signature")
	}

	// Tamper with the message.
	tampered := append([]byte{}, msg...)
	tampered[0] ^= 0xff
	if VerifyECDSA(&key.PublicKey, tampered, sig) {
		t.Fatal("VerifyECDSA() returned true for tampered message")
	}

	// Wrong key.
	key2, _ := GenerateKeyPair()
	if VerifyECDSA(&key2.PublicKey, msg, sig) {
		t.Fatal("VerifyECDSA() returned true for wrong key")
	}
}

func TestPrivateKeyRoundTrip(t *testing.T) {
	t.Run("Bytes", func(t *testing.T) {
		key, err := GenerateKeyPair()
		if err != nil {
			t.Fatalf("GenerateKeyPair() error: %v", err)
		}
		scalar := PrivateKeyToBytes(key)
		if len(scalar) != 32 {
			t.Fatalf("scalar length = %d, want 32", len(scalar))
		}
		recovered, err := PrivateKeyFromBytes(scalar)
		if err != nil {
			t.Fatalf("PrivateKeyFromBytes() error: %v", err)
		}
		if recovered.D.Cmp(key.D) != 0 {
			t.Fatal("private scalar mismatch")
		}
	})

	t.Run("DER", func(t *testing.T) {
		key, err := GenerateKeyPair()
		if err != nil {
			t.Fatalf("GenerateKeyPair() error: %v", err)
		}
		der, err := MarshalPrivateKeyDER(key)
		if err != nil {
			t.Fatalf("MarshalPrivateKeyDER() error: %v", err)
		}
		recovered, err := ParsePrivateKeyDER(der)
		if err != nil {
			t.Fatalf("ParsePrivateKeyDER() error: %v", err)
		}
		if recovered.D.Cmp(key.D) != 0 {
			t.Fatal("private scalar mismatch after DER round-trip")
		}
	})
}

func TestPublicKeyFromUncompressed_Invalid(t *testing.T) {
	tests := []struct {
		name string
		data []byte
	}{
		{"nil", nil},
		{"empty", []byte{}},
		{"too short", []byte{0x04, 0x01, 0x02}},
		{"wrong prefix", make([]byte, 65)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := PublicKeyFromUncompressed(tt.data)
			if err == nil {
				t.Fatal("expected error for invalid input")
			}
		})
	}
}

func TestPrivateKeyFromBytes_Invalid(t *testing.T) {
	tests := []struct {
		name   string
		scalar []byte
	}{
		{"zero", make([]byte, 32)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := PrivateKeyFromBytes(tt.scalar)
			if err == nil {
				t.Fatal("expected error for invalid scalar")
			}
		})
	}
}

func TestECDH_Consistency(t *testing.T) {
	// Test that ECDH and ECDHFromBytes produce the same result.
	keyA, _ := GenerateKeyPair()
	keyB, _ := GenerateKeyPair()

	s1, err := ECDH(keyA, &keyB.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	s2, err := ECDHFromBytes(PrivateKeyToBytes(keyA), PublicKeyToUncompressed(&keyB.PublicKey))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(s1, s2) {
		t.Fatal("ECDH and ECDHFromBytes produce different results")
	}
}

func TestVerifyECDSA_NilInputs(t *testing.T) {
	key, _ := GenerateKeyPair()
	// Should not panic with nil/empty inputs.
	result := VerifyECDSA(&key.PublicKey, nil, nil)
	if result {
		t.Fatal("expected false for nil inputs")
	}
}

// Verify that key generation produces distinct keys.
func TestGenerateKeyPair_Uniqueness(t *testing.T) {
	keys := make([]*ecdsa.PrivateKey, 5)
	for i := range keys {
		var err error
		keys[i], err = GenerateKeyPair()
		if err != nil {
			t.Fatalf("GenerateKeyPair() #%d error: %v", i, err)
		}
	}
	for i := 0; i < len(keys); i++ {
		for j := i + 1; j < len(keys); j++ {
			if keys[i].D.Cmp(keys[j].D) == 0 {
				t.Fatalf("keys %d and %d are identical", i, j)
			}
		}
	}
}
