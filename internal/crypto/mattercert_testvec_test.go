// Copyright 2026 matter-cli contributors
// SPDX-License-Identifier: Apache-2.0

package crypto

import (
	"bytes"
	"encoding/hex"
	"encoding/pem"
	"strings"
	"testing"
)

// Test vectors from the Matter specification section 6.6 (Operational_Certificate.adoc).
// Each vector pairs an X.509 PEM certificate with its expected Matter TLV encoding.
// This validates that X509ToMatterCert produces correct TLV field encoding
// (DN tags, extensions, integers, etc.) by comparing against the spec's known-good output.

const specRCACPEM = `-----BEGIN CERTIFICATE-----
MIIBnTCCAUOgAwIBAgIIWeqmMpR/VBwwCgYIKoZIzj0EAwIwIjEgMB4GCisGAQQB
gqJ8AQQMEENBQ0FDQUNBMDAwMDAwMDEwHhcNMjAxMDE1MTQyMzQzWhcNNDAxMDE1
MTQyMzQyWjAiMSAwHgYKKwYBBAGConwBBAwQQ0FDQUNBQ0EwMDAwMDAwMTBZMBMG
ByqGSM49AgEGCCqGSM49AwEHA0IABBNTo7PvHacIxJCASAFOQH1ZkM4ivE6zPppa
yyWoVgPrptzYITZmpORPWsoT63Z/r6fc3dwzQR+CowtUPdHSS6ijYzBhMA8GA1Ud
EwEB/wQFMAMBAf8wDgYDVR0PAQH/BAQDAgEGMB0GA1UdDgQWBBQTr4GrNzdLLtKp
ZJsSt6OkKH4VHTAfBgNVHSMEGDAWgBQTr4GrNzdLLtKpZJsSt6OkKH4VHTAKBggq
hkjOPQQDAgNIADBFAiBFgWRGbI8ZWrwKu3xstaJ6g/QdN/jVO+7FIKvSoNoFCQIh
ALinwlwELjDPZNww/jNOEgAZZk5RUEkTT1eBI4RE/HUx
-----END CERTIFICATE-----`

const specICACPEM = `-----BEGIN CERTIFICATE-----
MIIBnTCCAUOgAwIBAgIILbREhVZBrt8wCgYIKoZIzj0EAwIwIjEgMB4GCisGAQQB
gqJ8AQQMEENBQ0FDQUNBMDAwMDAwMDEwHhcNMjAxMDE1MTQyMzQzWhcNNDAxMDE1
MTQyMzQyWjAiMSAwHgYKKwYBBAGConwBAwwQQ0FDQUNBQ0EwMDAwMDAwMzBZMBMG
ByqGSM49AgEGCCqGSM49AwEHA0IABMXQhhu4+QxAXBIxTkxevuqTn3J3S8wzI54v
Wfb0avjcfUaCoOPMxkbm3ynqhr9WKucgqJgzfTg/MsCgnkFgGeqjYzBhMA8GA1Ud
EwEB/wQFMAMBAf8wDgYDVR0PAQH/BAQDAgEGMB0GA1UdDgQWBBRTUtcFnpwVpQiQ
aGKGSAGinx9B0zAfBgNVHSMEGDAWgBQTr4GrNzdLLtKpZJsSt6OkKH4VHTAKBggq
hkjOPQQDAgNIADBFAiEAhBoG1Dten+zSToexJE61HGos8g2bXmugfxHmAC9+DKMC
IE4ypgLDYJ0AktNIvb0ZihFGRr1BzxA3g2Qa4l4/I/0m
-----END CERTIFICATE-----`

const specNOCPEM = `-----BEGIN CERTIFICATE-----
MIIB4DCCAYagAwIBAgIIPvz/FwK5oXowCgYIKoZIzj0EAwIwIjEgMB4GCisGAQQB
gqJ8AQMMEENBQ0FDQUNBMDAwMDAwMDMwHhcNMjAxMDE1MTQyMzQzWhcNNDAxMDE1
MTQyMzQyWjBEMSAwHgYKKwYBBAGConwBAQwQREVERURFREUwMDAxMDAwMTEgMB4G
CisGAQQBgqJ8AQUMEEZBQjAwMDAwMDAwMDAwMUQwWTATBgcqhkjOPQIBBggqhkjO
PQMBBwNCAASaKiFvs53WtvohG4NciePmr7ZsFPdYMZVPn/T3o/ARLIoNjq8pxlMp
TUju4HCKAyzKOTk8OntG8YGuoHj+rYODo4GDMIGAMAwGA1UdEwEB/wQCMAAwDgYD
VR0PAQH/BAQDAgeAMCAGA1UdJQEB/wQWMBQGCCsGAQUFBwMCBggrBgEFBQcDATAd
BgNVHQ4EFgQUn1Wia35DA+YIg+kTv5T0+14qYWEwHwYDVR0jBBgwFoAUU1LXBZ6c
FaUIkGhihkgBop8fQdMwCgYIKoZIzj0EAwIDSAAwRQIgeVXCAmMLS6TVkSUmMi/f
KPie3+WvnA5XK9ihSqq7TRICIQC4PKF8ewX7Fkt315xSlhMxa8/ReJXksqTyQEuY
FzJxWQ==
-----END CERTIFICATE-----`

// Expected Matter TLV hex from the spec (whitespace stripped).
var specRCACTLVHex = stripHexWhitespace(`
15 30 01 08 59 ea a6 32 94 7f 54 1c 24 02 01 37 03 27 14 01 00 00 00 ca
ca ca ca 18 26 04 ef 17 1b 27 26 05 6e b5 b9 4c 37 06 27 14 01 00 00 00
ca ca ca ca 18 24 07 01 24 08 01 30 09 41 04 13 53 a3 b3 ef 1d a7 08 c4
90 80 48 01 4e 40 7d 59 90 ce 22 bc 4e b3 3e 9a 5a cb 25 a8 56 03 eb a6
dc d8 21 36 66 a4 e4 4f 5a ca 13 eb 76 7f af a7 dc dd dc 33 41 1f 82 a3
0b 54 3d d1 d2 4b a8 37 0a 35 01 29 01 18 24 02 60 30 04 14 13 af 81 ab
37 37 4b 2e d2 a9 64 9b 12 b7 a3 a4 28 7e 15 1d 30 05 14 13 af 81 ab 37
37 4b 2e d2 a9 64 9b 12 b7 a3 a4 28 7e 15 1d 18 30 0b 40 45 81 64 46 6c
8f 19 5a bc 0a bb 7c 6c b5 a2 7a 83 f4 1d 37 f8 d5 3b ee c5 20 ab d2 a0
da 05 09 b8 a7 c2 5c 04 2e 30 cf 64 dc 30 fe 33 4e 12 00 19 66 4e 51 50
49 13 4f 57 81 23 84 44 fc 75 31 18
`)

var specICACTLVHex = stripHexWhitespace(`
15 30 01 08 2d b4 44 85 56 41 ae df 24 02 01 37 03 27 14 01 00 00 00 ca
ca ca ca 18 26 04 ef 17 1b 27 26 05 6e b5 b9 4c 37 06 27 13 03 00 00 00
ca ca ca ca 18 24 07 01 24 08 01 30 09 41 04 c5 d0 86 1b b8 f9 0c 40 5c
12 31 4e 4c 5e be ea 93 9f 72 77 4b cc 33 23 9e 2f 59 f6 f4 6a f8 dc 7d
46 82 a0 e3 cc c6 46 e6 df 29 ea 86 bf 56 2a e7 20 a8 98 33 7d 38 3f 32
c0 a0 9e 41 60 19 ea 37 0a 35 01 29 01 18 24 02 60 30 04 14 53 52 d7 05
9e 9c 15 a5 08 90 68 62 86 48 01 a2 9f 1f 41 d3 30 05 14 13 af 81 ab 37
37 4b 2e d2 a9 64 9b 12 b7 a3 a4 28 7e 15 1d 18 30 0b 40 84 1a 06 d4 3b
5e 9f ec d2 4e 87 b1 24 4e b5 1c 6a 2c f2 0d 9b 5e 6b a0 7f 11 e6 00 2f
7e 0c a3 4e 32 a6 02 c3 60 9d 00 92 d3 48 bd bd 19 8a 11 46 46 bd 41 cf
10 37 83 64 1a e2 5e 3f 23 fd 26 18
`)

var specNOCTLVHex = stripHexWhitespace(`
15 30 01 08 3e fc ff 17 02 b9 a1 7a 24 02 01 37 03 27 13 03 00 00 00 ca
ca ca ca 18 26 04 ef 17 1b 27 26 05 6e b5 b9 4c 37 06 27 11 01 00 01 00
de de de de 27 15 1d 00 00 00 00 00 b0 fa 18 24 07 01 24 08 01 30 09 41
04 9a 2a 21 6f b3 9d d6 b6 fa 21 1b 83 5c 89 e3 e6 af b6 6c 14 f7 58 31
95 4f 9f f4 f7 a3 f0 11 2c 8a 0d 8e af 29 c6 53 29 4d 48 ee e0 70 8a 03
2c ca 39 39 3c 3a 7b 46 f1 81 ae a0 78 fe ad 83 83 37 0a 35 01 28 01 18
24 02 01 36 03 04 02 04 01 18 30 04 14 9f 55 a2 6b 7e 43 03 e6 08 83 e9
13 bf 94 f4 fb 5e 2a 61 61 30 05 14 53 52 d7 05 9e 9c 15 a5 08 90 68 62
86 48 01 a2 9f 1f 41 d3 18 30 0b 40 79 55 c2 02 63 0b 4b a4 d5 91 25 26
32 2f df 28 f8 9e df e5 af 9c 0e 57 2b d8 a1 4a aa bb 4d 12 b8 3c a1 7c
7b 05 fb 16 4b 77 d7 9c 52 96 13 31 6b cf d1 78 95 e4 b2 a4 f2 40 4b 98
17 32 71 59 18
`)

func stripHexWhitespace(s string) string {
	s = strings.ReplaceAll(s, " ", "")
	s = strings.ReplaceAll(s, "\n", "")
	s = strings.ReplaceAll(s, "\r", "")
	s = strings.ReplaceAll(s, "\t", "")
	return s
}

func pemToDER(pemData string) []byte {
	block, _ := pem.Decode([]byte(pemData))
	if block == nil {
		panic("failed to decode PEM")
	}
	return block.Bytes
}

func TestSpecTestVector_RCAC(t *testing.T) {
	der := pemToDER(specRCACPEM)
	got, err := X509ToMatterCert(der)
	if err != nil {
		t.Fatalf("X509ToMatterCert(RCAC): %v", err)
	}

	expected, err := hex.DecodeString(specRCACTLVHex)
	if err != nil {
		t.Fatalf("decoding expected hex: %v", err)
	}

	if !bytes.Equal(got, expected) {
		t.Errorf("RCAC TLV mismatch\n  got:    %s\n  expect: %s", hex.EncodeToString(got), hex.EncodeToString(expected))
		// Find first difference for debugging.
		for i := 0; i < len(got) && i < len(expected); i++ {
			if got[i] != expected[i] {
				t.Errorf("  first diff at byte %d: got 0x%02x, want 0x%02x", i, got[i], expected[i])
				break
			}
		}
		if len(got) != len(expected) {
			t.Errorf("  length mismatch: got %d, want %d", len(got), len(expected))
		}
	}
}

func TestSpecTestVector_ICAC(t *testing.T) {
	der := pemToDER(specICACPEM)
	got, err := X509ToMatterCert(der)
	if err != nil {
		t.Fatalf("X509ToMatterCert(ICAC): %v", err)
	}

	expected, err := hex.DecodeString(specICACTLVHex)
	if err != nil {
		t.Fatalf("decoding expected hex: %v", err)
	}

	if !bytes.Equal(got, expected) {
		t.Errorf("ICAC TLV mismatch\n  got:    %s\n  expect: %s", hex.EncodeToString(got), hex.EncodeToString(expected))
		for i := 0; i < len(got) && i < len(expected); i++ {
			if got[i] != expected[i] {
				t.Errorf("  first diff at byte %d: got 0x%02x, want 0x%02x", i, got[i], expected[i])
				break
			}
		}
		if len(got) != len(expected) {
			t.Errorf("  length mismatch: got %d, want %d", len(got), len(expected))
		}
	}
}

func TestSpecTestVector_NOC(t *testing.T) {
	der := pemToDER(specNOCPEM)
	got, err := X509ToMatterCert(der)
	if err != nil {
		t.Fatalf("X509ToMatterCert(NOC): %v", err)
	}

	expected, err := hex.DecodeString(specNOCTLVHex)
	if err != nil {
		t.Fatalf("decoding expected hex: %v", err)
	}

	if !bytes.Equal(got, expected) {
		t.Errorf("NOC TLV mismatch\n  got:    %s\n  expect: %s", hex.EncodeToString(got), hex.EncodeToString(expected))
		for i := 0; i < len(got) && i < len(expected); i++ {
			if got[i] != expected[i] {
				t.Errorf("  first diff at byte %d: got 0x%02x, want 0x%02x", i, got[i], expected[i])
				break
			}
		}
		if len(got) != len(expected) {
			t.Errorf("  length mismatch: got %d, want %d", len(got), len(expected))
		}
	}
}
