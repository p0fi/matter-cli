// Copyright 2026 matter-cli contributors
// SPDX-License-Identifier: Apache-2.0

package crypto

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"fmt"
	"math/big"
	"testing"

	"github.com/p0fi/matter-cli/internal/tlv"
)

func TestX509ToMatterCertResigned_RCAC(t *testing.T) {
	// Generate an RCAC and convert to Matter TLV.
	key, err := GenerateKeyPair()
	if err != nil {
		t.Fatal(err)
	}

	fabricID := uint64(1)
	rcacID := uint64(1)
	opts := DefaultCertificateOptions()
	rcacDER, err := GenerateRCAC(key, rcacID, fabricID, opts)
	if err != nil {
		t.Fatal(err)
	}

	rcacTLV, err := X509ToMatterCertResigned(rcacDER, key)
	if err != nil {
		t.Fatal(err)
	}

	t.Logf("RCAC TLV (%d bytes): %s", len(rcacTLV), hex.EncodeToString(rcacTLV))

	// Parse the TLV and verify the structure.
	r := tlv.NewReader(bytes.NewReader(rcacTLV))
	if err := r.Next(); err != nil {
		t.Fatalf("reading structure: %v", err)
	}
	if r.Type() != tlv.TypeStructure {
		t.Fatalf("expected structure, got %s", r.Type())
	}

	var foundTags []uint8
	var sigBytes []byte
	for {
		if err := r.Next(); err != nil {
			t.Fatalf("reading element: %v", err)
		}
		if r.Type() == tlv.TypeEndOfContainer {
			break
		}

		tagNum := uint8(r.TagValue().TagNum)
		foundTags = append(foundTags, tagNum)

		switch tagNum {
		case certTagSerialNumber:
			serial := r.Value().([]byte)
			t.Logf("  Tag %d (SerialNumber): %s (%d bytes)", tagNum, hex.EncodeToString(serial), len(serial))
			if len(serial) == 0 || len(serial) > 20 {
				t.Errorf("serial number has invalid length: %d", len(serial))
			}
		case certTagSigAlgo:
			val := r.Value().(uint64)
			t.Logf("  Tag %d (SigAlgo): %d", tagNum, val)
			if val != sigAlgoECDSASHA256 {
				t.Errorf("expected sig algo 1 (ECDSA-SHA256), got %d", val)
			}
		case certTagIssuer, certTagSubject:
			name := "Issuer"
			if tagNum == certTagSubject {
				name = "Subject"
			}
			t.Logf("  Tag %d (%s): List", tagNum, name)
			if r.Type() != tlv.TypeList {
				t.Errorf("expected List for %s, got %s", name, r.Type())
			}
			// Read DN entries.
			for {
				if err := r.Next(); err != nil {
					t.Fatal(err)
				}
				if r.Type() == tlv.TypeEndOfContainer {
					break
				}
				dnTag := r.TagValue().TagNum
				dnVal := r.Value().(uint64)
				t.Logf("    DN tag %d: %d (0x%016X)", dnTag, dnVal, dnVal)
			}
		case certTagNotBefore, certTagNotAfter:
			val := r.Value().(uint64)
			t.Logf("  Tag %d (NotBefore/After): %d", tagNum, val)
		case certTagPubKeyAlgo:
			val := r.Value().(uint64)
			t.Logf("  Tag %d (PubKeyAlgo): %d", tagNum, val)
			if val != pubKeyAlgoEC {
				t.Errorf("expected pub key algo 1, got %d", val)
			}
		case certTagECCurveID:
			val := r.Value().(uint64)
			t.Logf("  Tag %d (ECCurveID): %d", tagNum, val)
			if val != ecCurveP256 {
				t.Errorf("expected EC curve 1 (P-256), got %d", val)
			}
		case certTagECPubKey:
			pubKey := r.Value().([]byte)
			t.Logf("  Tag %d (ECPubKey): %d bytes, prefix=0x%02X", tagNum, len(pubKey), pubKey[0])
			if len(pubKey) != 65 {
				t.Errorf("expected 65-byte uncompressed key, got %d", len(pubKey))
			}
			if pubKey[0] != 0x04 {
				t.Errorf("expected 0x04 prefix, got 0x%02X", pubKey[0])
			}
		case certTagExtensions:
			t.Logf("  Tag %d (Extensions): List", tagNum)
			if r.Type() != tlv.TypeList {
				t.Errorf("expected List for extensions, got %s", r.Type())
			}
			readExtensions(t, r)
		case certTagSignature:
			sigBytes = r.Value().([]byte)
			t.Logf("  Tag %d (Signature): %d bytes", tagNum, len(sigBytes))
			if len(sigBytes) != 64 {
				t.Errorf("expected 64-byte signature, got %d", len(sigBytes))
			}
		default:
			t.Errorf("unexpected tag %d", tagNum)
		}
	}

	// Verify all tags 1-11 are present.
	expectedTags := []uint8{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11}
	if len(foundTags) != len(expectedTags) {
		t.Errorf("expected %d tags, got %d: %v", len(expectedTags), len(foundTags), foundTags)
	}
	for i, exp := range expectedTags {
		if i < len(foundTags) && foundTags[i] != exp {
			t.Errorf("tag[%d] = %d, want %d", i, foundTags[i], exp)
		}
	}

	// Verify the signature: construct TBS (tags 1-10) and verify ECDSA.
	cert, _ := x509.ParseCertificate(rcacDER)
	tbsWriter := tlv.NewWriter()
	_ = tbsWriter.StartStructure(tlv.AnonymousTag())
	_ = writeMatterCertTBS(tbsWriter, cert)
	_ = tbsWriter.EndContainer()
	tbsBytes := tbsWriter.Bytes()

	hash := sha256.Sum256(tbsBytes)

	rInt := new(big.Int).SetBytes(sigBytes[:32])
	sInt := new(big.Int).SetBytes(sigBytes[32:])
	if !ecdsa.Verify(&key.PublicKey, hash[:], rInt, sInt) {
		t.Error("RCAC TLV signature verification FAILED")
	} else {
		t.Log("RCAC TLV signature verification PASSED")
	}
}

func TestX509ToMatterCertResigned_ICAC(t *testing.T) {
	rootKey, err := GenerateKeyPair()
	if err != nil {
		t.Fatal(err)
	}

	fabricID := uint64(1)
	opts := DefaultCertificateOptions()

	rcacDER, err := GenerateRCAC(rootKey, 1, fabricID, opts)
	if err != nil {
		t.Fatal(err)
	}

	icacKey, err := GenerateKeyPair()
	if err != nil {
		t.Fatal(err)
	}

	icacDER, err := GenerateICAC(icacKey, 2, fabricID, rcacDER, rootKey, opts)
	if err != nil {
		t.Fatal(err)
	}

	icacTLV, err := X509ToMatterCertResigned(icacDER, rootKey)
	if err != nil {
		t.Fatal(err)
	}

	t.Logf("ICAC TLV (%d bytes): %s", len(icacTLV), hex.EncodeToString(icacTLV))

	// Verify the signature over TLV TBS.
	cert, _ := x509.ParseCertificate(icacDER)
	tbsWriter := tlv.NewWriter()
	_ = tbsWriter.StartStructure(tlv.AnonymousTag())
	_ = writeMatterCertTBS(tbsWriter, cert)
	_ = tbsWriter.EndContainer()
	tbsBytes := tbsWriter.Bytes()

	hash := sha256.Sum256(tbsBytes)

	// Parse signature from TLV cert.
	r := tlv.NewReader(bytes.NewReader(icacTLV))
	_ = r.Next() // structure
	var sigBytes []byte
	for {
		_ = r.Next()
		if r.Type() == tlv.TypeEndOfContainer {
			break
		}
		if r.TagValue().TagNum == certTagSignature {
			sigBytes = r.Value().([]byte)
		} else {
			skipTLVElement(r)
		}
	}

	rInt := new(big.Int).SetBytes(sigBytes[:32])
	sInt := new(big.Int).SetBytes(sigBytes[32:])
	if !ecdsa.Verify(&rootKey.PublicKey, hash[:], rInt, sInt) {
		t.Error("ICAC TLV signature verification FAILED")
	} else {
		t.Log("ICAC TLV signature verification PASSED")
	}
}

func TestMatterCertTLV_MatchesTBSInFullCert(t *testing.T) {
	// Verify that the TBS bytes computed separately match the TBS portion
	// embedded in the full certificate (i.e., that writeMatterCertTBS is
	// deterministic and produces the same bytes both times).
	key, err := GenerateKeyPair()
	if err != nil {
		t.Fatal(err)
	}

	opts := DefaultCertificateOptions()
	rcacDER, err := GenerateRCAC(key, 1, 1, opts)
	if err != nil {
		t.Fatal(err)
	}

	cert, _ := x509.ParseCertificate(rcacDER)

	// Build TBS separately.
	tbs1 := tlv.NewWriter()
	_ = tbs1.StartStructure(tlv.AnonymousTag())
	_ = writeMatterCertTBS(tbs1, cert)
	_ = tbs1.EndContainer()

	// Build TBS again.
	tbs2 := tlv.NewWriter()
	_ = tbs2.StartStructure(tlv.AnonymousTag())
	_ = writeMatterCertTBS(tbs2, cert)
	_ = tbs2.EndContainer()

	if !bytes.Equal(tbs1.Bytes(), tbs2.Bytes()) {
		t.Error("TBS encoding is NOT deterministic!")
		t.Logf("TBS1: %s", hex.EncodeToString(tbs1.Bytes()))
		t.Logf("TBS2: %s", hex.EncodeToString(tbs2.Bytes()))
	} else {
		t.Log("TBS encoding is deterministic")
	}

	// Also verify the TBS matches the beginning of the full cert (minus sig).
	rcacTLV, _ := X509ToMatterCertResigned(rcacDER, key)

	// The full cert starts with Structure(Anonymous) + tags 1-10 + tag 11 + EndOfContainer.
	// The TBS starts with Structure(Anonymous) + tags 1-10 + EndOfContainer.
	// So full cert = TBS[:-1] + [tag 11 bytes] + [EndOfContainer].
	tbsBytes := tbs1.Bytes()
	tbsWithoutEOC := tbsBytes[:len(tbsBytes)-1]

	if !bytes.HasPrefix(rcacTLV, tbsWithoutEOC) {
		t.Error("full cert does not start with TBS content")
		t.Logf("TBS without EOC: %s", hex.EncodeToString(tbsWithoutEOC))
		t.Logf("Full cert prefix: %s", hex.EncodeToString(rcacTLV[:len(tbsWithoutEOC)]))
	} else {
		t.Log("full cert starts with TBS content - correct")
	}
}

// readExtensions reads extension elements from the current List container.
func readExtensions(t *testing.T, r *tlv.Reader) {
	t.Helper()
	for {
		if err := r.Next(); err != nil {
			t.Fatal(err)
		}
		if r.Type() == tlv.TypeEndOfContainer {
			return
		}
		extTag := r.TagValue().TagNum
		switch extTag {
		case extTagBasicConstraints:
			t.Logf("    Ext %d (BasicConstraints): Structure", extTag)
			for {
				if err := r.Next(); err != nil {
					t.Fatal(err)
				}
				if r.Type() == tlv.TypeEndOfContainer {
					break
				}
				tag := r.TagValue().TagNum
				t.Logf("      BC tag %d: %v", tag, r.Value())
			}
		case extTagKeyUsage:
			val := r.Value().(uint64)
			t.Logf("    Ext %d (KeyUsage): 0x%04X (%d)", extTag, val, val)
		case extTagSubjectKeyID:
			skid := r.Value().([]byte)
			t.Logf("    Ext %d (SKID): %s (%d bytes)", extTag, hex.EncodeToString(skid), len(skid))
		case extTagAuthorityKeyID:
			akid := r.Value().([]byte)
			t.Logf("    Ext %d (AKID): %s (%d bytes)", extTag, hex.EncodeToString(akid), len(akid))
		default:
			t.Logf("    Ext %d (unknown): %v", extTag, r.Value())
			skipTLVElement(r)
		}
	}
}

// skipTLVElement skips the current element (handles containers).
func skipTLVElement(r *tlv.Reader) {
	switch r.Type() {
	case tlv.TypeStructure, tlv.TypeArray, tlv.TypeList:
		depth := 1
		for depth > 0 {
			_ = r.Next()
			switch r.Type() {
			case tlv.TypeStructure, tlv.TypeArray, tlv.TypeList:
				depth++
			case tlv.TypeEndOfContainer:
				depth--
			}
		}
	}
}

func TestAddTrustedRootCertEncoding(t *testing.T) {
	// Simulate what the commissioning flow does: generate RCAC, convert to
	// TLV, wrap in AddTrustedRootCertificate command fields.
	key, err := GenerateKeyPair()
	if err != nil {
		t.Fatal(err)
	}

	opts := DefaultCertificateOptions()
	rcacDER, err := GenerateRCAC(key, 1, 1, opts)
	if err != nil {
		t.Fatal(err)
	}

	rcacTLV, err := X509ToMatterCertResigned(rcacDER, key)
	if err != nil {
		t.Fatal(err)
	}

	// This is what encodeOctetStringField(0, rcacTLV) produces.
	w := tlv.NewWriter()
	if err := w.PutOctetString(tlv.ContextTag(0), rcacTLV); err != nil {
		t.Fatal(err)
	}
	cmdFields := w.Bytes()

	t.Logf("Command fields for AddTrustedRootCertificate (%d bytes): %s",
		len(cmdFields), hex.EncodeToString(cmdFields))

	// Verify we can parse the octet string back out.
	type addTrustedRootReq struct {
		RootCACertificate []byte `tlv:"0,octets"`
	}
	var parsed addTrustedRootReq
	if err := tlv.Unmarshal(tlv.WrapStruct(cmdFields), &parsed); err != nil {
		t.Fatalf("failed to parse command fields: %v", err)
	}

	if !bytes.Equal(parsed.RootCACertificate, rcacTLV) {
		t.Error("parsed certificate does not match original")
	}

	t.Logf("RCAC TLV inside command (%d bytes): %s",
		len(parsed.RootCACertificate), hex.EncodeToString(parsed.RootCACertificate))

	// Now verify the full InvokeRequest encoding by simulating what the
	// interaction client does. Build the InvokeRequest payload.
	type CommandPath struct {
		EndpointID uint16 `tlv:"0,uint"`
		ClusterID  uint32 `tlv:"1,uint"`
		CommandID  uint32 `tlv:"2,uint"`
	}
	type CommandDataIB struct {
		Path   CommandPath `tlv:"0,liststruct"`
		Fields []byte      `tlv:"1,rawstruct"`
	}
	type InvokeRequest struct {
		SuppressResponse bool            `tlv:"0,bool"`
		TimedRequest     bool            `tlv:"1,bool"`
		InvokeRequests   []CommandDataIB `tlv:"2,array"`
	}

	req := InvokeRequest{
		SuppressResponse: false,
		TimedRequest:     true,
		InvokeRequests: []CommandDataIB{
			{
				Path: CommandPath{
					EndpointID: 0,
					ClusterID:  0x003E,
					CommandID:  0x0B,
				},
				Fields: cmdFields,
			},
		},
	}

	encoded, err := tlv.Marshal(req)
	if err != nil {
		t.Fatalf("marshaling InvokeRequest: %v", err)
	}

	// Inject InteractionModelRevision before final EndOfContainer.
	if encoded[len(encoded)-1] == byte(tlv.TypeEndOfContainer) {
		imRevision := []byte{0x24, 0xFF, 0x0B}
		encoded = append(encoded[:len(encoded)-1], imRevision...)
		encoded = append(encoded, byte(tlv.TypeEndOfContainer))
	}

	t.Logf("Full InvokeRequest TLV (%d bytes):", len(encoded))
	// Dump in 32-byte lines for readability.
	for i := 0; i < len(encoded); i += 32 {
		end := i + 32
		if end > len(encoded) {
			end = len(encoded)
		}
		t.Logf("  %04x: %s", i, hex.EncodeToString(encoded[i:end]))
	}

	// Verify we can unmarshal the InvokeRequest back.
	var decoded InvokeRequest
	if err := tlv.Unmarshal(encoded, &decoded); err != nil {
		t.Fatalf("unmarshaling InvokeRequest: %v", err)
	}

	if !decoded.TimedRequest {
		t.Error("TimedRequest should be true")
	}
	if len(decoded.InvokeRequests) != 1 {
		t.Fatalf("expected 1 invoke request, got %d", len(decoded.InvokeRequests))
	}
	cmd := decoded.InvokeRequests[0]
	if cmd.Path.ClusterID != 0x003E || cmd.Path.CommandID != 0x0B {
		t.Errorf("wrong command path: cluster=0x%04X, command=0x%04X", cmd.Path.ClusterID, cmd.Path.CommandID)
	}

	fmt.Printf("RCAC TLV size: %d bytes\n", len(rcacTLV))
	fmt.Printf("Command fields size: %d bytes\n", len(cmdFields))
	fmt.Printf("Full InvokeRequest size: %d bytes\n", len(encoded))
}

func TestX509ToMatterCertResigned_CertChainRoundTrip(t *testing.T) {
	// Generate RCAC -> ICAC -> NOC chain, convert each to TLV with
	// X509ToMatterCertResigned, then verify the ECDSA signatures over the
	// TLV TBS against the issuer's public key.
	rootKey, err := GenerateKeyPair()
	if err != nil {
		t.Fatal(err)
	}

	opts := DefaultCertificateOptions()
	fabricID := uint64(0xFAB000000000001D)

	rcacDER, err := GenerateRCAC(rootKey, 1, fabricID, opts)
	if err != nil {
		t.Fatal(err)
	}

	icacKey, err := GenerateKeyPair()
	if err != nil {
		t.Fatal(err)
	}
	icacDER, err := GenerateICAC(icacKey, 2, fabricID, rcacDER, rootKey, opts)
	if err != nil {
		t.Fatal(err)
	}

	nodeKey, err := GenerateKeyPair()
	if err != nil {
		t.Fatal(err)
	}
	nocDER, err := GenerateNOC(nodeKey, 0xDEDE00010001, fabricID, icacDER, icacKey, opts)
	if err != nil {
		t.Fatal(err)
	}

	// Convert each to TLV with re-signing.
	rcacTLV, err := X509ToMatterCertResigned(rcacDER, rootKey) // self-signed
	if err != nil {
		t.Fatalf("RCAC resigned: %v", err)
	}
	icacTLV, err := X509ToMatterCertResigned(icacDER, rootKey) // signed by root
	if err != nil {
		t.Fatalf("ICAC resigned: %v", err)
	}
	nocTLV, err := X509ToMatterCertResigned(nocDER, icacKey) // signed by ICAC
	if err != nil {
		t.Fatalf("NOC resigned: %v", err)
	}

	// Verify each TLV cert's signature over TLV TBS against the issuer's public key.
	tests := []struct {
		name      string
		certDER   []byte
		certTLV   []byte
		verifyKey *ecdsa.PublicKey
	}{
		{"RCAC (self-signed)", rcacDER, rcacTLV, &rootKey.PublicKey},
		{"ICAC (signed by root)", icacDER, icacTLV, &rootKey.PublicKey},
		{"NOC (signed by ICAC)", nocDER, nocTLV, &icacKey.PublicKey},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Extract signature from TLV cert (tag 11).
			sigBytes := extractTLVSignature(t, tc.certTLV)
			if len(sigBytes) != 64 {
				t.Fatalf("expected 64-byte signature, got %d", len(sigBytes))
			}

			// Build TBS from the X.509 cert (same fields minus signature).
			cert, parseErr := ParseCertificate(tc.certDER)
			if parseErr != nil {
				t.Fatal(parseErr)
			}
			tbsWriter := tlv.NewWriter()
			_ = tbsWriter.StartStructure(tlv.AnonymousTag())
			_ = writeMatterCertTBS(tbsWriter, cert)
			_ = tbsWriter.EndContainer()
			tbsBytes := tbsWriter.Bytes()

			// Verify ECDSA signature over SHA-256(TLV TBS).
			hash := sha256.Sum256(tbsBytes)
			rInt := new(big.Int).SetBytes(sigBytes[:32])
			sInt := new(big.Int).SetBytes(sigBytes[32:])
			if !ecdsa.Verify(tc.verifyKey, hash[:], rInt, sInt) {
				t.Error("TLV signature verification FAILED")
			}
		})
	}
}

// TestSerialNumberLeadingZeroPreserved verifies that serial numbers with MSB
// set include the leading 0x00 byte in the TLV encoding. The CHIP SDK's
// TLV-to-DER converter writes TLV bytes verbatim as INTEGER content, so the
// leading zero is required for the DER round-trip to produce a positive integer.
func TestSerialNumberLeadingZeroPreserved(t *testing.T) {
	key, err := GenerateKeyPair()
	if err != nil {
		t.Fatal(err)
	}

	// Use a serial number with MSB set (0xFF...).
	serial := new(big.Int).SetBytes([]byte{0xFF, 0xAA, 0xBB, 0xCC, 0xDD, 0xEE, 0x11, 0x22})
	opts := CertificateOptions{
		SerialNumber: serial,
		NotBefore:    DefaultCertificateOptions().NotBefore,
		NotAfter:     DefaultCertificateOptions().NotAfter,
	}

	rcacDER, err := GenerateRCAC(key, 1, 1, opts)
	if err != nil {
		t.Fatal(err)
	}

	rcacTLV, err := X509ToMatterCert(rcacDER)
	if err != nil {
		t.Fatal(err)
	}

	// Extract serial number from TLV.
	r := tlv.NewReader(bytes.NewReader(rcacTLV))
	if err := r.Next(); err != nil {
		t.Fatal(err)
	}
	if err := r.Next(); err != nil {
		t.Fatal(err)
	}
	if r.TagValue().TagNum != certTagSerialNumber {
		t.Fatalf("expected tag 1, got %d", r.TagValue().TagNum)
	}
	serialBytes := r.Value().([]byte)
	t.Logf("TLV serial bytes: %s", hex.EncodeToString(serialBytes))

	// The TLV serial must have a leading 0x00 since the MSB of 0xFF is set.
	if len(serialBytes) == 0 || serialBytes[0] != 0x00 {
		t.Errorf("serial number missing leading 0x00: got %s", hex.EncodeToString(serialBytes))
	}

	// Verify the serial value is correct (0x00 + original bytes).
	expected := []byte{0x00, 0xFF, 0xAA, 0xBB, 0xCC, 0xDD, 0xEE, 0x11, 0x22}
	if !bytes.Equal(serialBytes, expected) {
		t.Errorf("serial bytes mismatch:\n  got:  %s\n  want: %s",
			hex.EncodeToString(serialBytes), hex.EncodeToString(expected))
	}

	// Also verify the original DER signature is valid over the reconstructed
	// DER TBS. Parse original cert, get original DER TBS, hash it, verify
	// that X509ToMatterCert's signature matches.
	origCert, _ := x509.ParseCertificate(rcacDER)

	// The original DER signature should verify against the original DER TBS hash.
	// That's guaranteed by Go's x509 library. What we need to check is that
	// the DER INTEGER encoding of the serial number in the original DER TBS
	// has the leading 0x00 — and our TLV preserves it.
	t.Logf("Original serial (big.Int): %s", origCert.SerialNumber.Text(16))
	t.Logf("Original serial Bytes(): %s", hex.EncodeToString(origCert.SerialNumber.Bytes()))
}

// extractTLVSignature reads a Matter TLV certificate and returns the signature
// bytes (tag 11 / certTagSignature).
func extractTLVSignature(t *testing.T, tlvCert []byte) []byte {
	t.Helper()
	r := tlv.NewReader(bytes.NewReader(tlvCert))
	if err := r.Next(); err != nil {
		t.Fatalf("reading structure: %v", err)
	}
	for {
		if err := r.Next(); err != nil {
			t.Fatalf("reading element: %v", err)
		}
		if r.Type() == tlv.TypeEndOfContainer {
			break
		}
		if r.TagValue().TagNum == certTagSignature {
			sig, ok := r.Value().([]byte)
			if !ok {
				t.Fatal("signature is not an octet string")
			}
			return sig
		}
		skipTLVElement(r)
	}
	t.Fatal("signature tag not found in TLV cert")
	return nil
}
