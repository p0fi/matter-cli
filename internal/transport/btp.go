// Copyright 2026 matter-cli contributors
// SPDX-License-Identifier: Apache-2.0

// Package transport provides network transport abstractions for the Matter
// protocol. This file implements the BLE Transport Protocol (BTP) engine as
// specified in Matter Specification §4.15.
//
// BTP provides reliable, ordered delivery of Matter messages over two GATT
// characteristics (C1 and C2) with its own segmentation/reassembly and
// handshake. Once BTP is established, BLEConn presents a datagram interface
// identical to UDPConn, allowing the rest of the Matter stack to operate
// without modification.
//
// # Wire formats
//
// Handshake request (6 bytes, written to C1):
//
//	[flags:1][opcode:1][supportedVersions:1][attMTU_lo:1][attMTU_hi:1][window:1]
//
// Handshake response (6 bytes, indicated on C2):
//
//	[flags:1][opcode:1][selectedVersion:1][attMTU_lo:1][attMTU_hi:1][window:1]
//
// Data segment PDU:
//
//	[flags:1] [ackNum:1 if A] [seqNum:1 always] [msgLen_lo:1 msgLen_hi:1 if B] [payload…]
package transport

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"sync"
	"time"
)

// ─── BTP header flag bits ─────────────────────────────────────────────────────

// BTP header flag constants as defined in Matter spec §4.15.3.3.
// These occupy bits [4:0] of the first byte of every BTP PDU.
const (
	// btpFlagHandshake (H, bit 0) is set in handshake management frames only.
	btpFlagHandshake = uint8(1 << 0)

	// btpFlagManage (M, bit 1) indicates a management opcode follows.
	// Per the spec this bit is reserved and must be 0 in data frames; it is
	// set together with H in handshake frames.
	btpFlagManage = uint8(1 << 1)

	// btpFlagAck (A, bit 2) signals that the AckNum byte is present.
	btpFlagAck = uint8(1 << 2)

	// btpFlagEnd (E, bit 3) marks the last (or only) segment of a message.
	btpFlagEnd = uint8(1 << 3)

	// btpFlagBegin (B, bit 4) marks the first (or only) segment of a message.
	btpFlagBegin = uint8(1 << 4)
)

// ─── Handshake opcodes ────────────────────────────────────────────────────────

const (
	// btpOpcodeHandshakeRequest is the management opcode for a BTP
	// HandshakeRequest frame (commissioner → device, written to C1).
	btpOpcodeHandshakeRequest = uint8(0x6C)

	// btpOpcodeHandshakeResponse is the management opcode for a BTP
	// HandshakeResponse frame (device → commissioner, indicated on C2).
	btpOpcodeHandshakeResponse = uint8(0x65)
)

// ─── BTP protocol constants ───────────────────────────────────────────────────

const (
	// btpCurrentVersion is the BTP protocol version implemented here.
	btpCurrentVersion = uint8(4)

	// btpDefaultWindowSize is the flow-control window proposed in the
	// handshake: the maximum number of unacknowledged outgoing segments.
	btpDefaultWindowSize = uint8(6)

	// btpDefaultATTMTU is the minimum ATT MTU for BLE 4.0 (23 bytes).
	btpDefaultATTMTU = uint16(23)

	// btpGATTOverhead is the byte count consumed by the ATT write/indicate
	// operation header: 1 byte opcode + 2 bytes handle.
	btpGATTOverhead = uint16(3)

	// btpDefaultSegmentSize is the default BTP segment size when no
	// handshake has been performed: btpDefaultATTMTU − btpGATTOverhead.
	btpDefaultSegmentSize = btpDefaultATTMTU - btpGATTOverhead // 20 bytes

	// btpSupportedVersions is the version-capability bitmask sent in the
	// HandshakeRequest: bit N = 1 means BTP version N is supported.
	// We support versions 3 and 4.
	btpSupportedVersions = uint8((1 << 3) | (1 << 4)) // 0x18

	// btpAckTimeout is the maximum time a BTP receiver may hold an
	// unacknowledged sequence number before sending a standalone ack.
	btpAckTimeout = 15 * time.Second

	// btpHandshakeFrameLen is the fixed wire size of both handshake frames.
	btpHandshakeFrameLen = 6

	// btpMessageBufferSize is the capacity of the internal completed-message
	// channel.  BTP flow control keeps this small in practice.
	btpMessageBufferSize = 8
)

// ─── Handshake helpers ────────────────────────────────────────────────────────

// btpHandshakeRequest constructs the 6-byte BTP HandshakeRequest PDU to be
// written to characteristic C1 to initiate a BTP session.
//
//   - supportedVersions: bitmask where bit N = 1 means version N is supported
//     (use btpSupportedVersions for the default).
//   - attMTU: the maximum ATT MTU the commissioner can handle (uint16 LE).
//   - windowSize: the flow-control window the commissioner proposes.
func btpHandshakeRequest(supportedVersions uint8, attMTU uint16, windowSize uint8) []byte {
	out := make([]byte, btpHandshakeFrameLen)
	out[0] = btpFlagHandshake
	out[1] = btpOpcodeHandshakeRequest
	out[2] = supportedVersions
	binary.LittleEndian.PutUint16(out[3:5], attMTU)
	out[5] = windowSize
	return out
}

// parseBTPHandshakeResponse parses the 6-byte BTP HandshakeResponse PDU
// indicated on characteristic C2 in response to a HandshakeRequest.
//
// Returns the negotiated BTP version, the agreed ATT MTU, the window size,
// and any parsing/validation error.
func parseBTPHandshakeResponse(data []byte) (version uint8, attMTU uint16, windowSize uint8, err error) {
	if len(data) < btpHandshakeFrameLen {
		return 0, 0, 0, fmt.Errorf("btp: handshake response too short: %d bytes (want %d)",
			len(data), btpHandshakeFrameLen)
	}
	flags := data[0]
	if flags&btpFlagHandshake == 0 {
		return 0, 0, 0, fmt.Errorf("btp: handshake response missing H flag (flags=0x%02X)", flags)
	}
	opcode := data[1]
	if opcode != btpOpcodeHandshakeResponse {
		return 0, 0, 0, fmt.Errorf("btp: unexpected handshake opcode 0x%02X (want 0x%02X)",
			opcode, btpOpcodeHandshakeResponse)
	}
	version = data[2]
	attMTU = binary.LittleEndian.Uint16(data[3:5])
	windowSize = data[5]

	if attMTU < btpDefaultATTMTU {
		return 0, 0, 0, fmt.Errorf("btp: negotiated ATT MTU %d is below minimum %d",
			attMTU, btpDefaultATTMTU)
	}
	if windowSize == 0 {
		return 0, 0, 0, fmt.Errorf("btp: negotiated window size is 0")
	}
	return version, attMTU, windowSize, nil
}

// ─── BTP session ─────────────────────────────────────────────────────────────

// btpSession manages the full BTP protocol state machine for one BLE
// connection: outgoing segmentation with flow-control, incoming reassembly,
// and acknowledgement tracking.
//
// All methods are safe for concurrent use by multiple goroutines.
type btpSession struct {
	// ── Negotiated parameters (set once by initHandshake) ──────────────────
	segmentSize uint16 // max bytes per GATT write = ATT_MTU − btpGATTOverhead
	version     uint8
	windowSize  uint8

	// ── Mutex + condvar for TX flow-control ────────────────────────────────
	mu   sync.Mutex
	cond *sync.Cond

	// ── TX state ────────────────────────────────────────────────────────────
	localSeq   uint8 // next outgoing sequence number to assign
	txInflight uint8 // segments sent but not yet peer-acknowledged

	// ── Pending ack to piggyback on next outgoing segment ──────────────────
	hasPendingAck bool  // true when the peer has sent at least one segment we must ack
	pendingAck    uint8 // the most recent peer sequence number that needs acking

	// ── RX reassembly state ─────────────────────────────────────────────────
	rxBuf      bytes.Buffer // byte accumulator for the message under assembly
	rxExpected uint16       // total message length declared in the B-segment MsgLen field
	rxActive   bool         // true while a multi-segment reassembly is in progress

	// ── Completed inbound messages ──────────────────────────────────────────
	messages chan []byte   // fully reassembled Matter messages (read by BLEConn.Receive)
	closed   chan struct{} // closed when the session is shut down
}

// newBTPSession allocates a btpSession with safe default parameters.
// Call initHandshake with the negotiated values once the BTP handshake
// exchange is complete.
func newBTPSession() *btpSession {
	s := &btpSession{
		segmentSize: btpDefaultSegmentSize,
		version:     btpCurrentVersion,
		windowSize:  btpDefaultWindowSize,
		messages:    make(chan []byte, btpMessageBufferSize),
		closed:      make(chan struct{}),
	}
	s.cond = sync.NewCond(&s.mu)
	return s
}

// initHandshake applies the BTP parameters negotiated during the handshake
// exchange.  Must be called exactly once, before the session handles any data.
func (s *btpSession) initHandshake(version uint8, attMTU uint16, windowSize uint8) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.version = version
	seg := attMTU - btpGATTOverhead
	if seg < btpDefaultSegmentSize {
		seg = btpDefaultSegmentSize
	}
	s.segmentSize = seg
	s.windowSize = windowSize
}

// closeSession tears down the session, unblocking any goroutine waiting in
// waitCanSend and preventing further message delivery.
// It is idempotent.
func (s *btpSession) closeSession() {
	select {
	case <-s.closed:
		// already closed — nothing to do
	default:
		close(s.closed)
		s.cond.Broadcast()
	}
}

// Messages returns the read end of the completed-message channel.
// BLEConn.Receive reads from this channel to obtain fully reassembled Matter
// messages.
func (s *btpSession) Messages() <-chan []byte {
	return s.messages
}

// ─── TX: segmentation ─────────────────────────────────────────────────────────

// segment splits a complete Matter message into one or more BTP segment PDUs
// ready to be written to characteristic C1.
//
// Sequence numbers are assigned atomically from s.localSeq.  If a pending
// peer ack exists it is piggybacked onto the first segment and cleared.
//
// The caller (BLEConn.Send) is responsible for flow-control: it must call
// waitCanSend before writing each segment and markSent afterwards.
func (s *btpSession) segment(msg []byte) [][]byte {
	s.mu.Lock()
	defer s.mu.Unlock()

	totalLen := uint16(len(msg))
	var out [][]byte

	// Empty message — one segment, B=1, E=1, MsgLen=0.
	if totalLen == 0 {
		out = append(out, s.buildSegment(nil, true, true, totalLen))
		return out
	}

	offset := 0
	isFirst := true
	for offset < len(msg) {
		// Per-segment header overhead:
		//   1 byte  flags        (always)
		//   1 byte  AckNum       (only on first segment when we have a pending ack)
		//   1 byte  SeqNum       (always, per BtpEngine reference implementation)
		//   2 bytes MsgLen       (only on first segment)
		overhead := 1 + 1 // flags + SeqNum
		if isFirst && s.hasPendingAck {
			overhead++ // AckNum
		}
		if isFirst {
			overhead += 2 // MsgLen
		}

		maxPayload := int(s.segmentSize) - overhead
		if maxPayload < 1 {
			maxPayload = 1 // always make progress
		}

		end := offset + maxPayload
		isLast := end >= len(msg)
		if isLast {
			end = len(msg)
		}

		out = append(out, s.buildSegment(msg[offset:end], isFirst, isLast, totalLen))
		offset = end
		isFirst = false
	}

	return out
}

// buildSegment encodes a single BTP data segment.
// msgLen is only written into the wire frame when beginSeg = true.
// Caller must hold s.mu.
func (s *btpSession) buildSegment(payload []byte, beginSeg, endSeg bool, msgLen uint16) []byte {
	var buf bytes.Buffer

	// ── Flags ──
	flags := uint8(0)
	if beginSeg {
		flags |= btpFlagBegin
	}
	if endSeg {
		flags |= btpFlagEnd
	}
	// Piggyback ack only on the first segment of each outgoing message.
	piggy := s.hasPendingAck && beginSeg
	if piggy {
		flags |= btpFlagAck
	}
	buf.WriteByte(flags)

	// ── AckNum ──
	if piggy {
		buf.WriteByte(s.pendingAck)
		s.hasPendingAck = false
	}

	// ── SeqNum (always) ──
	buf.WriteByte(s.localSeq)
	s.localSeq++ // wraps naturally at 256

	// ── MsgLen (first segment only) ──
	if beginSeg {
		var lb [2]byte
		binary.LittleEndian.PutUint16(lb[:], msgLen)
		buf.Write(lb[:])
	}

	// ── Payload ──
	buf.Write(payload)

	return buf.Bytes()
}

// standaloneAck generates a BTP segment that carries only an acknowledgement
// and no payload.  It should be sent when btpAckTimeout elapses without an
// outgoing data segment to piggyback on.
//
// Returns nil when there is no pending ack to send.
func (s *btpSession) standaloneAck() []byte {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.hasPendingAck {
		return nil
	}

	// Standalone ack: A=1, AckNum, SeqNum (always present), no payload.
	seg := []byte{
		btpFlagAck,   // flags: only A bit
		s.pendingAck, // AckNum
		s.localSeq,   // SeqNum
	}
	s.localSeq++
	s.hasPendingAck = false
	return seg
}

// ─── TX: flow control ─────────────────────────────────────────────────────────

// waitCanSend blocks until the TX flow-control window has room for at least
// one more segment, the context is cancelled, or the session is closed.
//
// BLEConn.Send must call this before writing each segment to C1.
func (s *btpSession) waitCanSend(ctx context.Context) error {
	// Spawn a short-lived watcher that broadcasts on the cond when the context
	// is cancelled, so cond.Wait() wakes up and we can return ctx.Err().
	// The localDone channel is closed in defer to clean up the goroutine
	// promptly when waitCanSend returns for any other reason.
	localDone := make(chan struct{})
	defer close(localDone)

	go func() {
		select {
		case <-ctx.Done():
			s.cond.Broadcast()
		case <-localDone:
			// waitCanSend returned normally; no broadcast needed.
		}
	}()

	s.mu.Lock()
	defer s.mu.Unlock()

	for {
		// Session closed?
		select {
		case <-s.closed:
			return fmt.Errorf("btp: session closed")
		default:
		}
		// Context cancelled?
		if ctx.Err() != nil {
			return ctx.Err()
		}
		// Window open?
		if s.txInflight < s.windowSize {
			return nil
		}
		// Release mu, sleep until something changes, re-acquire.
		s.cond.Wait()
	}
}

// markSent records that one segment has been written to C1 and is now
// awaiting acknowledgement from the peer.
//
// BLEConn.Send must call this immediately after each successful C1 write.
func (s *btpSession) markSent() {
	s.mu.Lock()
	s.txInflight++
	s.mu.Unlock()
}

// ─── RX: segment handling ─────────────────────────────────────────────────────

// handleSegment processes one incoming BTP PDU received via a C2 indication.
//
// It:
//  1. Processes any piggybacked acknowledgement (updating TX flow control).
//  2. Records the peer's sequence number as a pending ack.
//  3. Reassembles multi-segment messages.
//  4. Delivers the completed Matter message to the messages channel when the
//     final segment (E=1) arrives.
func (s *btpSession) handleSegment(data []byte) error {
	if len(data) == 0 {
		return fmt.Errorf("btp: received empty segment")
	}

	flags := data[0]
	idx := 1

	// Handshake frames must not appear in the data path.
	if flags&btpFlagHandshake != 0 {
		return fmt.Errorf("btp: unexpected handshake frame in data path (flags=0x%02X)", flags)
	}

	// ── AckNum (present when A=1) ──────────────────────────────────────────
	if flags&btpFlagAck != 0 {
		if idx >= len(data) {
			return fmt.Errorf("btp: truncated segment: missing AckNum")
		}
		s.processAck(data[idx])
		idx++
	}

	// ── SeqNum (always present) ────────────────────────────────────────────
	if idx >= len(data) {
		return fmt.Errorf("btp: truncated segment: missing SeqNum")
	}
	seqNum := data[idx]
	idx++

	// Record so we can ack the peer on the next outgoing segment.
	s.mu.Lock()
	s.pendingAck = seqNum
	s.hasPendingAck = true
	s.mu.Unlock()

	// ── MsgLen (present when B=1) ──────────────────────────────────────────
	if flags&btpFlagBegin != 0 {
		if idx+2 > len(data) {
			return fmt.Errorf("btp: truncated segment: missing MsgLen")
		}
		msgLen := binary.LittleEndian.Uint16(data[idx : idx+2])
		idx += 2

		s.mu.Lock()
		if s.rxActive {
			s.mu.Unlock()
			return fmt.Errorf("btp: received B-segment while reassembly already in progress")
		}
		s.rxBuf.Reset()
		s.rxExpected = msgLen
		s.rxActive = true
		s.mu.Unlock()
	}

	// ── Require active reassembly for any segment ──────────────────────────
	s.mu.Lock()
	active := s.rxActive
	s.mu.Unlock()

	if !active {
		return fmt.Errorf(
			"btp: received non-B segment (flags=0x%02X) with no active reassembly", flags)
	}

	// ── Payload ────────────────────────────────────────────────────────────
	s.mu.Lock()
	s.rxBuf.Write(data[idx:])

	if flags&btpFlagEnd == 0 {
		// More segments to come.
		s.mu.Unlock()
		return nil
	}

	// ── E=1: reassembly complete ───────────────────────────────────────────
	assembled := make([]byte, s.rxBuf.Len())
	copy(assembled, s.rxBuf.Bytes())
	s.rxBuf.Reset()
	s.rxActive = false
	s.mu.Unlock()

	if uint16(len(assembled)) != s.rxExpected {
		return fmt.Errorf("btp: message length mismatch: reassembled %d bytes, header declared %d",
			len(assembled), s.rxExpected)
	}

	select {
	case s.messages <- assembled:
	case <-s.closed:
		return fmt.Errorf("btp: session closed during message delivery")
	}
	return nil
}

// processAck updates TX flow control in response to an acknowledgement from
// the peer.
//
// BTP uses cumulative acknowledgements: the peer acks all our segments up to
// and including ackNum.  We compute the number of newly confirmed segments
// using modular uint8 arithmetic (sequence numbers wrap at 256, and the
// window is always ≤ 255 so the difference is unambiguous).
func (s *btpSession) processAck(ackNum uint8) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.txInflight == 0 {
		return
	}

	// Oldest unacknowledged outgoing sequence number.
	// s.localSeq is the *next* seq to assign, so the last assigned was
	// localSeq-1 and the oldest in-flight is localSeq-txInflight (mod 256).
	oldest := s.localSeq - s.txInflight // uint8 wraps naturally

	// How many sequence numbers from oldest..ackNum (inclusive) were acked?
	newlyAcked := ackNum - oldest + 1 // uint8 arithmetic, wraps correctly

	// Guard against a misbehaving peer sending a bogus ackNum.
	if newlyAcked > s.txInflight {
		newlyAcked = s.txInflight
	}

	if newlyAcked > 0 {
		s.txInflight -= newlyAcked
		s.cond.Broadcast()
	}
}

// ─── Ack timeout ──────────────────────────────────────────────────────────────

// NewAckTimer returns a time.Timer that fires after btpAckTimeout.
//
// BLEConn should reset this timer whenever it successfully piggybacks an
// acknowledgement in an outgoing segment.  When the timer fires, BLEConn
// should call standaloneAck and — if the result is non-nil — write it to C1.
func (s *btpSession) NewAckTimer() *time.Timer {
	return time.NewTimer(btpAckTimeout)
}
