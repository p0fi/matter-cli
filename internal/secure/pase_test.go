// Copyright 2026 matter-cli contributors
// SPDX-License-Identifier: Apache-2.0

package secure

import (
	"bytes"
	"github.com/p0fi/matter-cli/internal/tlv"
	"testing"
)

// testPASESalt is a fixed salt for deterministic test behavior.
var testPASESalt = bytes.Repeat([]byte{0xAA}, 32)

// testPASEIterations is a low iteration count suitable for tests.
const testPASEIterations = 1000

func TestPASEHandshakeSuccess(t *testing.T) {
	passcode := uint32(20202021) // default Matter test passcode

	initiator := NewPASEInitiator(passcode, 1)
	responder := NewPASEResponder(passcode, testPASESalt, testPASEIterations, 2)

	// Step 1: Initiator generates PBKDFParamRequest.
	reqBytes, err := initiator.GeneratePBKDFParamRequest()
	if err != nil {
		t.Fatalf("GeneratePBKDFParamRequest failed: %v", err)
	}
	if len(reqBytes) == 0 {
		t.Fatal("PBKDFParamRequest is empty")
	}

	// Step 2: Responder processes request, generates response.
	respBytes, err := responder.ProcessPBKDFParamRequest(reqBytes)
	if err != nil {
		t.Fatalf("ProcessPBKDFParamRequest failed: %v", err)
	}
	if len(respBytes) == 0 {
		t.Fatal("PBKDFParamResponse is empty")
	}

	// Step 3: Initiator processes response, generates PAKE1.
	pake1Bytes, peerSessionID, err := initiator.ProcessPBKDFParamResponse(reqBytes, respBytes)
	if err != nil {
		t.Fatalf("ProcessPBKDFParamResponse failed: %v", err)
	}
	if peerSessionID != 2 {
		t.Errorf("peer session ID = %d, want 2", peerSessionID)
	}
	if len(pake1Bytes) == 0 {
		t.Fatal("PAKE1 is empty")
	}

	// Step 4: Responder processes PAKE1, generates PAKE2.
	pake2Bytes, err := responder.ProcessPAKE1(pake1Bytes)
	if err != nil {
		t.Fatalf("ProcessPAKE1 failed: %v", err)
	}
	if len(pake2Bytes) == 0 {
		t.Fatal("PAKE2 is empty")
	}

	// Step 5: Initiator processes PAKE2, generates PAKE3.
	pake3Bytes, err := initiator.ProcessPAKE2(pake2Bytes)
	if err != nil {
		t.Fatalf("ProcessPAKE2 failed: %v", err)
	}
	if len(pake3Bytes) == 0 {
		t.Fatal("PAKE3 is empty")
	}

	// Step 6: Responder verifies PAKE3.
	if err := responder.ProcessPAKE3(pake3Bytes); err != nil {
		t.Fatalf("ProcessPAKE3 failed: %v", err)
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
	if len(iKeys.AttestationChallenge) != AttestationChallengeLength {
		t.Errorf("AttestationChallenge length = %d, want %d", len(iKeys.AttestationChallenge), AttestationChallengeLength)
	}
}

func TestPASEHandshakeWrongPasscode(t *testing.T) {
	initiatorPasscode := uint32(20202021)
	responderPasscode := uint32(99999999)

	initiator := NewPASEInitiator(initiatorPasscode, 1)
	responder := NewPASEResponder(responderPasscode, testPASESalt, testPASEIterations, 2)

	reqBytes, err := initiator.GeneratePBKDFParamRequest()
	if err != nil {
		t.Fatalf("GeneratePBKDFParamRequest failed: %v", err)
	}

	respBytes, err := responder.ProcessPBKDFParamRequest(reqBytes)
	if err != nil {
		t.Fatalf("ProcessPBKDFParamRequest failed: %v", err)
	}

	pake1Bytes, _, err := initiator.ProcessPBKDFParamResponse(reqBytes, respBytes)
	if err != nil {
		t.Fatalf("ProcessPBKDFParamResponse failed: %v", err)
	}

	pake2Bytes, err := responder.ProcessPAKE1(pake1Bytes)
	if err != nil {
		t.Fatalf("ProcessPAKE1 failed: %v", err)
	}

	// Initiator should fail to verify the responder's confirmation
	// because the passcodes differ.
	_, err = initiator.ProcessPAKE2(pake2Bytes)
	if err == nil {
		t.Fatal("expected ProcessPAKE2 to fail with wrong passcode, but it succeeded")
	}
	t.Logf("correctly failed with: %v", err)
}

func TestPASEHandshakeTamperedPAKE2(t *testing.T) {
	passcode := uint32(20202021)

	initiator := NewPASEInitiator(passcode, 1)
	responder := NewPASEResponder(passcode, testPASESalt, testPASEIterations, 2)

	reqBytes, err := initiator.GeneratePBKDFParamRequest()
	if err != nil {
		t.Fatalf("GeneratePBKDFParamRequest failed: %v", err)
	}

	respBytes, err := responder.ProcessPBKDFParamRequest(reqBytes)
	if err != nil {
		t.Fatalf("ProcessPBKDFParamRequest failed: %v", err)
	}

	pake1Bytes, _, err := initiator.ProcessPBKDFParamResponse(reqBytes, respBytes)
	if err != nil {
		t.Fatalf("ProcessPBKDFParamResponse failed: %v", err)
	}

	pake2Bytes, err := responder.ProcessPAKE1(pake1Bytes)
	if err != nil {
		t.Fatalf("ProcessPAKE1 failed: %v", err)
	}

	// Tamper with PAKE2 by flipping a byte.
	tampered := make([]byte, len(pake2Bytes))
	copy(tampered, pake2Bytes)
	if len(tampered) > 10 {
		tampered[10] ^= 0xFF
	}

	_, err = initiator.ProcessPAKE2(tampered)
	if err == nil {
		t.Fatal("expected ProcessPAKE2 to fail with tampered data, but it succeeded")
	}
	t.Logf("correctly failed with: %v", err)
}

func TestPASEHandshakeTamperedPAKE3(t *testing.T) {
	passcode := uint32(20202021)

	initiator := NewPASEInitiator(passcode, 1)
	responder := NewPASEResponder(passcode, testPASESalt, testPASEIterations, 2)

	reqBytes, err := initiator.GeneratePBKDFParamRequest()
	if err != nil {
		t.Fatalf("GeneratePBKDFParamRequest failed: %v", err)
	}

	respBytes, err := responder.ProcessPBKDFParamRequest(reqBytes)
	if err != nil {
		t.Fatalf("ProcessPBKDFParamRequest failed: %v", err)
	}

	pake1Bytes, _, err := initiator.ProcessPBKDFParamResponse(reqBytes, respBytes)
	if err != nil {
		t.Fatalf("ProcessPBKDFParamResponse failed: %v", err)
	}

	pake2Bytes, err := responder.ProcessPAKE1(pake1Bytes)
	if err != nil {
		t.Fatalf("ProcessPAKE1 failed: %v", err)
	}

	pake3Bytes, err := initiator.ProcessPAKE2(pake2Bytes)
	if err != nil {
		t.Fatalf("ProcessPAKE2 failed: %v", err)
	}

	// Tamper with PAKE3.
	tampered := make([]byte, len(pake3Bytes))
	copy(tampered, pake3Bytes)
	if len(tampered) > 5 {
		tampered[5] ^= 0xFF
	}

	err = responder.ProcessPAKE3(tampered)
	if err == nil {
		t.Fatal("expected ProcessPAKE3 to fail with tampered data, but it succeeded")
	}
	t.Logf("correctly failed with: %v", err)
}

func TestPASEMultiplePasscodes(t *testing.T) {
	passcodes := []uint32{20202021, 12345678, 1, 99999998}

	for _, passcode := range passcodes {
		t.Run("passcode_"+itoa(passcode), func(t *testing.T) {
			initiator := NewPASEInitiator(passcode, 10)
			responder := NewPASEResponder(passcode, testPASESalt, testPASEIterations, 20)

			reqBytes, err := initiator.GeneratePBKDFParamRequest()
			if err != nil {
				t.Fatalf("GeneratePBKDFParamRequest: %v", err)
			}

			respBytes, err := responder.ProcessPBKDFParamRequest(reqBytes)
			if err != nil {
				t.Fatalf("ProcessPBKDFParamRequest: %v", err)
			}

			pake1Bytes, _, err := initiator.ProcessPBKDFParamResponse(reqBytes, respBytes)
			if err != nil {
				t.Fatalf("ProcessPBKDFParamResponse: %v", err)
			}

			pake2Bytes, err := responder.ProcessPAKE1(pake1Bytes)
			if err != nil {
				t.Fatalf("ProcessPAKE1: %v", err)
			}

			pake3Bytes, err := initiator.ProcessPAKE2(pake2Bytes)
			if err != nil {
				t.Fatalf("ProcessPAKE2: %v", err)
			}

			if err := responder.ProcessPAKE3(pake3Bytes); err != nil {
				t.Fatalf("ProcessPAKE3: %v", err)
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

func TestPASEDifferentSalts(t *testing.T) {
	passcode := uint32(20202021)
	salt1 := bytes.Repeat([]byte{0x11}, 32)
	salt2 := bytes.Repeat([]byte{0x22}, 32)

	// Run handshake with salt1.
	keys1 := runPASEHandshake(t, passcode, salt1, testPASEIterations)
	// Run handshake with salt2.
	keys2 := runPASEHandshake(t, passcode, salt2, testPASEIterations)

	// Keys should differ because salts differ.
	if bytes.Equal(keys1.I2RKey, keys2.I2RKey) {
		t.Error("different salts should produce different I2RKeys")
	}
}

func TestPASETLVRoundTrip(t *testing.T) {
	t.Run("PBKDFParamRequest", func(t *testing.T) {

		initiator := NewPASEInitiator(20202021, 42)
		reqBytes, err := initiator.GeneratePBKDFParamRequest()
		if err != nil {
			t.Fatalf("GeneratePBKDFParamRequest: %v", err)
		}

		// Verify we can unmarshal the request.
		var req PBKDFParamRequest
		if err := unmarshalTLV(reqBytes, &req); err != nil {
			t.Fatalf("unmarshal PBKDFParamRequest: %v", err)
		}

		if len(req.InitiatorRandom) != 32 {
			t.Errorf("InitiatorRandom length = %d, want 32", len(req.InitiatorRandom))
		}
		if req.InitiatorSessionID != 42 {
			t.Errorf("InitiatorSessionID = %d, want 42", req.InitiatorSessionID)
		}
		if req.PasscodeID != 0 {
			t.Errorf("PasscodeID = %d, want 0", req.PasscodeID)
		}
		if req.HasPBKDFParameters {
			t.Error("HasPBKDFParameters should be false")
		}
	})
}

// runPASEHandshake is a helper that runs a full PASE handshake and returns
// the initiator's session keys.
func runPASEHandshake(t *testing.T, passcode uint32, salt []byte, iterations uint32) *SessionKeys {
	t.Helper()

	initiator := NewPASEInitiator(passcode, 1)
	responder := NewPASEResponder(passcode, salt, iterations, 2)

	reqBytes, err := initiator.GeneratePBKDFParamRequest()
	if err != nil {
		t.Fatalf("GeneratePBKDFParamRequest: %v", err)
	}

	respBytes, err := responder.ProcessPBKDFParamRequest(reqBytes)
	if err != nil {
		t.Fatalf("ProcessPBKDFParamRequest: %v", err)
	}

	pake1Bytes, _, err := initiator.ProcessPBKDFParamResponse(reqBytes, respBytes)
	if err != nil {
		t.Fatalf("ProcessPBKDFParamResponse: %v", err)
	}

	pake2Bytes, err := responder.ProcessPAKE1(pake1Bytes)
	if err != nil {
		t.Fatalf("ProcessPAKE1: %v", err)
	}

	pake3Bytes, err := initiator.ProcessPAKE2(pake2Bytes)
	if err != nil {
		t.Fatalf("ProcessPAKE2: %v", err)
	}

	if err := responder.ProcessPAKE3(pake3Bytes); err != nil {
		t.Fatalf("ProcessPAKE3: %v", err)
	}

	return initiator.SessionKeys()
}

// itoa converts uint32 to string without importing strconv.
func itoa(n uint32) string {
	if n == 0 {
		return "0"
	}
	var buf [10]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte(n%10) + '0'
		n /= 10
	}
	return string(buf[i:])
}

// unmarshalTLV is a test helper that imports tlv.Unmarshal.
func unmarshalTLV(data []byte, v any) error {
	return tlv.Unmarshal(data, v)
}
