// Copyright 2026 matter-cli contributors
// SPDX-License-Identifier: Apache-2.0

package commissioning

import (
	"crypto/x509"
	"fmt"

	"github.com/p0fi/matter-cli/internal/crypto"
)

// CertificateType identifies the type of certificate in the DAC chain.
type CertificateType int

const (
	// CertTypeDAC is a Device Attestation Certificate.
	CertTypeDAC CertificateType = iota
	// CertTypePAI is a Product Attestation Intermediate certificate.
	CertTypePAI
	// CertTypePAA is a Product Attestation Authority (root) certificate.
	CertTypePAA
)

// AttestationInfo holds the parsed attestation elements from a device.
type AttestationInfo struct {
	// Elements is the raw attestation elements TLV.
	Elements []byte
	// Signature is the attestation signature over the elements.
	Signature []byte
	// Nonce is the attestation nonce that was sent to the device.
	Nonce []byte
}

// DACChain holds the device attestation certificate chain.
type DACChain struct {
	// DAC is the DER-encoded Device Attestation Certificate.
	DAC []byte
	// PAI is the DER-encoded Product Attestation Intermediate certificate.
	PAI []byte
	// PAA is the DER-encoded Product Attestation Authority (root) certificate.
	PAA []byte
}

// DeviceInfo holds vendor/product information extracted from the DAC chain.
type DeviceInfo struct {
	// VendorID extracted from the DAC.
	VendorID uint16
	// ProductID extracted from the DAC.
	ProductID uint16
}

// AttestationValidator defines the interface for verifying device attestation.
type AttestationValidator interface {
	// ValidateDAC verifies the DAC chain (DAC -> PAI -> PAA) and returns
	// extracted device information.
	ValidateDAC(chain DACChain) (*DeviceInfo, error)

	// ValidateAttestation verifies the attestation elements signature using
	// the DAC's public key and the attestation challenge.
	ValidateAttestation(info AttestationInfo, dacDER []byte, challenge []byte) error
}

// DefaultAttestationValidator implements AttestationValidator with standard
// X.509 certificate chain validation.
type DefaultAttestationValidator struct {
	// TrustedRoots is a set of trusted PAA certificates. If nil, the PAA
	// from the DACChain itself is trusted (for development/testing).
	TrustedRoots []*x509.Certificate
}

// ValidateDAC verifies the DAC certificate chain.
func (v *DefaultAttestationValidator) ValidateDAC(chain DACChain) (*DeviceInfo, error) {
	dac, err := crypto.ParseCertificate(chain.DAC)
	if err != nil {
		return nil, fmt.Errorf("commissioning: parsing DAC: %w", err)
	}

	pai, err := crypto.ParseCertificate(chain.PAI)
	if err != nil {
		return nil, fmt.Errorf("commissioning: parsing PAI: %w", err)
	}

	paa, err := crypto.ParseCertificate(chain.PAA)
	if err != nil {
		return nil, fmt.Errorf("commissioning: parsing PAA: %w", err)
	}

	// Verify PAI was signed by PAA.
	if err := crypto.VerifyCertificateSignature(pai, paa); err != nil {
		return nil, fmt.Errorf("commissioning: PAI not signed by PAA: %w", err)
	}

	// Verify DAC was signed by PAI.
	if err := crypto.VerifyCertificateSignature(dac, pai); err != nil {
		return nil, fmt.Errorf("commissioning: DAC not signed by PAI: %w", err)
	}

	// If we have trusted roots, verify the PAA is trusted.
	if len(v.TrustedRoots) > 0 {
		trusted := false
		for _, root := range v.TrustedRoots {
			if paa.Equal(root) {
				trusted = true
				break
			}
		}
		if !trusted {
			return nil, fmt.Errorf("commissioning: PAA is not in the trusted root set")
		}
	}

	return &DeviceInfo{}, nil
}

// ValidateAttestation verifies the attestation elements using the DAC public key.
func (v *DefaultAttestationValidator) ValidateAttestation(info AttestationInfo, dacDER []byte, challenge []byte) error {
	pubKey, err := crypto.ExtractPublicKey(dacDER)
	if err != nil {
		return fmt.Errorf("commissioning: extracting DAC public key: %w", err)
	}

	// The signature is over (attestation_elements || attestation_challenge).
	tbs := make([]byte, len(info.Elements)+len(challenge))
	copy(tbs, info.Elements)
	copy(tbs[len(info.Elements):], challenge)

	if !crypto.VerifyECDSA(pubKey, tbs, info.Signature) {
		return fmt.Errorf("commissioning: attestation signature verification failed")
	}

	return nil
}
