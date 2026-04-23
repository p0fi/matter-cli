// Copyright 2026 matter-cli contributors
// SPDX-License-Identifier: Apache-2.0

package crypto

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/rand"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/asn1"
	"fmt"
	"io"
	"math/big"
	"strconv"
	"time"

	"github.com/p0fi/matter-cli/internal/tlv"
)

// Matter TLV certificate field tags (from Matter spec section 6.6).
const (
	certTagSerialNumber  = 1
	certTagSigAlgo       = 2
	certTagIssuer        = 3
	certTagNotBefore     = 4
	certTagNotAfter      = 5
	certTagSubject       = 6
	certTagPubKeyAlgo    = 7
	certTagECCurveID     = 8
	certTagECPubKey      = 9
	certTagExtensions    = 10
	certTagSignature     = 11
)

// Matter DN attribute tags.
const (
	dnTagNodeID   = 17 // 1.3.6.1.4.1.37244.1.1
	dnTagICACID   = 19 // 1.3.6.1.4.1.37244.1.3
	dnTagRCACID   = 20 // 1.3.6.1.4.1.37244.1.4
	dnTagFabricID = 21 // 1.3.6.1.4.1.37244.1.5
)

// Matter extension tags.
const (
	extTagBasicConstraints  = 1
	extTagKeyUsage          = 2
	extTagExtendedKeyUsage  = 3
	extTagSubjectKeyID      = 4
	extTagAuthorityKeyID    = 5
)

// Matter algorithm constants.
const (
	sigAlgoECDSASHA256 = 1
	pubKeyAlgoEC       = 1
	ecCurveP256        = 1
)

// matterEpoch is the Unix timestamp of the Matter epoch: 2000-01-01 00:00:00 UTC.
const matterEpoch int64 = 946684800

// oidToMatterDNTag maps Matter-specific X.509 OIDs to TLV DN tags.
var oidToMatterDNTag = map[string]uint8{
	oidMatterNodeID.String():   dnTagNodeID,
	oidMatterICAC.String():     dnTagICACID,
	oidMatterRCAC.String():     dnTagRCACID,
	oidMatterFabricID.String(): dnTagFabricID,
}

// X509ToMatterCert converts a DER-encoded X.509 certificate to Matter TLV
// certificate format as required by the Matter specification (section 6.6).
// The resulting TLV cert preserves the original X.509 DER signature. This is
// the correct function for device-bound certs (AddNOC, AddTrustedRootCertificate,
// CASE handshake) because the CHIP SDK verifies signatures by reconstructing
// X.509 DER TBS from the decoded TLV fields, not by hashing the TLV TBS directly.
func X509ToMatterCert(certDER []byte) ([]byte, error) {
	cert, err := x509.ParseCertificate(certDER)
	if err != nil {
		return nil, fmt.Errorf("crypto: parsing X.509 cert: %w", err)
	}

	w := tlv.NewWriter()
	if err := w.StartStructure(tlv.AnonymousTag()); err != nil {
		return nil, err
	}
	if err := writeMatterCertTBS(w, cert); err != nil {
		return nil, err
	}

	// Tag 11: Signature (raw r||s, 64 bytes) — from the X.509 DER signature.
	rawSig, err := derSignatureToRaw(cert.Signature)
	if err != nil {
		return nil, fmt.Errorf("converting signature: %w", err)
	}
	if err := w.PutOctetString(tlv.ContextTag(certTagSignature), rawSig); err != nil {
		return nil, err
	}

	if err := w.EndContainer(); err != nil {
		return nil, err
	}

	return w.Bytes(), nil
}

// X509ToMatterCertResigned converts a DER-encoded X.509 certificate to Matter
// TLV format and re-signs the TLV TBS (to-be-signed) portion with the provided
// signing key. This produces a TLV certificate whose signature is valid when
// verified over the TLV encoding, as required by Matter devices.
//
// signingKey is the key that signed this certificate:
//   - For RCAC (self-signed): pass the RCAC's own private key
//   - For ICAC: pass the RCAC's private key
//   - For NOC: pass the ICAC's private key
func X509ToMatterCertResigned(certDER []byte, signingKey *ecdsa.PrivateKey) ([]byte, error) {
	cert, err := x509.ParseCertificate(certDER)
	if err != nil {
		return nil, fmt.Errorf("crypto: parsing X.509 cert: %w", err)
	}

	// 1. Build the TBS structure (everything except the signature field).
	tbsWriter := tlv.NewWriter()
	if err := tbsWriter.StartStructure(tlv.AnonymousTag()); err != nil {
		return nil, err
	}
	if err := writeMatterCertTBS(tbsWriter, cert); err != nil {
		return nil, err
	}
	if err := tbsWriter.EndContainer(); err != nil {
		return nil, err
	}
	tbsBytes := tbsWriter.Bytes()

	// 2. Sign the TBS with ECDSA-SHA256.
	hash := sha256.Sum256(tbsBytes)
	r, s, err := ecdsa.Sign(rand.Reader, signingKey, hash[:])
	if err != nil {
		return nil, fmt.Errorf("crypto: signing TLV TBS: %w", err)
	}
	rawSig := make([]byte, 64)
	rBytes := r.Bytes()
	sBytes := s.Bytes()
	copy(rawSig[32-len(rBytes):32], rBytes)
	copy(rawSig[64-len(sBytes):64], sBytes)

	// 3. Build the complete cert: TBS fields + signature.
	w := tlv.NewWriter()
	if err := w.StartStructure(tlv.AnonymousTag()); err != nil {
		return nil, err
	}
	if err := writeMatterCertTBS(w, cert); err != nil {
		return nil, err
	}
	if err := w.PutOctetString(tlv.ContextTag(certTagSignature), rawSig); err != nil {
		return nil, err
	}
	if err := w.EndContainer(); err != nil {
		return nil, err
	}

	return w.Bytes(), nil
}

// writeMatterCertTBS writes all Matter TLV certificate fields except the
// signature (tags 1–10) into the given writer. The caller is responsible for
// starting and ending the structure container.
func writeMatterCertTBS(w *tlv.Writer, cert *x509.Certificate) error {
	// Tag 1: SerialNumber — preserve the DER INTEGER content encoding.
	// big.Int.Bytes() strips leading zeros, but the CHIP SDK's TLV-to-DER
	// converter writes TLV bytes verbatim as INTEGER content. If the MSB is
	// set, DER requires a leading 0x00 to keep the integer positive.
	serialBytes := cert.SerialNumber.Bytes()
	if len(serialBytes) > 0 && serialBytes[0]&0x80 != 0 {
		serialBytes = append([]byte{0x00}, serialBytes...)
	}
	if err := w.PutOctetString(tlv.ContextTag(certTagSerialNumber), serialBytes); err != nil {
		return err
	}
	// Tag 2: Signature Algorithm = ECDSA-SHA256
	if err := w.PutUnsignedInt(tlv.ContextTag(certTagSigAlgo), sigAlgoECDSASHA256); err != nil {
		return err
	}
	// Tag 3: Issuer DN
	if err := encodeMatterDN(w, certTagIssuer, cert.Issuer); err != nil {
		return fmt.Errorf("encoding issuer: %w", err)
	}
	// Tag 4: NotBefore
	if err := w.PutUnsignedInt(tlv.ContextTag(certTagNotBefore), uint64(toMatterTime(cert.NotBefore))); err != nil {
		return err
	}
	// Tag 5: NotAfter
	if err := w.PutUnsignedInt(tlv.ContextTag(certTagNotAfter), uint64(toMatterTime(cert.NotAfter))); err != nil {
		return err
	}
	// Tag 6: Subject DN
	if err := encodeMatterDN(w, certTagSubject, cert.Subject); err != nil {
		return fmt.Errorf("encoding subject: %w", err)
	}
	// Tag 7: Public Key Algorithm = EC
	if err := w.PutUnsignedInt(tlv.ContextTag(certTagPubKeyAlgo), pubKeyAlgoEC); err != nil {
		return err
	}
	// Tag 8: EC Curve = P-256
	if err := w.PutUnsignedInt(tlv.ContextTag(certTagECCurveID), ecCurveP256); err != nil {
		return err
	}
	// Tag 9: EC Public Key (uncompressed, 65 bytes)
	ecPub, ok := cert.PublicKey.(*ecdsa.PublicKey)
	if !ok {
		return fmt.Errorf("crypto: certificate public key is not ECDSA")
	}
	pubKeyBytes := PublicKeyToUncompressed(ecPub)
	if err := w.PutOctetString(tlv.ContextTag(certTagECPubKey), pubKeyBytes); err != nil {
		return err
	}
	// Tag 10: Extensions
	if err := encodeMatterExtensions(w, cert); err != nil {
		return fmt.Errorf("encoding extensions: %w", err)
	}
	return nil
}

// encodeMatterDN encodes a PKIX name as a Matter TLV DN list.
func encodeMatterDN(w *tlv.Writer, tag uint8, name pkix.Name) error {
	if err := w.StartList(tlv.ContextTag(tag)); err != nil {
		return err
	}

	// Process RDNs from the raw name to get Matter-specific OIDs.
	for _, rdn := range name.Names {
		oidStr := rdn.Type.String()
		dnTag, ok := oidToMatterDNTag[oidStr]
		if !ok {
			continue // skip non-Matter OIDs
		}

		// The value is a hex-encoded uint64 string like "000000000000000A".
		hexStr, ok := rdn.Value.(string)
		if !ok {
			return fmt.Errorf("DN value for OID %s is not a string", oidStr)
		}
		val, err := strconv.ParseUint(hexStr, 16, 64)
		if err != nil {
			return fmt.Errorf("parsing DN hex value %q: %w", hexStr, err)
		}
		if err := w.PutUnsignedInt(tlv.ContextTag(dnTag), val); err != nil {
			return err
		}
	}

	return w.EndContainer()
}

// encodeMatterExtensions encodes certificate extensions as Matter TLV.
func encodeMatterExtensions(w *tlv.Writer, cert *x509.Certificate) error {
	if err := w.StartList(tlv.ContextTag(certTagExtensions)); err != nil {
		return err
	}

	// BasicConstraints (tag 1): IsCA
	if cert.BasicConstraintsValid {
		if err := w.StartStructure(tlv.ContextTag(extTagBasicConstraints)); err != nil {
			return err
		}
		if err := w.PutBool(tlv.ContextTag(1), cert.IsCA); err != nil {
			return err
		}
		if cert.IsCA && cert.MaxPathLen >= 0 {
			if err := w.PutUnsignedInt(tlv.ContextTag(2), uint64(cert.MaxPathLen)); err != nil {
				return err
			}
		}
		if err := w.EndContainer(); err != nil {
			return err
		}
	}

	// KeyUsage (tag 2): bit flags
	if cert.KeyUsage != 0 {
		ku := convertKeyUsage(cert.KeyUsage)
		if err := w.PutUnsignedInt(tlv.ContextTag(extTagKeyUsage), uint64(ku)); err != nil {
			return err
		}
	}

	// ExtendedKeyUsage (tag 3): array of uint8 values
	// Matter TLV values: 1=serverAuth, 2=clientAuth
	if len(cert.ExtKeyUsage) > 0 {
		if err := w.StartArray(tlv.ContextTag(extTagExtendedKeyUsage)); err != nil {
			return err
		}
		for _, eku := range cert.ExtKeyUsage {
			var val uint8
			switch eku {
			case x509.ExtKeyUsageServerAuth:
				val = 1
			case x509.ExtKeyUsageClientAuth:
				val = 2
			default:
				continue
			}
			if err := w.PutUnsignedInt(tlv.AnonymousTag(), uint64(val)); err != nil {
				return err
			}
		}
		if err := w.EndContainer(); err != nil {
			return err
		}
	}

	// SubjectKeyIdentifier (tag 4)
	if len(cert.SubjectKeyId) > 0 {
		if err := w.PutOctetString(tlv.ContextTag(extTagSubjectKeyID), cert.SubjectKeyId); err != nil {
			return err
		}
	} else {
		// Compute from public key if not present.
		ecPub, ok := cert.PublicKey.(*ecdsa.PublicKey)
		if ok {
			skid := computeSKID(ecPub)
			if err := w.PutOctetString(tlv.ContextTag(extTagSubjectKeyID), skid); err != nil {
				return err
			}
		}
	}

	// AuthorityKeyIdentifier (tag 5) — only for non-self-signed certs
	if len(cert.AuthorityKeyId) > 0 {
		if err := w.PutOctetString(tlv.ContextTag(extTagAuthorityKeyID), cert.AuthorityKeyId); err != nil {
			return err
		}
	}

	return w.EndContainer()
}

// convertKeyUsage converts Go's x509.KeyUsage to Matter's uint16 key usage bits.
// Matter uses the same bit layout as RFC 5280 KeyUsage BIT STRING.
func convertKeyUsage(ku x509.KeyUsage) uint16 {
	var bits uint16
	if ku&x509.KeyUsageDigitalSignature != 0 {
		bits |= 0x0001
	}
	if ku&x509.KeyUsageContentCommitment != 0 {
		bits |= 0x0002
	}
	if ku&x509.KeyUsageKeyEncipherment != 0 {
		bits |= 0x0004
	}
	if ku&x509.KeyUsageDataEncipherment != 0 {
		bits |= 0x0008
	}
	if ku&x509.KeyUsageKeyAgreement != 0 {
		bits |= 0x0010
	}
	if ku&x509.KeyUsageCertSign != 0 {
		bits |= 0x0020
	}
	if ku&x509.KeyUsageCRLSign != 0 {
		bits |= 0x0040
	}
	return bits
}

// computeSKID computes a Subject Key Identifier as the SHA-1 hash of the
// uncompressed public key bytes.
func computeSKID(pub *ecdsa.PublicKey) []byte {
	raw := PublicKeyToUncompressed(pub)
	h := sha1.Sum(raw)
	return h[:]
}

// derSignatureToRaw converts an ASN.1 DER-encoded ECDSA signature to the
// raw r||s format (64 bytes) used by Matter TLV certificates.
func derSignatureToRaw(derSig []byte) ([]byte, error) {
	var sig struct {
		R, S *big.Int
	}
	if _, err := asn1.Unmarshal(derSig, &sig); err != nil {
		return nil, fmt.Errorf("parsing DER signature: %w", err)
	}

	raw := make([]byte, 64)
	rBytes := sig.R.Bytes()
	sBytes := sig.S.Bytes()
	copy(raw[32-len(rBytes):32], rBytes)
	copy(raw[64-len(sBytes):64], sBytes)
	return raw, nil
}

// toMatterTime converts a Go time.Time to Matter epoch seconds
// (seconds since 2000-01-01 00:00:00 UTC).
func toMatterTime(t time.Time) uint32 {
	unix := t.Unix()
	if unix < matterEpoch {
		return 0
	}
	return uint32(unix - matterEpoch)
}

// ExtractPublicKeyFromTLV extracts the ECDSA P-256 public key from a certificate
// that may be in either Matter TLV or X.509 DER format. It tries TLV first
// (looking for tag 9 / certTagECPubKey), then falls back to DER parsing.
func ExtractPublicKeyFromTLV(cert []byte) (*ecdsa.PublicKey, error) {
	// Try Matter TLV format first.
	if pub, err := extractPublicKeyTLV(cert); err == nil {
		return pub, nil
	}
	// Fall back to X.509 DER.
	return ExtractPublicKey(cert)
}

func extractPublicKeyTLV(tlvCert []byte) (*ecdsa.PublicKey, error) {
	// Quick sanity check: Matter TLV structures start with 0x15 (anonymous structure).
	// DER/X.509 starts with 0x30 (SEQUENCE). Reject obvious non-TLV data early.
	if len(tlvCert) == 0 || tlvCert[0] != 0x15 {
		return nil, fmt.Errorf("crypto: not a TLV certificate (first byte 0x%02x)", tlvCert[0])
	}
	r := tlv.NewReader(bytes.NewReader(tlvCert))
	for {
		if err := r.Next(); err != nil {
			if err == io.EOF {
				break
			}
			return nil, fmt.Errorf("crypto: reading TLV cert: %w", err)
		}
		tag := r.TagValue()
		if tag.TagNum == certTagECPubKey && tag.Type == tlv.TagContextSpecific {
			pubKeyBytes, ok := r.Value().([]byte)
			if !ok {
				return nil, fmt.Errorf("crypto: EC public key is not an octet string")
			}
			return PublicKeyFromUncompressed(pubKeyBytes)
		}
	}
	return nil, fmt.Errorf("crypto: EC public key tag not found in TLV certificate")
}
