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
// # Wire formats (matching CHIP SDK / connectedhomeip)
//
// Capabilities request (9 bytes, written to C1):
//
//	[magic1:1][magic2:1][versions:4][mtu_lo:1][mtu_hi:1][window:1]
//
// Capabilities response (6 bytes, indicated on C2):
//
//	[magic1:1][magic2:1][selectedVersion:1][fragmentSize_lo:1][fragmentSize_hi:1][window:1]
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

// BTP data-segment header flag bits, matching CHIP SDK BtpEngine.h HeaderFlags.
const (
	// btpFlagBegin (B, bit 0) marks the first (or only) segment of a message.
	btpFlagBegin = uint8(0x01)

	// btpFlagEnd (E, bit 2) marks the last (or only) segment of a message.
	btpFlagEnd = uint8(0x04)

	// btpFlagAck (A, bit 3) signals that the AckNum byte is present.
	btpFlagAck = uint8(0x08)
)

// ─── Capabilities (handshake) message constants ─────────────────────────────

const (
	// btpCapsMagic1 and btpCapsMagic2 are the two-byte magic prefix that
	// identifies a BTP capabilities request or response. They match
	// CAPABILITIES_MSG_CHECK_BYTE_{1,2} in the CHIP SDK (BleLayer.cpp).
	btpCapsMagic1 = uint8(0x65)
	btpCapsMagic2 = uint8(0x6C)
)

// ─── BTP protocol constants ───────────────────────────────────────────────────

const (
	// btpCurrentVersion is the BTP protocol version implemented here.
	btpCurrentVersion = uint8(4)

	// btpDefaultWindowSize is the flow-control window proposed in the
	// handshake: the maximum number of unacknowledged outgoing segments.
	btpDefaultWindowSize = uint8(6)

	// btpDefaultATTMTU is the ATT MTU we advertise in the BTP Capabilities
	// Request. 247 matches the value used by matter.js and is the typical
	// negotiated MTU on modern BLE stacks. The device will respond with the
	// actual fragment size = min(deviceMTU, ourMTU) − GATT overhead.
	btpDefaultATTMTU = uint16(247)

	// btpGATTOverhead is the byte count consumed by the ATT write/indicate
	// operation header: 1 byte opcode + 2 bytes handle.
	btpGATTOverhead = uint16(3)

	// btpMinSegmentSize is the minimum BTP segment size, derived from the
	// BLE 4.0 minimum ATT MTU (23) minus GATT overhead (3) = 20 bytes.
	// This is the floor enforced during handshake negotiation.
	btpMinSegmentSize = uint16(20)

	// btpDefaultSegmentSize is the default BTP segment size used before
	// handshake negotiation completes: btpDefaultATTMTU − btpGATTOverhead.
	btpDefaultSegmentSize = btpDefaultATTMTU - btpGATTOverhead // 244 bytes

	// btpAckTimeout is the maximum time a BTP receiver may hold an
	// unacknowledged sequence number before sending a standalone ack.
	btpAckTimeout = 15 * time.Second

	// btpCapsRequestLen is the wire size of a BTP Capabilities Request (9 bytes).
	btpCapsRequestLen = 9

	// btpCapsResponseLen is the wire size of a BTP Capabilities Response (6 bytes).
	btpCapsResponseLen = 6

	// btpMaxVersionSlots is the number of 4-bit version slots in a
	// Capabilities Request (8 slots packed into 4 bytes).
	btpMaxVersionSlots = 8

	// btpMessageBufferSize is the capacity of the internal completed-message
	// channel.  BTP flow control keeps this small in practice.
	btpMessageBufferSize = 8
)

// ─── Capabilities (handshake) helpers ────────────────────────────────────────

// btpSupportedVersionsList lists the BTP versions we support, highest first.
// Each entry occupies one 4-bit nibble slot in the Capabilities Request.
// Only version 4 is advertised, matching the CHIP SDK default.
var btpSupportedVersionsList = []uint8{4}

// isBTPCapabilitiesMessage returns true if the data starts with the two-byte
// magic prefix that identifies a BTP capabilities request or response.
func isBTPCapabilitiesMessage(data []byte) bool {
	return len(data) >= 2 && data[0] == btpCapsMagic1 && data[1] == btpCapsMagic2
}

// btpHandshakeRequest constructs the 9-byte BTP Capabilities Request PDU to
// be written to characteristic C1 to initiate a BTP session.
//
// The format matches the CHIP SDK BleTransportCapabilitiesRequestMessage:
//
//	[magic1:1][magic2:1][versions:4][mtu_lo:1][mtu_hi:1][window:1]
//
// Supported versions are encoded as 4-bit nibbles: even-index slots occupy
// the low nibble, odd-index slots the high nibble (matching SetSupportedProtocolVersion).
func btpHandshakeRequest(versions []uint8, attMTU uint16, windowSize uint8) []byte {
	out := make([]byte, btpCapsRequestLen)
	out[0] = btpCapsMagic1
	out[1] = btpCapsMagic2

	// Pack version numbers into 4-bit nibbles (8 slots → 4 bytes at offset 2).
	for i := 0; i < btpMaxVersionSlots && i < len(versions); i++ {
		byteIdx := 2 + i/2
		if i%2 == 0 {
			out[byteIdx] |= versions[i] & 0x0F        // low nibble
		} else {
			out[byteIdx] |= (versions[i] & 0x0F) << 4 // high nibble
		}
	}

	binary.LittleEndian.PutUint16(out[6:8], attMTU)
	out[8] = windowSize
	return out
}

// parseBTPHandshakeResponse parses the 6-byte BTP Capabilities Response PDU
// indicated on characteristic C2 in response to a Capabilities Request.
//
// The format matches the CHIP SDK BleTransportCapabilitiesResponseMessage:
//
//	[magic1:1][magic2:1][selectedVersion:1][fragmentSize_lo:1][fragmentSize_hi:1][window:1]
//
// Returns the negotiated BTP version, the fragment size (max BTP segment
// payload), the window size, and any parsing/validation error.
func parseBTPHandshakeResponse(data []byte) (version uint8, fragmentSize uint16, windowSize uint8, err error) {
	if len(data) < btpCapsResponseLen {
		return 0, 0, 0, fmt.Errorf("btp: handshake response too short: %d bytes (want %d)",
			len(data), btpCapsResponseLen)
	}
	if data[0] != btpCapsMagic1 || data[1] != btpCapsMagic2 {
		return 0, 0, 0, fmt.Errorf("btp: handshake response bad magic (0x%02X 0x%02X, want 0x%02X 0x%02X)",
			data[0], data[1], btpCapsMagic1, btpCapsMagic2)
	}
	version = data[2] & 0x0F // lower 4 bits per CHIP SDK
	fragmentSize = binary.LittleEndian.Uint16(data[3:5])
	windowSize = data[5]

	if fragmentSize == 0 {
		return 0, 0, 0, fmt.Errorf("btp: negotiated fragment size is 0")
	}
	if windowSize == 0 {
		return 0, 0, 0, fmt.Errorf("btp: negotiated window size is 0")
	}
	return version, fragmentSize, windowSize, nil
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

// initHandshake applies BTP parameters using an ATT MTU value (from which
// the segment size is derived by subtracting the GATT overhead).
func (s *btpSession) initHandshake(version uint8, attMTU uint16, windowSize uint8) {
	seg := attMTU - btpGATTOverhead
	if seg < btpMinSegmentSize {
		seg = btpMinSegmentSize
	}
	s.initHandshakeFromResponse(version, seg, windowSize)
}

// initHandshakeFromResponse applies the BTP parameters from a Capabilities
// Response where the fragment size (max BTP segment payload) is given directly.
func (s *btpSession) initHandshakeFromResponse(version uint8, fragmentSize uint16, windowSize uint8) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.version = version
	if fragmentSize < btpMinSegmentSize {
		fragmentSize = btpMinSegmentSize
	}
	s.segmentSize = fragmentSize
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

	// Capabilities (handshake) messages must not appear in the data path.
	if isBTPCapabilitiesMessage(data) {
		return fmt.Errorf("btp: unexpected capabilities message in data path")
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
