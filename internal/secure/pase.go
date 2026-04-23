// Copyright 2026 matter-cli contributors
// SPDX-License-Identifier: Apache-2.0

package secure

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"log/slog"

	"github.com/p0fi/matter-cli/internal/crypto"
	"github.com/p0fi/matter-cli/internal/protocol"
	"github.com/p0fi/matter-cli/internal/tlv"
)

// kSpake2pContext is the Matter-specified SPAKE2+ context string that seeds the
// commissioning hash before the PBKDFParamRequest and PBKDFParamResponse TLV
// bytes are added.
const kSpake2pContext = "CHIP PAKE V1 Commissioning"

// Secure Channel protocol ID and PASE opcodes per Matter spec.
const (
	ProtocolIDSecureChannel uint16 = 0x0000

	OpcodePBKDFParamRequest  byte = 0x20
	OpcodePBKDFParamResponse byte = 0x21
	OpcodePASEPake1          byte = 0x22
	OpcodePASEPake2          byte = 0x23
	OpcodePASEPake3          byte = 0x24
	OpcodeStatusReport       byte = 0x40
)

// PBKDFParamRequest is the first message in the PASE handshake,
// sent by the initiator (commissioner) to request PBKDF parameters.
type PBKDFParamRequest struct {
	InitiatorRandom    []byte `tlv:"1,octets"`
	InitiatorSessionID uint16 `tlv:"2,uint"`
	PasscodeID         uint16 `tlv:"3,uint"`
	HasPBKDFParameters bool   `tlv:"4,bool"`
}

// PBKDFParamResponse is the second message in the PASE handshake,
// sent by the responder (device) with PBKDF parameters.
type PBKDFParamResponse struct {
	InitiatorRandom    []byte      `tlv:"1,octets"`
	ResponderRandom    []byte      `tlv:"2,octets"`
	ResponderSessionID uint16      `tlv:"3,uint"`
	PBKDFParameters    PBKDFParams `tlv:"4,struct"`
}

// PBKDFParams contains the PBKDF2 parameters (iterations and salt).
type PBKDFParams struct {
	Iterations uint32 `tlv:"1,uint"`
	Salt       []byte `tlv:"2,octets"`
}

// PAKE1 is the third message in the PASE handshake (initiator's SPAKE2+ public share).
type PAKE1 struct {
	PA []byte `tlv:"1,octets"`
}

// PAKE2 is the fourth message in the PASE handshake (responder's public share and confirmation).
type PAKE2 struct {
	PB []byte `tlv:"1,octets"`
	CB []byte `tlv:"2,octets"`
}

// PAKE3 is the fifth message in the PASE handshake (initiator's confirmation).
type PAKE3 struct {
	CA []byte `tlv:"1,octets"`
}

// StatusReport is a general status message in the secure channel protocol.
type StatusReport struct {
	GeneralCode  uint16
	ProtocolID   uint32
	ProtocolCode uint16
}

// Status codes for PASE.
const (
	StatusSuccess        uint16 = 0x0000
	StatusFailure        uint16 = 0x0001
	StatusSessionEstReqd uint16 = 0x0004
	GeneralCodeSuccess   uint16 = 0x0000
	GeneralCodeFailure   uint16 = 0x0001
)

// PASEInitiator implements the commissioner (prover) side of the PASE protocol.
type PASEInitiator struct {
	passcode  uint32
	sessionID uint16

	// State accumulated during the handshake.
	initiatorRandom []byte
	spakeContext     []byte // pbkdfReqBytes || pbkdfRespBytes
	prover           *crypto.SPAKE2PProver
	sessionKeys      *SessionKeys
}

// NewPASEInitiator creates a new PASE initiator (commissioner side) for the given
// passcode and local session ID.
func NewPASEInitiator(passcode uint32, sessionID uint16) *PASEInitiator {
	return &PASEInitiator{
		passcode:  passcode,
		sessionID: sessionID,
	}
}

// GeneratePBKDFParamRequest creates the first PASE message (PBKDFParamRequest).
func (p *PASEInitiator) GeneratePBKDFParamRequest() ([]byte, error) {
	random := make([]byte, 32)
	if _, err := rand.Read(random); err != nil {
		return nil, fmt.Errorf("secure: generating initiator random: %w", err)
	}
	p.initiatorRandom = random

	req := PBKDFParamRequest{
		InitiatorRandom:    random,
		InitiatorSessionID: p.sessionID,
		PasscodeID:         0,
		HasPBKDFParameters: false,
	}

	data, err := tlv.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("secure: marshaling PBKDFParamRequest: %w", err)
	}
	return data, nil
}

// ProcessPBKDFParamResponse handles the responder's PBKDFParamResponse and
// produces the PAKE1 message containing the SPAKE2+ public share pA.
// It returns the PAKE1 TLV bytes and the peer session ID.
func (p *PASEInitiator) ProcessPBKDFParamResponse(reqBytes, respBytes []byte) (pake1Bytes []byte, peerSessionID uint16, err error) {
	var resp PBKDFParamResponse
	if err := tlv.Unmarshal(respBytes, &resp); err != nil {
		return nil, 0, fmt.Errorf("secure: unmarshaling PBKDFParamResponse: %w", err)
	}

	// Verify the initiator random was echoed back.
	if len(resp.InitiatorRandom) != 32 || !bytesEqual(resp.InitiatorRandom, p.initiatorRandom) {
		return nil, 0, fmt.Errorf("secure: PBKDFParamResponse initiator random mismatch")
	}

	// Build the SPAKE2+ context as SHA-256("CHIP PAKE V1 Commissioning" || reqBytes || respBytes).
	// The Matter/CHIP SDK hashes the context string and both parameter messages
	// into a single 32-byte digest before passing it to SPAKE2+.
	ctxHash := sha256.New()
	ctxHash.Write([]byte(kSpake2pContext))
	ctxHash.Write(reqBytes)
	ctxHash.Write(respBytes)
	p.spakeContext = ctxHash.Sum(nil)

	// Derive SPAKE2+ w0 and w1 from the passcode.
	w0, w1, err := crypto.DeriveSPAKE2PValues(
		p.passcode,
		resp.PBKDFParameters.Salt,
		int(resp.PBKDFParameters.Iterations),
	)
	if err != nil {
		return nil, 0, fmt.Errorf("secure: deriving SPAKE2+ values: %w", err)
	}

	prover, err := crypto.NewSPAKE2PProver(w0, w1)
	if err != nil {
		return nil, 0, fmt.Errorf("secure: creating SPAKE2+ prover: %w", err)
	}
	p.prover = prover

	pA, err := prover.ComputePublicShare()
	if err != nil {
		return nil, 0, fmt.Errorf("secure: computing SPAKE2+ public share: %w", err)
	}

	pake1 := PAKE1{PA: pA}
	pake1Data, err := tlv.Marshal(pake1)
	if err != nil {
		return nil, 0, fmt.Errorf("secure: marshaling PAKE1: %w", err)
	}

	return pake1Data, resp.ResponderSessionID, nil
}

// ProcessPAKE2 handles the responder's PAKE2 message and produces the PAKE3
// message containing the initiator's confirmation value cA.
// It also derives the session keys.
func (p *PASEInitiator) ProcessPAKE2(pake2Bytes []byte) (pake3Bytes []byte, err error) {
	var pake2 PAKE2
	if err := tlv.Unmarshal(pake2Bytes, &pake2); err != nil {
		return nil, fmt.Errorf("secure: unmarshaling PAKE2: %w", err)
	}

	// Compute the shared secret and confirmations.
	Ke, cA, expectedCB, err := p.prover.ComputeSecretAndConfirm(
		p.spakeContext,
		[]byte{}, // idProver (empty for Matter PASE)
		[]byte{}, // idVerifier (empty for Matter PASE)
		pake2.PB,
	)
	if err != nil {
		return nil, fmt.Errorf("secure: computing SPAKE2+ secret: %w", err)
	}

	// Verify the responder's confirmation.
	if !crypto.VerifyConfirmation(expectedCB, pake2.CB) {
		return nil, fmt.Errorf("secure: PAKE2 confirmation verification failed")
	}

	// Derive session keys from Ke.
	keys, err := DeriveSessionKeys(Ke)
	if err != nil {
		return nil, fmt.Errorf("secure: deriving session keys from Ke: %w", err)
	}
	p.sessionKeys = keys

	// Build PAKE3 with our confirmation.
	pake3 := PAKE3{CA: cA}
	pake3Data, err := tlv.Marshal(pake3)
	if err != nil {
		return nil, fmt.Errorf("secure: marshaling PAKE3: %w", err)
	}

	return pake3Data, nil
}

// SessionKeys returns the derived session keys after a successful handshake.
func (p *PASEInitiator) SessionKeys() *SessionKeys {
	return p.sessionKeys
}

// PASEResponder implements the device (verifier) side of the PASE protocol.
// This is provided for testing purposes and for devices that might be emulated.
type PASEResponder struct {
	passcode   uint32
	salt       []byte
	iterations uint32
	sessionID  uint16

	// State accumulated during the handshake.
	spakeContext []byte
	verifier     *crypto.SPAKE2PVerifier
	expectedCA   []byte
	sessionKeys  *SessionKeys
}

// NewPASEResponder creates a new PASE responder (device side) for the given
// passcode, salt, iterations, and local session ID.
func NewPASEResponder(passcode uint32, salt []byte, iterations uint32, sessionID uint16) *PASEResponder {
	return &PASEResponder{
		passcode:   passcode,
		salt:       salt,
		iterations: iterations,
		sessionID:  sessionID,
	}
}

// ProcessPBKDFParamRequest handles the initiator's PBKDFParamRequest and
// produces the PBKDFParamResponse message.
func (r *PASEResponder) ProcessPBKDFParamRequest(reqBytes []byte) (respBytes []byte, err error) {
	var req PBKDFParamRequest
	if err := tlv.Unmarshal(reqBytes, &req); err != nil {
		return nil, fmt.Errorf("secure: unmarshaling PBKDFParamRequest: %w", err)
	}

	respRandom := make([]byte, 32)
	if _, err := rand.Read(respRandom); err != nil {
		return nil, fmt.Errorf("secure: generating responder random: %w", err)
	}

	resp := PBKDFParamResponse{
		InitiatorRandom:    req.InitiatorRandom,
		ResponderRandom:    respRandom,
		ResponderSessionID: r.sessionID,
		PBKDFParameters: PBKDFParams{
			Iterations: r.iterations,
			Salt:       r.salt,
		},
	}

	respData, err := tlv.Marshal(resp)
	if err != nil {
		return nil, fmt.Errorf("secure: marshaling PBKDFParamResponse: %w", err)
	}

	// Build the SPAKE2+ context as SHA-256("CHIP PAKE V1 Commissioning" || reqBytes || respData).
	ctxHash := sha256.New()
	ctxHash.Write([]byte(kSpake2pContext))
	ctxHash.Write(reqBytes)
	ctxHash.Write(respData)
	r.spakeContext = ctxHash.Sum(nil)

	return respData, nil
}

// ProcessPAKE1 handles the initiator's PAKE1 message and produces the PAKE2
// message containing the responder's public share and confirmation.
func (r *PASEResponder) ProcessPAKE1(pake1Bytes []byte) (pake2Bytes []byte, err error) {
	var pake1 PAKE1
	if err := tlv.Unmarshal(pake1Bytes, &pake1); err != nil {
		return nil, fmt.Errorf("secure: unmarshaling PAKE1: %w", err)
	}

	// Derive w0 and w1 from passcode.
	w0, w1, err := crypto.DeriveSPAKE2PValues(r.passcode, r.salt, int(r.iterations))
	if err != nil {
		return nil, fmt.Errorf("secure: deriving SPAKE2+ values: %w", err)
	}

	// Compute L = w1 * G for the verifier.
	L := crypto.ComputeL(w1)

	verifier, err := crypto.NewSPAKE2PVerifier(w0, L)
	if err != nil {
		return nil, fmt.Errorf("secure: creating SPAKE2+ verifier: %w", err)
	}
	r.verifier = verifier

	pB, err := verifier.ComputePublicShare()
	if err != nil {
		return nil, fmt.Errorf("secure: computing verifier public share: %w", err)
	}

	// Compute shared secret and confirmations.
	Ke, cB, expectedCA, err := verifier.ComputeSecretAndConfirm(
		r.spakeContext,
		[]byte{}, // idProver
		[]byte{}, // idVerifier
		pake1.PA,
	)
	if err != nil {
		return nil, fmt.Errorf("secure: computing verifier secret: %w", err)
	}

	r.expectedCA = expectedCA

	// Derive session keys.
	keys, err := DeriveSessionKeys(Ke)
	if err != nil {
		return nil, fmt.Errorf("secure: deriving session keys: %w", err)
	}
	r.sessionKeys = keys

	pake2 := PAKE2{PB: pB, CB: cB}
	pake2Data, err := tlv.Marshal(pake2)
	if err != nil {
		return nil, fmt.Errorf("secure: marshaling PAKE2: %w", err)
	}

	return pake2Data, nil
}

// ProcessPAKE3 verifies the initiator's confirmation from the PAKE3 message.
func (r *PASEResponder) ProcessPAKE3(pake3Bytes []byte) error {
	var pake3 PAKE3
	if err := tlv.Unmarshal(pake3Bytes, &pake3); err != nil {
		return fmt.Errorf("secure: unmarshaling PAKE3: %w", err)
	}

	if !crypto.VerifyConfirmation(r.expectedCA, pake3.CA) {
		return fmt.Errorf("secure: PAKE3 confirmation verification failed")
	}

	return nil
}

// SessionKeys returns the derived session keys after a successful handshake.
func (r *PASEResponder) SessionKeys() *SessionKeys {
	return r.sessionKeys
}

// EstablishPASE performs the full PASE handshake over a protocol Exchange.
// It acts as the initiator (commissioner). proposedSessionID is the session ID
// the initiator proposes for the new secure session. On success, it returns the
// established session keys and the peer's session ID.
func EstablishPASE(ctx context.Context, exchange *protocol.Exchange, passcode uint32, proposedSessionID uint16) (*SessionKeys, uint16, error) {
	initiator := NewPASEInitiator(passcode, proposedSessionID)

	// Step 1: Generate and send PBKDFParamRequest.
	reqBytes, err := initiator.GeneratePBKDFParamRequest()
	if err != nil {
		return nil, 0, fmt.Errorf("secure: generating PBKDFParamRequest: %w", err)
	}

	slog.Debug("PASE: → PBKDFParamRequest", "sessionID", proposedSessionID)
	if err := sendSecureChannelMsg(ctx, exchange, OpcodePBKDFParamRequest, reqBytes); err != nil {
		return nil, 0, fmt.Errorf("secure: sending PBKDFParamRequest: %w", err)
	}

	// Step 2: Receive PBKDFParamResponse.
	slog.Debug("PASE: waiting for PBKDFParamResponse...")
	respMsg, err := exchange.Receive(ctx)
	if err != nil {
		return nil, 0, fmt.Errorf("secure: receiving PBKDFParamResponse: %w", err)
	}

	slog.Debug("PASE: ← PBKDFParamResponse received, running PBKDF2 + SPAKE2+")
	pake1Bytes, peerSessionID, err := initiator.ProcessPBKDFParamResponse(reqBytes, respMsg.Payload)
	if err != nil {
		return nil, 0, fmt.Errorf("secure: processing PBKDFParamResponse: %w", err)
	}

	// Step 3: Send PAKE1.
	slog.Debug("PASE: → PAKE1 (SPAKE2+ pA)")
	if err := sendSecureChannelMsg(ctx, exchange, OpcodePASEPake1, pake1Bytes); err != nil {
		return nil, 0, fmt.Errorf("secure: sending PAKE1: %w", err)
	}

	// Step 4: Receive PAKE2.
	slog.Debug("PASE: waiting for PAKE2...")
	pake2Msg, err := exchange.Receive(ctx)
	if err != nil {
		return nil, 0, fmt.Errorf("secure: receiving PAKE2: %w", err)
	}

	slog.Debug("PASE: ← PAKE2 received (SPAKE2+ pB+cB), verifying")
	pake3Bytes, err := initiator.ProcessPAKE2(pake2Msg.Payload)
	if err != nil {
		return nil, 0, fmt.Errorf("secure: processing PAKE2: %w", err)
	}

	// Step 5: Send PAKE3.
	slog.Debug("PASE: → PAKE3 (SPAKE2+ cA), session keys derived")
	if err := sendSecureChannelMsg(ctx, exchange, OpcodePASEPake3, pake3Bytes); err != nil {
		return nil, 0, fmt.Errorf("secure: sending PAKE3: %w", err)
	}

	// Step 6: Receive StatusReport.
	slog.Debug("PASE: waiting for status report...")
	statusMsg, err := exchange.Receive(ctx)
	if err != nil {
		return nil, 0, fmt.Errorf("secure: receiving status report: %w", err)
	}

	if statusMsg.Protocol.ProtocolOpcode != OpcodeStatusReport {
		return nil, 0, fmt.Errorf("secure: expected status report, got opcode 0x%02x", statusMsg.Protocol.ProtocolOpcode)
	}

	slog.Debug("PASE: session established", "peerSessionID", peerSessionID)
	return initiator.SessionKeys(), peerSessionID, nil
}

// sendSecureChannelMsg builds a secure channel protocol message and sends it via the exchange.
func sendSecureChannelMsg(ctx context.Context, exchange *protocol.Exchange, opcode byte, payload []byte) error {
	msg := &protocol.Message{
		Protocol: protocol.ProtocolHeader{
			ProtocolOpcode: opcode,
			ProtocolID:     ProtocolIDSecureChannel,
			ExchangeFlags:  protocol.ExFlagReliable,
		},
		Payload: payload,
	}
	return exchange.Send(ctx, msg)
}

// parseStatusReportError parses a StatusReport payload and returns a descriptive error.
// StatusReport payload format: GeneralCode(2 LE) + ProtocolId(4 LE) + ProtocolCode(2 LE).
func parseStatusReportError(payload []byte, context string) error {
	if len(payload) < 8 {
		return fmt.Errorf("secure: %s: malformed StatusReport (len=%d)", context, len(payload))
	}
	generalCode := binary.LittleEndian.Uint16(payload[0:2])
	protocolID := binary.LittleEndian.Uint32(payload[2:6])
	protocolCode := binary.LittleEndian.Uint16(payload[6:8])
	return fmt.Errorf("secure: %s: StatusReport general=0x%04X protocol=0x%08X code=0x%04X",
		context, generalCode, protocolID, protocolCode)
}

// bytesEqual compares two byte slices for equality.
func bytesEqual(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
