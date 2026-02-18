// Copyright 2026 matter-cli contributors
// SPDX-License-Identifier: Apache-2.0

package crypto

import (
	"crypto/sha256"
	"fmt"
	"math/big"

	"golang.org/x/crypto/pbkdf2"
)

// PBKDF2SHA256 derives key material from a password using PBKDF2 with SHA-256.
// The returned slice is keyLen bytes long.
func PBKDF2SHA256(password, salt []byte, iterations, keyLen int) []byte {
	return pbkdf2.Key(password, salt, iterations, keyLen, sha256.New)
}

// DeriveSPAKE2PValues derives the w0 and w1 scalars from a Matter passcode
// using PBKDF2 as specified in the Matter protocol.
// The passcode is encoded as a little-endian uint32.
// Returns w0 and w1 as 32-byte big-endian scalars reduced modulo the P-256 order.
func DeriveSPAKE2PValues(passcode uint32, salt []byte, iterations int) (w0, w1 []byte, err error) {
	if iterations < 1000 {
		return nil, nil, fmt.Errorf("crypto: PBKDF2 iterations too low: %d", iterations)
	}

	// Encode passcode as little-endian uint32.
	pw := make([]byte, 4)
	pw[0] = byte(passcode)
	pw[1] = byte(passcode >> 8)
	pw[2] = byte(passcode >> 16)
	pw[3] = byte(passcode >> 24)

	// Derive 80 bytes of key material (w0s || w1s, each 40 bytes).
	ws := PBKDF2SHA256(pw, salt, iterations, 80)

	// Split into two 40-byte values and reduce modulo the P-256 order.
	n := P256().Params().N

	w0Int := new(big.Int).SetBytes(ws[:40])
	w0Int.Mod(w0Int, n)

	w1Int := new(big.Int).SetBytes(ws[40:80])
	w1Int.Mod(w1Int, n)

	// Encode as zero-padded 32-byte big-endian.
	w0 = zeroPad(w0Int.Bytes(), 32)
	w1 = zeroPad(w1Int.Bytes(), 32)

	return w0, w1, nil
}

// zeroPad left-pads b with zeros to the given size.
func zeroPad(b []byte, size int) []byte {
	if len(b) >= size {
		return b
	}
	padded := make([]byte, size)
	copy(padded[size-len(b):], b)
	return padded
}
