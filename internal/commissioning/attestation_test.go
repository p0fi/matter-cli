// Copyright 2026 matter-cli contributors
// SPDX-License-Identifier: Apache-2.0

package commissioning

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"math/big"
	"testing"
	"time"

	mattercrypto "github.com/p0fi/matter-cli/internal/crypto"
)

// generateTestCertChain creates a PAA -> PAI -> DAC chain for testing.
func generateTestCertChain(t *testing.T) (paaKey, paiKey, dacKey *ecdsa.PrivateKey, paaDER, paiDER, dacDER []byte) {
	t.Helper()

	now := time.Now()
	notAfter := now.Add(10 * 365 * 24 * time.Hour)

	// Generate PAA (root).
	paaKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generating PAA key: %v", err)
	}
	paaTemplate := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "Test PAA"},
		NotBefore:             now,
		NotAfter:              notAfter,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
		MaxPathLen:            1,
	}
	paaDER, err = x509.CreateCertificate(rand.Reader, paaTemplate, paaTemplate, &paaKey.PublicKey, paaKey)
	if err != nil {
		t.Fatalf("creating PAA cert: %v", err)
	}

	paaCert, _ := x509.ParseCertificate(paaDER)

	// Generate PAI (intermediate).
	paiKey, err = ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generating PAI key: %v", err)
	}
	paiTemplate := &x509.Certificate{
		SerialNumber:          big.NewInt(2),
		Subject:               pkix.Name{CommonName: "Test PAI"},
		NotBefore:             now,
		NotAfter:              notAfter,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
		MaxPathLen:            0,
		MaxPathLenZero:        true,
	}
	paiDER, err = x509.CreateCertificate(rand.Reader, paiTemplate, paaCert, &paiKey.PublicKey, paaKey)
	if err != nil {
		t.Fatalf("creating PAI cert: %v", err)
	}

	paiCert, _ := x509.ParseCertificate(paiDER)

	// Generate DAC (leaf).
	dacKey, err = ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generating DAC key: %v", err)
	}
	dacTemplate := &x509.Certificate{
		SerialNumber:          big.NewInt(3),
		Subject:               pkix.Name{CommonName: "Test DAC"},
		NotBefore:             now,
		NotAfter:              notAfter,
		KeyUsage:              x509.KeyUsageDigitalSignature,
		BasicConstraintsValid: true,
		IsCA:                  false,
	}
	dacDER, err = x509.CreateCertificate(rand.Reader, dacTemplate, paiCert, &dacKey.PublicKey, paiKey)
	if err != nil {
		t.Fatalf("creating DAC cert: %v", err)
	}

	return paaKey, paiKey, dacKey, paaDER, paiDER, dacDER
}

func TestDefaultAttestationValidator_ValidateDAC(t *testing.T) {
	_, _, _, paaDER, paiDER, dacDER := generateTestCertChain(t)

	v := &DefaultAttestationValidator{}

	chain := DACChain{
		DAC: dacDER,
		PAI: paiDER,
		PAA: paaDER,
	}

	_, err := v.ValidateDAC(chain)
	if err != nil {
		t.Fatalf("ValidateDAC: %v", err)
	}
}

func TestDefaultAttestationValidator_ValidateDAC_WrongPAA(t *testing.T) {
	_, _, _, _, paiDER, dacDER := generateTestCertChain(t)

	// Generate a different PAA.
	wrongKey, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	wrongPAATemplate := &x509.Certificate{
		SerialNumber:          big.NewInt(99),
		Subject:               pkix.Name{CommonName: "Wrong PAA"},
		NotBefore:             time.Now(),
		NotAfter:              time.Now().Add(10 * 365 * 24 * time.Hour),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
	}
	wrongPAADER, _ := x509.CreateCertificate(rand.Reader, wrongPAATemplate, wrongPAATemplate, &wrongKey.PublicKey, wrongKey)

	v := &DefaultAttestationValidator{}
	chain := DACChain{
		DAC: dacDER,
		PAI: paiDER,
		PAA: wrongPAADER,
	}

	_, err := v.ValidateDAC(chain)
	if err == nil {
		t.Error("expected error when PAA does not sign PAI")
	}
}

func TestDefaultAttestationValidator_ValidateDAC_TrustedRoots(t *testing.T) {
	_, _, _, paaDER, paiDER, dacDER := generateTestCertChain(t)
	paaCert, _ := x509.ParseCertificate(paaDER)

	v := &DefaultAttestationValidator{
		TrustedRoots: []*x509.Certificate{paaCert},
	}

	chain := DACChain{
		DAC: dacDER,
		PAI: paiDER,
		PAA: paaDER,
	}

	_, err := v.ValidateDAC(chain)
	if err != nil {
		t.Fatalf("ValidateDAC with trusted root: %v", err)
	}
}

func TestDefaultAttestationValidator_ValidateDAC_UntrustedRoot(t *testing.T) {
	_, _, _, paaDER, paiDER, dacDER := generateTestCertChain(t)

	// Create a different trusted root.
	otherKey, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	otherTemplate := &x509.Certificate{
		SerialNumber:          big.NewInt(100),
		Subject:               pkix.Name{CommonName: "Other Root"},
		NotBefore:             time.Now(),
		NotAfter:              time.Now().Add(10 * 365 * 24 * time.Hour),
		KeyUsage:              x509.KeyUsageCertSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
	}
	otherDER, _ := x509.CreateCertificate(rand.Reader, otherTemplate, otherTemplate, &otherKey.PublicKey, otherKey)
	otherCert, _ := x509.ParseCertificate(otherDER)

	v := &DefaultAttestationValidator{
		TrustedRoots: []*x509.Certificate{otherCert},
	}

	chain := DACChain{
		DAC: dacDER,
		PAI: paiDER,
		PAA: paaDER,
	}

	_, err := v.ValidateDAC(chain)
	if err == nil {
		t.Error("expected error when PAA is not in trusted roots")
	}
}

func TestDefaultAttestationValidator_ValidateAttestation(t *testing.T) {
	_, _, dacKey, _, _, dacDER := generateTestCertChain(t)

	v := &DefaultAttestationValidator{}

	elements := []byte("test attestation elements")
	challenge := []byte("0123456789ABCDEF")

	// Create the TBS data: elements || challenge.
	tbs := make([]byte, len(elements)+len(challenge))
	copy(tbs, elements)
	copy(tbs[len(elements):], challenge)

	signature, err := mattercrypto.SignECDSA(dacKey, tbs)
	if err != nil {
		t.Fatalf("signing attestation: %v", err)
	}

	info := AttestationInfo{
		Elements:  elements,
		Signature: signature,
		Nonce:     challenge,
	}

	if err := v.ValidateAttestation(info, dacDER, challenge); err != nil {
		t.Fatalf("ValidateAttestation: %v", err)
	}
}

func TestDefaultAttestationValidator_ValidateAttestation_BadSignature(t *testing.T) {
	_, _, _, _, _, dacDER := generateTestCertChain(t)

	v := &DefaultAttestationValidator{}

	info := AttestationInfo{
		Elements:  []byte("test elements"),
		Signature: []byte("bad signature"),
		Nonce:     []byte("0123456789ABCDEF"),
	}

	err := v.ValidateAttestation(info, dacDER, info.Nonce)
	if err == nil {
		t.Error("expected error for bad signature")
	}
}

func TestCertificateType(t *testing.T) {
	if CertTypeDAC != 0 {
		t.Errorf("CertTypeDAC: got %d, want 0", CertTypeDAC)
	}
	if CertTypePAI != 1 {
		t.Errorf("CertTypePAI: got %d, want 1", CertTypePAI)
	}
	if CertTypePAA != 2 {
		t.Errorf("CertTypePAA: got %d, want 2", CertTypePAA)
	}
}
