// Copyright 2026 matter-cli contributors
// SPDX-License-Identifier: Apache-2.0

// Package crypto provides cryptographic primitives required by the Matter protocol,
// including SPAKE2+, AES-CCM, HKDF, PBKDF2, ECDH, ECDSA, and certificate operations.
package crypto

import (
	"crypto/ecdh"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"fmt"
	"math/big"
)

// P256 returns the NIST P-256 elliptic curve used throughout Matter.
func P256() elliptic.Curve {
	return elliptic.P256()
}

// GenerateKeyPair generates a new ECDSA P-256 key pair suitable for Matter operations.
func GenerateKeyPair() (*ecdsa.PrivateKey, error) {
	key, err := ecdsa.GenerateKey(P256(), rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("crypto: generate P-256 key: %w", err)
	}
	return key, nil
}

// PublicKeyToUncompressed returns the uncompressed SEC1 encoding of a P-256 public key (65 bytes: 0x04 || X || Y).
func PublicKeyToUncompressed(pub *ecdsa.PublicKey) []byte {
	return elliptic.Marshal(pub.Curve, pub.X, pub.Y)
}

// PublicKeyFromUncompressed parses an uncompressed SEC1-encoded P-256 public key.
func PublicKeyFromUncompressed(data []byte) (*ecdsa.PublicKey, error) {
	x, y := elliptic.Unmarshal(P256(), data)
	if x == nil {
		return nil, fmt.Errorf("crypto: invalid uncompressed P-256 point")
	}
	return &ecdsa.PublicKey{Curve: P256(), X: x, Y: y}, nil
}

// CompressPublicKey returns the compressed SEC1 encoding of a P-256 public key (33 bytes).
func CompressPublicKey(pub *ecdsa.PublicKey) []byte {
	return elliptic.MarshalCompressed(pub.Curve, pub.X, pub.Y)
}

// DecompressPublicKey parses a compressed SEC1-encoded P-256 public key.
func DecompressPublicKey(data []byte) (*ecdsa.PublicKey, error) {
	x, y := elliptic.UnmarshalCompressed(P256(), data)
	if x == nil {
		return nil, fmt.Errorf("crypto: invalid compressed P-256 point")
	}
	return &ecdsa.PublicKey{Curve: P256(), X: x, Y: y}, nil
}

// ECDH performs an Elliptic Curve Diffie-Hellman key exchange using P-256.
// It returns the raw shared secret (the x-coordinate of the shared point).
func ECDH(privKey *ecdsa.PrivateKey, pubKey *ecdsa.PublicKey) ([]byte, error) {
	priv, err := privKey.ECDH()
	if err != nil {
		return nil, fmt.Errorf("crypto: convert private key for ECDH: %w", err)
	}
	pub, err := pubKey.ECDH()
	if err != nil {
		return nil, fmt.Errorf("crypto: convert public key for ECDH: %w", err)
	}
	secret, err := priv.ECDH(pub)
	if err != nil {
		return nil, fmt.Errorf("crypto: ECDH exchange: %w", err)
	}
	return secret, nil
}

// ECDHFromBytes performs ECDH given a private key scalar (32 bytes) and a peer's
// uncompressed public key (65 bytes).
func ECDHFromBytes(privScalar, peerPubUncompressed []byte) ([]byte, error) {
	priv, err := ecdh.P256().NewPrivateKey(privScalar)
	if err != nil {
		return nil, fmt.Errorf("crypto: parse private scalar: %w", err)
	}
	// Convert uncompressed to the format ecdh expects (same as uncompressed).
	pub, err := ecdh.P256().NewPublicKey(peerPubUncompressed)
	if err != nil {
		return nil, fmt.Errorf("crypto: parse peer public key: %w", err)
	}
	secret, err := priv.ECDH(pub)
	if err != nil {
		return nil, fmt.Errorf("crypto: ECDH exchange: %w", err)
	}
	return secret, nil
}

// SignECDSA signs the SHA-256 hash of msg using the given P-256 private key.
// It returns the DER-encoded ASN.1 signature.
func SignECDSA(privKey *ecdsa.PrivateKey, msg []byte) ([]byte, error) {
	hash := sha256.Sum256(msg)
	sig, err := ecdsa.SignASN1(rand.Reader, privKey, hash[:])
	if err != nil {
		return nil, fmt.Errorf("crypto: ECDSA sign: %w", err)
	}
	return sig, nil
}

// VerifyECDSA verifies a DER-encoded ASN.1 ECDSA signature over the SHA-256 hash of msg.
func VerifyECDSA(pubKey *ecdsa.PublicKey, msg, sig []byte) bool {
	hash := sha256.Sum256(msg)
	return ecdsa.VerifyASN1(pubKey, hash[:], sig)
}

// SignECDSARaw signs the SHA-256 hash of msg using the given P-256 private key.
// It returns the signature in IEEE P1363 format (raw r||s, 64 bytes) as used by
// the Matter protocol for CASE signatures.
func SignECDSARaw(privKey *ecdsa.PrivateKey, msg []byte) ([]byte, error) {
	hash := sha256.Sum256(msg)
	r, s, err := ecdsa.Sign(rand.Reader, privKey, hash[:])
	if err != nil {
		return nil, fmt.Errorf("crypto: ECDSA sign: %w", err)
	}
	raw := make([]byte, 64)
	rBytes := r.Bytes()
	sBytes := s.Bytes()
	copy(raw[32-len(rBytes):32], rBytes)
	copy(raw[64-len(sBytes):64], sBytes)
	return raw, nil
}

// VerifyECDSARaw verifies an IEEE P1363 (raw r||s, 64 bytes) ECDSA signature
// over the SHA-256 hash of msg. This is the format used by the Matter protocol
// for CASE signatures.
func VerifyECDSARaw(pubKey *ecdsa.PublicKey, msg, sig []byte) bool {
	if len(sig) != 64 {
		return false
	}
	hash := sha256.Sum256(msg)
	r := new(big.Int).SetBytes(sig[:32])
	s := new(big.Int).SetBytes(sig[32:])
	return ecdsa.Verify(pubKey, hash[:], r, s)
}

// PrivateKeyToBytes returns the raw 32-byte scalar of a P-256 private key.
func PrivateKeyToBytes(privKey *ecdsa.PrivateKey) []byte {
	b := privKey.D.Bytes()
	// Pad to 32 bytes.
	if len(b) < 32 {
		padded := make([]byte, 32)
		copy(padded[32-len(b):], b)
		return padded
	}
	return b
}

// PrivateKeyFromBytes reconstructs a P-256 ECDSA private key from a 32-byte scalar.
func PrivateKeyFromBytes(scalar []byte) (*ecdsa.PrivateKey, error) {
	curve := P256()
	d := new(big.Int).SetBytes(scalar)
	if d.Sign() == 0 || d.Cmp(curve.Params().N) >= 0 {
		return nil, fmt.Errorf("crypto: private scalar out of range")
	}
	priv := new(ecdsa.PrivateKey)
	priv.PublicKey.Curve = curve
	priv.D = d
	priv.PublicKey.X, priv.PublicKey.Y = curve.ScalarBaseMult(scalar)
	return priv, nil
}

// MarshalPrivateKeyDER returns the PKCS#8 DER encoding of a P-256 private key.
func MarshalPrivateKeyDER(privKey *ecdsa.PrivateKey) ([]byte, error) {
	der, err := x509.MarshalPKCS8PrivateKey(privKey)
	if err != nil {
		return nil, fmt.Errorf("crypto: marshal private key DER: %w", err)
	}
	return der, nil
}

// ParsePrivateKeyDER parses a PKCS#8 DER-encoded P-256 private key.
func ParsePrivateKeyDER(der []byte) (*ecdsa.PrivateKey, error) {
	key, err := x509.ParsePKCS8PrivateKey(der)
	if err != nil {
		return nil, fmt.Errorf("crypto: parse private key DER: %w", err)
	}
	ecKey, ok := key.(*ecdsa.PrivateKey)
	if !ok {
		return nil, fmt.Errorf("crypto: DER key is not ECDSA")
	}
	if ecKey.Curve != P256() {
		return nil, fmt.Errorf("crypto: DER key is not P-256")
	}
	return ecKey, nil
}
