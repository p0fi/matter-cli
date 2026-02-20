// Copyright 2026 matter-cli contributors
// SPDX-License-Identifier: Apache-2.0

package secure

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"testing"
	"time"

	"github.com/p0fi/matter-cli/internal/crypto"
	"github.com/p0fi/matter-cli/internal/protocol"
)

// ---------------------------------------------------------------------------
// Mock-transport helpers
// ---------------------------------------------------------------------------

// wirePair creates two ExchangeManagers wired so that sends from one land in
// the HandleMessage of the other.  It returns (initiatorEM, responderEM).
func wirePair() (*protocol.ExchangeManager, *protocol.ExchangeManager) {
	initEM := protocol.NewExchangeManager()
	respEM := protocol.NewExchangeManager()

	initEM.DefaultSendFunc = func(ctx context.Context, msg *protocol.Message) error {
		return respEM.HandleMessage(ctx, msg)
	}
	respEM.DefaultSendFunc = func(ctx context.Context, msg *protocol.Message) error {
		return initEM.HandleMessage(ctx, msg)
	}

	return initEM, respEM
}

// sendRespMsg sends a raw Secure Channel message from the responder side using
// sendSecureChannelMsg, which is the same helper used by EstablishPASE /
// EstablishCASE.  Keeping it as a thin wrapper makes the mock responder goroutines
// read almost identically to the real initiator code.
func sendRespMsg(ctx context.Context, exchange *protocol.Exchange, opcode byte, payload []byte) error {
	return sendSecureChannelMsg(ctx, exchange, opcode, payload)
}

// buildSuccessStatusReport returns an 8-byte StatusReport payload that encodes
// GeneralCode=0, ProtocolID=0, ProtocolCode=0 (success).
func buildSuccessStatusReport() []byte {
	buf := make([]byte, 8)
	// All zeros = success.
	return buf
}

// buildErrorStatusReport builds a StatusReport payload with the given codes.
func buildErrorStatusReport(generalCode uint16, protocolID uint32, protocolCode uint16) []byte {
	buf := make([]byte, 8)
	binary.LittleEndian.PutUint16(buf[0:2], generalCode)
	binary.LittleEndian.PutUint32(buf[2:6], protocolID)
	binary.LittleEndian.PutUint16(buf[6:8], protocolCode)
	return buf
}

// ---------------------------------------------------------------------------
// TestEstablishPASE_OverExchange
// ---------------------------------------------------------------------------

// TestEstablishPASE_OverExchange verifies the full PASE handshake (EstablishPASE)
// over two in-memory ExchangeManagers wired together to simulate a network
// connection.  This covers EstablishPASE, sendSecureChannelMsg, and the
// integration: PASE over mock transport completion criterion.
func TestEstablishPASE_OverExchange(t *testing.T) {
	const passcode = uint32(20202021)
	salt := bytes.Repeat([]byte{0xAB}, 32)
	const iterations = uint32(1000)
	const respSessionID = uint16(99)

	initEM, respEM := wirePair()

	respErrs := make(chan error, 1)

	// When respEM receives an unknown initiator exchange it runs the PASE responder.
	respEM.OnUnhandled = func(ctx context.Context, exchange *protocol.Exchange, _ *protocol.Message) error {
		// Wire the responder's send back through initEM so the initiator can
		// receive replies.
		exchange.SendFunc = func(ctx context.Context, msg *protocol.Message) error {
			return initEM.HandleMessage(ctx, msg)
		}

		go func() {
			responder := NewPASEResponder(passcode, salt, iterations, respSessionID)

			// Step 1 – receive PBKDFParamRequest (already buffered by HandleMessage).
			reqMsg, err := exchange.Receive(ctx)
			if err != nil {
				respErrs <- fmt.Errorf("responder receive PBKDFParamRequest: %w", err)
				return
			}

			respBytes, err := responder.ProcessPBKDFParamRequest(reqMsg.Payload)
			if err != nil {
				respErrs <- fmt.Errorf("responder ProcessPBKDFParamRequest: %w", err)
				return
			}

			// Step 2 – send PBKDFParamResponse.
			if err := sendRespMsg(ctx, exchange, OpcodePBKDFParamResponse, respBytes); err != nil {
				respErrs <- fmt.Errorf("responder send PBKDFParamResponse: %w", err)
				return
			}

			// Step 3 – receive PAKE1.
			pake1Msg, err := exchange.Receive(ctx)
			if err != nil {
				respErrs <- fmt.Errorf("responder receive PAKE1: %w", err)
				return
			}

			pake2Bytes, err := responder.ProcessPAKE1(pake1Msg.Payload)
			if err != nil {
				respErrs <- fmt.Errorf("responder ProcessPAKE1: %w", err)
				return
			}

			// Step 4 – send PAKE2.
			if err := sendRespMsg(ctx, exchange, OpcodePASEPake2, pake2Bytes); err != nil {
				respErrs <- fmt.Errorf("responder send PAKE2: %w", err)
				return
			}

			// Step 5 – receive PAKE3.
			pake3Msg, err := exchange.Receive(ctx)
			if err != nil {
				respErrs <- fmt.Errorf("responder receive PAKE3: %w", err)
				return
			}

			if err := responder.ProcessPAKE3(pake3Msg.Payload); err != nil {
				respErrs <- fmt.Errorf("responder ProcessPAKE3: %w", err)
				return
			}

			// Step 6 – send success StatusReport.
			if err := sendRespMsg(ctx, exchange, OpcodeStatusReport, buildSuccessStatusReport()); err != nil {
				respErrs <- fmt.Errorf("responder send StatusReport: %w", err)
				return
			}

			respErrs <- nil
		}()
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	session := &protocol.Session{ID: 5, Type: protocol.SessionUnsecured}
	exchange, err := initEM.NewExchange(ctx, session)
	if err != nil {
		t.Fatalf("NewExchange: %v", err)
	}

	keys, peerSessionID, err := EstablishPASE(ctx, exchange, passcode, 1)
	if err != nil {
		t.Fatalf("EstablishPASE: %v", err)
	}

	// Wait for the responder goroutine to finish cleanly.
	select {
	case respErr := <-respErrs:
		if respErr != nil {
			t.Fatalf("responder side error: %v", respErr)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for responder goroutine")
	}

	if peerSessionID != respSessionID {
		t.Errorf("peerSessionID = %d, want %d", peerSessionID, respSessionID)
	}
	if keys == nil {
		t.Fatal("keys should not be nil after EstablishPASE")
	}
	if len(keys.I2RKey) != SessionKeyLength {
		t.Errorf("I2RKey length = %d, want %d", len(keys.I2RKey), SessionKeyLength)
	}
	if len(keys.R2IKey) != SessionKeyLength {
		t.Errorf("R2IKey length = %d, want %d", len(keys.R2IKey), SessionKeyLength)
	}
	if len(keys.AttestationChallenge) != AttestationChallengeLength {
		t.Errorf("AttestationChallenge length = %d, want %d",
			len(keys.AttestationChallenge), AttestationChallengeLength)
	}
}

// TestEstablishPASE_WrongPasscode verifies that EstablishPASE propagates the
// SPAKE2+ confirmation failure when the responder uses a different passcode.
func TestEstablishPASE_WrongPasscode(t *testing.T) {
	const initiatorPasscode = uint32(20202021)
	const responderPasscode = uint32(99999999) // intentionally wrong
	salt := bytes.Repeat([]byte{0xCC}, 32)
	const iterations = uint32(1000)

	initEM, respEM := wirePair()
	respErrs := make(chan error, 1)

	respEM.OnUnhandled = func(ctx context.Context, exchange *protocol.Exchange, _ *protocol.Message) error {
		exchange.SendFunc = func(ctx context.Context, msg *protocol.Message) error {
			return initEM.HandleMessage(ctx, msg)
		}
		go func() {
			responder := NewPASEResponder(responderPasscode, salt, iterations, 2)

			reqMsg, err := exchange.Receive(ctx)
			if err != nil {
				respErrs <- nil // initiator already failed
				return
			}
			respBytes, err := responder.ProcessPBKDFParamRequest(reqMsg.Payload)
			if err != nil {
				respErrs <- nil
				return
			}
			if err := sendRespMsg(ctx, exchange, OpcodePBKDFParamResponse, respBytes); err != nil {
				respErrs <- nil
				return
			}
			pake1Msg, err := exchange.Receive(ctx)
			if err != nil {
				respErrs <- nil
				return
			}
			pake2Bytes, err := responder.ProcessPAKE1(pake1Msg.Payload)
			if err != nil {
				respErrs <- nil
				return
			}
			if err := sendRespMsg(ctx, exchange, OpcodePASEPake2, pake2Bytes); err != nil {
				respErrs <- nil
				return
			}
			// The initiator will not send PAKE3 because it will fail first.
			respErrs <- nil
		}()
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	session := &protocol.Session{ID: 6, Type: protocol.SessionUnsecured}
	exchange, err := initEM.NewExchange(ctx, session)
	if err != nil {
		t.Fatalf("NewExchange: %v", err)
	}

	_, _, err = EstablishPASE(ctx, exchange, initiatorPasscode, 1)
	if err == nil {
		t.Fatal("expected EstablishPASE to fail with wrong passcode")
	}
	t.Logf("correctly failed with: %v", err)

	<-respErrs // drain responder goroutine
}

// TestEstablishPASE_UnexpectedOpcode exercises the path where EstablishPASE
// receives a message with an unexpected opcode instead of the final StatusReport.
func TestEstablishPASE_UnexpectedOpcode(t *testing.T) {
	const passcode = uint32(20202021)
	salt := bytes.Repeat([]byte{0xDD}, 32)
	const iterations = uint32(1000)

	initEM, respEM := wirePair()
	respErrs := make(chan error, 1)

	respEM.OnUnhandled = func(ctx context.Context, exchange *protocol.Exchange, _ *protocol.Message) error {
		exchange.SendFunc = func(ctx context.Context, msg *protocol.Message) error {
			return initEM.HandleMessage(ctx, msg)
		}
		go func() {
			responder := NewPASEResponder(passcode, salt, iterations, 3)

			reqMsg, _ := exchange.Receive(ctx)
			respBytes, err := responder.ProcessPBKDFParamRequest(reqMsg.Payload)
			if err != nil {
				respErrs <- nil
				return
			}
			_ = sendRespMsg(ctx, exchange, OpcodePBKDFParamResponse, respBytes)

			pake1Msg, _ := exchange.Receive(ctx)
			pake2Bytes, err := responder.ProcessPAKE1(pake1Msg.Payload)
			if err != nil {
				respErrs <- nil
				return
			}
			_ = sendRespMsg(ctx, exchange, OpcodePASEPake2, pake2Bytes)

			pake3Msg, _ := exchange.Receive(ctx)
			_ = responder.ProcessPAKE3(pake3Msg.Payload)

			// Send wrong opcode instead of StatusReport.
			_ = sendRespMsg(ctx, exchange, OpcodePBKDFParamRequest, []byte{0x01})

			respErrs <- nil
		}()
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	session := &protocol.Session{ID: 7, Type: protocol.SessionUnsecured}
	exchange, _ := initEM.NewExchange(ctx, session)

	_, _, err := EstablishPASE(ctx, exchange, passcode, 1)
	if err == nil {
		t.Fatal("expected EstablishPASE to fail when StatusReport opcode is wrong")
	}
	t.Logf("correctly failed with: %v", err)
	<-respErrs
}

// ---------------------------------------------------------------------------
// TestEstablishCASE_OverExchange
// ---------------------------------------------------------------------------

// TestEstablishCASE_OverExchange tests the full CASE (SIGMA) handshake over
// two in-memory ExchangeManagers, covering EstablishCASE and its helper paths.
func TestEstablishCASE_OverExchange(t *testing.T) {
	fabricID := uint64(0x0001)
	initNodeID := uint64(0x1000)
	respNodeID := uint64(0x2000)
	const respSessionID = uint16(77)

	opts := crypto.DefaultCertificateOptions()

	rcacKey, err := crypto.GenerateKeyPair()
	if err != nil {
		t.Fatalf("generate RCAC key: %v", err)
	}
	rcacDER, err := crypto.GenerateRCAC(rcacKey, 1, fabricID, opts)
	if err != nil {
		t.Fatalf("generate RCAC: %v", err)
	}

	initNodeKey, err := crypto.GenerateKeyPair()
	if err != nil {
		t.Fatalf("generate init node key: %v", err)
	}
	initNOC, err := crypto.GenerateNOC(initNodeKey, initNodeID, fabricID, rcacDER, rcacKey, opts)
	if err != nil {
		t.Fatalf("generate init NOC: %v", err)
	}

	respNodeKey, err := crypto.GenerateKeyPair()
	if err != nil {
		t.Fatalf("generate resp node key: %v", err)
	}
	respNOC, err := crypto.GenerateNOC(respNodeKey, respNodeID, fabricID, rcacDER, rcacKey, opts)
	if err != nil {
		t.Fatalf("generate resp NOC: %v", err)
	}

	rootPubKey := crypto.PublicKeyToUncompressed(&rcacKey.PublicKey)
	ipk := bytes.Repeat([]byte{0x42}, 16)

	initEM, respEM := wirePair()
	respErrs := make(chan error, 1)

	respEM.OnUnhandled = func(ctx context.Context, exchange *protocol.Exchange, _ *protocol.Message) error {
		exchange.SendFunc = func(ctx context.Context, msg *protocol.Message) error {
			return initEM.HandleMessage(ctx, msg)
		}
		go func() {
			responder := NewCASEResponder(CASEResponderConfig{
				SessionID:  respSessionID,
				NodeKey:    respNodeKey,
				NOC:        respNOC,
				ICAC:       nil,
				IPK:        ipk,
				RootPubKey: rootPubKey,
				FabricID:   fabricID,
				NodeID:     respNodeID,
			})

			// Step 1 – receive Sigma1 (already buffered).
			sigma1Msg, err := exchange.Receive(ctx)
			if err != nil {
				respErrs <- fmt.Errorf("responder receive Sigma1: %w", err)
				return
			}

			sigma2Bytes, err := responder.ProcessSigma1(sigma1Msg.Payload)
			if err != nil {
				respErrs <- fmt.Errorf("responder ProcessSigma1: %w", err)
				return
			}

			// Step 2 – send Sigma2.
			if err := sendRespMsg(ctx, exchange, OpcodeSigma2, sigma2Bytes); err != nil {
				respErrs <- fmt.Errorf("responder send Sigma2: %w", err)
				return
			}

			// Step 3 – receive Sigma3.
			sigma3Msg, err := exchange.Receive(ctx)
			if err != nil {
				respErrs <- fmt.Errorf("responder receive Sigma3: %w", err)
				return
			}

			if err := responder.ProcessSigma3(sigma3Msg.Payload); err != nil {
				respErrs <- fmt.Errorf("responder ProcessSigma3: %w", err)
				return
			}

			// Step 4 – send success StatusReport.
			if err := sendRespMsg(ctx, exchange, OpcodeStatusReport, buildSuccessStatusReport()); err != nil {
				respErrs <- fmt.Errorf("responder send StatusReport: %w", err)
				return
			}

			respErrs <- nil
		}()
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	session := &protocol.Session{ID: 10, Type: protocol.SessionUnsecured}
	exchange, err := initEM.NewExchange(ctx, session)
	if err != nil {
		t.Fatalf("NewExchange: %v", err)
	}

	initCfg := CASEInitiatorConfig{
		SessionID:  1,
		NodeKey:    initNodeKey,
		NOC:        initNOC,
		ICAC:       nil,
		IPK:        ipk,
		RootPubKey: rootPubKey,
		FabricID:   fabricID,
		PeerNodeID: respNodeID,
	}

	keys, peerSessionID, err := EstablishCASE(ctx, exchange, initCfg)
	if err != nil {
		t.Fatalf("EstablishCASE: %v", err)
	}

	select {
	case respErr := <-respErrs:
		if respErr != nil {
			t.Fatalf("responder side error: %v", respErr)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for responder goroutine")
	}

	if peerSessionID != respSessionID {
		t.Errorf("peerSessionID = %d, want %d", peerSessionID, respSessionID)
	}
	if keys == nil {
		t.Fatal("keys should not be nil after EstablishCASE")
	}
	if len(keys.I2RKey) != SessionKeyLength {
		t.Errorf("I2RKey length = %d, want %d", len(keys.I2RKey), SessionKeyLength)
	}
	if len(keys.R2IKey) != SessionKeyLength {
		t.Errorf("R2IKey length = %d, want %d", len(keys.R2IKey), SessionKeyLength)
	}
}

// TestEstablishCASE_OverExchange_WithICAC repeats the exchange test with an
// ICAC chain to exercise that code path in EstablishCASE.
func TestEstablishCASE_OverExchange_WithICAC(t *testing.T) {
	fabricID := uint64(0x0002)
	initNodeID := uint64(0x3000)
	respNodeID := uint64(0x4000)

	opts := crypto.DefaultCertificateOptions()

	rcacKey, _ := crypto.GenerateKeyPair()
	rcacDER, _ := crypto.GenerateRCAC(rcacKey, 1, fabricID, opts)

	icacKey, _ := crypto.GenerateKeyPair()
	icacDER, _ := crypto.GenerateICAC(icacKey, 2, fabricID, rcacDER, rcacKey, opts)

	initNodeKey, _ := crypto.GenerateKeyPair()
	initNOC, _ := crypto.GenerateNOC(initNodeKey, initNodeID, fabricID, icacDER, icacKey, opts)

	respNodeKey, _ := crypto.GenerateKeyPair()
	respNOC, _ := crypto.GenerateNOC(respNodeKey, respNodeID, fabricID, icacDER, icacKey, opts)

	rootPubKey := crypto.PublicKeyToUncompressed(&rcacKey.PublicKey)
	ipk := bytes.Repeat([]byte{0x77}, 16)

	initEM, respEM := wirePair()
	respErrs := make(chan error, 1)

	respEM.OnUnhandled = func(ctx context.Context, exchange *protocol.Exchange, _ *protocol.Message) error {
		exchange.SendFunc = func(ctx context.Context, msg *protocol.Message) error {
			return initEM.HandleMessage(ctx, msg)
		}
		go func() {
			responder := NewCASEResponder(CASEResponderConfig{
				SessionID:  88,
				NodeKey:    respNodeKey,
				NOC:        respNOC,
				ICAC:       icacDER,
				IPK:        ipk,
				RootPubKey: rootPubKey,
				FabricID:   fabricID,
				NodeID:     respNodeID,
			})

			sigma1Msg, err := exchange.Receive(ctx)
			if err != nil {
				respErrs <- fmt.Errorf("receive Sigma1: %w", err)
				return
			}
			sigma2Bytes, err := responder.ProcessSigma1(sigma1Msg.Payload)
			if err != nil {
				respErrs <- fmt.Errorf("ProcessSigma1: %w", err)
				return
			}
			_ = sendRespMsg(ctx, exchange, OpcodeSigma2, sigma2Bytes)

			sigma3Msg, err := exchange.Receive(ctx)
			if err != nil {
				respErrs <- fmt.Errorf("receive Sigma3: %w", err)
				return
			}
			if err := responder.ProcessSigma3(sigma3Msg.Payload); err != nil {
				respErrs <- fmt.Errorf("ProcessSigma3: %w", err)
				return
			}
			_ = sendRespMsg(ctx, exchange, OpcodeStatusReport, buildSuccessStatusReport())
			respErrs <- nil
		}()
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	session := &protocol.Session{ID: 11, Type: protocol.SessionUnsecured}
	exchange, _ := initEM.NewExchange(ctx, session)

	keys, _, err := EstablishCASE(ctx, exchange, CASEInitiatorConfig{
		SessionID:  2,
		NodeKey:    initNodeKey,
		NOC:        initNOC,
		ICAC:       icacDER,
		IPK:        ipk,
		RootPubKey: rootPubKey,
		FabricID:   fabricID,
		PeerNodeID: respNodeID,
	})
	if err != nil {
		t.Fatalf("EstablishCASE with ICAC: %v", err)
	}
	if keys == nil {
		t.Fatal("keys should not be nil")
	}

	select {
	case respErr := <-respErrs:
		if respErr != nil {
			t.Fatalf("responder: %v", respErr)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out")
	}
}

// TestEstablishCASE_Sigma2Rejected exercises the path in EstablishCASE where
// the responder rejects Sigma1 by replying with a StatusReport instead of
// Sigma2. This also covers parseStatusReportError indirectly.
func TestEstablishCASE_Sigma2Rejected(t *testing.T) {
	opts := crypto.DefaultCertificateOptions()
	rcacKey, _ := crypto.GenerateKeyPair()
	rcacDER, _ := crypto.GenerateRCAC(rcacKey, 1, 1, opts)
	initNodeKey, _ := crypto.GenerateKeyPair()
	initNOC, _ := crypto.GenerateNOC(initNodeKey, 0x1000, 1, rcacDER, rcacKey, opts)
	rootPubKey := crypto.PublicKeyToUncompressed(&rcacKey.PublicKey)
	ipk := bytes.Repeat([]byte{0x11}, 16)

	initEM, respEM := wirePair()

	respEM.OnUnhandled = func(ctx context.Context, exchange *protocol.Exchange, _ *protocol.Message) error {
		exchange.SendFunc = func(ctx context.Context, msg *protocol.Message) error {
			return initEM.HandleMessage(ctx, msg)
		}
		go func() {
			// Drain Sigma1 and immediately reply with an error StatusReport.
			_, _ = exchange.Receive(ctx)
			_ = sendRespMsg(ctx, exchange, OpcodeStatusReport,
				buildErrorStatusReport(0x0002, 0x00000000, 0x0001))
		}()
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	session := &protocol.Session{ID: 20, Type: protocol.SessionUnsecured}
	exchange, _ := initEM.NewExchange(ctx, session)

	_, _, err := EstablishCASE(ctx, exchange, CASEInitiatorConfig{
		SessionID:  1,
		NodeKey:    initNodeKey,
		NOC:        initNOC,
		IPK:        ipk,
		RootPubKey: rootPubKey,
		FabricID:   1,
		PeerNodeID: 0x2000,
	})
	if err == nil {
		t.Fatal("expected EstablishCASE to fail when Sigma2 is replaced by StatusReport")
	}
	t.Logf("correctly failed with: %v", err)
}

// TestEstablishCASE_UnexpectedOpcodeAfterSigma3 exercises the branch in
// EstablishCASE that checks whether the final message is a StatusReport.
func TestEstablishCASE_UnexpectedOpcodeAfterSigma3(t *testing.T) {
	fabricID := uint64(0x0005)
	initNodeID := uint64(0x5000)
	respNodeID := uint64(0x6000)

	opts := crypto.DefaultCertificateOptions()
	rcacKey, _ := crypto.GenerateKeyPair()
	rcacDER, _ := crypto.GenerateRCAC(rcacKey, 1, fabricID, opts)
	initNodeKey, _ := crypto.GenerateKeyPair()
	initNOC, _ := crypto.GenerateNOC(initNodeKey, initNodeID, fabricID, rcacDER, rcacKey, opts)
	respNodeKey, _ := crypto.GenerateKeyPair()
	respNOC, _ := crypto.GenerateNOC(respNodeKey, respNodeID, fabricID, rcacDER, rcacKey, opts)
	rootPubKey := crypto.PublicKeyToUncompressed(&rcacKey.PublicKey)
	ipk := bytes.Repeat([]byte{0x33}, 16)

	initEM, respEM := wirePair()
	done := make(chan struct{})

	respEM.OnUnhandled = func(ctx context.Context, exchange *protocol.Exchange, _ *protocol.Message) error {
		exchange.SendFunc = func(ctx context.Context, msg *protocol.Message) error {
			return initEM.HandleMessage(ctx, msg)
		}
		go func() {
			defer close(done)
			responder := NewCASEResponder(CASEResponderConfig{
				SessionID:  55,
				NodeKey:    respNodeKey,
				NOC:        respNOC,
				IPK:        ipk,
				RootPubKey: rootPubKey,
				FabricID:   fabricID,
				NodeID:     respNodeID,
			})
			sigma1Msg, _ := exchange.Receive(ctx)
			sigma2Bytes, _ := responder.ProcessSigma1(sigma1Msg.Payload)
			_ = sendRespMsg(ctx, exchange, OpcodeSigma2, sigma2Bytes)
			_, _ = exchange.Receive(ctx) // drain Sigma3 but ignore
			// Send wrong opcode – not a StatusReport.
			_ = sendRespMsg(ctx, exchange, OpcodeSigma1, []byte{0x01})
		}()
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	session := &protocol.Session{ID: 21, Type: protocol.SessionUnsecured}
	exchange, _ := initEM.NewExchange(ctx, session)

	_, _, err := EstablishCASE(ctx, exchange, CASEInitiatorConfig{
		SessionID:  1,
		NodeKey:    initNodeKey,
		NOC:        initNOC,
		IPK:        ipk,
		RootPubKey: rootPubKey,
		FabricID:   fabricID,
		PeerNodeID: respNodeID,
	})
	if err == nil {
		t.Fatal("expected EstablishCASE to fail when final response is not StatusReport")
	}
	t.Logf("correctly failed with: %v", err)
	<-done
}

// ---------------------------------------------------------------------------
// TestParseStatusReportError
// ---------------------------------------------------------------------------

// TestParseStatusReportError exercises all branches of the unexported
// parseStatusReportError helper directly.
func TestParseStatusReportError(t *testing.T) {
	t.Run("success codes still returns an error describing them", func(t *testing.T) {
		payload := buildSuccessStatusReport() // all zeros
		err := parseStatusReportError(payload, "test context")
		if err == nil {
			t.Fatal("parseStatusReportError should always return a non-nil error")
		}
		t.Logf("error: %v", err)
	})

	t.Run("non-zero codes appear in the error message", func(t *testing.T) {
		payload := buildErrorStatusReport(0x0002, 0x00000030, 0x00C3)
		err := parseStatusReportError(payload, "sigma2 rejected")
		if err == nil {
			t.Fatal("expected error")
		}
		msg := err.Error()
		// Verify the context string and all three code values appear.
		for _, want := range []string{"sigma2 rejected", "0x0002", "0x00000030", "0x00C3"} {
			// Use a simple contains check.
			if !containsStr(msg, want) {
				t.Errorf("error message %q does not contain %q", msg, want)
			}
		}
	})

	t.Run("payload too short returns descriptive error", func(t *testing.T) {
		for _, length := range []int{0, 1, 4, 7} {
			err := parseStatusReportError(make([]byte, length), "short payload test")
			if err == nil {
				t.Fatalf("length=%d: expected error for short payload", length)
			}
			if !containsStr(err.Error(), "malformed") {
				t.Errorf("length=%d: expected 'malformed' in error, got: %v", length, err)
			}
		}
	})

	t.Run("exactly 8 bytes parses successfully", func(t *testing.T) {
		payload := buildErrorStatusReport(0x0001, 0x00000000, 0x0005)
		err := parseStatusReportError(payload, "exact")
		if err == nil {
			t.Fatal("expected error")
		}
		// Should not say "malformed".
		if containsStr(err.Error(), "malformed") {
			t.Errorf("unexpected 'malformed' in error: %v", err)
		}
	})
}

// ---------------------------------------------------------------------------
// TestBytesEqual
// ---------------------------------------------------------------------------

// TestBytesEqual exercises all branches of the unexported bytesEqual helper.
func TestBytesEqual(t *testing.T) {
	t.Run("equal slices", func(t *testing.T) {
		if !bytesEqual([]byte{1, 2, 3}, []byte{1, 2, 3}) {
			t.Error("expected true for equal slices")
		}
	})

	t.Run("different lengths – returns false without comparing content", func(t *testing.T) {
		// This is the previously uncovered branch (len(a) != len(b)).
		if bytesEqual([]byte{1, 2}, []byte{1, 2, 3}) {
			t.Error("expected false for slices of different lengths")
		}
	})

	t.Run("same length but different content", func(t *testing.T) {
		if bytesEqual([]byte{1, 2, 3}, []byte{1, 2, 4}) {
			t.Error("expected false for slices with different content")
		}
	})

	t.Run("both nil", func(t *testing.T) {
		if !bytesEqual(nil, nil) {
			t.Error("expected true for both nil slices")
		}
	})

	t.Run("nil vs empty", func(t *testing.T) {
		if !bytesEqual(nil, []byte{}) {
			t.Error("expected true: nil and empty slice have the same length and equal content")
		}
	})

	t.Run("nil vs non-empty", func(t *testing.T) {
		if bytesEqual(nil, []byte{1}) {
			t.Error("expected false: nil vs non-empty")
		}
	})

	t.Run("empty slices", func(t *testing.T) {
		if !bytesEqual([]byte{}, []byte{}) {
			t.Error("expected true for two empty slices")
		}
	})
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// containsStr reports whether sub is a substring of s.
func containsStr(s, sub string) bool {
	if len(sub) == 0 {
		return true
	}
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// Context-cancellation / receive-error paths in EstablishPASE & EstablishCASE
// ---------------------------------------------------------------------------

// TestEstablishPASE_ContextTimeout exercises the "receiving PBKDFParamResponse"
// receive-error branch of EstablishPASE.  With no responder registered, the
// initiator sends PBKDFParamRequest and then blocks on Receive until the
// context deadline fires.
func TestEstablishPASE_ContextTimeout(t *testing.T) {
	initEM := protocol.NewExchangeManager()
	// Wire sends to a respEM that has no OnUnhandled handler, so messages
	// are silently dropped and the initiator's Receive will time out.
	respEM := protocol.NewExchangeManager()
	initEM.DefaultSendFunc = func(ctx context.Context, msg *protocol.Message) error {
		return respEM.HandleMessage(ctx, msg)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()

	session := &protocol.Session{ID: 200, Type: protocol.SessionUnsecured}
	exchange, err := initEM.NewExchange(ctx, session)
	if err != nil {
		t.Fatalf("NewExchange: %v", err)
	}

	_, _, err = EstablishPASE(ctx, exchange, 20202021, 1)
	if err == nil {
		t.Fatal("expected error when context times out during PASE")
	}
	t.Logf("correctly failed with: %v", err)
}

// TestEstablishCASE_ContextTimeout exercises the "receiving Sigma2" receive-error
// branch of EstablishCASE.  With no responder registered, the initiator sends
// Sigma1 and then blocks on Receive until the context deadline fires.
func TestEstablishCASE_ContextTimeout(t *testing.T) {
	opts := crypto.DefaultCertificateOptions()
	rcacKey, _ := crypto.GenerateKeyPair()
	rcacDER, _ := crypto.GenerateRCAC(rcacKey, 1, 1, opts)
	initNodeKey, _ := crypto.GenerateKeyPair()
	initNOC, _ := crypto.GenerateNOC(initNodeKey, 0x1000, 1, rcacDER, rcacKey, opts)
	rootPubKey := crypto.PublicKeyToUncompressed(&rcacKey.PublicKey)
	ipk := bytes.Repeat([]byte{0x55}, 16)

	initEM := protocol.NewExchangeManager()
	respEM := protocol.NewExchangeManager()
	initEM.DefaultSendFunc = func(ctx context.Context, msg *protocol.Message) error {
		return respEM.HandleMessage(ctx, msg)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()

	session := &protocol.Session{ID: 201, Type: protocol.SessionUnsecured}
	exchange, err := initEM.NewExchange(ctx, session)
	if err != nil {
		t.Fatalf("NewExchange: %v", err)
	}

	_, _, err = EstablishCASE(ctx, exchange, CASEInitiatorConfig{
		SessionID:  1,
		NodeKey:    initNodeKey,
		NOC:        initNOC,
		IPK:        ipk,
		RootPubKey: rootPubKey,
		FabricID:   1,
		PeerNodeID: 0x2000,
	})
	if err == nil {
		t.Fatal("expected error when context times out during CASE")
	}
	t.Logf("correctly failed with: %v", err)
}

// TestEstablishPASE_ContextTimeoutAfterPAKE2 exercises the "receiving status
// report" receive-error branch in EstablishPASE.  The responder completes
// PAKE2 but never sends a StatusReport, so the context times out.
func TestEstablishPASE_ContextTimeoutAfterPAKE2(t *testing.T) {
	const passcode = uint32(20202021)
	salt := bytes.Repeat([]byte{0xEE}, 32)
	const iterations = uint32(1000)

	initEM, respEM := wirePair()

	respEM.OnUnhandled = func(ctx context.Context, exchange *protocol.Exchange, _ *protocol.Message) error {
		exchange.SendFunc = func(ctx context.Context, msg *protocol.Message) error {
			return initEM.HandleMessage(ctx, msg)
		}
		go func() {
			responder := NewPASEResponder(passcode, salt, iterations, 5)

			reqMsg, err := exchange.Receive(ctx)
			if err != nil {
				return
			}
			respBytes, err := responder.ProcessPBKDFParamRequest(reqMsg.Payload)
			if err != nil {
				return
			}
			_ = sendRespMsg(ctx, exchange, OpcodePBKDFParamResponse, respBytes)

			pake1Msg, err := exchange.Receive(ctx)
			if err != nil {
				return
			}
			pake2Bytes, err := responder.ProcessPAKE1(pake1Msg.Payload)
			if err != nil {
				return
			}
			_ = sendRespMsg(ctx, exchange, OpcodePASEPake2, pake2Bytes)

			pake3Msg, err := exchange.Receive(ctx)
			if err != nil {
				return
			}
			_ = responder.ProcessPAKE3(pake3Msg.Payload)

			// Deliberately do NOT send StatusReport — let the context time out.
		}()
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	session := &protocol.Session{ID: 202, Type: protocol.SessionUnsecured}
	exchange, err := initEM.NewExchange(ctx, session)
	if err != nil {
		t.Fatalf("NewExchange: %v", err)
	}

	_, _, err = EstablishPASE(ctx, exchange, passcode, 1)
	if err == nil {
		t.Fatal("expected error when StatusReport is never sent")
	}
	t.Logf("correctly failed with: %v", err)
}

// TestEstablishCASE_ContextTimeoutAfterSigma2 exercises the "receiving status
// report" receive-error branch in EstablishCASE.  The responder sends Sigma2
// but never sends a StatusReport after Sigma3.
func TestEstablishCASE_ContextTimeoutAfterSigma2(t *testing.T) {
	fabricID := uint64(0x0099)
	initNodeID := uint64(0xAAAA)
	respNodeID := uint64(0xBBBB)

	opts := crypto.DefaultCertificateOptions()
	rcacKey, _ := crypto.GenerateKeyPair()
	rcacDER, _ := crypto.GenerateRCAC(rcacKey, 1, fabricID, opts)
	initNodeKey, _ := crypto.GenerateKeyPair()
	initNOC, _ := crypto.GenerateNOC(initNodeKey, initNodeID, fabricID, rcacDER, rcacKey, opts)
	respNodeKey, _ := crypto.GenerateKeyPair()
	respNOC, _ := crypto.GenerateNOC(respNodeKey, respNodeID, fabricID, rcacDER, rcacKey, opts)
	rootPubKey := crypto.PublicKeyToUncompressed(&rcacKey.PublicKey)
	ipk := bytes.Repeat([]byte{0xCC}, 16)

	initEM, respEM := wirePair()

	respEM.OnUnhandled = func(ctx context.Context, exchange *protocol.Exchange, _ *protocol.Message) error {
		exchange.SendFunc = func(ctx context.Context, msg *protocol.Message) error {
			return initEM.HandleMessage(ctx, msg)
		}
		go func() {
			responder := NewCASEResponder(CASEResponderConfig{
				SessionID:  33,
				NodeKey:    respNodeKey,
				NOC:        respNOC,
				IPK:        ipk,
				RootPubKey: rootPubKey,
				FabricID:   fabricID,
				NodeID:     respNodeID,
			})

			sigma1Msg, err := exchange.Receive(ctx)
			if err != nil {
				return
			}
			sigma2Bytes, err := responder.ProcessSigma1(sigma1Msg.Payload)
			if err != nil {
				return
			}
			_ = sendRespMsg(ctx, exchange, OpcodeSigma2, sigma2Bytes)

			// Drain Sigma3 but never send StatusReport — let the context time out.
			_, _ = exchange.Receive(ctx)
		}()
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	session := &protocol.Session{ID: 203, Type: protocol.SessionUnsecured}
	exchange, err := initEM.NewExchange(ctx, session)
	if err != nil {
		t.Fatalf("NewExchange: %v", err)
	}

	_, _, err = EstablishCASE(ctx, exchange, CASEInitiatorConfig{
		SessionID:  1,
		NodeKey:    initNodeKey,
		NOC:        initNOC,
		IPK:        ipk,
		RootPubKey: rootPubKey,
		FabricID:   fabricID,
		PeerNodeID: respNodeID,
	})
	if err == nil {
		t.Fatal("expected error when StatusReport is never sent after Sigma3")
	}
	t.Logf("correctly failed with: %v", err)
}
