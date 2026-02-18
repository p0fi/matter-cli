// Copyright 2026 matter-cli contributors
// SPDX-License-Identifier: Apache-2.0

// Package secure implements PASE (SPAKE2+ based) and CASE (SIGMA based)
// session establishment protocols for the Matter protocol.
package secure

import (
	"fmt"

	"github.com/p0fi/matter-cli/internal/crypto"
)

// SessionKeyLength is the length in bytes of an individual session key (AES-128).
const SessionKeyLength = 16

// AttestationChallengeLength is the length in bytes of the attestation challenge.
const AttestationChallengeLength = 16

// DerivedKeyMaterialLength is the total length of derived key material:
// I2RKey (16) + R2IKey (16) + AttestationChallenge (16) = 48.
const DerivedKeyMaterialLength = SessionKeyLength + SessionKeyLength + AttestationChallengeLength

// sessionKeysInfo is the HKDF info string used for session key derivation.
var sessionKeysInfo = []byte("SessionKeys")

// SessionKeys holds the symmetric keys derived from a session establishment.
type SessionKeys struct {
	// I2RKey is the Initiator-to-Responder encryption key (16 bytes).
	I2RKey []byte
	// R2IKey is the Responder-to-Initiator encryption key (16 bytes).
	R2IKey []byte
	// AttestationChallenge is the attestation challenge (16 bytes).
	AttestationChallenge []byte
}

// DeriveSessionKeys derives I2R, R2I, and AttestationChallenge keys from a shared
// secret using HKDF-SHA256 with no salt and info="SessionKeys".
func DeriveSessionKeys(sharedSecret []byte) (*SessionKeys, error) {
	if len(sharedSecret) == 0 {
		return nil, fmt.Errorf("secure: shared secret must not be empty")
	}

	keys, err := crypto.HKDFSHA256(sharedSecret, nil, sessionKeysInfo, DerivedKeyMaterialLength)
	if err != nil {
		return nil, fmt.Errorf("secure: deriving session keys: %w", err)
	}

	return &SessionKeys{
		I2RKey:               keys[0:SessionKeyLength],
		R2IKey:               keys[SessionKeyLength : 2*SessionKeyLength],
		AttestationChallenge: keys[2*SessionKeyLength : DerivedKeyMaterialLength],
	}, nil
}
