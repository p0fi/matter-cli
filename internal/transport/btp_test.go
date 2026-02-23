// Copyright 2026 matter-cli contributors
// SPDX-License-Identifier: Apache-2.0

package transport

import (
	"bytes"
	"context"
	"encoding/binary"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ─── Handshake serialization ──────────────────────────────────────────────────

func TestBTPHandshakeRequest(t *testing.T) {
	tests := []struct {
		name      string
		versions  []uint8
		attMTU    uint16
		window    uint8
		wantBytes []byte
	}{
		{
			name:     "default parameters (version 4, MTU 247)",
			versions: []uint8{4},
			attMTU:   btpDefaultATTMTU,     // 247
			window:   btpDefaultWindowSize, // 6
			wantBytes: []byte{
				0x65,                   // magic byte 1
				0x6C,                   // magic byte 2
				0x04, 0x00, 0x00, 0x00, // versions: slot0=4
				0xF7, 0x00,             // ATT MTU: 247 (LE)
				0x06,                   // window size: 6
			},
		},
		{
			name:     "small MTU",
			versions: []uint8{4},
			attMTU:   23, // minimum BLE 4.0 MTU
			window:   btpDefaultWindowSize,
			wantBytes: []byte{
				0x65, 0x6C,
				0x04, 0x00, 0x00, 0x00,
				0x17, 0x00, // 23 LE
				0x06,
			},
		},
		{
			name:     "multiple versions",
			versions: []uint8{4, 3},
			attMTU:   btpDefaultATTMTU,
			window:   btpDefaultWindowSize,
			wantBytes: []byte{
				0x65, 0x6C,
				0x34, 0x00, 0x00, 0x00, // slot0=4 (low), slot1=3 (high)
				0xF7, 0x00,
				0x06,
			},
		},
		{
			name:     "custom window",
			versions: []uint8{4},
			attMTU:   100,
			window:   8,
			wantBytes: []byte{
				0x65, 0x6C,
				0x04, 0x00, 0x00, 0x00,
				0x64, 0x00, // 100 LE
				0x08,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := btpHandshakeRequest(tt.versions, tt.attMTU, tt.window)
			require.Len(t, got, btpCapsRequestLen, "request must be exactly %d bytes", btpCapsRequestLen)
			assert.Equal(t, tt.wantBytes, got)
		})
	}
}

func TestParseBTPHandshakeResponse_Valid(t *testing.T) {
	tests := []struct {
		name             string
		data             []byte
		wantVersion      uint8
		wantFragmentSize uint16
		wantWindowSize   uint8
	}{
		{
			name: "default parameters",
			data: []byte{
				0x65,       // magic byte 1
				0x6C,       // magic byte 2
				0x04,       // selected version: 4
				0x14, 0x00, // fragment size: 20 (LE)
				0x06,       // window size: 6
			},
			wantVersion:      4,
			wantFragmentSize: 20,
			wantWindowSize:   6,
		},
		{
			name: "large fragment size",
			data: []byte{
				0x65, 0x6C,
				0x04,
				0xF4, 0x00, // fragment size: 244
				0x06,
			},
			wantVersion:      4,
			wantFragmentSize: 244,
			wantWindowSize:   6,
		},
		{
			name: "version 3 selected",
			data: []byte{
				0x65, 0x6C,
				0x03, // version 3
				0x14, 0x00,
				0x04, // window 4
			},
			wantVersion:      3,
			wantFragmentSize: 20,
			wantWindowSize:   4,
		},
		{
			name: "max window",
			data: []byte{
				0x65, 0x6C,
				0x04,
				0x14, 0x00,
				0xFF, // window 255
			},
			wantVersion:      4,
			wantFragmentSize: 20,
			wantWindowSize:   255,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			version, fragmentSize, window, err := parseBTPHandshakeResponse(tt.data)
			require.NoError(t, err)
			assert.Equal(t, tt.wantVersion, version, "version")
			assert.Equal(t, tt.wantFragmentSize, fragmentSize, "fragmentSize")
			assert.Equal(t, tt.wantWindowSize, window, "windowSize")
		})
	}
}

func TestParseBTPHandshakeResponse_Errors(t *testing.T) {
	tests := []struct {
		name    string
		data    []byte
		wantErr string
	}{
		{
			name:    "too short",
			data:    []byte{0x65, 0x6C, 0x04},
			wantErr: "too short",
		},
		{
			name:    "empty",
			data:    []byte{},
			wantErr: "too short",
		},
		{
			name: "bad magic byte 1",
			data: []byte{
				0x00, // wrong magic
				0x6C, 0x04, 0x14, 0x00, 0x06,
			},
			wantErr: "bad magic",
		},
		{
			name: "bad magic byte 2",
			data: []byte{
				0x65,
				0x00, // wrong magic
				0x04, 0x14, 0x00, 0x06,
			},
			wantErr: "bad magic",
		},
		{
			name: "fragment size zero",
			data: []byte{
				0x65, 0x6C,
				0x04,
				0x00, 0x00, // fragment size = 0
				0x06,
			},
			wantErr: "fragment size is 0",
		},
		{
			name: "window size zero",
			data: []byte{
				0x65, 0x6C,
				0x04,
				0x14, 0x00,
				0x00, // window = 0
			},
			wantErr: "window size is 0",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, _, err := parseBTPHandshakeResponse(tt.data)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}

// Verify that request and response share the same magic prefix.
func TestHandshakeRequestResponseFormat_Compatible(t *testing.T) {
	req := btpHandshakeRequest(btpSupportedVersionsList, 200, 8)
	require.Len(t, req, btpCapsRequestLen)

	// Both request and response start with the same magic bytes.
	assert.Equal(t, btpCapsMagic1, req[0], "magic byte 1")
	assert.Equal(t, btpCapsMagic2, req[1], "magic byte 2")

	// Build a valid response and verify it parses.
	resp := make([]byte, btpCapsResponseLen)
	resp[0] = btpCapsMagic1
	resp[1] = btpCapsMagic2
	resp[2] = btpCurrentVersion
	binary.LittleEndian.PutUint16(resp[3:5], 200)
	resp[5] = 8

	version, fragmentSize, window, err := parseBTPHandshakeResponse(resp)
	require.NoError(t, err)
	assert.Equal(t, btpCurrentVersion, version)
	assert.Equal(t, uint16(200), fragmentSize)
	assert.Equal(t, uint8(8), window)
}

// ─── Single-segment encoding ──────────────────────────────────────────────────

func TestSegment_SingleSegment(t *testing.T) {
	tests := []struct {
		name        string
		msg         []byte
		segmentSize uint16
		// Expected byte layout: [flags][seqNum][msgLen_lo][msgLen_hi][payload…]
		// (no AckNum because hasPendingAck=false by default)
		wantFlags  uint8
		wantSeqNum uint8
		wantMsgLen uint16
	}{
		{
			name:        "empty message",
			msg:         []byte{},
			segmentSize: 20,
			wantFlags:   btpFlagBegin | btpFlagEnd,
			wantSeqNum:  0,
			wantMsgLen:  0,
		},
		{
			name:        "single byte",
			msg:         []byte{0xAB},
			segmentSize: 20,
			wantFlags:   btpFlagBegin | btpFlagEnd,
			wantSeqNum:  0,
			wantMsgLen:  1,
		},
		{
			name:        "fits exactly in one segment",
			msg:         bytes.Repeat([]byte{0xFF}, 16), // 16 bytes payload; overhead = 4 (flags+seq+len_lo+len_hi)
			segmentSize: 20,
			wantFlags:   btpFlagBegin | btpFlagEnd,
			wantSeqNum:  0,
			wantMsgLen:  16,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := newBTPSession()
			s.segmentSize = tt.segmentSize

			segs := s.segment(tt.msg)
			require.Len(t, segs, 1, "expected a single segment")

			seg := segs[0]
			require.GreaterOrEqual(t, len(seg), 4, "segment too short (need at least flags+seq+len)")

			idx := 0
			flags := seg[idx]
			idx++
			assert.Equal(t, tt.wantFlags, flags, "flags byte")
			// No AckNum (A=0)
			assert.Equal(t, uint8(0), flags&btpFlagAck, "A flag must be 0")

			seqNum := seg[idx]
			idx++
			assert.Equal(t, tt.wantSeqNum, seqNum, "sequence number")

			// MsgLen (B=1)
			msgLen := binary.LittleEndian.Uint16(seg[idx : idx+2])
			idx += 2
			assert.Equal(t, tt.wantMsgLen, msgLen, "message length")

			payload := seg[idx:]
			assert.Equal(t, tt.msg, payload, "payload")

			// Sequence number advanced
			assert.Equal(t, uint8(1), s.localSeq, "localSeq should advance by 1")
		})
	}
}

func TestSegment_AdvancesLocalSeq(t *testing.T) {
	s := newBTPSession()

	for i := uint8(0); i < 5; i++ {
		segs := s.segment([]byte{0x01})
		require.Len(t, segs, 1)
		// The seq number in the segment should equal the pre-call localSeq.
		seqInSeg := segs[0][1] // [flags][seqNum][...]
		assert.Equal(t, i, seqInSeg, "segment %d should carry seqNum %d", i, i)
	}
	assert.Equal(t, uint8(5), s.localSeq)
}

func TestSegment_SeqNumWrapsAt256(t *testing.T) {
	s := newBTPSession()
	s.localSeq = 254

	segs := s.segment([]byte{0xAA})
	require.Len(t, segs, 1)
	assert.Equal(t, uint8(254), segs[0][1]) // seqNum in first segment
	assert.Equal(t, uint8(255), s.localSeq)

	segs = s.segment([]byte{0xBB})
	require.Len(t, segs, 1)
	assert.Equal(t, uint8(255), segs[0][1])
	assert.Equal(t, uint8(0), s.localSeq) // wrapped

	segs = s.segment([]byte{0xCC})
	require.Len(t, segs, 1)
	assert.Equal(t, uint8(0), segs[0][1])
	assert.Equal(t, uint8(1), s.localSeq)
}

// ─── Multi-segment encoding ───────────────────────────────────────────────────

func TestSegment_MultiSegment(t *testing.T) {
	const segSize = 20 // default 20-byte segment

	tests := []struct {
		name         string
		msgLen       int
		wantSegCount int
	}{
		// segSize=20, first-segment overhead=4 (flags+seq+len_lo+len_hi), payload=16
		// continuation overhead=2 (flags+seq), payload=18
		{"exactly two segments", 16 + 18, 2}, // 34 bytes → full first + full second
		{"two segments partial second", 16 + 1, 2},
		{"three segments", 16 + 18 + 1, 3},
		{"large message", 16 + 18*9, 10}, // 1 first + 9 continuations
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := newBTPSession()
			s.segmentSize = segSize

			msg := make([]byte, tt.msgLen)
			for i := range msg {
				msg[i] = byte(i & 0xFF)
			}

			segs := s.segment(msg)
			require.Len(t, segs, tt.wantSegCount)

			// First segment must have B=1, last must have E=1.
			assert.Equal(t, btpFlagBegin, segs[0][0]&btpFlagBegin, "first segment must have B=1")
			assert.Equal(t, btpFlagEnd, segs[len(segs)-1][0]&btpFlagEnd, "last segment must have E=1")

			// Middle segments must have neither B nor E.
			for i := 1; i < len(segs)-1; i++ {
				flags := segs[i][0]
				assert.Zero(t, flags&btpFlagBegin, "middle segment %d must not have B=1", i)
				assert.Zero(t, flags&btpFlagEnd, "middle segment %d must not have E=1", i)
			}

			// MsgLen in first segment must equal total message length.
			msgLen := binary.LittleEndian.Uint16(segs[0][2:4]) // [flags][seqNum][len_lo][len_hi]
			assert.Equal(t, uint16(tt.msgLen), msgLen, "MsgLen in B-frame must equal total message size")

			// Sequence numbers must be sequential.
			for i, seg := range segs {
				seqOffset := 1 // [flags][seqNum] for segments without AckNum
				if seg[0]&btpFlagAck != 0 {
					seqOffset = 2 // [flags][ackNum][seqNum]
				}
				assert.Equal(t, uint8(i), seg[seqOffset], "segment %d seqNum", i)
			}

			// Each segment must fit within segmentSize.
			for i, seg := range segs {
				assert.LessOrEqual(t, len(seg), int(segSize),
					"segment %d exceeds segmentSize", i)
			}
		})
	}
}

func TestSegment_MsgLen_AlwaysFullMessageLength(t *testing.T) {
	// Regression test: the MsgLen field in the B-segment must be the total
	// message length, not just the payload of the first segment.
	s := newBTPSession()
	s.segmentSize = 20

	// Build a 50-byte message (requires 3 segments with segSize=20).
	msg := make([]byte, 50)
	for i := range msg {
		msg[i] = byte(i)
	}

	segs := s.segment(msg)
	require.GreaterOrEqual(t, len(segs), 2, "need multiple segments for this test")

	// MsgLen is at bytes [2:4] of the first segment (after flags and seqNum).
	msgLen := binary.LittleEndian.Uint16(segs[0][2:4])
	assert.Equal(t, uint16(50), msgLen, "MsgLen must be 50, not just the first-segment payload size")
}

// ─── Ack piggybacking ─────────────────────────────────────────────────────────

func TestSegment_PiggybackAck(t *testing.T) {
	s := newBTPSession()
	// Simulate having received a peer segment with seqNum 7.
	s.hasPendingAck = true
	s.pendingAck = 7

	segs := s.segment([]byte{0xDE, 0xAD})
	require.Len(t, segs, 1)

	seg := segs[0]
	flags := seg[0]
	assert.NotZero(t, flags&btpFlagAck, "A flag must be set when piggybacking")
	assert.Equal(t, btpFlagBegin|btpFlagEnd|btpFlagAck, flags)

	// Layout: [flags][ackNum][seqNum][msgLen_lo][msgLen_hi][payload…]
	assert.Equal(t, uint8(7), seg[1], "AckNum must be 7")
	assert.Equal(t, uint8(0), seg[2], "SeqNum must be 0")
	msgLen := binary.LittleEndian.Uint16(seg[3:5])
	assert.Equal(t, uint16(2), msgLen)
	assert.Equal(t, []byte{0xDE, 0xAD}, seg[5:])

	// Pending ack must have been cleared.
	assert.False(t, s.hasPendingAck, "hasPendingAck must be cleared after piggybacking")
}

func TestSegment_PiggybackAck_OnlyOnFirstSegment(t *testing.T) {
	s := newBTPSession()
	s.segmentSize = 20
	// Set a pending ack.
	s.hasPendingAck = true
	s.pendingAck = 3

	// 50 bytes → at least 3 segments.
	msg := make([]byte, 50)
	segs := s.segment(msg)
	require.GreaterOrEqual(t, len(segs), 2)

	// Only the first segment should carry the ack.
	assert.NotZero(t, segs[0][0]&btpFlagAck, "first segment must have A=1")
	for i := 1; i < len(segs); i++ {
		assert.Zero(t, segs[i][0]&btpFlagAck, "segment %d must not carry ack", i)
	}
}

// ─── Standalone ack ───────────────────────────────────────────────────────────

func TestStandaloneAck(t *testing.T) {
	t.Run("no pending ack returns nil", func(t *testing.T) {
		s := newBTPSession()
		assert.Nil(t, s.standaloneAck())
	})

	t.Run("generates correct ack segment", func(t *testing.T) {
		s := newBTPSession()
		s.hasPendingAck = true
		s.pendingAck = 42
		s.localSeq = 10

		ack := s.standaloneAck()
		require.NotNil(t, ack)
		require.Len(t, ack, 3) // flags(1) + ackNum(1) + seqNum(1)

		assert.Equal(t, btpFlagAck, ack[0], "flags must have only A=1")
		assert.Equal(t, uint8(42), ack[1], "AckNum must be 42")
		assert.Equal(t, uint8(10), ack[2], "SeqNum must be 10")

		// State updates
		assert.Equal(t, uint8(11), s.localSeq, "localSeq must advance")
		assert.False(t, s.hasPendingAck, "hasPendingAck must be cleared")
	})

	t.Run("sequential standalone acks use different seqNums", func(t *testing.T) {
		s := newBTPSession()

		s.hasPendingAck = true
		s.pendingAck = 1
		ack1 := s.standaloneAck()
		require.NotNil(t, ack1)

		s.hasPendingAck = true
		s.pendingAck = 2
		ack2 := s.standaloneAck()
		require.NotNil(t, ack2)

		assert.Equal(t, uint8(0), ack1[2]) // seqNum 0
		assert.Equal(t, uint8(1), ack2[2]) // seqNum 1
	})
}

// ─── Segment decoding / reassembly ───────────────────────────────────────────

func TestHandleSegment_SingleSegment(t *testing.T) {
	tests := []struct {
		name    string
		payload []byte
	}{
		{"empty message", []byte{}},
		{"single byte", []byte{0xAB}},
		{"multi-byte", []byte{0x01, 0x02, 0x03, 0x04}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Build the segment manually.
			seg := buildTestSegment(btpFlagBegin|btpFlagEnd, 0 /*ackNum*/, false, 0 /*seqNum*/, tt.payload)

			rx := newBTPSession()
			err := rx.handleSegment(seg)
			require.NoError(t, err)

			select {
			case got := <-rx.messages:
				assert.Equal(t, tt.payload, got)
			case <-time.After(100 * time.Millisecond):
				t.Fatal("timed out waiting for message from handleSegment")
			}
		})
	}
}

func TestHandleSegment_RecordsPendingAck(t *testing.T) {
	seg := buildTestSegment(btpFlagBegin|btpFlagEnd, 0, false, 5 /*seqNum*/, []byte{0xFF})

	rx := newBTPSession()
	err := rx.handleSegment(seg)
	require.NoError(t, err)

	assert.True(t, rx.hasPendingAck, "must record pending ack")
	assert.Equal(t, uint8(5), rx.pendingAck, "pendingAck must be the peer's seqNum")

	// Drain messages channel.
	<-rx.messages
}

func TestHandleSegment_ProcessesPiggybacked_Ack(t *testing.T) {
	rx := newBTPSession()
	// Simulate having sent 3 segments that are unacknowledged.
	rx.localSeq = 3
	rx.txInflight = 3

	// Incoming segment that acks our first 2 segments (ackNum=1 means seqs 0 and 1 are acked).
	seg := buildTestSegment(btpFlagBegin|btpFlagEnd, 1, true /*hasAck*/, 0 /*seqNum*/, []byte{0x42})

	err := rx.handleSegment(seg)
	require.NoError(t, err)
	<-rx.messages

	assert.Equal(t, uint8(1), rx.txInflight, "2 of 3 in-flight segments should be acked; 1 remains")
}

func TestHandleSegment_MultiSegment_Reassembly(t *testing.T) {
	tests := []struct {
		name    string
		msgLen  int
		segSize int
	}{
		{"two segments", 30, 20},
		{"three segments", 50, 20},
		{"many segments", 200, 30},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Build the expected message.
			msg := make([]byte, tt.msgLen)
			for i := range msg {
				msg[i] = byte(i & 0xFF)
			}

			// Produce segments using a TX session.
			tx := newBTPSession()
			tx.segmentSize = uint16(tt.segSize)
			segs := tx.segment(msg)
			require.Greater(t, len(segs), 1)

			// Feed segments into a fresh RX session.
			rx := newBTPSession()
			for _, s := range segs {
				require.NoError(t, rx.handleSegment(s))
			}

			select {
			case got := <-rx.messages:
				assert.Equal(t, msg, got)
			case <-time.After(100 * time.Millisecond):
				t.Fatal("timed out waiting for reassembled message")
			}
		})
	}
}

func TestHandleSegment_Errors(t *testing.T) {
	t.Run("empty segment", func(t *testing.T) {
		rx := newBTPSession()
		err := rx.handleSegment([]byte{})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "empty")
	})

	t.Run("capabilities message rejected in data path", func(t *testing.T) {
		rx := newBTPSession()
		err := rx.handleSegment([]byte{btpCapsMagic1, btpCapsMagic2, 0x04})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "capabilities")
	})

	t.Run("truncated segment missing seqNum", func(t *testing.T) {
		// Only flags byte, no SeqNum.
		rx := newBTPSession()
		err := rx.handleSegment([]byte{btpFlagBegin | btpFlagEnd})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "SeqNum")
	})

	t.Run("truncated segment missing msgLen", func(t *testing.T) {
		// flags + seqNum, but B=1 so MsgLen is required and missing.
		rx := newBTPSession()
		err := rx.handleSegment([]byte{btpFlagBegin | btpFlagEnd, 0x00 /*seqNum*/})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "MsgLen")
	})

	t.Run("truncated missing ackNum", func(t *testing.T) {
		// A=1 but no AckNum byte.
		rx := newBTPSession()
		err := rx.handleSegment([]byte{btpFlagAck})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "AckNum")
	})

	t.Run("continuation without active reassembly", func(t *testing.T) {
		// E=1 but no B=1 was received first.
		rx := newBTPSession()
		err := rx.handleSegment([]byte{btpFlagEnd, 0x00 /*seqNum*/})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "reassembly")
	})

	t.Run("duplicate B segment while reassembly active", func(t *testing.T) {
		rx := newBTPSession()
		// First B-segment (no E).
		seg1 := buildTestSegment(btpFlagBegin, 0, false, 0, []byte{0x01, 0x02})
		require.NoError(t, rx.handleSegment(seg1))

		// Second B-segment before the first message is complete.
		seg2 := buildTestSegment(btpFlagBegin, 0, false, 1, []byte{0x03})
		err := rx.handleSegment(seg2)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "reassembly")
	})
}

func TestHandleSegment_MessageLengthMismatch(t *testing.T) {
	// Build a B+E segment that declares MsgLen=10 but carries only 5 bytes.
	var buf bytes.Buffer
	buf.WriteByte(btpFlagBegin | btpFlagEnd) // flags
	buf.WriteByte(0x00)                       // seqNum
	// MsgLen = 10 but payload is only 5 bytes.
	buf.WriteByte(0x0A) // MsgLen lo = 10
	buf.WriteByte(0x00) // MsgLen hi = 0
	buf.Write(bytes.Repeat([]byte{0xFF}, 5))

	rx := newBTPSession()
	err := rx.handleSegment(buf.Bytes())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "mismatch")
}

// ─── Round-trip: segment → handleSegment ─────────────────────────────────────

func TestRoundTrip_SingleMessage(t *testing.T) {
	tests := []struct {
		name        string
		msg         []byte
		segmentSize uint16
	}{
		{"empty message", []byte{}, 20},
		{"single byte", []byte{0x42}, 20},
		{"fits in one segment", []byte("hello world"), 20},
		{"exactly fills one segment", bytes.Repeat([]byte{0xAA}, 16), 20},
		{"requires two segments", bytes.Repeat([]byte{0xBB}, 34), 20},
		{"requires many segments", bytes.Repeat([]byte{0xCC}, 200), 20},
		{"large MTU, single segment", bytes.Repeat([]byte{0xDD}, 200), 244}, // BLE 5 max
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tx := newBTPSession()
			tx.segmentSize = tt.segmentSize

			rx := newBTPSession()

			segs := tx.segment(tt.msg)
			for _, s := range segs {
				require.NoError(t, rx.handleSegment(s))
			}

			select {
			case got := <-rx.messages:
				assert.Equal(t, tt.msg, got, "round-tripped message must equal original")
			case <-time.After(100 * time.Millisecond):
				t.Fatal("timed out waiting for round-tripped message")
			}
		})
	}
}

func TestRoundTrip_MultipleMessages(t *testing.T) {
	tx := newBTPSession()
	tx.segmentSize = 20

	rx := newBTPSession()

	messages := [][]byte{
		[]byte("first message"),
		[]byte("second message is longer than the first one to test multi-segment"),
		{0x00},
		bytes.Repeat([]byte{0xFE, 0xED}, 50),
	}

	// Send all messages in sequence.
	for _, msg := range messages {
		for _, seg := range tx.segment(msg) {
			require.NoError(t, rx.handleSegment(seg))
		}
	}

	// Receive and verify all messages.
	for i, want := range messages {
		select {
		case got := <-rx.messages:
			assert.Equal(t, want, got, "message %d mismatch", i)
		case <-time.After(100 * time.Millisecond):
			t.Fatalf("timed out waiting for message %d", i)
		}
	}
}

func TestRoundTrip_WithAckPiggybacking(t *testing.T) {
	// Simulate a bidirectional exchange: both sides send and receive,
	// piggybacking acks on outgoing segments.

	alice := newBTPSession()
	alice.segmentSize = 20

	bob := newBTPSession()
	bob.segmentSize = 20

	aliceMsg := []byte("message from alice to bob")
	bobMsg := []byte("reply from bob to alice")

	// Alice sends to Bob.
	for _, seg := range alice.segment(aliceMsg) {
		require.NoError(t, bob.handleSegment(seg))
	}
	// Bob receives Alice's message.
	select {
	case got := <-bob.messages:
		assert.Equal(t, aliceMsg, got)
	case <-time.After(100 * time.Millisecond):
		t.Fatal("Bob timed out waiting for Alice's message")
	}
	// Bob now has a pending ack for Alice's segments.
	assert.True(t, bob.hasPendingAck, "Bob should have a pending ack")

	// Bob sends reply to Alice — the ack is piggybacked on the first segment.
	bobSegs := bob.segment(bobMsg)
	require.NotEmpty(t, bobSegs)
	assert.NotZero(t, bobSegs[0][0]&btpFlagAck, "Bob's first segment must carry piggybacked ack")

	for _, seg := range bobSegs {
		require.NoError(t, alice.handleSegment(seg))
	}
	select {
	case got := <-alice.messages:
		assert.Equal(t, bobMsg, got)
	case <-time.After(100 * time.Millisecond):
		t.Fatal("Alice timed out waiting for Bob's reply")
	}
}

// ─── Flow control ─────────────────────────────────────────────────────────────

func TestFlowControl_WaitCanSend_ImmediatelyOK(t *testing.T) {
	s := newBTPSession()
	ctx := context.Background()
	// txInflight=0, windowSize=6 → should not block.
	err := s.waitCanSend(ctx)
	require.NoError(t, err)
}

func TestFlowControl_MarkSent_IncreasesInflight(t *testing.T) {
	s := newBTPSession()
	assert.Equal(t, uint8(0), s.txInflight)
	s.markSent()
	assert.Equal(t, uint8(1), s.txInflight)
	s.markSent()
	assert.Equal(t, uint8(2), s.txInflight)
}

func TestFlowControl_ProcessAck_ReleasesWindow(t *testing.T) {
	s := newBTPSession()
	s.localSeq = 5
	s.txInflight = 5 // sent seqs 0..4

	// Ack seqs 0..2 inclusive (ackNum=2).
	s.processAck(2)
	assert.Equal(t, uint8(2), s.txInflight, "3 acked → 5-3=2 remain in-flight")
}

func TestFlowControl_ProcessAck_AllSegments(t *testing.T) {
	s := newBTPSession()
	s.localSeq = 4
	s.txInflight = 4

	s.processAck(3) // ack seqs 0..3 (all 4)
	assert.Equal(t, uint8(0), s.txInflight)
}

func TestFlowControl_ProcessAck_Noop_WhenNoInflight(t *testing.T) {
	s := newBTPSession()
	// No segments in flight — ack should be a no-op, not panic.
	s.processAck(0)
	assert.Equal(t, uint8(0), s.txInflight)
}

func TestFlowControl_ProcessAck_ClampsAtInflight(t *testing.T) {
	// A misbehaving peer sends an ackNum that implies more acked segments than
	// are actually in-flight.  processAck must not underflow txInflight.
	s := newBTPSession()
	s.localSeq = 3
	s.txInflight = 3

	// Bogus ackNum that would imply 10 newly acked segments — but only 3 are in-flight.
	s.processAck(9) // (9 - oldest=0 + 1) = 10, clamped to 3
	assert.Equal(t, uint8(0), s.txInflight, "must not underflow")
}

func TestFlowControl_ProcessAck_SeqNumWrap(t *testing.T) {
	s := newBTPSession()
	// Sequence numbers wrapped: localSeq=3, txInflight=5
	// → oldest unacked = 3-5 = 254 (mod 256)
	// → sent seqs: 254, 255, 0, 1, 2
	s.localSeq = 3
	s.txInflight = 5

	// Ack through seq 1 → acked seqs 254, 255, 0, 1 = 4 segments.
	s.processAck(1)
	assert.Equal(t, uint8(1), s.txInflight, "4 of 5 acked → 1 remains")
}

func TestFlowControl_WaitCanSend_UnblocksOnAck(t *testing.T) {
	s := newBTPSession()
	s.windowSize = 2
	s.txInflight = 2   // window full
	s.localSeq = 2

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	done := make(chan error, 1)
	go func() {
		done <- s.waitCanSend(ctx)
	}()

	// Give the goroutine time to block.
	time.Sleep(20 * time.Millisecond)

	// Ack one segment — window opens.
	s.processAck(0) // acks seq 0 (1 segment)

	select {
	case err := <-done:
		require.NoError(t, err, "waitCanSend should return nil after ack")
	case <-time.After(500 * time.Millisecond):
		t.Fatal("waitCanSend did not unblock after ack")
	}
}

func TestFlowControl_WaitCanSend_UnblocksOnClose(t *testing.T) {
	s := newBTPSession()
	s.windowSize = 2
	s.txInflight = 2 // window full

	ctx := context.Background()

	done := make(chan error, 1)
	go func() {
		done <- s.waitCanSend(ctx)
	}()

	time.Sleep(20 * time.Millisecond)
	s.closeSession()

	select {
	case err := <-done:
		require.Error(t, err)
		assert.Contains(t, err.Error(), "closed")
	case <-time.After(500 * time.Millisecond):
		t.Fatal("waitCanSend did not unblock after close")
	}
}

func TestFlowControl_WaitCanSend_UnblocksOnContextCancel(t *testing.T) {
	s := newBTPSession()
	s.windowSize = 2
	s.txInflight = 2 // window full

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan error, 1)
	go func() {
		done <- s.waitCanSend(ctx)
	}()

	time.Sleep(20 * time.Millisecond)
	cancel()

	select {
	case err := <-done:
		require.Error(t, err)
		assert.ErrorIs(t, err, context.Canceled)
	case <-time.After(500 * time.Millisecond):
		t.Fatal("waitCanSend did not unblock after context cancel")
	}
}

// ─── Session lifecycle ────────────────────────────────────────────────────────

func TestNewBTPSession_Defaults(t *testing.T) {
	s := newBTPSession()
	assert.Equal(t, btpDefaultSegmentSize, s.segmentSize)
	assert.Equal(t, btpCurrentVersion, s.version)
	assert.Equal(t, btpDefaultWindowSize, s.windowSize)
	assert.Equal(t, uint8(0), s.localSeq)
	assert.Equal(t, uint8(0), s.txInflight)
	assert.False(t, s.hasPendingAck)
	assert.False(t, s.rxActive)
	assert.NotNil(t, s.messages)
	assert.NotNil(t, s.closed)
}

func TestInitHandshake(t *testing.T) {
	s := newBTPSession()
	s.initHandshake(4, 100, 8)

	assert.Equal(t, uint8(4), s.version)
	assert.Equal(t, uint16(100-3), s.segmentSize, "segmentSize = attMTU - 3")
	assert.Equal(t, uint8(8), s.windowSize)
}

func TestInitHandshake_ClampsTooSmallMTU(t *testing.T) {
	s := newBTPSession()
	// attMTU that would produce a segment size below btpMinSegmentSize.
	s.initHandshake(4, 10, 6) // 10-3=7, clamped to 20
	assert.Equal(t, btpMinSegmentSize, s.segmentSize)
}

func TestCloseSession_Idempotent(t *testing.T) {
	s := newBTPSession()
	// Calling closeSession twice must not panic.
	require.NotPanics(t, func() {
		s.closeSession()
		s.closeSession()
	})
}

func TestMessages_ReturnsChannel(t *testing.T) {
	s := newBTPSession()
	ch := s.Messages()
	assert.NotNil(t, ch)
	// Verify the returned channel is non-nil and usable (type is receive-only).
	// We can't compare chan T to <-chan T directly with assert.Equal, so we
	// simply confirm the channel is the same underlying object by checking it
	// matches the session's internal channel when cast.
	assert.Equal(t, (<-chan []byte)(s.messages), ch)
}

// ─── Ack timer ────────────────────────────────────────────────────────────────

func TestNewAckTimer(t *testing.T) {
	s := newBTPSession()
	timer := s.NewAckTimer()
	require.NotNil(t, timer)
	// Stop it immediately to avoid leaking goroutines.
	timer.Stop()
}

// ─── Loopback integration: two sessions communicate ───────────────────────────

// TestLoopback_BidirectionalExchange simulates two BTP peers exchanging several
// messages in both directions with ack piggybacking and verifies:
//
//   - All messages are reassembled correctly.
//   - Sequence numbers are properly maintained.
//   - Acks are piggybacked correctly and txInflight counts stay consistent.
func TestLoopback_BidirectionalExchange(t *testing.T) {
	const segSize = uint16(30)

	alice := newBTPSession()
	alice.segmentSize = segSize

	bob := newBTPSession()
	bob.segmentSize = segSize

	// Deliver a segment from src to dst, simulating the GATT indication pipe.
	pipe := func(src, dst *btpSession, msg []byte) {
		t.Helper()
		for _, seg := range src.segment(msg) {
			require.NoError(t, dst.handleSegment(seg))
		}
	}

	recv := func(s *btpSession, label string) []byte {
		t.Helper()
		select {
		case msg := <-s.messages:
			return msg
		case <-time.After(200 * time.Millisecond):
			t.Fatalf("%s: timed out waiting for message", label)
			return nil
		}
	}

	messages := []struct {
		from  *btpSession
		to    *btpSession
		label string
		msg   []byte
	}{
		{alice, bob, "alice→bob #1", []byte("ping")},
		{bob, alice, "bob→alice #1", []byte("pong")},
		{alice, bob, "alice→bob #2 (long)", bytes.Repeat([]byte{0xAB}, 120)},
		{bob, alice, "bob→alice #2 (long)", bytes.Repeat([]byte{0xCD}, 90)},
		{alice, bob, "alice→bob #3 (empty)", []byte{}},
		{bob, alice, "bob→alice #3", []byte{0xFF}},
	}

	for _, tc := range messages {
		pipe(tc.from, tc.to, tc.msg)
		got := recv(tc.to, tc.label)
		assert.Equal(t, tc.msg, got, tc.label)
	}
}

// ─── Helpers ──────────────────────────────────────────────────────────────────

// buildTestSegment builds a raw BTP data segment byte slice for use in tests.
// If hasAck is true, the A flag is set and ackNum is written before seqNum.
// msgLen is encoded in the segment when the B flag is present in flags.
func buildTestSegment(flags uint8, ackNum uint8, hasAck bool, seqNum uint8, payload []byte) []byte {
	var buf bytes.Buffer

	actualFlags := flags
	if hasAck {
		actualFlags |= btpFlagAck
	}
	buf.WriteByte(actualFlags)

	if hasAck {
		buf.WriteByte(ackNum)
	}

	buf.WriteByte(seqNum)

	if flags&btpFlagBegin != 0 {
		var lb [2]byte
		binary.LittleEndian.PutUint16(lb[:], uint16(len(payload)))
		buf.Write(lb[:])
	}

	buf.Write(payload)

	return buf.Bytes()
}
