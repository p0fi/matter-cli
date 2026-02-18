// Copyright 2026 matter-cli contributors
// SPDX-License-Identifier: Apache-2.0

package secure

import (
	"bytes"
	"crypto/ecdsa"
	"testing"

	"github.com/p0fi/matter-cli/internal/crypto"
	"github.com/p0fi/matter-cli/internal/tlv"
)

// setupCASEMaterial generates all the crypto material for a CASE test.
func setupCASEMaterial(t *testing.T) (
	rcacKey *ecdsa.PrivateKey, rcacDER []byte,
	initNodeKey *ecdsa.PrivateKey, initNOC []byte,
	respNodeKey *ecdsa.PrivateKey, respNOC []byte,
	rootPubKey, ipk []byte,
	fabricID, initNodeID, respNodeID uint64,
) {
	t.Helper()

	fabricID = 0x0001
	initNodeID = 0x1000
	respNodeID = 0x2000

	var err error
	opts := crypto.DefaultCertificateOptions()

	// Generate root CA key and RCAC.
	rcacKey, err = crypto.GenerateKeyPair()
	if err != nil {
		t.Fatalf("generate RCAC key: %v", err)
	}

	rcacDER, err = crypto.GenerateRCAC(rcacKey, 1, fabricID, opts)
	if err != nil {
		t.Fatalf("generate RCAC: %v", err)
	}

	// Generate initiator node key and NOC.
	initNodeKey, err = crypto.GenerateKeyPair()
	if err != nil {
		t.Fatalf("generate initiator node key: %v", err)
	}

	initNOC, err = crypto.GenerateNOC(initNodeKey, initNodeID, fabricID, rcacDER, rcacKey, opts)
	if err != nil {
		t.Fatalf("generate initiator NOC: %v", err)
	}

	// Generate responder node key and NOC.
	respNodeKey, err = crypto.GenerateKeyPair()
	if err != nil {
		t.Fatalf("generate responder node key: %v", err)
	}

	respNOC, err = crypto.GenerateNOC(respNodeKey, respNodeID, fabricID, rcacDER, rcacKey, opts)
	if err != nil {
		t.Fatalf("generate responder NOC: %v", err)
	}

	rootPubKey = crypto.PublicKeyToUncompressed(&rcacKey.PublicKey)
	ipk = bytes.Repeat([]byte{0x42}, 16)

	return
}

func TestCASEHandshakeSuccess(t *testing.T) {
	_, _, initNodeKey, initNOC, respNodeKey, respNOC, rootPubKey, ipk, fabricID, _, respNodeID := setupCASEMaterial(t)

	initiator := NewCASEInitiator(CASEInitiatorConfig{
		SessionID:  1,
		NodeKey:    initNodeKey,
		NOC:        initNOC,
		ICAC:       nil,
		IPK:        ipk,
		RootPubKey: rootPubKey,
		FabricID:   fabricID,
		PeerNodeID: respNodeID,
	})

	responder := NewCASEResponder(CASEResponderConfig{
		SessionID:  2,
		NodeKey:    respNodeKey,
		NOC:        respNOC,
		ICAC:       nil,
		IPK:        ipk,
		RootPubKey: rootPubKey,
		FabricID:   fabricID,
		NodeID:     respNodeID,
	})

	// Step 1: Initiator generates Sigma1.
	sigma1Bytes, err := initiator.GenerateSigma1()
	if err != nil {
		t.Fatalf("GenerateSigma1 failed: %v", err)
	}
	if len(sigma1Bytes) == 0 {
		t.Fatal("Sigma1 is empty")
	}

	// Step 2: Responder processes Sigma1, generates Sigma2.
	sigma2Bytes, err := responder.ProcessSigma1(sigma1Bytes)
	if err != nil {
		t.Fatalf("ProcessSigma1 failed: %v", err)
	}
	if len(sigma2Bytes) == 0 {
		t.Fatal("Sigma2 is empty")
	}

	// Step 3: Initiator processes Sigma2.
	if err := initiator.ProcessSigma2(sigma2Bytes); err != nil {
		t.Fatalf("ProcessSigma2 failed: %v", err)
	}

	// Step 4: Initiator generates Sigma3.
	sigma3Bytes, err := initiator.GenerateSigma3()
	if err != nil {
		t.Fatalf("GenerateSigma3 failed: %v", err)
	}
	if len(sigma3Bytes) == 0 {
		t.Fatal("Sigma3 is empty")
	}

	// Step 5: Responder processes Sigma3.
	if err := responder.ProcessSigma3(sigma3Bytes); err != nil {
		t.Fatalf("ProcessSigma3 failed: %v", err)
	}

	// Verify both sides derived the same session keys.
	iKeys := initiator.SessionKeys()
	rKeys := responder.SessionKeys()

	if iKeys == nil {
		t.Fatal("initiator session keys are nil")
	}
	if rKeys == nil {
		t.Fatal("responder session keys are nil")
	}

	if !bytes.Equal(iKeys.I2RKey, rKeys.I2RKey) {
		t.Error("I2RKey mismatch between initiator and responder")
	}
	if !bytes.Equal(iKeys.R2IKey, rKeys.R2IKey) {
		t.Error("R2IKey mismatch between initiator and responder")
	}
	if !bytes.Equal(iKeys.AttestationChallenge, rKeys.AttestationChallenge) {
		t.Error("AttestationChallenge mismatch between initiator and responder")
	}

	// Verify key lengths.
	if len(iKeys.I2RKey) != SessionKeyLength {
		t.Errorf("I2RKey length = %d, want %d", len(iKeys.I2RKey), SessionKeyLength)
	}
	if len(iKeys.R2IKey) != SessionKeyLength {
		t.Errorf("R2IKey length = %d, want %d", len(iKeys.R2IKey), SessionKeyLength)
	}
}

func TestCASEHandshakeTamperedSigma2(t *testing.T) {
	_, _, initNodeKey, initNOC, respNodeKey, respNOC, rootPubKey, ipk, fabricID, _, respNodeID := setupCASEMaterial(t)

	initiator := NewCASEInitiator(CASEInitiatorConfig{
		SessionID:  1,
		NodeKey:    initNodeKey,
		NOC:        initNOC,
		IPK:        ipk,
		RootPubKey: rootPubKey,
		FabricID:   fabricID,
		PeerNodeID: respNodeID,
	})

	responder := NewCASEResponder(CASEResponderConfig{
		SessionID:  2,
		NodeKey:    respNodeKey,
		NOC:        respNOC,
		IPK:        ipk,
		RootPubKey: rootPubKey,
		FabricID:   fabricID,
		NodeID:     respNodeID,
	})

	sigma1Bytes, err := initiator.GenerateSigma1()
	if err != nil {
		t.Fatalf("GenerateSigma1 failed: %v", err)
	}

	sigma2Bytes, err := responder.ProcessSigma1(sigma1Bytes)
	if err != nil {
		t.Fatalf("ProcessSigma1 failed: %v", err)
	}

	// Tamper with Sigma2 — flip a byte near the end to hit the Encrypted2
	// field (the first ~109 bytes are unencrypted header fields like
	// ResponderRandom and ResponderEphPubKey, which are not authenticated
	// by AES-CCM).
	tampered := make([]byte, len(sigma2Bytes))
	copy(tampered, sigma2Bytes)
	if len(tampered) > 5 {
		tampered[len(tampered)-5] ^= 0xFF
	}

	err = initiator.ProcessSigma2(tampered)
	if err == nil {
		t.Fatal("expected ProcessSigma2 to fail with tampered data, but it succeeded")
	}
	t.Logf("correctly failed with: %v", err)
}

func TestCASEHandshakeTamperedSigma3(t *testing.T) {
	_, _, initNodeKey, initNOC, respNodeKey, respNOC, rootPubKey, ipk, fabricID, _, respNodeID := setupCASEMaterial(t)

	initiator := NewCASEInitiator(CASEInitiatorConfig{
		SessionID:  1,
		NodeKey:    initNodeKey,
		NOC:        initNOC,
		IPK:        ipk,
		RootPubKey: rootPubKey,
		FabricID:   fabricID,
		PeerNodeID: respNodeID,
	})

	responder := NewCASEResponder(CASEResponderConfig{
		SessionID:  2,
		NodeKey:    respNodeKey,
		NOC:        respNOC,
		IPK:        ipk,
		RootPubKey: rootPubKey,
		FabricID:   fabricID,
		NodeID:     respNodeID,
	})

	sigma1Bytes, err := initiator.GenerateSigma1()
	if err != nil {
		t.Fatalf("GenerateSigma1 failed: %v", err)
	}

	sigma2Bytes, err := responder.ProcessSigma1(sigma1Bytes)
	if err != nil {
		t.Fatalf("ProcessSigma1 failed: %v", err)
	}

	if err := initiator.ProcessSigma2(sigma2Bytes); err != nil {
		t.Fatalf("ProcessSigma2 failed: %v", err)
	}

	sigma3Bytes, err := initiator.GenerateSigma3()
	if err != nil {
		t.Fatalf("GenerateSigma3 failed: %v", err)
	}

	// Tamper with Sigma3.
	tampered := make([]byte, len(sigma3Bytes))
	copy(tampered, sigma3Bytes)
	if len(tampered) > 10 {
		tampered[10] ^= 0xFF
	}

	err = responder.ProcessSigma3(tampered)
	if err == nil {
		t.Fatal("expected ProcessSigma3 to fail with tampered data, but it succeeded")
	}
	t.Logf("correctly failed with: %v", err)
}

func TestCASEHandshakeWrongResponderKey(t *testing.T) {
	_, _, initNodeKey, initNOC, _, respNOC, rootPubKey, ipk, fabricID, _, respNodeID := setupCASEMaterial(t)

	// Generate a different key for the responder (not matching its NOC).
	wrongKey, err := crypto.GenerateKeyPair()
	if err != nil {
		t.Fatalf("generate wrong key: %v", err)
	}

	initiator := NewCASEInitiator(CASEInitiatorConfig{
		SessionID:  1,
		NodeKey:    initNodeKey,
		NOC:        initNOC,
		IPK:        ipk,
		RootPubKey: rootPubKey,
		FabricID:   fabricID,
		PeerNodeID: respNodeID,
	})

	// Responder uses the wrong key - signature will not match the NOC public key.
	responder := NewCASEResponder(CASEResponderConfig{
		SessionID:  2,
		NodeKey:    wrongKey,
		NOC:        respNOC,
		IPK:        ipk,
		RootPubKey: rootPubKey,
		FabricID:   fabricID,
		NodeID:     respNodeID,
	})

	sigma1Bytes, err := initiator.GenerateSigma1()
	if err != nil {
		t.Fatalf("GenerateSigma1 failed: %v", err)
	}

	sigma2Bytes, err := responder.ProcessSigma1(sigma1Bytes)
	if err != nil {
		t.Fatalf("ProcessSigma1 failed: %v", err)
	}

	// The initiator should detect the signature mismatch.
	err = initiator.ProcessSigma2(sigma2Bytes)
	if err == nil {
		t.Fatal("expected ProcessSigma2 to fail with wrong responder key, but it succeeded")
	}
	t.Logf("correctly failed with: %v", err)
}

func TestCASEDestinationID(t *testing.T) {
	ipk := bytes.Repeat([]byte{0x01}, 16)
	random := bytes.Repeat([]byte{0x02}, 32)
	rootPub := bytes.Repeat([]byte{0x03}, 65)
	fabricID := uint64(1)
	nodeID := uint64(2)

	destID1 := ComputeDestinationID(ipk, random, rootPub, fabricID, nodeID)
	if len(destID1) != 32 {
		t.Errorf("DestinationID length = %d, want 32", len(destID1))
	}

	// Same inputs should produce the same output.
	destID2 := ComputeDestinationID(ipk, random, rootPub, fabricID, nodeID)
	if !bytes.Equal(destID1, destID2) {
		t.Error("DestinationID should be deterministic")
	}

	// Different nodeID should produce different output.
	destID3 := ComputeDestinationID(ipk, random, rootPub, fabricID, nodeID+1)
	if bytes.Equal(destID1, destID3) {
		t.Error("different nodeIDs should produce different DestinationIDs")
	}

	// Different IPK should produce different output.
	ipk2 := bytes.Repeat([]byte{0x99}, 16)
	destID4 := ComputeDestinationID(ipk2, random, rootPub, fabricID, nodeID)
	if bytes.Equal(destID1, destID4) {
		t.Error("different IPKs should produce different DestinationIDs")
	}
}

func TestCASESigmaTLVRoundTrip(t *testing.T) {
	t.Run("Sigma1", func(t *testing.T) {
		original := Sigma1{
			InitiatorRandom:    bytes.Repeat([]byte{0xAA}, 32),
			InitiatorSessionID: 42,
			DestinationID:      bytes.Repeat([]byte{0xBB}, 32),
			InitiatorEphPubKey: bytes.Repeat([]byte{0xCC}, 65),
		}

		data, err := tlv.Marshal(original)
		if err != nil {
			t.Fatalf("marshal Sigma1: %v", err)
		}

		var decoded Sigma1
		if err := tlv.Unmarshal(data, &decoded); err != nil {
			t.Fatalf("unmarshal Sigma1: %v", err)
		}

		if !bytes.Equal(decoded.InitiatorRandom, original.InitiatorRandom) {
			t.Error("InitiatorRandom mismatch")
		}
		if decoded.InitiatorSessionID != original.InitiatorSessionID {
			t.Error("InitiatorSessionID mismatch")
		}
		if !bytes.Equal(decoded.DestinationID, original.DestinationID) {
			t.Error("DestinationID mismatch")
		}
		if !bytes.Equal(decoded.InitiatorEphPubKey, original.InitiatorEphPubKey) {
			t.Error("InitiatorEphPubKey mismatch")
		}
	})

	t.Run("Sigma2", func(t *testing.T) {
		original := Sigma2{
			ResponderRandom:    bytes.Repeat([]byte{0x11}, 32),
			ResponderSessionID: 99,
			ResponderEphPubKey: bytes.Repeat([]byte{0x22}, 65),
			Encrypted2:         bytes.Repeat([]byte{0x33}, 100),
		}

		data, err := tlv.Marshal(original)
		if err != nil {
			t.Fatalf("marshal Sigma2: %v", err)
		}

		var decoded Sigma2
		if err := tlv.Unmarshal(data, &decoded); err != nil {
			t.Fatalf("unmarshal Sigma2: %v", err)
		}

		if decoded.ResponderSessionID != original.ResponderSessionID {
			t.Error("ResponderSessionID mismatch")
		}
		if !bytes.Equal(decoded.Encrypted2, original.Encrypted2) {
			t.Error("Encrypted2 mismatch")
		}
	})

	t.Run("Sigma3", func(t *testing.T) {
		original := Sigma3{
			Encrypted3: bytes.Repeat([]byte{0x44}, 80),
		}

		data, err := tlv.Marshal(original)
		if err != nil {
			t.Fatalf("marshal Sigma3: %v", err)
		}

		var decoded Sigma3
		if err := tlv.Unmarshal(data, &decoded); err != nil {
			t.Fatalf("unmarshal Sigma3: %v", err)
		}

		if !bytes.Equal(decoded.Encrypted3, original.Encrypted3) {
			t.Error("Encrypted3 mismatch")
		}
	})
}

func TestCASEMultipleHandshakes(t *testing.T) {
	// Run multiple handshakes to verify consistency.
	for i := 0; i < 3; i++ {
		t.Run(itoa(uint32(i)), func(t *testing.T) {
			_, _, initNodeKey, initNOC, respNodeKey, respNOC, rootPubKey, ipk, fabricID, _, respNodeID := setupCASEMaterial(t)

			initiator := NewCASEInitiator(CASEInitiatorConfig{
				SessionID:  uint16(10 + i),
				NodeKey:    initNodeKey,
				NOC:        initNOC,
				IPK:        ipk,
				RootPubKey: rootPubKey,
				FabricID:   fabricID,
				PeerNodeID: respNodeID,
			})

			responder := NewCASEResponder(CASEResponderConfig{
				SessionID:  uint16(20 + i),
				NodeKey:    respNodeKey,
				NOC:        respNOC,
				IPK:        ipk,
				RootPubKey: rootPubKey,
				FabricID:   fabricID,
				NodeID:     respNodeID,
			})

			sigma1, err := initiator.GenerateSigma1()
			if err != nil {
				t.Fatalf("GenerateSigma1: %v", err)
			}

			sigma2, err := responder.ProcessSigma1(sigma1)
			if err != nil {
				t.Fatalf("ProcessSigma1: %v", err)
			}

			if err := initiator.ProcessSigma2(sigma2); err != nil {
				t.Fatalf("ProcessSigma2: %v", err)
			}

			sigma3, err := initiator.GenerateSigma3()
			if err != nil {
				t.Fatalf("GenerateSigma3: %v", err)
			}

			if err := responder.ProcessSigma3(sigma3); err != nil {
				t.Fatalf("ProcessSigma3: %v", err)
			}

			iKeys := initiator.SessionKeys()
			rKeys := responder.SessionKeys()

			if !bytes.Equal(iKeys.I2RKey, rKeys.I2RKey) {
				t.Error("I2RKey mismatch")
			}
			if !bytes.Equal(iKeys.R2IKey, rKeys.R2IKey) {
				t.Error("R2IKey mismatch")
			}
			if !bytes.Equal(iKeys.AttestationChallenge, rKeys.AttestationChallenge) {
				t.Error("AttestationChallenge mismatch")
			}
		})
	}
}

func TestCASEWithICAC(t *testing.T) {
	fabricID := uint64(0x0001)
	initNodeID := uint64(0x1000)
	respNodeID := uint64(0x2000)

	opts := crypto.DefaultCertificateOptions()

	// Generate root CA.
	rcacKey, err := crypto.GenerateKeyPair()
	if err != nil {
		t.Fatalf("generate RCAC key: %v", err)
	}
	rcacDER, err := crypto.GenerateRCAC(rcacKey, 1, fabricID, opts)
	if err != nil {
		t.Fatalf("generate RCAC: %v", err)
	}

	// Generate ICAC.
	icacKey, err := crypto.GenerateKeyPair()
	if err != nil {
		t.Fatalf("generate ICAC key: %v", err)
	}
	icacDER, err := crypto.GenerateICAC(icacKey, 2, fabricID, rcacDER, rcacKey, opts)
	if err != nil {
		t.Fatalf("generate ICAC: %v", err)
	}

	// Generate initiator NOC signed by ICAC.
	initNodeKey, err := crypto.GenerateKeyPair()
	if err != nil {
		t.Fatalf("generate init node key: %v", err)
	}
	initNOC, err := crypto.GenerateNOC(initNodeKey, initNodeID, fabricID, icacDER, icacKey, opts)
	if err != nil {
		t.Fatalf("generate init NOC: %v", err)
	}

	// Generate responder NOC signed by ICAC.
	respNodeKey, err := crypto.GenerateKeyPair()
	if err != nil {
		t.Fatalf("generate resp node key: %v", err)
	}
	respNOC, err := crypto.GenerateNOC(respNodeKey, respNodeID, fabricID, icacDER, icacKey, opts)
	if err != nil {
		t.Fatalf("generate resp NOC: %v", err)
	}

	rootPubKey := crypto.PublicKeyToUncompressed(&rcacKey.PublicKey)
	ipk := bytes.Repeat([]byte{0x55}, 16)

	initiator := NewCASEInitiator(CASEInitiatorConfig{
		SessionID:  1,
		NodeKey:    initNodeKey,
		NOC:        initNOC,
		ICAC:       icacDER,
		IPK:        ipk,
		RootPubKey: rootPubKey,
		FabricID:   fabricID,
		PeerNodeID: respNodeID,
	})

	responder := NewCASEResponder(CASEResponderConfig{
		SessionID:  2,
		NodeKey:    respNodeKey,
		NOC:        respNOC,
		ICAC:       icacDER,
		IPK:        ipk,
		RootPubKey: rootPubKey,
		FabricID:   fabricID,
		NodeID:     respNodeID,
	})

	sigma1, err := initiator.GenerateSigma1()
	if err != nil {
		t.Fatalf("GenerateSigma1: %v", err)
	}

	sigma2, err := responder.ProcessSigma1(sigma1)
	if err != nil {
		t.Fatalf("ProcessSigma1: %v", err)
	}

	if err := initiator.ProcessSigma2(sigma2); err != nil {
		t.Fatalf("ProcessSigma2: %v", err)
	}

	sigma3, err := initiator.GenerateSigma3()
	if err != nil {
		t.Fatalf("GenerateSigma3: %v", err)
	}

	if err := responder.ProcessSigma3(sigma3); err != nil {
		t.Fatalf("ProcessSigma3: %v", err)
	}

	iKeys := initiator.SessionKeys()
	rKeys := responder.SessionKeys()

	if !bytes.Equal(iKeys.I2RKey, rKeys.I2RKey) {
		t.Error("I2RKey mismatch")
	}
	if !bytes.Equal(iKeys.R2IKey, rKeys.R2IKey) {
		t.Error("R2IKey mismatch")
	}
	if !bytes.Equal(iKeys.AttestationChallenge, rKeys.AttestationChallenge) {
		t.Error("AttestationChallenge mismatch")
	}
}
