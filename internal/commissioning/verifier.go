// Copyright 2026 matter-cli contributors
// SPDX-License-Identifier: Apache-2.0

package commissioning

import (
	"crypto/rand"
	"encoding/binary"
	"fmt"

	"github.com/p0fi/matter-cli/internal/crypto"
)

// PAKE parameter constraints from Matter spec §6 (AdministratorCommissioning).
const (
	// PAKEVerifierSize is the byte length of the (w0 || L) PAKE passcode verifier.
	PAKEVerifierSize = 97
	// PAKEVerifierW0Size is the size in bytes of the w0 scalar portion of the verifier.
	PAKEVerifierW0Size = 32
	// PAKEVerifierLSize is the size in bytes of the uncompressed L point portion.
	PAKEVerifierLSize = 65

	// MinIterations is the minimum permitted PAKE iteration count (Crypto_PBKDFParameterSet).
	MinIterations = 1000
	// MaxIterations is the maximum permitted PAKE iteration count.
	MaxIterations = 100000

	// MinSaltLength is the minimum permitted PAKE salt length in bytes.
	MinSaltLength = 16
	// MaxSaltLength is the maximum permitted PAKE salt length in bytes.
	MaxSaltLength = 32

	// MaxPasscode is the largest permitted 27-bit setup passcode.
	MaxPasscode uint32 = 0x7FFFFFF
	// MaxDiscriminator is the largest permitted 12-bit discriminator.
	MaxDiscriminator uint16 = 0xFFF

	// MinCommissioningTimeoutSeconds is the minimum commissioning window timeout (spec: 180s).
	MinCommissioningTimeoutSeconds uint16 = 180
	// MaxCommissioningTimeoutSeconds is the maximum commissioning window timeout (spec: 900s).
	MaxCommissioningTimeoutSeconds uint16 = 900
)

// ComputePAKEVerifier derives the 97-byte (w0 || L) PAKE passcode verifier for
// an Enhanced Commissioning Method OpenCommissioningWindow invocation.
//
// The returned slice is the concatenation of the 32-byte w0 scalar (derived from
// the passcode via PBKDF2) and the 65-byte uncompressed L = w1 * P point.
func ComputePAKEVerifier(passcode uint32, salt []byte, iterations int) ([]byte, error) {
	if err := ValidatePasscode(passcode); err != nil {
		return nil, err
	}
	if len(salt) < MinSaltLength || len(salt) > MaxSaltLength {
		return nil, fmt.Errorf("commissioning: salt length %d outside [%d,%d]",
			len(salt), MinSaltLength, MaxSaltLength)
	}
	if iterations < MinIterations || iterations > MaxIterations {
		return nil, fmt.Errorf("commissioning: iterations %d outside [%d,%d]",
			iterations, MinIterations, MaxIterations)
	}

	w0, w1, err := crypto.DeriveSPAKE2PValues(passcode, salt, iterations)
	if err != nil {
		return nil, fmt.Errorf("deriving SPAKE2+ values: %w", err)
	}
	L := crypto.ComputeL(w1)

	if len(w0) != PAKEVerifierW0Size {
		return nil, fmt.Errorf("commissioning: unexpected w0 size %d, want %d", len(w0), PAKEVerifierW0Size)
	}
	if len(L) != PAKEVerifierLSize {
		return nil, fmt.Errorf("commissioning: unexpected L size %d, want %d", len(L), PAKEVerifierLSize)
	}

	verifier := make([]byte, 0, PAKEVerifierSize)
	verifier = append(verifier, w0...)
	verifier = append(verifier, L...)
	return verifier, nil
}

// ValidatePasscode reports whether a setup passcode is valid per the Matter
// spec. It rejects 0, values exceeding 27 bits, and the 11 disallowed trivial
// sequences listed in the spec.
func ValidatePasscode(passcode uint32) error {
	if passcode > MaxPasscode {
		return fmt.Errorf("commissioning: passcode %d exceeds 27 bits", passcode)
	}
	return validatePasscode(passcode)
}

// GenerateRandomPasscode returns a cryptographically random 27-bit passcode
// that passes ValidatePasscode. It retries until it lands on an allowed value.
func GenerateRandomPasscode() (uint32, error) {
	var buf [4]byte
	for i := 0; i < 16; i++ {
		if _, err := rand.Read(buf[:]); err != nil {
			return 0, fmt.Errorf("commissioning: reading random passcode: %w", err)
		}
		candidate := binary.BigEndian.Uint32(buf[:]) & MaxPasscode
		if ValidatePasscode(candidate) == nil {
			return candidate, nil
		}
	}
	return 0, fmt.Errorf("commissioning: failed to generate random passcode")
}

// GenerateRandomSalt returns a cryptographically random PAKE salt of the
// requested length. Length must be within [MinSaltLength, MaxSaltLength].
func GenerateRandomSalt(length int) ([]byte, error) {
	if length < MinSaltLength || length > MaxSaltLength {
		return nil, fmt.Errorf("commissioning: salt length %d outside [%d,%d]",
			length, MinSaltLength, MaxSaltLength)
	}
	salt := make([]byte, length)
	if _, err := rand.Read(salt); err != nil {
		return nil, fmt.Errorf("commissioning: reading random salt: %w", err)
	}
	return salt, nil
}

// GenerateRandomDiscriminator returns a cryptographically random 12-bit
// discriminator.
func GenerateRandomDiscriminator() (uint16, error) {
	var buf [2]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return 0, fmt.Errorf("commissioning: reading random discriminator: %w", err)
	}
	return binary.BigEndian.Uint16(buf[:]) & MaxDiscriminator, nil
}
