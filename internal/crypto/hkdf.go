// Copyright 2026 matter-cli contributors
// SPDX-License-Identifier: Apache-2.0

package crypto

import (
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"io"

	"golang.org/x/crypto/hkdf"
)

// HKDFExpandSHA256 performs HKDF-Expand using SHA-256, returning length bytes of
// output keying material. The prk must be a pseudorandom key from a prior extract step.
func HKDFExpandSHA256(prk, info []byte, length int) ([]byte, error) {
	r := hkdf.Expand(sha256.New, prk, info)
	out := make([]byte, length)
	if _, err := io.ReadFull(r, out); err != nil {
		return nil, fmt.Errorf("crypto: HKDF expand: %w", err)
	}
	return out, nil
}

// HKDFExtractSHA256 performs HKDF-Extract using SHA-256, returning a pseudorandom
// key suitable for use with HKDFExpandSHA256.
func HKDFExtractSHA256(salt, ikm []byte) []byte {
	return hkdf.Extract(sha256.New, ikm, salt)
}

// HKDFSHA256 performs the full HKDF (Extract-then-Expand) using SHA-256.
// It derives length bytes of keying material from the input keying material ikm,
// optional salt, and optional info.
func HKDFSHA256(ikm, salt, info []byte, length int) ([]byte, error) {
	r := hkdf.New(sha256.New, ikm, salt, info)
	out := make([]byte, length)
	if _, err := io.ReadFull(r, out); err != nil {
		return nil, fmt.Errorf("crypto: HKDF: %w", err)
	}
	return out, nil
}

// CompressedFabricID derives the 8-byte Compressed Fabric Identifier from the
// root CA public key and fabric ID, per Matter spec section 4.3.2.2:
//
//	CompressedFabricID = HKDF-SHA256(
//	    IKM  = rootPubKey[1:],       // uncompressed key minus 0x04 prefix
//	    Salt = fabricID (LE uint64),
//	    Info = "CompressedFabric",
//	    L    = 8)
func CompressedFabricID(rootPubKey []byte, fabricID uint64) ([]byte, error) {
	// Strip the 0x04 uncompressed prefix to get the raw X||Y (64 bytes).
	if len(rootPubKey) != 65 || rootPubKey[0] != 0x04 {
		return nil, fmt.Errorf("crypto: root public key must be 65-byte uncompressed SEC1")
	}
	ikm := rootPubKey[1:]

	// The Matter spec encodes fabricID as big-endian for this HKDF salt.
	var salt [8]byte
	binary.BigEndian.PutUint64(salt[:], fabricID)

	return HKDFSHA256(ikm, salt[:], []byte("CompressedFabric"), 8)
}

// DeriveGroupOperationalKey derives an operational group key from an epoch key
// and the compressed fabric ID, per Matter spec section 4.14.2.6.1:
//
//	OperationalKey = HKDF-SHA256(
//	    IKM  = epochKey,
//	    Salt = compressedFabricID (8 bytes),
//	    Info = "GroupKey v1.0",
//	    L    = 16)
//
// For the Identity Protection Key (IPK, key set 0), this produces the
// Operational Identity Protection Key used in CASE DestinationID computation.
func DeriveGroupOperationalKey(epochKey, compressedFabricID []byte) ([]byte, error) {
	return HKDFSHA256(epochKey, compressedFabricID, []byte("GroupKey v1.0"), 16)
}
