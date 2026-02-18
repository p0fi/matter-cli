// Copyright 2026 matter-cli contributors
// SPDX-License-Identifier: Apache-2.0

package secure

import (
	"context"
	"crypto/ecdsa"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"fmt"

	"github.com/p0fi/matter-cli/internal/crypto"
	"github.com/p0fi/matter-cli/internal/protocol"
	"github.com/p0fi/matter-cli/internal/tlv"
)

// CASE (SIGMA) opcodes per Matter spec.
const (
	OpcodeSigma1 byte = 0x30
	OpcodeSigma2 byte = 0x31
	OpcodeSigma3 byte = 0x32
)

// CASE nonces as specified in the Matter spec (13 bytes each).
var (
	caseNonceSigma2 = []byte("NCASE_Sigma2N")
	caseNonceSigma3 = []byte("NCASE_Sigma3N")
)

// Sigma1 is the first CASE message, sent by the initiator.
type Sigma1 struct {
	InitiatorRandom    []byte `tlv:"1,octets"`
	InitiatorSessionID uint16 `tlv:"2,uint"`
	DestinationID      []byte `tlv:"3,octets"`
	InitiatorEphPubKey []byte `tlv:"4,octets"`
}

// Sigma2 is the second CASE message, sent by the responder.
type Sigma2 struct {
	ResponderRandom    []byte `tlv:"1,octets"`
	ResponderSessionID uint16 `tlv:"2,uint"`
	ResponderEphPubKey []byte `tlv:"3,octets"`
	Encrypted2         []byte `tlv:"4,octets"`
}

// Sigma3 is the third CASE message, sent by the initiator.
type Sigma3 struct {
	Encrypted3 []byte `tlv:"1,octets"`
}

// Sigma2TBSData is the to-be-signed data for Sigma2.
type Sigma2TBSData struct {
	ResponderNOC       []byte `tlv:"1,octets"`
	ResponderICAC      []byte `tlv:"2,octets"`
	ResponderEphPubKey []byte `tlv:"3,octets"`
	InitiatorEphPubKey []byte `tlv:"4,octets"`
}

// Sigma2TBEData is the to-be-encrypted data for Sigma2.
type Sigma2TBEData struct {
	ResponderNOC  []byte `tlv:"1,octets"`
	ResponderICAC []byte `tlv:"2,octets"`
	Signature     []byte `tlv:"3,octets"`
	ResumptionID  []byte `tlv:"4,octets"`
}

// Sigma3TBSData is the to-be-signed data for Sigma3.
type Sigma3TBSData struct {
	InitiatorNOC       []byte `tlv:"1,octets"`
	InitiatorICAC      []byte `tlv:"2,octets"`
	InitiatorEphPubKey []byte `tlv:"3,octets"`
	ResponderEphPubKey []byte `tlv:"4,octets"`
}

// Sigma3TBEData is the to-be-encrypted data for Sigma3.
type Sigma3TBEData struct {
	InitiatorNOC  []byte `tlv:"1,octets"`
	InitiatorICAC []byte `tlv:"2,octets"`
	Signature     []byte `tlv:"3,octets"`
}

// CASEInitiator implements the commissioner (initiator) side of the CASE protocol.
type CASEInitiator struct {
	sessionID  uint16
	nodeKey    *ecdsa.PrivateKey
	noc        []byte // Matter TLV-encoded NOC
	icac       []byte // Matter TLV-encoded ICAC (optional, can be nil)
	ipk        []byte // Operational Identity Protection Key (16 bytes)
	rootPubKey []byte // uncompressed root public key
	fabricID   uint64
	peerNodeID uint64

	// State accumulated during the handshake.
	ephPrivKey         *ecdsa.PrivateKey
	ephPubKeyBytes     []byte
	initiatorRandom    []byte
	sharedSecret       []byte
	sessionKeys        *SessionKeys
	responderEphPubKey []byte
	sigma1Bytes        []byte // raw Sigma1 payload for transcript hashing
	sigma2Bytes        []byte // raw Sigma2 payload for transcript hashing
	sigma3Bytes        []byte // raw Sigma3 payload for transcript hashing
}

// CASEInitiatorConfig holds configuration for creating a CASEInitiator.
type CASEInitiatorConfig struct {
	SessionID  uint16
	NodeKey    *ecdsa.PrivateKey
	NOC        []byte
	ICAC       []byte
	IPK        []byte
	RootPubKey []byte
	FabricID   uint64
	PeerNodeID uint64
}

// NewCASEInitiator creates a new CASE initiator.
func NewCASEInitiator(cfg CASEInitiatorConfig) *CASEInitiator {
	return &CASEInitiator{
		sessionID:  cfg.SessionID,
		nodeKey:    cfg.NodeKey,
		noc:        cfg.NOC,
		icac:       cfg.ICAC,
		ipk:        cfg.IPK,
		rootPubKey: cfg.RootPubKey,
		fabricID:   cfg.FabricID,
		peerNodeID: cfg.PeerNodeID,
	}
}

// GenerateSigma1 creates the first CASE message (Sigma1).
func (c *CASEInitiator) GenerateSigma1() ([]byte, error) {
	// Generate ephemeral key pair.
	ephKey, err := crypto.GenerateKeyPair()
	if err != nil {
		return nil, fmt.Errorf("secure: generating ephemeral key: %w", err)
	}
	c.ephPrivKey = ephKey
	c.ephPubKeyBytes = crypto.PublicKeyToUncompressed(&ephKey.PublicKey)

	// Generate initiator random.
	c.initiatorRandom = make([]byte, 32)
	if _, err := rand.Read(c.initiatorRandom); err != nil {
		return nil, fmt.Errorf("secure: generating initiator random: %w", err)
	}

	// Compute DestinationID = HMAC-SHA256(IPK, initiatorRandom || rootPubKey || fabricID_LE || nodeID_LE).
	destID, err := c.computeDestinationID()
	if err != nil {
		return nil, fmt.Errorf("secure: computing destination ID: %w", err)
	}

	sigma1 := Sigma1{
		InitiatorRandom:    c.initiatorRandom,
		InitiatorSessionID: c.sessionID,
		DestinationID:      destID,
		InitiatorEphPubKey: c.ephPubKeyBytes,
	}

	data, err := tlv.Marshal(sigma1)
	if err != nil {
		return nil, fmt.Errorf("secure: marshaling Sigma1: %w", err)
	}

	c.sigma1Bytes = data
	return data, nil
}

// computeDestinationID computes the HMAC-SHA256 destination identifier.
func (c *CASEInitiator) computeDestinationID() ([]byte, error) {
	mac := hmac.New(sha256.New, c.ipk)

	// Write initiatorRandom.
	mac.Write(c.initiatorRandom)
	// Write rootPubKey.
	mac.Write(c.rootPubKey)
	// Write fabricID as little-endian uint64.
	var buf [8]byte
	binary.LittleEndian.PutUint64(buf[:], c.fabricID)
	mac.Write(buf[:])
	// Write peerNodeID as little-endian uint64.
	binary.LittleEndian.PutUint64(buf[:], c.peerNodeID)
	mac.Write(buf[:])

	return mac.Sum(nil), nil
}

// ProcessSigma2 handles the responder's Sigma2 message.
// It verifies the responder's identity and derives the Sigma2 decryption key.
func (c *CASEInitiator) ProcessSigma2(sigma2Bytes []byte) error {
	c.sigma2Bytes = sigma2Bytes

	var sigma2 Sigma2
	if err := tlv.Unmarshal(sigma2Bytes, &sigma2); err != nil {
		return fmt.Errorf("secure: unmarshaling Sigma2: %w", err)
	}

	c.responderEphPubKey = sigma2.ResponderEphPubKey

	// Compute ECDH shared secret.
	respPubKey, err := crypto.PublicKeyFromUncompressed(sigma2.ResponderEphPubKey)
	if err != nil {
		return fmt.Errorf("secure: parsing responder ephemeral public key: %w", err)
	}

	sharedSecret, err := crypto.ECDH(c.ephPrivKey, respPubKey)
	if err != nil {
		return fmt.Errorf("secure: computing ECDH shared secret: %w", err)
	}
	c.sharedSecret = sharedSecret

	// Derive S2K: HKDF(IKM=sharedSecret, Salt=sigma2Salt, Info="Sigma2", L=16)
	// sigma2Salt = concat(IPK, responderRandom, responderEphPubKey, SHA256(sigma1Bytes))
	sigma1Hash := sha256.Sum256(c.sigma1Bytes)
	sigma2Salt := concat(c.ipk, sigma2.ResponderRandom, sigma2.ResponderEphPubKey, sigma1Hash[:])

	s2k, err := crypto.HKDFSHA256(sharedSecret, sigma2Salt, []byte("Sigma2"), 16)
	if err != nil {
		return fmt.Errorf("secure: deriving S2K: %w", err)
	}

	// Decrypt the TBE data.
	tbeBytes, err := crypto.AESCCMDecrypt(s2k, caseNonceSigma2, sigma2.Encrypted2, []byte{})
	if err != nil {
		return fmt.Errorf("secure: decrypting Sigma2 TBE: %w", err)
	}

	var tbe Sigma2TBEData
	if err := tlv.Unmarshal(tbeBytes, &tbe); err != nil {
		return fmt.Errorf("secure: unmarshaling Sigma2 TBE: %w", err)
	}

	// Verify the responder's signature over TBS data.
	tbs := Sigma2TBSData{
		ResponderNOC:       tbe.ResponderNOC,
		ResponderICAC:      tbe.ResponderICAC,
		ResponderEphPubKey: sigma2.ResponderEphPubKey,
		InitiatorEphPubKey: c.ephPubKeyBytes,
	}
	tbsBytes, err := tlv.Marshal(tbs)
	if err != nil {
		return fmt.Errorf("secure: marshaling Sigma2 TBS: %w", err)
	}

	respPubKeyFromNOC, err := crypto.ExtractPublicKeyFromTLV(tbe.ResponderNOC)
	if err != nil {
		return fmt.Errorf("secure: extracting public key from responder NOC: %w", err)
	}

	if !crypto.VerifyECDSARaw(respPubKeyFromNOC, tbsBytes, tbe.Signature) {
		return fmt.Errorf("secure: Sigma2 signature verification failed")
	}

	return nil
}

// GenerateSigma3 creates the third CASE message (Sigma3) and derives session keys.
func (c *CASEInitiator) GenerateSigma3() ([]byte, error) {
	// Build TBS data.
	tbs := Sigma3TBSData{
		InitiatorNOC:       c.noc,
		InitiatorICAC:      c.icac,
		InitiatorEphPubKey: c.ephPubKeyBytes,
		ResponderEphPubKey: c.responderEphPubKey,
	}
	tbsBytes, err := tlv.Marshal(tbs)
	if err != nil {
		return nil, fmt.Errorf("secure: marshaling Sigma3 TBS: %w", err)
	}

	// Sign the TBS data with our node key (raw r||s format for Matter).
	signature, err := crypto.SignECDSARaw(c.nodeKey, tbsBytes)
	if err != nil {
		return nil, fmt.Errorf("secure: signing Sigma3 TBS: %w", err)
	}

	// Build TBE data.
	tbe := Sigma3TBEData{
		InitiatorNOC:  c.noc,
		InitiatorICAC: c.icac,
		Signature:     signature,
	}
	tbeBytes, err := tlv.Marshal(tbe)
	if err != nil {
		return nil, fmt.Errorf("secure: marshaling Sigma3 TBE: %w", err)
	}

	// Derive S3K: HKDF(IKM=sharedSecret, Salt=sigma3Salt, Info="Sigma3", L=16)
	// sigma3Salt = concat(IPK, SHA256(sigma1Bytes || sigma2Bytes))
	transcriptHash := hashTranscript(c.sigma1Bytes, c.sigma2Bytes)
	sigma3Salt := concat(c.ipk, transcriptHash[:])

	s3k, err := crypto.HKDFSHA256(c.sharedSecret, sigma3Salt, []byte("Sigma3"), 16)
	if err != nil {
		return nil, fmt.Errorf("secure: deriving S3K: %w", err)
	}

	encrypted, err := crypto.AESCCMEncrypt(s3k, caseNonceSigma3, tbeBytes, []byte{})
	if err != nil {
		return nil, fmt.Errorf("secure: encrypting Sigma3 TBE: %w", err)
	}

	sigma3 := Sigma3{Encrypted3: encrypted}
	data, err := tlv.Marshal(sigma3)
	if err != nil {
		return nil, fmt.Errorf("secure: marshaling Sigma3: %w", err)
	}

	c.sigma3Bytes = data

	// Derive session keys: HKDF(IKM=sharedSecret, Salt=sessionSalt, Info="SessionKeys", L=48)
	// sessionSalt = concat(IPK, SHA256(sigma1Bytes || sigma2Bytes || sigma3Bytes))
	sessionTranscriptHash := hashTranscript(c.sigma1Bytes, c.sigma2Bytes, data)
	sessionSalt := concat(c.ipk, sessionTranscriptHash[:])

	keys, err := crypto.HKDFSHA256(c.sharedSecret, sessionSalt, sessionKeysInfo, DerivedKeyMaterialLength)
	if err != nil {
		return nil, fmt.Errorf("secure: deriving session keys: %w", err)
	}
	c.sessionKeys = &SessionKeys{
		I2RKey:               keys[0:SessionKeyLength],
		R2IKey:               keys[SessionKeyLength : 2*SessionKeyLength],
		AttestationChallenge: keys[2*SessionKeyLength : DerivedKeyMaterialLength],
	}

	return data, nil
}

// SessionKeys returns the derived session keys after a successful handshake.
func (c *CASEInitiator) SessionKeys() *SessionKeys {
	return c.sessionKeys
}

// CASEResponder implements the device (responder) side of the CASE protocol.
// This is provided for testing purposes.
type CASEResponder struct {
	sessionID  uint16
	nodeKey    *ecdsa.PrivateKey
	noc        []byte
	icac       []byte
	ipk        []byte
	rootPubKey []byte
	fabricID   uint64
	nodeID     uint64

	// State accumulated during the handshake.
	ephPrivKey         *ecdsa.PrivateKey
	ephPubKeyBytes     []byte
	initiatorEphPubKey []byte
	sharedSecret       []byte
	sessionKeys        *SessionKeys
	sigma1Bytes        []byte
	sigma2Bytes        []byte
}

// CASEResponderConfig holds configuration for creating a CASEResponder.
type CASEResponderConfig struct {
	SessionID  uint16
	NodeKey    *ecdsa.PrivateKey
	NOC        []byte
	ICAC       []byte
	IPK        []byte
	RootPubKey []byte
	FabricID   uint64
	NodeID     uint64
}

// NewCASEResponder creates a new CASE responder.
func NewCASEResponder(cfg CASEResponderConfig) *CASEResponder {
	return &CASEResponder{
		sessionID:  cfg.SessionID,
		nodeKey:    cfg.NodeKey,
		noc:        cfg.NOC,
		icac:       cfg.ICAC,
		ipk:        cfg.IPK,
		rootPubKey: cfg.RootPubKey,
		fabricID:   cfg.FabricID,
		nodeID:     cfg.NodeID,
	}
}

// ProcessSigma1 handles the initiator's Sigma1 message and produces the Sigma2 message.
func (r *CASEResponder) ProcessSigma1(sigma1Bytes []byte) ([]byte, error) {
	r.sigma1Bytes = sigma1Bytes

	var sigma1 Sigma1
	if err := tlv.Unmarshal(sigma1Bytes, &sigma1); err != nil {
		return nil, fmt.Errorf("secure: unmarshaling Sigma1: %w", err)
	}

	r.initiatorEphPubKey = sigma1.InitiatorEphPubKey

	// Generate ephemeral key pair.
	ephKey, err := crypto.GenerateKeyPair()
	if err != nil {
		return nil, fmt.Errorf("secure: generating ephemeral key: %w", err)
	}
	r.ephPrivKey = ephKey
	r.ephPubKeyBytes = crypto.PublicKeyToUncompressed(&ephKey.PublicKey)

	// Compute ECDH shared secret.
	initiatorPubKey, err := crypto.PublicKeyFromUncompressed(sigma1.InitiatorEphPubKey)
	if err != nil {
		return nil, fmt.Errorf("secure: parsing initiator ephemeral public key: %w", err)
	}

	sharedSecret, err := crypto.ECDH(r.ephPrivKey, initiatorPubKey)
	if err != nil {
		return nil, fmt.Errorf("secure: computing ECDH shared secret: %w", err)
	}
	r.sharedSecret = sharedSecret

	// Generate responder random.
	respRandom := make([]byte, 32)
	if _, err := rand.Read(respRandom); err != nil {
		return nil, fmt.Errorf("secure: generating responder random: %w", err)
	}

	// Build TBS data for Sigma2.
	tbs := Sigma2TBSData{
		ResponderNOC:       r.noc,
		ResponderICAC:      r.icac,
		ResponderEphPubKey: r.ephPubKeyBytes,
		InitiatorEphPubKey: sigma1.InitiatorEphPubKey,
	}
	tbsBytes, err := tlv.Marshal(tbs)
	if err != nil {
		return nil, fmt.Errorf("secure: marshaling Sigma2 TBS: %w", err)
	}

	// Sign TBS (raw r||s format for Matter).
	signature, err := crypto.SignECDSARaw(r.nodeKey, tbsBytes)
	if err != nil {
		return nil, fmt.Errorf("secure: signing Sigma2 TBS: %w", err)
	}

	// Build TBE data.
	tbe := Sigma2TBEData{
		ResponderNOC:  r.noc,
		ResponderICAC: r.icac,
		Signature:     signature,
	}
	tbeBytes, err := tlv.Marshal(tbe)
	if err != nil {
		return nil, fmt.Errorf("secure: marshaling Sigma2 TBE: %w", err)
	}

	// Derive S2K: HKDF(IKM=sharedSecret, Salt=sigma2Salt, Info="Sigma2", L=16)
	// sigma2Salt = concat(IPK, responderRandom, responderEphPubKey, SHA256(sigma1Bytes))
	sigma1Hash := sha256.Sum256(sigma1Bytes)
	sigma2Salt := concat(r.ipk, respRandom, r.ephPubKeyBytes, sigma1Hash[:])

	s2k, err := crypto.HKDFSHA256(sharedSecret, sigma2Salt, []byte("Sigma2"), 16)
	if err != nil {
		return nil, fmt.Errorf("secure: deriving S2K: %w", err)
	}

	encrypted, err := crypto.AESCCMEncrypt(s2k, caseNonceSigma2, tbeBytes, []byte{})
	if err != nil {
		return nil, fmt.Errorf("secure: encrypting Sigma2 TBE: %w", err)
	}

	sigma2 := Sigma2{
		ResponderRandom:    respRandom,
		ResponderSessionID: r.sessionID,
		ResponderEphPubKey: r.ephPubKeyBytes,
		Encrypted2:         encrypted,
	}

	data, err := tlv.Marshal(sigma2)
	if err != nil {
		return nil, fmt.Errorf("secure: marshaling Sigma2: %w", err)
	}

	r.sigma2Bytes = data
	return data, nil
}

// ProcessSigma3 handles the initiator's Sigma3 message and verifies the initiator's identity.
func (r *CASEResponder) ProcessSigma3(sigma3Bytes []byte) error {
	var sigma3 Sigma3
	if err := tlv.Unmarshal(sigma3Bytes, &sigma3); err != nil {
		return fmt.Errorf("secure: unmarshaling Sigma3: %w", err)
	}

	// Derive S3K: HKDF(IKM=sharedSecret, Salt=sigma3Salt, Info="Sigma3", L=16)
	// sigma3Salt = concat(IPK, SHA256(sigma1Bytes || sigma2Bytes))
	transcriptHash := hashTranscript(r.sigma1Bytes, r.sigma2Bytes)
	sigma3Salt := concat(r.ipk, transcriptHash[:])

	s3k, err := crypto.HKDFSHA256(r.sharedSecret, sigma3Salt, []byte("Sigma3"), 16)
	if err != nil {
		return fmt.Errorf("secure: deriving S3K: %w", err)
	}

	tbeBytes, err := crypto.AESCCMDecrypt(s3k, caseNonceSigma3, sigma3.Encrypted3, []byte{})
	if err != nil {
		return fmt.Errorf("secure: decrypting Sigma3 TBE: %w", err)
	}

	var tbe Sigma3TBEData
	if err := tlv.Unmarshal(tbeBytes, &tbe); err != nil {
		return fmt.Errorf("secure: unmarshaling Sigma3 TBE: %w", err)
	}

	// Reconstruct TBS and verify signature.
	tbs := Sigma3TBSData{
		InitiatorNOC:       tbe.InitiatorNOC,
		InitiatorICAC:      tbe.InitiatorICAC,
		InitiatorEphPubKey: r.initiatorEphPubKey,
		ResponderEphPubKey: r.ephPubKeyBytes,
	}
	tbsBytes, err := tlv.Marshal(tbs)
	if err != nil {
		return fmt.Errorf("secure: marshaling Sigma3 TBS: %w", err)
	}

	initiatorPubKey, err := crypto.ExtractPublicKeyFromTLV(tbe.InitiatorNOC)
	if err != nil {
		return fmt.Errorf("secure: extracting public key from initiator NOC: %w", err)
	}

	if !crypto.VerifyECDSARaw(initiatorPubKey, tbsBytes, tbe.Signature) {
		return fmt.Errorf("secure: Sigma3 signature verification failed")
	}

	// Derive session keys: HKDF(IKM=sharedSecret, Salt=sessionSalt, Info="SessionKeys", L=48)
	// sessionSalt = concat(IPK, SHA256(sigma1Bytes || sigma2Bytes || sigma3Bytes))
	sessionTranscriptHash := hashTranscript(r.sigma1Bytes, r.sigma2Bytes, sigma3Bytes)
	sessionSalt := concat(r.ipk, sessionTranscriptHash[:])

	keys, err := crypto.HKDFSHA256(r.sharedSecret, sessionSalt, sessionKeysInfo, DerivedKeyMaterialLength)
	if err != nil {
		return fmt.Errorf("secure: deriving session keys: %w", err)
	}
	r.sessionKeys = &SessionKeys{
		I2RKey:               keys[0:SessionKeyLength],
		R2IKey:               keys[SessionKeyLength : 2*SessionKeyLength],
		AttestationChallenge: keys[2*SessionKeyLength : DerivedKeyMaterialLength],
	}

	return nil
}

// SessionKeys returns the derived session keys after a successful handshake.
func (r *CASEResponder) SessionKeys() *SessionKeys {
	return r.sessionKeys
}

// EstablishCASE performs the full CASE handshake over a protocol Exchange.
// It acts as the initiator. On success, it returns the established session keys
// and the peer's session ID.
func EstablishCASE(ctx context.Context, exchange *protocol.Exchange, cfg CASEInitiatorConfig) (*SessionKeys, uint16, error) {
	initiator := NewCASEInitiator(cfg)

	// Step 1: Generate and send Sigma1.
	sigma1Bytes, err := initiator.GenerateSigma1()
	if err != nil {
		return nil, 0, fmt.Errorf("secure: generating Sigma1: %w", err)
	}

	if err := sendSecureChannelMsg(ctx, exchange, OpcodeSigma1, sigma1Bytes); err != nil {
		return nil, 0, fmt.Errorf("secure: sending Sigma1: %w", err)
	}

	// Step 2: Receive Sigma2 (or StatusReport on failure).
	sigma2Msg, err := exchange.Receive(ctx)
	if err != nil {
		return nil, 0, fmt.Errorf("secure: receiving Sigma2: %w", err)
	}

	// Check for StatusReport error (e.g. device doesn't recognize our DestinationID).
	if sigma2Msg.Protocol.ProtocolOpcode == OpcodeStatusReport {
		return nil, 0, parseStatusReportError(sigma2Msg.Payload, "CASE Sigma2 rejected")
	}

	if err := initiator.ProcessSigma2(sigma2Msg.Payload); err != nil {
		return nil, 0, fmt.Errorf("secure: processing Sigma2: %w", err)
	}

	// Step 3: Generate and send Sigma3 (also derives session keys).
	sigma3Bytes, err := initiator.GenerateSigma3()
	if err != nil {
		return nil, 0, fmt.Errorf("secure: generating Sigma3: %w", err)
	}

	if err := sendSecureChannelMsg(ctx, exchange, OpcodeSigma3, sigma3Bytes); err != nil {
		return nil, 0, fmt.Errorf("secure: sending Sigma3: %w", err)
	}

	// Step 4: Receive StatusReport.
	statusMsg, err := exchange.Receive(ctx)
	if err != nil {
		return nil, 0, fmt.Errorf("secure: receiving status report: %w", err)
	}

	if statusMsg.Protocol.ProtocolOpcode != OpcodeStatusReport {
		return nil, 0, fmt.Errorf("secure: expected status report, got opcode 0x%02x", statusMsg.Protocol.ProtocolOpcode)
	}

	// Parse the responder session ID from the Sigma2 message.
	var sigma2 Sigma2
	if err := tlv.Unmarshal(sigma2Msg.Payload, &sigma2); err != nil {
		return nil, 0, fmt.Errorf("secure: re-parsing Sigma2 for session ID: %w", err)
	}

	return initiator.SessionKeys(), sigma2.ResponderSessionID, nil
}

// ComputeDestinationID computes the CASE destination identifier using HMAC-SHA256.
// This is exported for use by other packages that need to compute destination IDs.
func ComputeDestinationID(ipk, initiatorRandom, rootPubKey []byte, fabricID, nodeID uint64) []byte {
	mac := hmac.New(sha256.New, ipk)
	mac.Write(initiatorRandom)
	mac.Write(rootPubKey)

	var buf [8]byte
	binary.LittleEndian.PutUint64(buf[:], fabricID)
	mac.Write(buf[:])
	binary.LittleEndian.PutUint64(buf[:], nodeID)
	mac.Write(buf[:])

	return mac.Sum(nil)
}

// hashTranscript computes SHA-256 of the concatenation of the provided byte slices.
func hashTranscript(parts ...[]byte) [32]byte {
	h := sha256.New()
	for _, p := range parts {
		h.Write(p)
	}
	var out [32]byte
	copy(out[:], h.Sum(nil))
	return out
}

// concat concatenates multiple byte slices into one.
func concat(parts ...[]byte) []byte {
	total := 0
	for _, p := range parts {
		total += len(p)
	}
	out := make([]byte, 0, total)
	for _, p := range parts {
		out = append(out, p...)
	}
	return out
}
