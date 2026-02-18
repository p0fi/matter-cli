package crypto

import (
	"crypto/x509"
	"encoding/hex"
	"fmt"
	"testing"
)

func TestRCACDERvsReconstructedDER(t *testing.T) {
	// Generate a key pair and RCAC
	key, err := GenerateKeyPair()
	if err != nil {
		t.Fatal(err)
	}

	opts := DefaultCertificateOptions()
	rcacDER, err := GenerateRCAC(key, 1, 1, opts)
	if err != nil {
		t.Fatal(err)
	}

	// Parse the DER cert to get the TBS
	cert, err := x509.ParseCertificate(rcacDER)
	if err != nil {
		t.Fatal(err)
	}

	// The raw TBS is cert.RawTBSCertificate
	fmt.Printf("Original DER TBS hex: %s\n", hex.EncodeToString(cert.RawTBSCertificate))
	fmt.Printf("Original DER TBS length: %d\n", len(cert.RawTBSCertificate))
	fmt.Printf("Full DER cert hex: %s\n", hex.EncodeToString(rcacDER))
	fmt.Printf("Full DER cert length: %d\n\n", len(rcacDER))

	// Convert to Matter TLV (preserving DER signature)
	tlvCert, err := X509ToMatterCert(rcacDER)
	if err != nil {
		t.Fatal(err)
	}
	fmt.Printf("TLV cert hex: %s\n", hex.EncodeToString(tlvCert))
	fmt.Printf("TLV cert length: %d\n\n", len(tlvCert))

	// Print subject info for cross-reference
	fmt.Printf("Subject Names:\n")
	for _, n := range cert.Subject.Names {
		fmt.Printf("  OID: %s, Value: %v (type: %T)\n", n.Type, n.Value, n.Value)
	}
	fmt.Printf("SubjectKeyId: %s\n", hex.EncodeToString(cert.SubjectKeyId))
	fmt.Printf("AuthorityKeyId: %s\n", hex.EncodeToString(cert.AuthorityKeyId))
	fmt.Printf("NotBefore: %v\n", cert.NotBefore)
	fmt.Printf("NotAfter: %v\n", cert.NotAfter)
}
