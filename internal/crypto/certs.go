// Copyright 2026 matter-cli contributors
// SPDX-License-Identifier: Apache-2.0

package crypto

import (
	"crypto/ecdsa"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/asn1"
	"fmt"
	"math/big"
	"time"
)

// Matter OID arc: 1.3.6.1.4.1.37244.1.x
var (
	// oidMatterRCAC is the OID for the Matter Root CA Certificate subject field.
	oidMatterRCAC = asn1.ObjectIdentifier{1, 3, 6, 1, 4, 1, 37244, 1, 4}
	// oidMatterICAC is the OID for the Matter Intermediate CA Certificate subject field.
	oidMatterICAC = asn1.ObjectIdentifier{1, 3, 6, 1, 4, 1, 37244, 1, 3}
	// oidMatterNodeID is the OID for the Matter Node ID in NOC subject.
	oidMatterNodeID = asn1.ObjectIdentifier{1, 3, 6, 1, 4, 1, 37244, 1, 1}
	// oidMatterFabricID is the OID for the Matter Fabric ID in certificate subject.
	oidMatterFabricID = asn1.ObjectIdentifier{1, 3, 6, 1, 4, 1, 37244, 1, 5}

	// Standard X.509 extension OIDs for ExtraExtensions.
	oidBasicConstraints       = asn1.ObjectIdentifier{2, 5, 29, 19}
	oidKeyUsage               = asn1.ObjectIdentifier{2, 5, 29, 15}
	oidExtKeyUsage            = asn1.ObjectIdentifier{2, 5, 29, 37}
	oidSubjectKeyIdentifier   = asn1.ObjectIdentifier{2, 5, 29, 14}
	oidAuthorityKeyIdentifier = asn1.ObjectIdentifier{2, 5, 29, 35}

	// Extended Key Usage purpose OIDs.
	oidExtKeyUsageServerAuth = asn1.ObjectIdentifier{1, 3, 6, 1, 5, 5, 7, 3, 1}
	oidExtKeyUsageClientAuth = asn1.ObjectIdentifier{1, 3, 6, 1, 5, 5, 7, 3, 2}
)

// matterDNAttr creates an AttributeTypeAndValue that forces UTF8String encoding
// for the value. Go's default ASN.1 marshalling uses PrintableString for hex
// digit strings, but Matter implementations expect UTF8String (tag 0x0C).
func matterDNAttr(oid asn1.ObjectIdentifier, val uint64) pkix.AttributeTypeAndValue {
	s := fmt.Sprintf("%016X", val)
	return pkix.AttributeTypeAndValue{
		Type: oid,
		Value: asn1.RawValue{
			Tag:   asn1.TagUTF8String,
			Bytes: []byte(s),
		},
	}
}

// CertificateOptions holds parameters for Matter certificate generation.
type CertificateOptions struct {
	// SerialNumber for the certificate. If nil, a random one is generated.
	SerialNumber *big.Int
	// NotBefore is the start of the certificate validity period.
	NotBefore time.Time
	// NotAfter is the end of the certificate validity period.
	NotAfter time.Time
}

// DefaultCertificateOptions returns sane defaults: valid from now for 10 years.
func DefaultCertificateOptions() CertificateOptions {
	now := time.Now()
	return CertificateOptions{
		NotBefore: now,
		NotAfter:  now.Add(10 * 365 * 24 * time.Hour),
	}
}

// GenerateRCAC generates a self-signed Matter Root CA Certificate (RCAC).
// The rcacID is a hex-encoded identifier used in the subject.
// Returns the DER-encoded certificate.
func GenerateRCAC(key *ecdsa.PrivateKey, rcacID uint64, fabricID uint64, opts CertificateOptions) ([]byte, error) {
	serial, err := ensureSerial(opts.SerialNumber)
	if err != nil {
		return nil, err
	}

	subject := pkix.Name{
		ExtraNames: []pkix.AttributeTypeAndValue{
			matterDNAttr(oidMatterRCAC, rcacID),
			matterDNAttr(oidMatterFabricID, fabricID),
		},
	}

	skid := computeSKID(&key.PublicKey)

	// Build extensions in the canonical Matter order (BasicConstraints,
	// KeyUsage, SubjectKeyIdentifier, AuthorityKeyIdentifier) using
	// ExtraExtensions so Go doesn't reorder them.
	exts, err := buildMatterExtensions(true, 1, x509.KeyUsageCertSign|x509.KeyUsageCRLSign, skid, skid, nil)
	if err != nil {
		return nil, err
	}

	tmpl := &x509.Certificate{
		SerialNumber:    serial,
		Subject:         subject,
		NotBefore:       opts.NotBefore,
		NotAfter:        opts.NotAfter,
		ExtraExtensions: exts,
	}

	certDER, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		return nil, fmt.Errorf("crypto: create RCAC: %w", err)
	}
	return certDER, nil
}

// GenerateICAC generates a Matter Intermediate CA Certificate (ICAC) signed by the RCAC key.
// Returns the DER-encoded certificate.
func GenerateICAC(icacKey *ecdsa.PrivateKey, icacID uint64, fabricID uint64, rcacCertDER []byte, rcacKey *ecdsa.PrivateKey, opts CertificateOptions) ([]byte, error) {
	serial, err := ensureSerial(opts.SerialNumber)
	if err != nil {
		return nil, err
	}

	rcacCert, err := x509.ParseCertificate(rcacCertDER)
	if err != nil {
		return nil, fmt.Errorf("crypto: parse RCAC for ICAC signing: %w", err)
	}

	subject := pkix.Name{
		ExtraNames: []pkix.AttributeTypeAndValue{
			matterDNAttr(oidMatterICAC, icacID),
			matterDNAttr(oidMatterFabricID, fabricID),
		},
	}

	skid := computeSKID(&icacKey.PublicKey)
	akid := rcacCert.SubjectKeyId

	exts, err := buildMatterExtensions(true, 0, x509.KeyUsageCertSign|x509.KeyUsageCRLSign, skid, akid, nil)
	if err != nil {
		return nil, err
	}

	tmpl := &x509.Certificate{
		SerialNumber:    serial,
		Subject:         subject,
		NotBefore:       opts.NotBefore,
		NotAfter:        opts.NotAfter,
		ExtraExtensions: exts,
	}

	certDER, err := x509.CreateCertificate(rand.Reader, tmpl, rcacCert, &icacKey.PublicKey, rcacKey)
	if err != nil {
		return nil, fmt.Errorf("crypto: create ICAC: %w", err)
	}
	return certDER, nil
}

// GenerateNOCForPublicKey generates a Matter Node Operational Certificate (NOC)
// for a given public key, signed by the CA key (either RCAC or ICAC).
// This is used when the device provides its public key via a CSR.
func GenerateNOCForPublicKey(nodePubKey *ecdsa.PublicKey, nodeID uint64, fabricID uint64, caCertDER []byte, caKey *ecdsa.PrivateKey, opts CertificateOptions) ([]byte, error) {
	serial, err := ensureSerial(opts.SerialNumber)
	if err != nil {
		return nil, err
	}

	caCert, err := x509.ParseCertificate(caCertDER)
	if err != nil {
		return nil, fmt.Errorf("crypto: parse CA cert for NOC signing: %w", err)
	}

	subject := pkix.Name{
		ExtraNames: []pkix.AttributeTypeAndValue{
			matterDNAttr(oidMatterNodeID, nodeID),
			matterDNAttr(oidMatterFabricID, fabricID),
		},
	}

	skid := computeSKID(nodePubKey)
	akid := caCert.SubjectKeyId

	nocEKU := []asn1.ObjectIdentifier{oidExtKeyUsageServerAuth, oidExtKeyUsageClientAuth}
	exts, err := buildMatterExtensions(false, -1, x509.KeyUsageDigitalSignature, skid, akid, nocEKU)
	if err != nil {
		return nil, err
	}

	tmpl := &x509.Certificate{
		SerialNumber:    serial,
		Subject:         subject,
		NotBefore:       opts.NotBefore,
		NotAfter:        opts.NotAfter,
		ExtraExtensions: exts,
	}

	certDER, err := x509.CreateCertificate(rand.Reader, tmpl, caCert, nodePubKey, caKey)
	if err != nil {
		return nil, fmt.Errorf("crypto: create NOC: %w", err)
	}
	return certDER, nil
}

// ParseCSR parses a DER-encoded PKCS#10 Certificate Signing Request and
// extracts the ECDSA P-256 public key from it.
func ParseCSR(csrDER []byte) (*ecdsa.PublicKey, error) {
	csr, err := x509.ParseCertificateRequest(csrDER)
	if err != nil {
		return nil, fmt.Errorf("crypto: parse CSR: %w", err)
	}
	pub, ok := csr.PublicKey.(*ecdsa.PublicKey)
	if !ok {
		return nil, fmt.Errorf("crypto: CSR key is not ECDSA")
	}
	return pub, nil
}

// GenerateNOC generates a Matter Node Operational Certificate (NOC) signed by the given
// CA key (either RCAC or ICAC).
// nodeID is the Matter node ID, fabricID is the fabric ID.
// Returns the DER-encoded certificate.
func GenerateNOC(nodeKey *ecdsa.PrivateKey, nodeID uint64, fabricID uint64, caCertDER []byte, caKey *ecdsa.PrivateKey, opts CertificateOptions) ([]byte, error) {
	serial, err := ensureSerial(opts.SerialNumber)
	if err != nil {
		return nil, err
	}

	caCert, err := x509.ParseCertificate(caCertDER)
	if err != nil {
		return nil, fmt.Errorf("crypto: parse CA cert for NOC signing: %w", err)
	}

	subject := pkix.Name{
		ExtraNames: []pkix.AttributeTypeAndValue{
			matterDNAttr(oidMatterNodeID, nodeID),
			matterDNAttr(oidMatterFabricID, fabricID),
		},
	}

	skid := computeSKID(&nodeKey.PublicKey)
	akid := caCert.SubjectKeyId

	nocEKU := []asn1.ObjectIdentifier{oidExtKeyUsageServerAuth, oidExtKeyUsageClientAuth}
	exts, err := buildMatterExtensions(false, -1, x509.KeyUsageDigitalSignature, skid, akid, nocEKU)
	if err != nil {
		return nil, err
	}

	tmpl := &x509.Certificate{
		SerialNumber:    serial,
		Subject:         subject,
		NotBefore:       opts.NotBefore,
		NotAfter:        opts.NotAfter,
		ExtraExtensions: exts,
	}

	certDER, err := x509.CreateCertificate(rand.Reader, tmpl, caCert, &nodeKey.PublicKey, caKey)
	if err != nil {
		return nil, fmt.Errorf("crypto: create NOC: %w", err)
	}
	return certDER, nil
}

// ParseCertificate parses a DER-encoded X.509 certificate.
func ParseCertificate(der []byte) (*x509.Certificate, error) {
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		return nil, fmt.Errorf("crypto: parse certificate: %w", err)
	}
	return cert, nil
}

// VerifyCertificateSignature verifies that child was signed by parent's key.
func VerifyCertificateSignature(child, parent *x509.Certificate) error {
	if err := child.CheckSignatureFrom(parent); err != nil {
		return fmt.Errorf("crypto: verify certificate signature: %w", err)
	}
	return nil
}

// ExtractPublicKey extracts the ECDSA P-256 public key from a DER-encoded certificate.
func ExtractPublicKey(certDER []byte) (*ecdsa.PublicKey, error) {
	cert, err := x509.ParseCertificate(certDER)
	if err != nil {
		return nil, fmt.Errorf("crypto: parse certificate: %w", err)
	}
	pub, ok := cert.PublicKey.(*ecdsa.PublicKey)
	if !ok {
		return nil, fmt.Errorf("crypto: certificate key is not ECDSA")
	}
	return pub, nil
}

// buildMatterExtensions builds X.509 extensions in the canonical Matter order:
// BasicConstraints, KeyUsage, [ExtendedKeyUsage], SubjectKeyIdentifier, AuthorityKeyIdentifier.
// pathLen < 0 means no pathLen constraint (for leaf certs).
// eku may be nil for CA certs; for NOCs it should contain serverAuth and clientAuth OIDs.
func buildMatterExtensions(isCA bool, pathLen int, ku x509.KeyUsage, skid, akid []byte, eku []asn1.ObjectIdentifier) ([]pkix.Extension, error) {
	var exts []pkix.Extension

	// 1. BasicConstraints
	var bcVal []byte
	var err error
	if isCA {
		if pathLen >= 0 {
			bcVal, err = asn1.Marshal(struct {
				IsCA       bool `asn1:"optional"`
				MaxPathLen int  `asn1:"optional"`
			}{IsCA: true, MaxPathLen: pathLen})
		} else {
			bcVal, err = asn1.Marshal(struct {
				IsCA bool `asn1:"optional"`
			}{IsCA: true})
		}
	} else {
		bcVal, err = asn1.Marshal(struct{}{}) // empty SEQUENCE
	}
	if err != nil {
		return nil, fmt.Errorf("encoding basic constraints: %w", err)
	}
	exts = append(exts, pkix.Extension{
		Id:       oidBasicConstraints,
		Critical: true,
		Value:    bcVal,
	})

	// 2. KeyUsage
	kuBits := keyUsageToBitString(ku)
	kuVal, err := asn1.Marshal(kuBits)
	if err != nil {
		return nil, fmt.Errorf("encoding key usage: %w", err)
	}
	exts = append(exts, pkix.Extension{
		Id:       oidKeyUsage,
		Critical: true,
		Value:    kuVal,
	})

	// 3. ExtendedKeyUsage (only for NOC leaf certs)
	if len(eku) > 0 {
		ekuVal, err := asn1.Marshal(eku)
		if err != nil {
			return nil, fmt.Errorf("encoding EKU: %w", err)
		}
		exts = append(exts, pkix.Extension{
			Id:       oidExtKeyUsage,
			Critical: true,
			Value:    ekuVal,
		})
	}

	// 4. SubjectKeyIdentifier
	skidVal, err := asn1.Marshal(skid)
	if err != nil {
		return nil, fmt.Errorf("encoding SKID: %w", err)
	}
	exts = append(exts, pkix.Extension{
		Id:    oidSubjectKeyIdentifier,
		Value: skidVal,
	})

	// 5. AuthorityKeyIdentifier
	if len(akid) > 0 {
		// AKID is a SEQUENCE { keyIdentifier [0] IMPLICIT OCTET STRING }
		akidVal, err := asn1.Marshal(struct {
			KeyIdentifier []byte `asn1:"optional,tag:0"`
		}{KeyIdentifier: akid})
		if err != nil {
			return nil, fmt.Errorf("encoding AKID: %w", err)
		}
		exts = append(exts, pkix.Extension{
			Id:    oidAuthorityKeyIdentifier,
			Value: akidVal,
		})
	}

	return exts, nil
}

// keyUsageToBitString converts x509.KeyUsage to an ASN.1 BIT STRING.
func keyUsageToBitString(ku x509.KeyUsage) asn1.BitString {
	var b byte
	if ku&x509.KeyUsageDigitalSignature != 0 {
		b |= 0x80
	}
	if ku&x509.KeyUsageContentCommitment != 0 {
		b |= 0x40
	}
	if ku&x509.KeyUsageKeyEncipherment != 0 {
		b |= 0x20
	}
	if ku&x509.KeyUsageDataEncipherment != 0 {
		b |= 0x10
	}
	if ku&x509.KeyUsageKeyAgreement != 0 {
		b |= 0x08
	}
	if ku&x509.KeyUsageCertSign != 0 {
		b |= 0x04
	}
	if ku&x509.KeyUsageCRLSign != 0 {
		b |= 0x02
	}
	if ku&x509.KeyUsageEncipherOnly != 0 {
		b |= 0x01
	}

	// Calculate padding bits
	padding := 0
	for i := 0; i < 8; i++ {
		if b&(1<<uint(i)) != 0 {
			break
		}
		padding++
	}

	return asn1.BitString{Bytes: []byte{b}, BitLength: 8 - padding}
}

// ensureSerial returns the provided serial or generates a random one.
func ensureSerial(serial *big.Int) (*big.Int, error) {
	if serial != nil {
		return serial, nil
	}
	s, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return nil, fmt.Errorf("crypto: generate serial number: %w", err)
	}
	return s, nil
}
